package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// A handle names one live stream only. It carries no access authority.
type terminalLiveController struct {
	handle  string
	request *http.Request
	session *terminalChildSession
	lease   *terminalTargetLease
	active  bool // guarded by session.writeMutex
}

func (bridge *TerminalBridge) registerController(request *http.Request, session *terminalChildSession, lease *terminalTargetLease) (*terminalLiveController, error) {
	var opaque [32]byte
	if _, err := rand.Read(opaque[:]); err != nil {
		return nil, err
	}
	controller := &terminalLiveController{handle: hex.EncodeToString(opaque[:]), request: request, session: session, lease: lease, active: true}
	bridge.mutex.Lock()
	bridge.controllers[controller.handle] = controller
	bridge.mutex.Unlock()
	return controller, nil
}

func (bridge *TerminalBridge) unregisterController(controller *terminalLiveController) {
	bridge.mutex.Lock()
	delete(bridge.controllers, controller.handle)
	bridge.mutex.Unlock()
	controller.session.writeMutex.Lock()
	controller.active = false
	controller.session.writeMutex.Unlock()
}

func (bridge *TerminalBridge) sendControllerBatch(request *http.Request, handle, pane, terminal string, validate func() error, chunks []string) (terminalBatchOutcome, error) {
	bridge.mutex.Lock()
	controller := bridge.controllers[handle]
	bridge.mutex.Unlock()
	if controller == nil || controller.lease.target.pane != pane || controller.lease.target.terminal != terminal {
		return terminalBatchNotSent, errors.New("controlling connection is unavailable")
	}
	identity, protected := sessionFromRequest(request)
	owner, ownerProtected := sessionFromRequest(controller.request)
	if protected != ownerProtected || protected && identity != owner {
		return terminalBatchNotSent, errors.New("controlling connection belongs to another sign-in")
	}
	commands, err := terminalInputCommands(chunks)
	if err != nil {
		return terminalBatchNotSent, err
	}
	controller.session.writeMutex.Lock()
	defer controller.session.writeMutex.Unlock()
	if !controller.active || !controller.session.controlled || controller.request.Context().Err() != nil {
		return terminalBatchNotSent, errors.New("controlling connection ended")
	}
	select {
	case <-controller.session.exited:
		return terminalBatchNotSent, errors.New("controlling child ended")
	default:
	}
	if stdin, ok := controller.session.stdin.(*os.File); ok {
		_ = stdin.SetWriteDeadline(time.Now().Add(terminalBridgeShutdownWait))
	}
	// The same identity is checked by forwardTerminalBatch's authority guard.
	// Do not nest another authority lock for the stream request.
	return forwardTerminalBatch(request, controller.lease, controller, commands, func() error {
		if controller.request.Context().Err() != nil {
			return errors.New("controlling connection ended")
		}
		return validate()
	})
}

// Called only while this controller's stdin lock is held, inside the upload's
// authority guard. The stream lifetime must still hold for every chunk.
func (controller *terminalLiveController) Write(data []byte) (int, error) {
	if !controller.active || !controller.session.controlled {
		return 0, errors.New("control ended")
	}
	if err := controller.request.Context().Err(); err != nil {
		return 0, err
	}
	select {
	case <-controller.session.exited:
		return 0, errors.New("controlling child ended")
	default:
	}
	return controller.session.stdin.Write(data)
}

type terminalBatchOutcome uint8

const (
	terminalBatchNotSent terminalBatchOutcome = iota
	terminalBatchForwarded
	terminalBatchOccupied
	terminalBatchUnknown
)

type terminalBatchWriteCounter struct {
	io.Writer
	written int64
}

func (counter *terminalBatchWriteCounter) Write(data []byte) (int, error) {
	written, err := counter.Writer.Write(data)
	counter.written += int64(written)
	return written, err
}

func forwardTerminalBatch(request *http.Request, lease *terminalTargetLease, writer io.Writer, commands []any, validateBeforeFirstWrite func() error) (terminalBatchOutcome, error) {
	counter := &terminalBatchWriteCounter{Writer: writer}
	encoder := json.NewEncoder(counter)
	validated := false
	for _, command := range commands {
		if err := request.Context().Err(); err != nil {
			if counter.written > 0 {
				return terminalBatchUnknown, err
			}
			return terminalBatchNotSent, err
		}
		if lease != nil && !lease.Valid() {
			err := errors.New("terminal is no longer current")
			if counter.written > 0 {
				return terminalBatchUnknown, err
			}
			return terminalBatchNotSent, err
		}
		if !requestAuthorityValid(request) {
			err := errors.New("session authority is no longer current")
			if counter.written > 0 {
				return terminalBatchUnknown, err
			}
			return terminalBatchNotSent, err
		}
		err := withCommitAuthority(request, func() error {
			if lease != nil && !lease.Valid() {
				return errors.New("terminal is no longer current")
			}
			if !validated && validateBeforeFirstWrite != nil {
				if err := validateBeforeFirstWrite(); err != nil {
					return err
				}
				validated = true
			}
			return encoder.Encode(command)
		})
		if err != nil {
			if counter.written > 0 {
				return terminalBatchUnknown, err
			}
			return terminalBatchNotSent, err
		}
	}
	if counter.written == 0 {
		return terminalBatchNotSent, errors.New("terminal batch is empty")
	}
	return terminalBatchForwarded, nil
}

func terminalInputCommands(chunks []string) ([]any, error) {
	commands := make([]any, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk == "" {
			return nil, errors.New("terminal input batch contains an empty chunk")
		}
		commands = append(commands, map[string]any{"type": "terminal.input", "text": chunk})
	}
	if len(commands) == 0 {
		return nil, errors.New("terminal input batch is empty")
	}
	return commands, nil
}

// SendBatch uses the browser Terminal bridge lifecycle for one short control
// session. validate runs after control is acquired and immediately before the
// guarded input writes.
func (bridge *TerminalBridge) SendBatch(request *http.Request, pane, terminal string, takeover bool, validate func() error, chunks []string) (terminalBatchOutcome, error) {
	if validate == nil {
		return terminalBatchNotSent, errors.New("terminal batch pre-write validation is required")
	}
	commands, err := terminalInputCommands(chunks)
	if err != nil {
		return terminalBatchNotSent, err
	}
	lease, ok := bridge.acquireTarget(pane, terminal)
	if !ok {
		return terminalBatchNotSent, errors.New("terminal is unavailable")
	}
	defer lease.Close()
	mode := "control"
	settings := terminalBridgeSettings{
		cols: lease.target.cols, mode: mode, pane: pane, rows: lease.target.rows, takeover: takeover,
	}
	session, _, err := bridge.startTerminalSession(request.Context(), lease.done, settings, terminal)
	if err != nil {
		return terminalBatchNotSent, err
	}
	acquired := false
	defer func() { session.close(acquired) }()
	select {
	case event := <-session.events:
		if event.line == nil {
			detail := strings.TrimSpace(session.stderr.String())
			if strings.Contains(detail, "already has an attached client") {
				return terminalBatchOccupied, errors.New(detail)
			}
			if detail == "" {
				detail = "Herdr terminal stream ended before control was acquired"
			}
			return terminalBatchNotSent, errors.New(detail)
		}
		closedReason, validationErr := (&terminalFrameValidator{}).Accept(event.line)
		if validationErr != nil {
			return terminalBatchNotSent, validationErr
		}
		if closedReason != "" {
			if strings.Contains(closedReason, "already has an attached client") {
				return terminalBatchOccupied, errors.New(closedReason)
			}
			return terminalBatchNotSent, errors.New(closedReason)
		}
		acquired = true
	case <-request.Context().Done():
		return terminalBatchNotSent, request.Context().Err()
	case <-lease.done:
		return terminalBatchNotSent, errors.New("terminal is no longer current")
	case <-bridge.context.Done():
		return terminalBatchNotSent, errors.New("terminal bridge is closed")
	}

	outcome, err := forwardTerminalBatch(request, lease, session.stdin, commands, validate)
	if outcome != terminalBatchForwarded {
		return outcome, err
	}
	if err := withCommitAuthority(request, func() error { return nil }); err != nil {
		return terminalBatchUnknown, err
	}
	return terminalBatchForwarded, nil
}

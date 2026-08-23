package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

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

func forwardTerminalBatch(request *http.Request, lease *terminalTargetLease, writer io.Writer, commands []any) (terminalBatchOutcome, error) {
	counter := &terminalBatchWriteCounter{Writer: writer}
	encoder := json.NewEncoder(counter)
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

// SendBatch uses the same guarded batch writer as the browser Terminal bridge,
// but owns one short control session for the HTTP request.
func (bridge *TerminalBridge) SendBatch(request *http.Request, pane, terminal string, takeover bool, chunks []string) (terminalBatchOutcome, error) {
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
	ctx, cancel := bridge.commandContext(request.Context(), lease.done)
	defer cancel()
	command := bridge.command(ctx, terminalSessionArguments(settings, terminal)...)
	command.Env = append(os.Environ(), "HERDR_SOCKET_PATH="+bridge.socketPath)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return terminalBatchNotSent, err
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return terminalBatchNotSent, err
	}
	stderr := &boundedLog{limit: 8192}
	command.Stderr = stderr
	if !bridge.reserve(command) {
		return terminalBatchNotSent, errors.New("terminal bridge is closed")
	}
	if err := command.Start(); err != nil {
		bridge.forget(command)
		_ = stdin.Close()
		return terminalBatchNotSent, err
	}

	events, processExited := bridge.scanBatchSession(command, stdout)
	acquired := false
	defer func() { bridge.finishBatchSession(command, stdin, processExited, acquired, cancel) }()
	select {
	case event := <-events:
		if event.line == nil {
			detail := strings.TrimSpace(stderr.String())
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

	outcome, err := forwardTerminalBatch(request, lease, stdin, commands)
	if outcome != terminalBatchForwarded {
		return outcome, err
	}
	if err := withCommitAuthority(request, func() error { return nil }); err != nil {
		return terminalBatchUnknown, err
	}
	return terminalBatchForwarded, nil
}

func (bridge *TerminalBridge) scanBatchSession(command *exec.Cmd, stdout io.Reader) (<-chan terminalStreamEvent, <-chan struct{}) {
	events := make(chan terminalStreamEvent, 1)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), terminalBridgeMaxFrameBytes)
		if scanner.Scan() {
			events <- terminalStreamEvent{line: append([]byte(nil), scanner.Bytes()...)}
			for scanner.Scan() {
			}
		}
		scanErr := scanner.Err()
		waitErr := command.Wait()
		bridge.forget(command)
		if scanErr != nil {
			waitErr = scanErr
		}
		select {
		case events <- terminalStreamEvent{err: waitErr}:
		default:
		}
	}()
	return events, exited
}

func (bridge *TerminalBridge) finishBatchSession(command *exec.Cmd, stdin io.WriteCloser, exited <-chan struct{}, acquired bool, cancel context.CancelFunc) {
	if acquired {
		_ = json.NewEncoder(stdin).Encode(map[string]string{"type": "terminal.release"})
	}
	_ = stdin.Close()
	select {
	case <-exited:
		return
	case <-time.After(terminalBridgeShutdownWait):
	}
	cancel()
	select {
	case <-exited:
	case <-time.After(terminalBridgeShutdownWait):
		_ = command.Process.Kill()
		bridge.logger.Error("terminal batch child did not exit after cancellation")
	}
}

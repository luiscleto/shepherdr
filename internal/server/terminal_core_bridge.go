package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/luiscleto/shepherdr/internal/herdr"
)

// CoreTerminalBridge serves Herdr Core terminals through a framed attach in
// this process. Control is an active attach and Release an observing one;
// there is no Take over.
type CoreTerminalBridge struct {
	client   *herdr.CoreClient
	context  context.Context
	cancel   context.CancelFunc
	logger   *slog.Logger
	targets  TerminalStateSource
	upgrader websocket.Upgrader

	controllers map[string]*coreLiveController
	holders     map[*coreLiveSession]struct{} // controlling streams
	keeps       atomic.Uint64                 // orders the pages' keep:true
	mutex       sync.Mutex
}

func NewCoreTerminalBridge(client *herdr.CoreClient, logger *slog.Logger, targets TerminalStateSource) *CoreTerminalBridge {
	ctx, cancel := context.WithCancel(context.Background())
	return &CoreTerminalBridge{
		client: client, context: ctx, cancel: cancel, logger: logger, targets: targets,
		controllers: make(map[string]*coreLiveController),
		holders:     make(map[*coreLiveSession]struct{}),
		upgrader:    websocket.Upgrader{HandshakeTimeout: 5 * time.Second},
	}
}

func (bridge *CoreTerminalBridge) Close() { bridge.cancel() }

func (bridge *CoreTerminalBridge) acquireTarget(pane, terminal string) (*terminalTargetLease, bool) {
	return acquireTerminalTarget(bridge.context, bridge.targets, coreTerminalTargetFromState, pane, terminal)
}

// productionRead returns Reader's older output: the first paint of a short
// observing attach with history, which never changes the terminal's size.
func (bridge *CoreTerminalBridge) productionRead(writer http.ResponseWriter, request *http.Request) {
	settings, err := parseTerminalReadSettings(request)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	terminal := request.URL.Query().Get("terminal")
	lease, ok := bridge.acquireTarget(settings.pane, terminal)
	if !ok {
		http.Error(writer, "Terminal unavailable", http.StatusNotFound)
		return
	}
	defer lease.Close()
	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()
	stop := context.AfterFunc(channelContext(lease.done), cancel)
	defer stop()
	paint, cols, rows, err := bridge.client.ReadFirstPaint(ctx, settings.pane, settings.lines)
	if !lease.Valid() {
		http.Error(writer, "Terminal unavailable", http.StatusNotFound)
		return
	}
	if err == nil && (cols < 2 || cols > 1000 || rows < 1 || rows > 1000) {
		err = errors.New("Herdr Core terminal size is outside Reader's bounds")
	}
	if err != nil {
		bridge.logger.Info("terminal reader failed", "herdr", "core", "pane", settings.pane, "terminal", terminal, "error", err)
		http.Error(writer, "Could not read this Herdr terminal", http.StatusBadGateway)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json")
	response := terminalReadResponse{
		ANSI: string(herdr.CoreReaderANSI(paint)), Cols: cols, Generation: lease.target.generation, Rows: rows, TerminalID: lease.target.terminal,
	}
	if err := json.NewEncoder(writer).Encode(response); err != nil {
		bridge.logger.Debug("terminal reader response closed", "pane", settings.pane, "error", err)
	}
}

// coreLiveSession is one browser stream. writeMutex orders text, keys, files
// and resizes on the attach. size is the terminal's current size in core,
// cols<<16 | rows, from the attach and core's size events. takeBack asks the
// command loop to give the terminal this browser's size again.
type coreLiveSession struct {
	attachment *herdr.CoreAttachment
	terminal   string
	controlled bool
	writeMutex sync.Mutex
	ended      chan struct{}
	size       atomic.Uint32
	hold       coreSizeHold
	takeBack   chan struct{}
}

func coreSize(cols, rows int) uint32 { return uint32(cols)<<16 | uint32(rows)&0xffff }

// coreSizeHold keeps this browser's size while its page is in view and
// controlling. Herdr Core gives the terminal the size of the viewer used
// last, and focus, layout changes, typing and a program's automatic replies
// in the Herdr app window all count; each width change leaves a program's
// redrawn screen in history. The page says when it comes into view, with a
// person's input and on a view switch (keep:true), and when it goes out of
// view; Release and leaving end the stream.
//
// Each keep:true allows one take-back. A program can answer every size
// change with something the app window replies to, which gives the app the
// size again, so a second foreign size before the next keep:true leaves the
// size with the app until then.
type coreSizeHold struct {
	mutex   sync.Mutex
	holding bool
	armed   bool     // a take-back is allowed
	kept    uint64   // order of this page's latest keep:true
	own     []uint32 // this browser's recent sizes, newest first
}

// asked records a size this browser asked for.
func (hold *coreSizeHold) asked(size uint32) {
	hold.mutex.Lock()
	defer hold.mutex.Unlock()
	own := []uint32{size}
	for _, previous := range hold.own {
		if previous != size && len(own) < 8 {
			own = append(own, previous)
		}
	}
	hold.own = own
}

// keep starts holding, or holds again, and allows one take-back. order is
// the bridge's count of keep:true across pages.
func (hold *coreSizeHold) keep(order uint64) {
	hold.mutex.Lock()
	defer hold.mutex.Unlock()
	hold.holding, hold.armed, hold.kept = true, true, order
}

// stop ends holding while the page is out of view.
func (hold *coreSizeHold) stop() {
	hold.mutex.Lock()
	defer hold.mutex.Unlock()
	hold.holding, hold.armed = false, false
}

// latest reports whether this page holds, and the order of its latest
// keep:true.
func (hold *coreSizeHold) latest() (uint64, bool) {
	hold.mutex.Lock()
	defer hold.mutex.Unlock()
	return hold.kept, hold.holding
}

// spend uses this page's one take-back, if it has it.
func (hold *coreSizeHold) spend() bool {
	hold.mutex.Lock()
	defer hold.mutex.Unlock()
	armed := hold.armed
	hold.armed = false
	return armed
}

// owns reports a size this browser asked for, even one arriving late.
func (hold *coreSizeHold) owns(size uint32) bool {
	hold.mutex.Lock()
	defer hold.mutex.Unlock()
	return slices.Contains(hold.own, size)
}

// wanted is the size to keep, while holding.
func (hold *coreSizeHold) wanted() (uint32, bool) {
	hold.mutex.Lock()
	defer hold.mutex.Unlock()
	if !hold.holding || len(hold.own) == 0 {
		return 0, false
	}
	return hold.own[0], true
}

type coreLiveController struct {
	handle  string
	request *http.Request
	session *coreLiveSession
	lease   *terminalTargetLease
	active  bool // guarded by session.writeMutex
}

type coreStreamItem struct {
	event herdr.CoreStreamEvent
	err   error
	end   bool
}

func (bridge *CoreTerminalBridge) productionSocket(writer http.ResponseWriter, request *http.Request) {
	settings, err := parseTerminalBridgeSettings(request)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	if settings.mode == "takeover" {
		http.Error(writer, "mode must be observe or control", http.StatusBadRequest)
		return
	}
	terminal := request.URL.Query().Get("terminal")
	lease, ok := bridge.acquireTarget(settings.pane, terminal)
	if !ok {
		http.Error(writer, "Terminal unavailable", http.StatusNotFound)
		return
	}
	defer lease.Close()
	connection, err := bridge.upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return
	}
	defer connection.Close()
	connection.SetReadLimit(terminalBridgeMaxCommandBytes)
	_ = connection.SetReadDeadline(time.Now().Add(terminalBridgeLivenessTimeout))
	connection.SetPongHandler(func(string) error {
		return connection.SetReadDeadline(time.Now().Add(terminalBridgeLivenessTimeout))
	})

	control := settings.mode == "control"
	session := &coreLiveSession{terminal: settings.pane, controlled: control, ended: make(chan struct{}), takeBack: make(chan struct{}, 1)}
	// Known with its asked size before attaching, so another holder never
	// takes this browser's first size for a foreign one.
	session.hold.asked(coreSize(settings.cols, settings.rows))
	if control {
		bridge.mutex.Lock()
		bridge.holders[session] = struct{}{}
		bridge.mutex.Unlock()
	}
	defer func() {
		bridge.mutex.Lock()
		delete(bridge.holders, session)
		bridge.mutex.Unlock()
	}()
	attachment, err := bridge.client.Attach(request.Context(), settings.pane, settings.cols, settings.rows, !control, true, 0)
	if err != nil {
		bridge.logger.Info("terminal attach failed", "herdr", "core", "pane", settings.pane, "mode", settings.mode, "error", err)
		_ = writeTerminalStatus(connection, "Could not open this Herdr terminal")
		return
	}
	session.attachment = attachment
	session.size.Store(coreSize(attachment.Cols, attachment.Rows))
	defer func() {
		session.writeMutex.Lock()
		session.controlled = false
		session.writeMutex.Unlock()
		close(session.ended)
		_ = attachment.Close()
	}()
	var controller *coreLiveController
	defer func() {
		if controller != nil {
			bridge.unregisterController(controller)
		}
	}()

	stream := make(chan coreStreamItem, 32)
	go func() {
		err := attachment.Run(func(event herdr.CoreStreamEvent) error {
			// Kept here, not where the browser loop takes the event, so a
			// slow browser link can't leave the size behind core's.
			if event.Data == nil && !event.Closed {
				next := coreSize(event.Cols, event.Rows)
				if previous := session.size.Swap(next); previous != next && bridge.takesSizeBack(session, next) {
					select {
					case session.takeBack <- struct{}{}:
					default:
					}
				}
			}
			select {
			case stream <- coreStreamItem{event: event}:
				return nil
			case <-session.ended:
				return errors.New("browser stream ended")
			}
		})
		select {
		case stream <- coreStreamItem{err: err, end: true}:
		case <-session.ended:
		}
	}()

	// The page says when it comes into view and goes out of it; while it is
	// in view, the terminal is marked seen as a Herdr client marks it.
	views := make(chan bool, 1)
	go bridge.markSeen(request, lease, settings.pane, views)

	clientMessages := make(chan []byte, 16)
	clientClosed := make(chan struct{})
	go func() {
		defer close(clientClosed)
		for {
			messageType, message, err := connection.ReadMessage()
			if err != nil {
				return
			}
			if messageType != websocket.TextMessage {
				continue
			}
			select {
			case clientMessages <- message:
			case <-session.ended:
				return
			}
		}
	}()

	type inputResult struct {
		command terminalBrowserCommand
		outcome terminalBatchOutcome
		err     error
	}
	inputResults := make(chan inputResult, 1)
	go func() {
		for {
			select {
			case message := <-clientMessages:
				if visible, ok := coreViewMessage(message); ok {
					select {
					case <-views:
					default:
					}
					views <- visible
					continue
				}
				command, err := validatedTerminalCommand(message)
				outcome := terminalBatchNotSent
				if err == nil {
					outcome, err = bridge.applyCommand(request, session, lease, settings.pane, command)
				}
				select {
				case inputResults <- inputResult{command, outcome, err}:
				case <-session.ended:
					return
				}
				if command.release || !command.isKey && command.requestID > 0 && outcome != terminalBatchForwarded {
					return
				}
			case <-session.takeBack:
				bridge.takeSizeBack(session, settings.pane)
			case <-session.ended:
				return
			}
		}
	}()

	cols, rows := attachment.Cols, attachment.Rows
	var sequence uint64
	heartbeat := time.NewTicker(terminalBridgeHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case item := <-stream:
			if item.end {
				bridge.logger.Info("terminal stream ended", "herdr", "core", "pane", settings.pane, "mode", settings.mode, "error", item.err)
				_ = withCommitAuthority(request, func() error {
					return writeTerminalStatus(connection, "Herdr terminal stream exited")
				})
				return
			}
			event := item.event
			if event.Closed {
				_ = withCommitAuthority(request, func() error { return writeTerminalStatus(connection, event.Reason) })
				return
			}
			if event.Data == nil {
				cols, rows = event.Cols, event.Rows
				continue
			}
			if !lease.Valid() {
				_ = writeTerminalStatus(connection, "This terminal was replaced")
				return
			}
			sequence++
			if controller == nil && control {
				controller, err = bridge.registerController(request, session, lease)
				if err != nil {
					return
				}
				if err := withCommitAuthority(request, func() error {
					return writeJSON(connection, map[string]string{"type": "terminal.controller", "handle": controller.handle})
				}); err != nil {
					return
				}
			}
			// Every paint resets the screen itself, so only the first chunk is
			// full; later repaints arrive inside the byte stream.
			frame := map[string]any{
				"type": "terminal.frame", "encoding": "ansi", "full": sequence == 1, "seq": sequence,
				"width": min(max(cols, 1), 1000), "height": min(max(rows, 1), 1000),
				"bytes": base64.StdEncoding.EncodeToString(event.Data),
			}
			if err := withCommitAuthority(request, func() error { return writeJSON(connection, frame) }); err != nil {
				return
			}
			if err := attachment.CreditStream(len(event.Data)); err != nil {
				return
			}
		case result := <-inputResults:
			if result.command.isKey {
				outcome := "not_sent"
				if result.outcome == terminalBatchForwarded {
					outcome = "accepted"
				}
				if result.outcome == terminalBatchUnknown {
					outcome = "unknown"
				}
				if coreKeyRefused(result.err) {
					outcome = "refused"
				}
				if err := withCommitAuthority(request, func() error {
					return writeJSON(connection, map[string]any{"type": "terminal.key-result", "request_id": result.command.requestID, "result": outcome})
				}); err != nil {
					return
				}
				continue
			}
			if result.command.release {
				return
			}
			if result.command.requestID == 0 {
				continue
			}
			if !control {
				_ = writeTerminalStatus(connection, "An observing terminal cannot send input")
				continue
			}
			if !requestAuthorityValid(request) {
				_ = writeTerminalStatus(connection, "Sign in again")
				return
			}
			if result.outcome != terminalBatchForwarded {
				message := "Herdr input stream closed"
				if !lease.Valid() {
					message = "This terminal was replaced"
				}
				_ = writeTerminalStatus(connection, message)
				if result.err != nil {
					bridge.logger.Debug("terminal input batch stopped", "herdr", "core", "pane", settings.pane, "outcome", result.outcome, "error", result.err)
				}
				return
			}
			if err := withCommitAuthority(request, func() error {
				return writeJSON(connection, map[string]any{"type": "terminal.input-forwarded", "request_id": result.command.requestID})
			}); err != nil {
				return
			}
		case <-heartbeat.C:
			if !lease.Valid() {
				_ = writeTerminalStatus(connection, "This terminal was replaced")
				return
			}
			deadline := time.Now().Add(10 * time.Second)
			if err := withCommitAuthority(request, func() error {
				if err := connection.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
					return err
				}
				return writeJSON(connection, map[string]string{"type": "terminal.heartbeat"})
			}); err != nil {
				return
			}
		case <-clientClosed:
			return
		case <-request.Context().Done():
			return
		case <-bridge.context.Done():
			return
		case <-lease.done:
			_ = writeTerminalStatus(connection, "This terminal is no longer current")
			return
		}
	}
}

// applyCommand carries out one browser command. Text, keys and resizes need
// control; Herdr Core has no scroll command, so wheel scrolling stays local.
// A resize to the size the terminal already has is not sent: core counts a
// resize as activity, and the browser asks again at a person's actions. A
// page going out of view only stops holding its size.
func (bridge *CoreTerminalBridge) applyCommand(request *http.Request, session *coreLiveSession, lease *terminalTargetLease, pane string, command terminalBrowserCommand) (terminalBatchOutcome, error) {
	session.writeMutex.Lock()
	defer session.writeMutex.Unlock()
	if command.release {
		session.controlled = false
		return terminalBatchNotSent, nil
	}
	if !session.controlled {
		return terminalBatchNotSent, errors.New("terminal is not controlled")
	}
	if command.isKey {
		return bridge.sendKey(request, session, lease, pane, command.keySelection)
	}
	chunks, resize, err := coreCommandBytes(command)
	if err != nil {
		return terminalBatchNotSent, err
	}
	if resize != nil {
		size := coreSize(resize[0], resize[1])
		session.hold.asked(size)
		if command.holdSize != nil && *command.holdSize {
			session.hold.keep(bridge.keeps.Add(1))
		} else if command.holdSize != nil {
			session.hold.stop()
		}
		if command.holdSize != nil && !*command.holdSize || session.size.Load() == size {
			return terminalBatchNotSent, nil
		}
		return terminalBatchNotSent, session.attachment.Resize(resize[0], resize[1])
	}
	if chunks == nil {
		return terminalBatchNotSent, nil
	}
	return forwardCoreBatch(request, lease, session.attachment, chunks, nil)
}

// takesSizeBack reports whether this browser takes a new size back. Only a
// size no controlling Shepherdr browser on this terminal asked for is taken
// back, and only by the holding page whose keep:true came last, once per
// keep:true. A take-back asks for the holder's own size, so two holding
// browsers never answer each other, and a hidden page never takes it back.
func (bridge *CoreTerminalBridge) takesSizeBack(session *coreLiveSession, size uint32) bool {
	bridge.mutex.Lock()
	defer bridge.mutex.Unlock()
	var latest *coreLiveSession
	var latestKept uint64
	for holder := range bridge.holders {
		if holder.terminal != session.terminal {
			continue
		}
		if holder.hold.owns(size) {
			return false
		}
		if kept, holding := holder.hold.latest(); holding && (latest == nil || kept > latestKept) {
			latest, latestKept = holder, kept
		}
	}
	return latest == session && session.hold.spend()
}

// takeSizeBack gives the terminal this browser's size again after another
// viewer took it while this page was in view and controlling.
func (bridge *CoreTerminalBridge) takeSizeBack(session *coreLiveSession, pane string) {
	session.writeMutex.Lock()
	defer session.writeMutex.Unlock()
	size, ok := session.hold.wanted()
	if !session.controlled || !ok || session.size.Load() == size {
		return
	}
	bridge.logger.Debug("terminal size taken back", "herdr", "core", "pane", pane, "cols", size>>16, "rows", size&0xffff)
	if err := session.attachment.Resize(int(size>>16), int(size&0xffff)); err != nil {
		bridge.logger.Debug("terminal size not taken back", "herdr", "core", "pane", pane, "error", err)
	}
}

// sendKey presses one key with terminal.keys. Core may wait up to about 3 s
// after the last typing before it presses a key.
func (bridge *CoreTerminalBridge) sendKey(request *http.Request, session *coreLiveSession, lease *terminalTargetLease, pane string, key terminalKey) (terminalBatchOutcome, error) {
	name, err := coreKeyName(key)
	if err != nil {
		return terminalBatchNotSent, err
	}
	submitted := false
	err = withCommitAuthority(request, func() error {
		if !lease.Valid() {
			return errors.New("terminal unavailable")
		}
		submitted = true
		return session.attachment.Keys(request.Context(), pane, name)
	})
	if err == nil {
		return terminalBatchForwarded, nil
	}
	var apiError *herdr.APIError
	if !submitted || errors.As(err, &apiError) {
		return terminalBatchNotSent, err
	}
	return terminalBatchUnknown, err
}

// coreKeyRefused reports core turning a key down as invalid for the
// program's current keyboard mode, such as ctrl+super+c in an ordinary shell.
func coreKeyRefused(err error) bool {
	var apiError *herdr.APIError
	return errors.As(err, &apiError) && apiError.Code == "invalid_params"
}

// coreCommandBytes turns a validated browser command into the raw bytes to
// write, or the size to set.
func coreCommandBytes(command terminalBrowserCommand) ([][]byte, *[2]int, error) {
	var chunks [][]byte
	for _, child := range command.childCommands {
		fields, ok := child.(map[string]any)
		if !ok {
			continue
		}
		switch fields["type"] {
		case "terminal.input":
			if text, ok := fields["text"].(string); ok {
				chunks = append(chunks, []byte(text))
			} else if encoded, ok := fields["bytes"].(string); ok {
				decoded, err := base64.StdEncoding.DecodeString(encoded)
				if err != nil {
					return nil, nil, err
				}
				chunks = append(chunks, decoded)
			}
		case "terminal.resize":
			cols, _ := fields["cols"].(int)
			rows, _ := fields["rows"].(int)
			return nil, &[2]int{cols, rows}, nil
		}
	}
	return chunks, nil, nil
}

// forwardCoreBatch writes chunks in order as DATA on the attach. Accepted
// means every chunk was written to the core socket; a failed or partial
// write is unknown, and nothing is retried.
func forwardCoreBatch(request *http.Request, lease *terminalTargetLease, attachment *herdr.CoreAttachment, chunks [][]byte, validateBeforeFirstWrite func() error) (terminalBatchOutcome, error) {
	if len(chunks) == 0 {
		return terminalBatchNotSent, errors.New("terminal batch is empty")
	}
	attempted := false
	outcome := func(err error) (terminalBatchOutcome, error) {
		if attempted {
			return terminalBatchUnknown, err
		}
		return terminalBatchNotSent, err
	}
	for _, chunk := range chunks {
		if err := request.Context().Err(); err != nil {
			return outcome(err)
		}
		if !lease.Valid() {
			return outcome(errors.New("terminal is no longer current"))
		}
		err := withCommitAuthority(request, func() error {
			if !lease.Valid() {
				return errors.New("terminal is no longer current")
			}
			if !attempted && validateBeforeFirstWrite != nil {
				if err := validateBeforeFirstWrite(); err != nil {
					return err
				}
			}
			attempted = true
			_, err := attachment.Write(chunk)
			return err
		})
		if err != nil {
			return outcome(err)
		}
	}
	return terminalBatchForwarded, nil
}

func (bridge *CoreTerminalBridge) registerController(request *http.Request, session *coreLiveSession, lease *terminalTargetLease) (*coreLiveController, error) {
	var opaque [32]byte
	if _, err := rand.Read(opaque[:]); err != nil {
		return nil, err
	}
	controller := &coreLiveController{handle: hex.EncodeToString(opaque[:]), request: request, session: session, lease: lease, active: true}
	bridge.mutex.Lock()
	bridge.controllers[controller.handle] = controller
	bridge.mutex.Unlock()
	return controller, nil
}

func (bridge *CoreTerminalBridge) unregisterController(controller *coreLiveController) {
	bridge.mutex.Lock()
	delete(bridge.controllers, controller.handle)
	bridge.mutex.Unlock()
	controller.session.writeMutex.Lock()
	controller.active = false
	controller.session.writeMutex.Unlock()
}

// sendControllerBatch writes a file send's chunks on the browser's own
// controlling attach, after the other input on it.
func (bridge *CoreTerminalBridge) sendControllerBatch(request *http.Request, handle, pane, terminal string, validate func() error, chunks []string) (terminalBatchOutcome, error) {
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
	data := make([][]byte, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk == "" {
			return terminalBatchNotSent, errors.New("terminal input batch contains an empty chunk")
		}
		data = append(data, []byte(chunk))
	}
	controller.session.writeMutex.Lock()
	defer controller.session.writeMutex.Unlock()
	if !controller.active || !controller.session.controlled || controller.request.Context().Err() != nil {
		return terminalBatchNotSent, errors.New("controlling connection ended")
	}
	return forwardCoreBatch(request, controller.lease, controller.session.attachment, data, func() error {
		if controller.request.Context().Err() != nil {
			return errors.New("controlling connection ended")
		}
		return validate()
	})
}

// coreViewMessage reads {"type":"terminal.view","visible":bool}: whether the
// page showing this terminal is in view.
func coreViewMessage(message []byte) (bool, bool) {
	var view struct {
		Type    string `json:"type"`
		Visible *bool  `json:"visible"`
	}
	decoder := json.NewDecoder(bytes.NewReader(message))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&view) != nil || requireJSONEnd(decoder) != nil || view.Type != "terminal.view" || view.Visible == nil {
		return false, false
	}
	return *view.Visible, true
}

// markSeen follows the terminal while its page is in view and writes
// herdr.seen=<turn> the way the Herdr app does (its docs/labels.md): on
// coming into view, and once when each turn ends while it stays in view; only
// to raise it, or when it is missing; only for an agent; and only while the
// page's exact terminal is still current. The turn comes from core, never
// from the browser. Out of view, nothing is written, and the stream's end
// stops it.
func (bridge *CoreTerminalBridge) markSeen(request *http.Request, lease *terminalTargetLease, address string, views <-chan bool) {
	ctx, cancel := context.WithCancel(bridge.context)
	defer cancel()
	stop := context.AfterFunc(request.Context(), cancel)
	defer stop()
	stopLease := context.AfterFunc(channelContext(lease.done), cancel)
	defer stopLease()
	inView := false
	for {
		if !inView {
			select {
			case <-ctx.Done():
				return
			case inView = <-views:
				continue
			}
		}
		if !bridge.followSeen(ctx, request, lease, address, views) {
			inView = false
			continue
		}
		// Core went away while the page stayed in view: follow it again.
		select {
		case <-ctx.Done():
			return
		case inView = <-views:
		case <-time.After(time.Second):
		}
	}
}

// followSeen marks the terminal seen until the page goes out of view, which
// returns false, or the follow fails, which returns true to try again.
func (bridge *CoreTerminalBridge) followSeen(ctx context.Context, request *http.Request, lease *terminalTargetLease, address string, views <-chan bool) bool {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	follow, err := bridge.client.FollowTerminal(ctx, address)
	if err != nil {
		bridge.logger.Debug("terminal seen follow failed", "herdr", "core", "pane", address, "error", err)
		return ctx.Err() == nil
	}
	closeFollow := context.AfterFunc(ctx, func() { _ = follow.Close() })
	defer closeFollow()
	defer follow.Close()
	bridge.raiseSeen(request, lease, follow.Terminal())
	turns := make(chan herdr.CoreTerminal)
	failed := make(chan error, 1)
	go func() {
		for {
			turnEnded, err := follow.Next()
			if err != nil {
				failed <- err
				return
			}
			if !turnEnded {
				continue
			}
			select {
			case turns <- follow.Terminal():
			case <-ctx.Done():
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return false
		case visible := <-views:
			// Still in view: this turn was already marked once.
			if !visible {
				return false
			}
		case terminal := <-turns:
			bridge.raiseSeen(request, lease, terminal)
		case err := <-failed:
			bridge.logger.Debug("terminal seen follow ended", "herdr", "core", "pane", address, "error", err)
			return ctx.Err() == nil && !errors.Is(err, herdr.ErrCoreTerminalGone)
		}
	}
}

func (bridge *CoreTerminalBridge) raiseSeen(request *http.Request, lease *terminalTargetLease, terminal herdr.CoreTerminal) {
	if terminal.Status == nil || !herdr.CoreSeenNeedsRaise(terminal) || !lease.Valid() {
		return
	}
	turn := strconv.FormatUint(terminal.Turn, 10)
	err := withCommitAuthority(request, func() error {
		_, err := bridge.client.Label(bridge.context, terminal.Address, map[string]string{herdr.CoreSeenLabel: turn})
		return err
	})
	if err != nil {
		bridge.logger.Debug("terminal not marked seen", "herdr", "core", "pane", terminal.Address, "error", err)
	}
}

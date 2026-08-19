package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/luisc/shepherdr/internal/herdr"
)

const (
	maxTerminalFrame          = 16 << 20
	terminalHeartbeatInterval = 2 * time.Second
	terminalPeerTimeout       = 6 * time.Second
	terminalExitGrace         = 750 * time.Millisecond
	terminalKillWait          = 1500 * time.Millisecond
	terminalReplacementWait   = time.Second
	terminalCleanupWait       = 2*terminalExitGrace + terminalKillWait + 250*time.Millisecond
)

var errTerminalControlledElsewhere = errors.New("terminal is controlled elsewhere")

type targetExpectation struct {
	Gap        uint64
	PaneID     string
	TerminalID string
}

type terminalClientMessage struct {
	Bytes     string `json:"bytes,omitempty"`
	Cols      uint16 `json:"cols,omitempty"`
	Confirmed bool   `json:"confirmed,omitempty"`
	Direction string `json:"direction,omitempty"`
	Lines     uint16 `json:"lines,omitempty"`
	Rows      uint16 `json:"rows,omitempty"`
	Text      string `json:"text,omitempty"`
	Type      string `json:"type"`
}

type terminalServerMessage struct {
	Bytes      string `json:"bytes,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Epoch      string `json:"epoch"`
	Full       bool   `json:"full,omitempty"`
	Gap        uint64 `json:"gap"`
	Height     uint16 `json:"height,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Seq        uint64 `json:"seq,omitempty"`
	TerminalID string `json:"terminal_id,omitempty"`
	Type       string `json:"type"`
	Width      uint16 `json:"width,omitempty"`
}

type terminalFrame struct {
	Bytes    string `json:"bytes"`
	Encoding string `json:"encoding"`
	Full     bool   `json:"full"`
	Height   uint16 `json:"height"`
	Seq      uint64 `json:"seq"`
	Type     string `json:"type"`
	Width    uint16 `json:"width"`
}

type processEvent struct {
	err   error
	frame *terminalFrame
}

type terminalProcess struct {
	cancel      context.CancelFunc
	cleanupDone chan struct{}
	cleanupErr  error
	cleanupOnce sync.Once
	ctx         context.Context
	done        chan struct{}
	events      chan processEvent
	input       io.WriteCloser
	interrupt   func() error
	kill        func() error
	exitGrace   time.Duration
	killWait    time.Duration
	mu          sync.Mutex
}

type terminalChild struct {
	attachment     *terminalAttachment
	cleanupStarted bool
	controller     bool
	finishOnce     sync.Once
	paneID         string
	process        *terminalProcess
}

type terminalAttachment struct {
	cancel     context.CancelFunc
	children   map[*terminalChild]struct{}
	closeOnce  sync.Once
	closing    bool
	ctx        context.Context
	paneID     string
	server     *Server
	stopServer func() bool
}

func (s *Server) terminalSocket(writer http.ResponseWriter, request *http.Request) {
	expectation, cols, rows, err := parseTerminalRequest(request)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	connection, err := s.upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return
	}
	defer connection.Close()
	connection.SetReadLimit(64 * 1024)
	attachment := s.beginTerminalAttachment(request.Context(), expectation.PaneID)
	defer attachment.close()

	if err := s.serveTerminal(attachment, connection, expectation, cols, rows); err != nil {
		s.logTerminalError("terminal bridge ended", err)
	}
}

func (s *Server) serveTerminal(attachment *terminalAttachment, connection *websocket.Conn, expected targetExpectation, cols, rows uint16) error {
	ctx := attachment.ctx
	state := s.projector.Current()
	resolved, reason := resolveTarget(state, expected)
	if reason != "" {
		_ = s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: reason, Gap: state.Gap})
		return nil
	}
	expected.TerminalID = resolved.TerminalID
	expected.Gap = state.Gap
	if err := s.waitForPaneCleanup(ctx, expected.PaneID); err != nil {
		_ = s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: "connection_failed"})
		return err
	}

	observer, err := attachment.startChild("observe", false, cols, rows)
	if err != nil {
		_ = s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: "terminal_unavailable"})
		return err
	}
	messages := make(chan terminalClientMessage, 32)
	closed := make(chan struct{})
	_ = connection.SetReadDeadline(time.Now().Add(terminalPeerTimeout))
	connection.SetPongHandler(func(string) error {
		return connection.SetReadDeadline(time.Now().Add(terminalPeerTimeout))
	})
	go readClientMessage(connection, messages, closed)
	updates, unsubscribe := s.projector.Subscribe()
	defer unsubscribe()
	heartbeat := time.NewTicker(terminalHeartbeatInterval)
	defer heartbeat.Stop()

	mode := "connecting"
	if err := s.writeTerminalMessage(connection, terminalServerMessage{
		Type:       "terminal.status",
		Mode:       mode,
		Gap:        expected.Gap,
		TerminalID: expected.TerminalID,
	}); err != nil {
		return err
	}
	var controller *terminalChild
	var controllerEvents <-chan processEvent
	var observerEvents <-chan processEvent = observer.process.events
	controlStarted := false

	for {
		select {
		case <-closed:
			return nil
		case <-ctx.Done():
			return nil
		case <-heartbeat.C:
			if err := s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.heartbeat"}); err != nil {
				return err
			}
			_ = connection.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if err := connection.WriteControl(websocket.PingMessage, nil, time.Now().Add(2*time.Second)); err != nil {
				return err
			}
		case state := <-updates:
			current, reason := resolveTarget(state, expected)
			if reason != "" {
				if err := s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: reason, Gap: state.Gap}); err != nil {
					return err
				}
				return nil
			}
			if current.TerminalID != expected.TerminalID {
				if err := s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: "reconnecting"}); err != nil {
					return err
				}
				return nil
			}
		case event, ok := <-observerEvents:
			if !ok {
				observerEvents = nil
				if controller == nil {
					_ = s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: "reconnecting"})
					return event.err
				}
				continue
			}
			if event.err != nil {
				observerEvents = nil
				if controller == nil {
					_ = s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: "reconnecting"})
					return event.err
				}
				continue
			}
			if event.frame != nil {
				if err := s.writeTerminalFrame(connection, event.frame); err != nil {
					return err
				}
				if event.frame.Full && !controlStarted {
					controlStarted = true
					controller, err = s.startController(attachment, false, cols, rows)
					if err != nil {
						mode = "controlled_elsewhere"
						if !errors.Is(err, errTerminalControlledElsewhere) {
							mode = "connection_failed"
						}
						_ = s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: mode})
					} else {
						controllerEvents = controller.process.events
					}
				}
			}
		case event, ok := <-controllerEvents:
			if !ok {
				controllerEvents = nil
				controller = nil
				continue
			}
			if event.err != nil {
				previousMode := mode
				controllerEvents = nil
				controller = nil
				if previousMode == "controlled" || previousMode == "taking_over" {
					_ = s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: "reconnecting"})
					return event.err
				}
				current := s.projector.Current()
				_, reason := resolveTarget(current, expected)
				if reason == "" {
					mode = "controlled_elsewhere"
				} else {
					mode = reason
				}
				if err := s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: mode, Gap: current.Gap}); err != nil {
					return err
				}
				continue
			}
			if event.frame != nil {
				if mode != "controlled" {
					mode = "controlled"
					if observer != nil {
						if err := s.collectChild(observer, false); err != nil {
							_ = s.collectChild(controller, true)
							controller = nil
							controllerEvents = nil
							_ = s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: "connection_failed"})
							return err
						}
						observer = nil
					}
					observerEvents = nil
					if err := s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: mode}); err != nil {
						return err
					}
				}
				if err := s.writeTerminalFrame(connection, event.frame); err != nil {
					return err
				}
			}
		case message := <-messages:
			switch message.Type {
			case "terminal.input":
				if mode == "controlled" && controller != nil {
					payload := map[string]any{"type": "terminal.input"}
					if message.Bytes != "" {
						payload["bytes"] = message.Bytes
					} else {
						payload["text"] = message.Text
					}
					_ = controller.process.send(payload)
				}
			case "terminal.resize":
				if mode == "controlled" && controller != nil && validSize(message.Cols, message.Rows) {
					cols, rows = message.Cols, message.Rows
					_ = controller.process.send(map[string]any{"type": "terminal.resize", "cols": cols, "rows": rows})
				}
			case "terminal.scroll":
				if mode == "controlled" && controller != nil && message.Lines > 0 && (message.Direction == "up" || message.Direction == "down") {
					_ = controller.process.send(map[string]any{"type": "terminal.scroll", "direction": message.Direction, "lines": message.Lines, "source": "wheel"})
				}
			case "terminal.takeover":
				if mode == "controlled_elsewhere" && message.Confirmed && controller == nil {
					mode = "taking_over"
					if err := s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: mode}); err != nil {
						return err
					}
					controller, err = s.startController(attachment, true, cols, rows)
					if err != nil {
						controller = nil
						mode = "connection_failed"
						if errors.Is(err, errTerminalControlledElsewhere) {
							mode = "controlled_elsewhere"
						}
						_ = s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: mode})
					} else {
						controllerEvents = controller.process.events
					}
				}
			case "terminal.release":
				if mode == "controlled" && controller != nil {
					mode = "releasing"
					_ = s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: mode})
					if err := s.collectChild(controller, true); err != nil {
						controller = nil
						controllerEvents = nil
						_ = s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: "connection_failed"})
						return err
					}
					controller = nil
					controllerEvents = nil
					controlStarted = true
					mode = "observing"
					if observer == nil {
						observer, err = attachment.startChild("observe", false, cols, rows)
						if err != nil {
							_ = s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: "connection_failed"})
							return err
						}
						observerEvents = observer.process.events
					}
					if err := s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: mode}); err != nil {
						return err
					}
				}
			case "terminal.control":
				if mode == "observing" && controller == nil {
					mode = "taking_control"
					if err := s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: mode}); err != nil {
						return err
					}
					controller, err = s.startController(attachment, false, cols, rows)
					if err != nil {
						controller = nil
						mode = "controlled_elsewhere"
						if !errors.Is(err, errTerminalControlledElsewhere) {
							mode = "connection_failed"
						}
						_ = s.writeTerminalMessage(connection, terminalServerMessage{Type: "terminal.status", Mode: mode})
					} else {
						controllerEvents = controller.process.events
					}
				}
			}
		}
	}
}

func resolveTarget(state herdr.State, expected targetExpectation) (herdr.Terminal, string) {
	switch state.Connection {
	case herdr.ConnectionNotRunning:
		return herdr.Terminal{}, "herdr_not_running"
	case herdr.ConnectionIncompatible:
		return herdr.Terminal{}, "incompatible"
	case herdr.ConnectionReconnecting:
		return herdr.Terminal{}, "reconnecting"
	}
	for _, workspace := range state.Home.Workspaces {
		for _, tab := range workspace.Tabs {
			for _, terminal := range tab.Terminals {
				if terminal.PaneID != expected.PaneID {
					continue
				}
				if state.Gap == expected.Gap && expected.TerminalID != "" && terminal.TerminalID != expected.TerminalID {
					return herdr.Terminal{}, "terminal_unavailable"
				}
				return terminal, ""
			}
		}
	}
	return herdr.Terminal{}, "terminal_unavailable"
}

func parseTerminalRequest(request *http.Request) (targetExpectation, uint16, uint16, error) {
	query := request.URL.Query()
	colsValue, err := strconv.ParseUint(query.Get("cols"), 10, 16)
	if err != nil {
		return targetExpectation{}, 0, 0, errors.New("terminal columns are required")
	}
	rowsValue, err := strconv.ParseUint(query.Get("rows"), 10, 16)
	if err != nil {
		return targetExpectation{}, 0, 0, errors.New("terminal rows are required")
	}
	gap, err := strconv.ParseUint(query.Get("gap"), 10, 64)
	if err != nil {
		return targetExpectation{}, 0, 0, errors.New("terminal connection generation is required")
	}
	expected := targetExpectation{
		Gap:        gap,
		PaneID:     query.Get("pane"),
		TerminalID: query.Get("terminal"),
	}
	if expected.PaneID == "" || expected.TerminalID == "" || !validSize(uint16(colsValue), uint16(rowsValue)) {
		return targetExpectation{}, 0, 0, errors.New("a current pane, terminal freshness, and valid terminal size are required")
	}
	return expected, uint16(colsValue), uint16(rowsValue), nil
}

func validSize(cols, rows uint16) bool {
	return cols > 0 && rows > 0 && cols <= 1000 && rows <= 500
}

func (s *Server) startTerminalProcess(parent context.Context, mode string, takeover bool, paneID string, cols, rows uint16) (*terminalProcess, error) {
	ctx, cancel := context.WithCancel(parent)
	arguments := terminalArguments(mode, takeover, paneID, cols, rows)
	command := exec.Command(s.herdrBinary, arguments...)
	command.Env = environmentWithSocket(os.Environ(), s.socketPath)
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	var input io.WriteCloser
	if mode == "control" {
		input, err = command.StdinPipe()
		if err != nil {
			cancel()
			return nil, err
		}
	}
	if err := command.Start(); err != nil {
		cancel()
		return nil, err
	}
	process := &terminalProcess{
		cancel:      cancel,
		cleanupDone: make(chan struct{}),
		ctx:         ctx,
		done:        make(chan struct{}),
		events:      make(chan processEvent, 64),
		input:       input,
		exitGrace:   terminalExitGrace,
		killWait:    terminalKillWait,
	}
	process.interrupt = func() error { return command.Process.Signal(os.Interrupt) }
	process.kill = command.Process.Kill
	go process.read(command, stdout, stderr)
	return process, nil
}

func (p *terminalProcess) read(command *exec.Cmd, stdout, stderr io.ReadCloser) {
	var errorsOutput bytes.Buffer
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&errorsOutput, io.LimitReader(stderr, 32*1024))
		close(stderrDone)
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maxTerminalFrame)
	for scanner.Scan() {
		var frame terminalFrame
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			p.emit(processEvent{err: fmt.Errorf("decode Herdr terminal frame: %w", err)})
			p.abort()
			break
		}
		if frame.Type == "terminal.closed" {
			break
		}
		if frame.Type != "terminal.frame" || frame.Encoding != "ansi" || !validSize(frame.Width, frame.Height) {
			p.emit(processEvent{err: errors.New("Herdr returned an unsupported terminal frame")})
			p.abort()
			break
		}
		if _, err := base64.StdEncoding.DecodeString(frame.Bytes); err != nil {
			p.emit(processEvent{err: errors.New("Herdr returned invalid terminal bytes")})
			p.abort()
			break
		}
		if !p.emit(processEvent{frame: &frame}) {
			break
		}
	}
	waitErr := command.Wait()
	<-stderrDone
	if scanErr := scanner.Err(); scanErr != nil {
		waitErr = scanErr
	}
	detail := strings.TrimSpace(errorsOutput.String())
	if detail != "" {
		waitErr = fmt.Errorf("%s", detail)
	}
	if waitErr == nil {
		waitErr = io.EOF
	}
	p.emit(processEvent{err: waitErr})
	close(p.events)
	close(p.done)
}

func (p *terminalProcess) send(value any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.input == nil {
		return errors.New("terminal process is read-only")
	}
	return json.NewEncoder(p.input).Encode(value)
}

func (p *terminalProcess) emit(event processEvent) bool {
	select {
	case p.events <- event:
		return true
	case <-p.ctx.Done():
		return false
	}
}

func (p *terminalProcess) stopAndCollect() error { return p.collect(false) }

func (p *terminalProcess) releaseAndCollect() error { return p.collect(true) }

func (p *terminalProcess) abort() {
	p.cancel()
	if p.kill != nil {
		_ = p.kill()
	}
}

func (p *terminalProcess) collect(release bool) error {
	p.cleanupOnce.Do(func() {
		go func() {
			p.cleanupErr = p.collectExactChild(release)
			close(p.cleanupDone)
		}()
	})
	<-p.cleanupDone
	return p.cleanupErr
}

func (p *terminalProcess) collectExactChild(release bool) error {
	if release {
		_ = p.send(map[string]any{"type": "terminal.release"})
		if waitForExit(p.done, p.exitGrace) {
			return nil
		}
	}
	p.cancel()
	p.mu.Lock()
	if p.input != nil {
		_ = p.input.Close()
		p.input = nil
	}
	p.mu.Unlock()
	if p.interrupt != nil {
		_ = p.interrupt()
	}
	if waitForExit(p.done, p.exitGrace) {
		return nil
	}
	if p.kill != nil {
		_ = p.kill()
	}
	if waitForExit(p.done, p.killWait) {
		return nil
	}
	return errors.New("terminal child did not exit after exact kill")
}

func waitForExit(done <-chan struct{}, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

func (s *Server) beginTerminalAttachment(parent context.Context, paneID string) *terminalAttachment {
	ctx, cancel := context.WithCancel(parent)
	attachment := &terminalAttachment{
		cancel:   cancel,
		children: make(map[*terminalChild]struct{}),
		ctx:      ctx,
		paneID:   paneID,
		server:   s,
	}
	if s.terminalCtx != nil {
		attachment.stopServer = context.AfterFunc(s.terminalCtx, cancel)
	}
	s.lifecycleMu.Lock()
	if s.attachments == nil {
		s.attachments = make(map[*terminalAttachment]struct{})
	}
	s.attachments[attachment] = struct{}{}
	s.signalLifecycleChangeLocked()
	s.lifecycleMu.Unlock()
	return attachment
}

func (a *terminalAttachment) startChild(mode string, takeover bool, cols, rows uint16) (*terminalChild, error) {
	process, err := a.server.startTerminalProcess(a.ctx, mode, takeover, a.paneID, cols, rows)
	if err != nil {
		return nil, err
	}
	child := &terminalChild{
		attachment: a,
		controller: mode == "control",
		paneID:     a.paneID,
		process:    process,
	}
	a.server.lifecycleMu.Lock()
	if a.closing {
		a.server.lifecycleMu.Unlock()
		_ = process.stopAndCollect()
		return nil, errors.New("terminal attachment is closing")
	}
	a.children[child] = struct{}{}
	if child.controller {
		a.server.controllers[a.paneID] = child
	}
	a.server.signalLifecycleChangeLocked()
	a.server.lifecycleMu.Unlock()
	go func() {
		<-process.done
		a.server.finishTerminalChild(child)
	}()
	return child, nil
}

func (a *terminalAttachment) close() {
	a.closeOnce.Do(func() {
		if a.stopServer != nil {
			a.stopServer()
		}
		a.server.lifecycleMu.Lock()
		a.closing = true
		children := make([]*terminalChild, 0, len(a.children))
		for child := range a.children {
			child.cleanupStarted = true
			children = append(children, child)
		}
		a.server.signalLifecycleChangeLocked()
		a.server.lifecycleMu.Unlock()
		a.cancel()
		for _, child := range children {
			if err := a.server.collectChild(child, child.controller); err != nil {
				a.server.logTerminalError("collect terminal child", err)
			}
		}
		a.server.finishTerminalAttachment(a)
	})
}

func (s *Server) collectChild(child *terminalChild, release bool) error {
	if child == nil {
		return nil
	}
	s.lifecycleMu.Lock()
	child.cleanupStarted = true
	s.signalLifecycleChangeLocked()
	s.lifecycleMu.Unlock()
	var err error
	if release {
		err = child.process.releaseAndCollect()
	} else {
		err = child.process.stopAndCollect()
	}
	if err == nil {
		s.finishTerminalChild(child)
	}
	return err
}

func (s *Server) finishTerminalChild(child *terminalChild) {
	if child == nil {
		return
	}
	child.finishOnce.Do(func() {
		s.lifecycleMu.Lock()
		delete(child.attachment.children, child)
		if s.controllers[child.paneID] == child {
			delete(s.controllers, child.paneID)
		}
		s.signalLifecycleChangeLocked()
		closingAndEmpty := child.attachment.closing && len(child.attachment.children) == 0
		s.lifecycleMu.Unlock()
		if closingAndEmpty {
			s.finishTerminalAttachment(child.attachment)
		}
	})
}

func (s *Server) finishTerminalAttachment(attachment *terminalAttachment) {
	s.lifecycleMu.Lock()
	if !attachment.closing || len(attachment.children) != 0 {
		s.lifecycleMu.Unlock()
		return
	}
	if _, exists := s.attachments[attachment]; exists {
		delete(s.attachments, attachment)
		s.signalLifecycleChangeLocked()
	}
	s.lifecycleMu.Unlock()
}

func (s *Server) signalLifecycleChangeLocked() {
	if s.lifecycleChanged != nil {
		close(s.lifecycleChanged)
	}
	s.lifecycleChanged = make(chan struct{})
}

func (s *Server) waitForPaneCleanup(ctx context.Context, paneID string) error {
	timer := time.NewTimer(terminalCleanupWait)
	defer timer.Stop()
	for {
		s.lifecycleMu.Lock()
		blocked := false
		for attachment := range s.attachments {
			for child := range attachment.children {
				if child.paneID == paneID && child.cleanupStarted {
					blocked = true
					break
				}
			}
			if blocked {
				break
			}
		}
		if !blocked {
			s.lifecycleMu.Unlock()
			return nil
		}
		changed := s.lifecycleChanged
		s.lifecycleMu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errors.New("prior terminal child cleanup is incomplete")
		}
	}
}

func (s *Server) startController(attachment *terminalAttachment, takeover bool, cols, rows uint16) (*terminalChild, error) {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	s.lifecycleMu.Lock()
	current := s.controllers[attachment.paneID]
	s.lifecycleMu.Unlock()
	if current != nil {
		select {
		case <-current.process.done:
			s.finishTerminalChild(current)
			current = nil
		default:
		}
	}
	if current != nil && !takeover {
		timer := time.NewTimer(terminalReplacementWait)
		select {
		case <-current.process.done:
			s.finishTerminalChild(current)
			current = nil
		case <-timer.C:
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		if current != nil {
			return nil, errTerminalControlledElsewhere
		}
	}
	if current != nil {
		if err := s.collectChild(current, true); err != nil {
			return nil, err
		}
	}
	return attachment.startChild("control", takeover, cols, rows)
}

func terminalArguments(mode string, takeover bool, paneID string, cols, rows uint16) []string {
	arguments := []string{"terminal", "session", mode, paneID}
	if takeover {
		arguments = append(arguments, "--takeover")
	}
	return append(arguments, "--cols", strconv.Itoa(int(cols)), "--rows", strconv.Itoa(int(rows)))
}

func (s *Server) writeTerminalMessage(connection *websocket.Conn, message terminalServerMessage) error {
	message.Epoch = s.epoch
	return writeJSON(connection, message)
}

func (s *Server) writeTerminalFrame(connection *websocket.Conn, frame *terminalFrame) error {
	return s.writeTerminalMessage(connection, terminalServerMessage{
		Bytes:  frame.Bytes,
		Full:   frame.Full,
		Height: frame.Height,
		Seq:    frame.Seq,
		Type:   "terminal.frame",
		Width:  frame.Width,
	})
}

func environmentWithSocket(environment []string, socketPath string) []string {
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, "HERDR_SOCKET_PATH=") {
			result = append(result, entry)
		}
	}
	return append(result, "HERDR_SOCKET_PATH="+socketPath)
}

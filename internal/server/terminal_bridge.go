package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	terminalBridgeHeartbeat       = 2 * time.Second
	terminalBridgeLivenessTimeout = 7 * time.Second
	terminalBridgeMaxCommandBytes = 1 << 20
	terminalBridgeMaxFrameBytes   = 4 << 20
	terminalBridgeMaxReadBytes    = 16 << 20
	terminalBridgeShutdownWait    = time.Second
)

type terminalCommandFactory func(context.Context, ...string) *exec.Cmd

type TerminalBridge struct {
	binary     string
	command    terminalCommandFactory
	context    context.Context
	cancel     context.CancelFunc
	logger     *slog.Logger
	socketPath string
	targets    TerminalStateSource
	upgrader   websocket.Upgrader

	closeOnce sync.Once
	children  map[*exec.Cmd]struct{}
	mutex     sync.Mutex
	wait      sync.WaitGroup
}

func NewTerminalBridge(binary, socketPath string, logger *slog.Logger, targets TerminalStateSource) *TerminalBridge {
	ctx, cancel := context.WithCancel(context.Background())
	bridge := &TerminalBridge{
		binary:     binary,
		context:    ctx,
		cancel:     cancel,
		children:   make(map[*exec.Cmd]struct{}),
		logger:     logger,
		socketPath: socketPath,
		targets:    targets,
		upgrader: websocket.Upgrader{
			HandshakeTimeout: 5 * time.Second,
		},
	}
	bridge.command = func(ctx context.Context, arguments ...string) *exec.Cmd {
		return exec.CommandContext(ctx, bridge.binary, arguments...)
	}
	return bridge
}

func (bridge *TerminalBridge) Close() {
	bridge.closeOnce.Do(func() {
		bridge.mutex.Lock()
		bridge.cancel()
		bridge.mutex.Unlock()
		bridge.wait.Wait()
	})
}

type terminalReadResponse struct {
	ANSI       string `json:"ansi"`
	Cols       int    `json:"cols"`
	Generation uint64 `json:"generation"`
	Rows       int    `json:"rows"`
	TerminalID string `json:"terminal_id"`
}

func (bridge *TerminalBridge) productionRead(writer http.ResponseWriter, request *http.Request) {
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

	ansi, err := bridge.output(request.Context(), lease.done, terminalReadArguments(settings)...)
	if !lease.Valid() {
		http.Error(writer, "Terminal unavailable", http.StatusNotFound)
		return
	}
	if err != nil {
		bridge.logger.Info("terminal reader failed", "pane", settings.pane, "terminal", terminal, "error", err)
		http.Error(writer, "Could not read this Herdr terminal", http.StatusBadGateway)
		return
	}
	response := terminalReadResponse{
		ANSI: string(ansi), Cols: lease.target.cols, Generation: lease.target.generation,
		Rows: lease.target.rows, TerminalID: lease.target.terminal,
	}
	if !lease.Valid() {
		http.Error(writer, "Terminal unavailable", http.StatusNotFound)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(response); err != nil {
		bridge.logger.Debug("terminal reader response closed", "pane", settings.pane, "error", err)
	}
}

func (bridge *TerminalBridge) read(writer http.ResponseWriter, request *http.Request) {
	settings, err := parseTerminalReadSettings(request)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	ansi, err := bridge.output(request.Context(), nil, terminalReadArguments(settings)...)
	if err != nil {
		bridge.logger.Info("terminal lab reader failed", "pane", settings.pane, "error", err)
		http.Error(writer, "Could not read this Herdr terminal", http.StatusBadGateway)
		return
	}
	layout, err := bridge.output(request.Context(), nil, "pane", "layout", "--pane="+settings.pane)
	if err != nil {
		http.Error(writer, "Could not read this Herdr terminal layout", http.StatusBadGateway)
		return
	}
	var envelope struct {
		Result struct {
			Layout struct {
				Panes []struct {
					PaneID string `json:"pane_id"`
					Rect   struct {
						Height int `json:"height"`
						Width  int `json:"width"`
					} `json:"rect"`
				} `json:"panes"`
			} `json:"layout"`
		} `json:"result"`
	}
	if err := json.Unmarshal(layout, &envelope); err != nil {
		http.Error(writer, "Herdr returned an invalid terminal layout", http.StatusBadGateway)
		return
	}
	response := terminalReadResponse{ANSI: string(ansi)}
	for _, candidate := range envelope.Result.Layout.Panes {
		if candidate.PaneID == settings.pane {
			response.Cols = candidate.Rect.Width
			response.Rows = candidate.Rect.Height
			break
		}
	}
	if response.Cols < 2 || response.Rows < 1 {
		http.Error(writer, "Herdr did not return this terminal's dimensions", http.StatusBadGateway)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(response)
}

func (bridge *TerminalBridge) commandContext(requestContext context.Context, fence <-chan struct{}) (context.Context, func()) {
	ctx, cancel := context.WithCancel(bridge.context)
	stopRequest := context.AfterFunc(requestContext, cancel)
	stopFence := func() bool { return true }
	if fence != nil {
		stopFence = context.AfterFunc(channelContext(fence), cancel)
	}
	return ctx, func() {
		stopRequest()
		stopFence()
		cancel()
	}
}

func channelContext(done <-chan struct{}) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-done
		cancel()
	}()
	return ctx
}

func (bridge *TerminalBridge) reserve(command *exec.Cmd) bool {
	bridge.mutex.Lock()
	defer bridge.mutex.Unlock()
	select {
	case <-bridge.context.Done():
		return false
	default:
	}
	bridge.children[command] = struct{}{}
	bridge.wait.Add(1)
	return true
}

func (bridge *TerminalBridge) forget(command *exec.Cmd) {
	bridge.mutex.Lock()
	delete(bridge.children, command)
	bridge.mutex.Unlock()
	bridge.wait.Done()
}

func (bridge *TerminalBridge) output(requestContext context.Context, fence <-chan struct{}, arguments ...string) ([]byte, error) {
	ctx, cancel := bridge.commandContext(requestContext, fence)
	defer cancel()
	command := bridge.command(ctx, arguments...)
	command.Env = append(os.Environ(), "HERDR_SOCKET_PATH="+bridge.socketPath)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &boundedLog{limit: 8192}
	command.Stderr = stderr
	if !bridge.reserve(command) {
		return nil, errors.New("terminal bridge is closed")
	}
	if err := command.Start(); err != nil {
		bridge.forget(command)
		return nil, err
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, terminalBridgeMaxReadBytes+1))
	if readErr != nil {
		cancel()
	}
	if len(output) > terminalBridgeMaxReadBytes {
		cancel()
		readErr = errors.New("Herdr terminal history exceeded the reader limit")
	}
	waitErr := command.Wait()
	bridge.forget(command)
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil {
		return nil, fmt.Errorf("Herdr command failed: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}

type terminalStreamEvent struct {
	err  error
	line []byte
}

type terminalChildSession struct {
	bridge   *TerminalBridge
	cancel   context.CancelFunc
	command  *exec.Cmd
	done     chan struct{}
	events   chan terminalStreamEvent
	exited   chan struct{}
	settings terminalBridgeSettings
	stdin    io.WriteCloser
	stderr   *boundedLog
}

func (bridge *TerminalBridge) startTerminalSession(
	requestContext context.Context,
	fence <-chan struct{},
	settings terminalBridgeSettings,
	target string,
) (*terminalChildSession, string, error) {
	ctx, cancel := bridge.commandContext(requestContext, fence)
	command := bridge.command(ctx, terminalSessionArguments(settings, target)...)
	command.Env = append(os.Environ(), "HERDR_SOCKET_PATH="+bridge.socketPath)
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		return nil, "Could not create the Herdr output stream", err
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		cancel()
		return nil, "Could not create the Herdr input stream", err
	}
	stderr := &boundedLog{limit: 8192}
	command.Stderr = stderr
	if !bridge.reserve(command) {
		_ = stdin.Close()
		cancel()
		return nil, "Terminal service is stopping", errors.New("terminal bridge is closed")
	}
	if err := command.Start(); err != nil {
		bridge.forget(command)
		_ = stdin.Close()
		cancel()
		return nil, "Could not start the Herdr terminal stream", err
	}

	session := &terminalChildSession{
		bridge: bridge, cancel: cancel, command: command, done: make(chan struct{}),
		events: make(chan terminalStreamEvent, 32), exited: make(chan struct{}),
		settings: settings, stdin: stdin, stderr: stderr,
	}
	go session.scan(stdout)
	return session, "", nil
}

func (session *terminalChildSession) scan(stdout io.Reader) {
	defer close(session.exited)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), terminalBridgeMaxFrameBytes)
	for scanner.Scan() {
		line := bytes.Clone(scanner.Bytes())
		select {
		case session.events <- terminalStreamEvent{line: line}:
		case <-session.done:
			session.cancel()
			_ = session.command.Wait()
			session.bridge.forget(session.command)
			return
		}
	}
	scanErr := scanner.Err()
	waitErr := session.command.Wait()
	session.bridge.forget(session.command)
	if scanErr != nil {
		waitErr = scanErr
	}
	select {
	case session.events <- terminalStreamEvent{err: waitErr}:
	case <-session.done:
	}
}

func (session *terminalChildSession) close(release bool) {
	defer session.cancel()
	if release {
		_ = json.NewEncoder(session.stdin).Encode(map[string]string{"type": "terminal.release"})
	}
	_ = session.stdin.Close()
	close(session.done)
	select {
	case <-session.exited:
		return
	case <-time.After(terminalBridgeShutdownWait):
	}
	session.cancel()
	select {
	case <-session.exited:
	case <-time.After(terminalBridgeShutdownWait):
		_ = session.command.Process.Kill()
		session.bridge.logger.Error("terminal child did not exit after cancellation", "pane", session.settings.pane, "mode", session.settings.mode)
	}
}

func (bridge *TerminalBridge) productionSocket(writer http.ResponseWriter, request *http.Request) {
	settings, err := parseTerminalBridgeSettings(request)
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
	bridge.serveSocket(writer, request, settings, terminal, lease)
}

func (bridge *TerminalBridge) socket(writer http.ResponseWriter, request *http.Request) {
	settings, err := parseTerminalBridgeSettings(request)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	bridge.serveSocket(writer, request, settings, settings.pane, nil)
}

func (bridge *TerminalBridge) serveSocket(writer http.ResponseWriter, request *http.Request, settings terminalBridgeSettings, target string, lease *terminalTargetLease) {
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

	var fence <-chan struct{}
	if lease != nil {
		fence = lease.done
	}
	session, status, err := bridge.startTerminalSession(request.Context(), fence, settings, target)
	if err != nil {
		_ = writeTerminalStatus(connection, status)
		return
	}
	defer session.close(settings.mode != "observe")

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
			case <-session.done:
				return
			}
		}
	}()

	validator := terminalFrameValidator{}
	heartbeat := time.NewTicker(terminalBridgeHeartbeat)
	defer heartbeat.Stop()
	bridge.logger.Info("terminal stream started", "pane", settings.pane, "terminal", target, "mode", settings.mode, "cols", settings.cols, "rows", settings.rows)
	for {
		select {
		case event := <-session.events:
			if event.line != nil {
				if lease != nil && !lease.Valid() {
					_ = writeTerminalStatus(connection, "This terminal was replaced")
					return
				}
				closedReason, err := validator.Accept(event.line)
				if err != nil {
					bridge.logger.Info("invalid Herdr terminal frame", "pane", settings.pane, "error", err)
					_ = writeTerminalStatus(connection, "Herdr emitted an invalid terminal frame")
					return
				}
				if closedReason != "" {
					_ = writeTerminalStatus(connection, closedReason)
					return
				}
				if err := withCommitAuthority(request, func() error {
					_ = connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
					return connection.WriteMessage(websocket.TextMessage, event.line)
				}); err != nil {
					return
				}
				continue
			}
			detail := strings.TrimSpace(session.stderr.String())
			if event.err != nil || detail != "" {
				bridge.logger.Info("terminal stream ended", "pane", settings.pane, "terminal", target, "mode", settings.mode, "error", event.err, "stderr", detail)
				if detail == "" {
					detail = "Herdr terminal stream exited"
				}
				_ = withCommitAuthority(request, func() error { return writeTerminalStatus(connection, detail) })
			}
			return
		case message := <-clientMessages:
			if settings.mode == "observe" {
				_ = writeTerminalStatus(connection, "An observing terminal cannot send input")
				continue
			}
			if lease != nil && !lease.Valid() {
				_ = writeTerminalStatus(connection, "This terminal was replaced")
				return
			}
			browserCommand, err := validatedTerminalCommand(message)
			if err != nil {
				_ = writeTerminalStatus(connection, "Rejected browser command: "+err.Error())
				continue
			}
			if !requestAuthorityValid(request) {
				_ = writeTerminalStatus(connection, "Sign in again")
				return
			}
			outcome, err := forwardTerminalBatch(request, lease, session.stdin, browserCommand.childCommands, nil)
			if outcome != terminalBatchForwarded {
				if requestAuthorityValid(request) {
					message := "Herdr input stream closed"
					if lease != nil && !lease.Valid() {
						message = "This terminal was replaced"
					}
					_ = writeTerminalStatus(connection, message)
				}
				if err != nil {
					bridge.logger.Debug("terminal input batch stopped", "pane", settings.pane, "outcome", outcome, "error", err)
				}
				return
			}
			if browserCommand.requestID > 0 {
				if err := withCommitAuthority(request, func() error {
					return writeJSON(connection, map[string]any{"type": "terminal.input-forwarded", "request_id": browserCommand.requestID})
				}); err != nil {
					return
				}
			}
			if browserCommand.release {
				return
			}
		case <-heartbeat.C:
			if lease != nil && !lease.Valid() {
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
		case <-fence:
			_ = writeTerminalStatus(connection, "This terminal is no longer current")
			return
		}
	}
}

func writeTerminalStatus(connection *websocket.Conn, message string) error {
	return writeJSON(connection, map[string]string{"type": "terminal.status", "message": message})
}

type boundedLog struct {
	buffer bytes.Buffer
	limit  int
	mutex  sync.Mutex
}

func (log *boundedLog) Write(value []byte) (int, error) {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	written := len(value)
	remaining := log.limit - log.buffer.Len()
	if remaining > 0 {
		_, _ = log.buffer.Write(value[:min(len(value), remaining)])
	}
	return written, nil
}

func (log *boundedLog) String() string {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	return log.buffer.String()
}

var _ io.Writer = (*boundedLog)(nil)

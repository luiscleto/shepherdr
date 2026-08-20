package server

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gorilla/websocket"
)

const (
	terminalBridgeMaxCommandBytes = 1 << 20
	terminalBridgeMaxFrameBytes   = 4 << 20
	terminalBridgeMaxReadBytes    = 16 << 20
	terminalBridgeShutdownWait    = time.Second
)

type TerminalBridge struct {
	binary     string
	logger     *slog.Logger
	socketPath string
	upgrader   websocket.Upgrader
}

func NewTerminalBridge(binary, socketPath string, logger *slog.Logger) *TerminalBridge {
	return &TerminalBridge{
		binary:     binary,
		logger:     logger,
		socketPath: socketPath,
		upgrader: websocket.Upgrader{
			HandshakeTimeout: 5 * time.Second,
		},
	}
}

type terminalBridgeSettings struct {
	cols     int
	mode     string
	pane     string
	rows     int
	takeover bool
}

func parseTerminalBridgeSettings(request *http.Request) (terminalBridgeSettings, error) {
	query := request.URL.Query()
	pane := query.Get("pane")
	if !validTerminalPane(pane) {
		return terminalBridgeSettings{}, errors.New("pane must be a non-empty Herdr target")
	}
	mode := query.Get("mode")
	if mode != "observe" && mode != "control" && mode != "takeover" {
		return terminalBridgeSettings{}, errors.New("mode must be observe, control, or takeover")
	}
	cols, err := strconv.Atoi(query.Get("cols"))
	if err != nil || cols < 2 || cols > 1000 {
		return terminalBridgeSettings{}, errors.New("cols must be between 2 and 1000")
	}
	rows, err := strconv.Atoi(query.Get("rows"))
	if err != nil || rows < 1 || rows > 1000 {
		return terminalBridgeSettings{}, errors.New("rows must be between 1 and 1000")
	}
	return terminalBridgeSettings{cols: cols, mode: mode, pane: pane, rows: rows, takeover: mode == "takeover"}, nil
}

func validTerminalPane(pane string) bool {
	return pane != "" && len(pane) <= 256 && strings.IndexFunc(pane, unicode.IsControl) < 0
}

type terminalReadResponse struct {
	ANSI string `json:"ansi"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func (lab *TerminalBridge) read(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	pane := query.Get("pane")
	if !validTerminalPane(pane) {
		http.Error(writer, "pane must be a non-empty Herdr target", http.StatusBadRequest)
		return
	}
	lines, err := strconv.Atoi(query.Get("lines"))
	if err != nil || lines < 1 || lines > 20_000 {
		http.Error(writer, "lines must be between 1 and 20000", http.StatusBadRequest)
		return
	}
	source := query.Get("source")
	if source != "recent" && source != "recent-unwrapped" {
		http.Error(writer, "source must be recent or recent-unwrapped", http.StatusBadRequest)
		return
	}

	ansi, err := lab.output(request, "pane", "read", pane, "--source", source, "--lines", strconv.Itoa(lines), "--format", "ansi", "--raw")
	if err != nil {
		lab.logger.Info("terminal reader failed", "pane", pane, "error", err)
		http.Error(writer, "Could not read this Herdr terminal", http.StatusBadGateway)
		return
	}
	layout, err := lab.output(request, "pane", "layout", "--pane", pane)
	if err != nil {
		lab.logger.Info("terminal reader layout failed", "pane", pane, "error", err)
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
		if candidate.PaneID == pane {
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
	if err := json.NewEncoder(writer).Encode(response); err != nil {
		lab.logger.Debug("terminal reader response closed", "pane", pane, "error", err)
	}
}

func (lab *TerminalBridge) output(request *http.Request, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(request.Context(), lab.binary, arguments...)
	command.Env = append(os.Environ(), "HERDR_SOCKET_PATH="+lab.socketPath)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &boundedLog{limit: 8192}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return nil, err
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, terminalBridgeMaxReadBytes+1))
	if readErr != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, readErr
	}
	if len(output) > terminalBridgeMaxReadBytes {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, errors.New("Herdr terminal history exceeded the reader limit")
	}
	waitErr := command.Wait()
	if waitErr != nil {
		return nil, fmt.Errorf("Herdr command failed: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}

type terminalStreamEvent struct {
	err  error
	line []byte
}

func (lab *TerminalBridge) socket(writer http.ResponseWriter, request *http.Request) {
	settings, err := parseTerminalBridgeSettings(request)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	connection, err := lab.upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return
	}
	defer connection.Close()
	connection.SetReadLimit(terminalBridgeMaxCommandBytes)

	operation := settings.mode
	if operation == "takeover" {
		operation = "control"
	}
	arguments := []string{"terminal", "session", operation, settings.pane, "--cols", strconv.Itoa(settings.cols), "--rows", strconv.Itoa(settings.rows)}
	if settings.takeover {
		arguments = append(arguments, "--takeover")
	}
	command := exec.Command(lab.binary, arguments...)
	command.Env = append(os.Environ(), "HERDR_SOCKET_PATH="+lab.socketPath)
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = writeTerminalStatus(connection, "Could not create the Herdr output stream")
		return
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		_ = writeTerminalStatus(connection, "Could not create the Herdr input stream")
		return
	}
	stderr := &boundedLog{limit: 8192}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		_ = writeTerminalStatus(connection, "Could not start the Herdr terminal stream")
		return
	}

	handlerDone := make(chan struct{})
	processExited := make(chan struct{})
	streamEvents := make(chan terminalStreamEvent, 32)
	go func() {
		defer close(processExited)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), terminalBridgeMaxFrameBytes)
		for scanner.Scan() {
			line := bytes.Clone(scanner.Bytes())
			select {
			case streamEvents <- terminalStreamEvent{line: line}:
			case <-handlerDone:
				_ = command.Process.Kill()
				_ = command.Wait()
				return
			}
		}
		scanErr := scanner.Err()
		waitErr := command.Wait()
		if scanErr != nil {
			waitErr = scanErr
		}
		select {
		case streamEvents <- terminalStreamEvent{err: waitErr}:
		case <-handlerDone:
		}
	}()

	cleanup := func() {
		if settings.mode != "observe" {
			_ = json.NewEncoder(stdin).Encode(map[string]string{"type": "terminal.release"})
		}
		_ = stdin.Close()
		select {
		case <-processExited:
			return
		case <-time.After(terminalBridgeShutdownWait):
		}
		_ = command.Process.Kill()
		select {
		case <-processExited:
		case <-time.After(terminalBridgeShutdownWait):
			lab.logger.Error("terminal child did not exit after kill", "pane", settings.pane, "mode", settings.mode)
		}
	}
	defer cleanup()
	defer close(handlerDone)

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
			case <-handlerDone:
				return
			}
		}
	}()

	lab.logger.Info("terminal stream started", "pane", settings.pane, "mode", settings.mode, "cols", settings.cols, "rows", settings.rows)
	for {
		select {
		case event := <-streamEvents:
			if event.line != nil {
				if !validTerminalFrame(event.line) {
					_ = writeTerminalStatus(connection, "Herdr emitted an invalid terminal frame")
					return
				}
				_ = connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := connection.WriteMessage(websocket.TextMessage, event.line); err != nil {
					return
				}
				continue
			}
			detail := strings.TrimSpace(stderr.String())
			if event.err != nil || detail != "" {
				lab.logger.Info("terminal stream ended", "pane", settings.pane, "mode", settings.mode, "error", event.err, "stderr", detail)
				if detail == "" {
					detail = "Herdr terminal stream exited"
				}
				_ = writeTerminalStatus(connection, detail)
			}
			return
		case message := <-clientMessages:
			if settings.mode == "observe" {
				_ = writeTerminalStatus(connection, "An observing terminal cannot send input")
				continue
			}
			command, requestID, err := validatedTerminalCommand(message)
			if err != nil {
				_ = writeTerminalStatus(connection, "Rejected browser command: "+err.Error())
				continue
			}
			if err := json.NewEncoder(stdin).Encode(command); err != nil {
				_ = writeTerminalStatus(connection, "Herdr input stream closed")
				return
			}
			if requestID > 0 {
				if err := writeJSON(connection, map[string]any{"type": "terminal.input-accepted", "request_id": requestID}); err != nil {
					return
				}
			}
		case <-clientClosed:
			return
		case <-request.Context().Done():
			return
		}
	}
}

func validTerminalFrame(line []byte) bool {
	var envelope struct {
		Bytes    string `json:"bytes"`
		Encoding string `json:"encoding"`
		Height   int    `json:"height"`
		Seq      uint64 `json:"seq"`
		Type     string `json:"type"`
		Width    int    `json:"width"`
	}
	if json.Unmarshal(line, &envelope) != nil {
		return false
	}
	if envelope.Type == "terminal.closed" {
		return true
	}
	if envelope.Type != "terminal.frame" || envelope.Encoding != "ansi" || envelope.Width < 1 || envelope.Height < 1 || envelope.Seq < 1 {
		return false
	}
	_, err := base64.StdEncoding.DecodeString(envelope.Bytes)
	return err == nil
}

func validatedTerminalCommand(message []byte) (any, int, error) {
	var envelope struct {
		Bytes     *string `json:"bytes"`
		Cols      int     `json:"cols"`
		Direction string  `json:"direction"`
		Lines     int     `json:"lines"`
		RequestID int     `json:"request_id"`
		Rows      int     `json:"rows"`
		Text      *string `json:"text"`
		Type      string  `json:"type"`
	}
	if err := json.Unmarshal(message, &envelope); err != nil {
		return nil, 0, errors.New("invalid JSON")
	}
	switch envelope.Type {
	case "terminal.input":
		if envelope.RequestID < 0 {
			return nil, 0, errors.New("request_id must not be negative")
		}
		if (envelope.Text == nil) == (envelope.Bytes == nil) {
			return nil, 0, errors.New("input must contain exactly one of text or bytes")
		}
		if envelope.Text != nil {
			if len(*envelope.Text) > terminalBridgeMaxCommandBytes {
				return nil, 0, errors.New("input is too large")
			}
			return map[string]any{"type": envelope.Type, "text": *envelope.Text}, envelope.RequestID, nil
		}
		decoded, err := base64.StdEncoding.DecodeString(*envelope.Bytes)
		if err != nil || len(decoded) > terminalBridgeMaxCommandBytes {
			return nil, 0, errors.New("bytes must be valid base64 within the size limit")
		}
		return map[string]any{"type": envelope.Type, "bytes": *envelope.Bytes}, envelope.RequestID, nil
	case "terminal.resize":
		if envelope.Cols < 2 || envelope.Cols > 1000 || envelope.Rows < 1 || envelope.Rows > 1000 {
			return nil, 0, errors.New("resize is outside the lab limits")
		}
		return map[string]any{"type": envelope.Type, "cols": envelope.Cols, "rows": envelope.Rows}, 0, nil
	case "terminal.scroll":
		if envelope.Direction != "up" && envelope.Direction != "down" {
			return nil, 0, errors.New("scroll direction must be up or down")
		}
		if envelope.Lines < 1 || envelope.Lines > 1000 {
			return nil, 0, errors.New("scroll lines are outside the lab limits")
		}
		return map[string]any{"type": envelope.Type, "direction": envelope.Direction, "lines": envelope.Lines, "source": "page_key"}, 0, nil
	case "terminal.release":
		return map[string]string{"type": envelope.Type}, 0, nil
	default:
		return nil, 0, fmt.Errorf("unsupported type %q", envelope.Type)
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

package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

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
	if !validTerminalIdentity(pane) {
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

type terminalReadSettings struct {
	lines  int
	pane   string
	source string
}

func parseTerminalReadSettings(request *http.Request) (terminalReadSettings, error) {
	query := request.URL.Query()
	pane := query.Get("pane")
	if !validTerminalIdentity(pane) {
		return terminalReadSettings{}, errors.New("pane must be a non-empty Herdr target")
	}
	lines, err := strconv.Atoi(query.Get("lines"))
	if err != nil || lines < 1 || lines > 20_000 {
		return terminalReadSettings{}, errors.New("lines must be between 1 and 20000")
	}
	source := query.Get("source")
	if source != "recent" && source != "recent-unwrapped" {
		return terminalReadSettings{}, errors.New("source must be recent or recent-unwrapped")
	}
	return terminalReadSettings{lines: lines, pane: pane, source: source}, nil
}

func terminalReadArguments(settings terminalReadSettings) []string {
	return []string{"pane", "read", settings.pane, "--source", settings.source, "--lines", strconv.Itoa(settings.lines), "--format", "ansi", "--raw"}
}

func terminalSessionArguments(settings terminalBridgeSettings, target string) []string {
	operation := settings.mode
	if operation == "takeover" {
		operation = "control"
	}
	arguments := []string{"terminal", "session", operation, target, "--cols", strconv.Itoa(settings.cols), "--rows", strconv.Itoa(settings.rows)}
	if settings.takeover {
		arguments = append(arguments, "--takeover")
	}
	return arguments
}

type terminalFrameEnvelope struct {
	Bytes    *string `json:"bytes"`
	Encoding *string `json:"encoding"`
	Full     *bool   `json:"full"`
	Height   *int    `json:"height"`
	Seq      *uint64 `json:"seq"`
	Type     string  `json:"type"`
	Width    *int    `json:"width"`
}

type terminalFrameValidator struct {
	lastSeq uint64
	started bool
}

func (validator *terminalFrameValidator) Accept(line []byte) (string, error) {
	var kind struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(line, &kind); err != nil {
		return "", fmt.Errorf("decode frame kind: %w", err)
	}
	if kind.Type == "terminal.closed" {
		var closed struct {
			Reason string `json:"reason"`
			Type   string `json:"type"`
		}
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&closed); err != nil || closed.Type != "terminal.closed" || closed.Reason == "" || len(closed.Reason) > 8_192 {
			return "", errors.New("terminal closure fields are invalid")
		}
		if err := requireJSONEnd(decoder); err != nil {
			return "", err
		}
		return closed.Reason, nil
	}
	var frame terminalFrameEnvelope
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&frame); err != nil {
		return "", fmt.Errorf("decode frame: %w", err)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return "", err
	}
	if frame.Type != "terminal.frame" || frame.Encoding == nil || *frame.Encoding != "ansi" || frame.Full == nil ||
		frame.Width == nil || *frame.Width < 1 || *frame.Width > 1000 || frame.Height == nil || *frame.Height < 1 || *frame.Height > 1000 ||
		frame.Seq == nil || *frame.Seq < 1 || frame.Bytes == nil {
		return "", errors.New("frame fields are missing or outside protocol bounds")
	}
	if !validator.started && !*frame.Full {
		return "", errors.New("first frame is not full")
	}
	if validator.started && *frame.Seq <= validator.lastSeq {
		return "", fmt.Errorf("sequence %d does not follow %d", *frame.Seq, validator.lastSeq)
	}
	decoded, err := base64.StdEncoding.DecodeString(*frame.Bytes)
	if err != nil || len(decoded) > terminalBridgeMaxFrameBytes {
		return "", errors.New("frame bytes are not valid base64 within the size limit")
	}
	validator.started = true
	validator.lastSeq = *frame.Seq
	return "", nil
}

type terminalBrowserCommand struct {
	childCommands []any
	release       bool
	requestID     uint64
}

func validatedTerminalCommand(message []byte) (terminalBrowserCommand, error) {
	var envelope struct {
		Bytes     *string  `json:"bytes"`
		Chunks    []string `json:"chunks"`
		Cols      int      `json:"cols"`
		Direction string   `json:"direction"`
		Lines     int      `json:"lines"`
		RequestID uint64   `json:"request_id"`
		Rows      int      `json:"rows"`
		Text      *string  `json:"text"`
		Type      string   `json:"type"`
	}
	decoder := json.NewDecoder(bytes.NewReader(message))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return terminalBrowserCommand{}, errors.New("invalid JSON")
	}
	if err := requireJSONEnd(decoder); err != nil {
		return terminalBrowserCommand{}, errors.New("invalid JSON")
	}
	inputFieldsOnly := envelope.Cols == 0 && envelope.Rows == 0 && envelope.Direction == "" && envelope.Lines == 0
	switch envelope.Type {
	case "terminal.input":
		if !inputFieldsOnly || envelope.RequestID < 1 || envelope.RequestID > 1<<53-1 || len(envelope.Chunks) > 0 || (envelope.Text == nil) == (envelope.Bytes == nil) {
			return terminalBrowserCommand{}, errors.New("input fields are invalid")
		}
		if envelope.Text != nil {
			if len(*envelope.Text) > terminalBridgeMaxCommandBytes {
				return terminalBrowserCommand{}, errors.New("input is too large")
			}
			return terminalBrowserCommand{childCommands: []any{map[string]any{"type": envelope.Type, "text": *envelope.Text}}, requestID: envelope.RequestID}, nil
		}
		decoded, err := base64.StdEncoding.DecodeString(*envelope.Bytes)
		if err != nil || len(decoded) > terminalBridgeMaxCommandBytes {
			return terminalBrowserCommand{}, errors.New("bytes must be valid base64 within the size limit")
		}
		return terminalBrowserCommand{childCommands: []any{map[string]any{"type": envelope.Type, "bytes": *envelope.Bytes}}, requestID: envelope.RequestID}, nil
	case "terminal.input-batch":
		if !inputFieldsOnly || envelope.RequestID < 1 || envelope.RequestID > 1<<53-1 || envelope.Text != nil || envelope.Bytes != nil || len(envelope.Chunks) < 1 || len(envelope.Chunks) > 256 {
			return terminalBrowserCommand{}, errors.New("input batch fields are invalid")
		}
		total := 0
		commands := make([]any, 0, len(envelope.Chunks))
		for _, chunk := range envelope.Chunks {
			total += len(chunk)
			if chunk == "" || total > terminalBridgeMaxCommandBytes {
				return terminalBrowserCommand{}, errors.New("input batch is empty or too large")
			}
			commands = append(commands, map[string]any{"type": "terminal.input", "text": chunk})
		}
		return terminalBrowserCommand{childCommands: commands, requestID: envelope.RequestID}, nil
	case "terminal.resize":
		if envelope.RequestID != 0 || envelope.Text != nil || envelope.Bytes != nil || len(envelope.Chunks) > 0 || envelope.Direction != "" || envelope.Lines != 0 ||
			envelope.Cols < 2 || envelope.Cols > 1000 || envelope.Rows < 1 || envelope.Rows > 1000 {
			return terminalBrowserCommand{}, errors.New("resize fields are invalid")
		}
		return terminalBrowserCommand{childCommands: []any{map[string]any{"type": envelope.Type, "cols": envelope.Cols, "rows": envelope.Rows}}}, nil
	case "terminal.scroll":
		if envelope.RequestID != 0 || envelope.Text != nil || envelope.Bytes != nil || len(envelope.Chunks) > 0 || envelope.Cols != 0 || envelope.Rows != 0 ||
			(envelope.Direction != "up" && envelope.Direction != "down") || envelope.Lines < 1 || envelope.Lines > 1000 {
			return terminalBrowserCommand{}, errors.New("scroll fields are invalid")
		}
		return terminalBrowserCommand{childCommands: []any{map[string]any{"type": envelope.Type, "direction": envelope.Direction, "lines": envelope.Lines, "source": "page_key"}}}, nil
	case "terminal.release":
		if envelope.RequestID != 0 || envelope.Text != nil || envelope.Bytes != nil || len(envelope.Chunks) > 0 || envelope.Cols != 0 || envelope.Rows != 0 || envelope.Direction != "" || envelope.Lines != 0 {
			return terminalBrowserCommand{}, errors.New("release fields are invalid")
		}
		return terminalBrowserCommand{childCommands: []any{map[string]string{"type": envelope.Type}}, release: true}, nil
	default:
		return terminalBrowserCommand{}, fmt.Errorf("unsupported type %q", envelope.Type)
	}
}

func requireJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("JSON has trailing content")
	}
	return nil
}

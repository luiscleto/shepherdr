package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/luiscleto/shepherdr/internal/herdr"
	"github.com/luiscleto/shepherdr/internal/uploads"
)

// terminalFileRequest opens a file send's body. The body is this JSON
// object, a newline, then each listed file's bytes in order, so the files
// stream to disk without being held in memory.
type terminalFileRequest struct {
	Controller string `json:"controller"`
	Intent     string `json:"intent"`
	Files      []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	} `json:"files"`
	PaneID      string `json:"pane_id"`
	Takeover    bool   `json:"takeover"`
	TerminalID  string `json:"terminal_id"`
	Text        string `json:"text"`
	WorkspaceID string `json:"workspace_id"`
}

type terminalFileResponse struct {
	Message string                `json:"message"`
	Result  uploads.ForwardResult `json:"result"`
}

type uploadTarget struct {
	label      string
	paneID     string
	tabID      string
	terminalID string
	workspace  string
}

// SetFileUploads keeps Herdr TUI file records in manager.
func (s *Server) SetFileUploads(manager *uploads.Manager, limit uploads.Limit) {
	s.fileUploads = manager
	s.fileUploadLimit = limit
	s.fileUploadState = s.projector
}

// SetCoreFileUploads keeps Herdr Core file records in their own manager.
func (s *Server) SetCoreFileUploads(manager *uploads.Manager) {
	s.coreFileUploads = manager
}

// uploadSide is what one Herdr's file sends use: its records, its Home, its
// target rule and its terminal bridge.
type uploadSide struct {
	manager *uploads.Manager
	state   TerminalStateSource
	resolve func(herdr.State, string, string, string) (uploadTarget, bool)
	send    func(request *http.Request, envelope terminalFileRequest, target uploadTarget, validate func() error, chunks []string) (terminalBatchOutcome, error)
	logger  func() *slog.Logger
}

func (s *Server) uploadSide(name string) (uploadSide, bool) {
	switch {
	case name == "tui" && s.terminal != nil && s.fileUploads != nil && s.fileUploadState != nil:
		return uploadSide{
			manager: s.fileUploads, state: s.fileUploadState, resolve: resolveUploadTarget,
			send: func(request *http.Request, envelope terminalFileRequest, target uploadTarget, validate func() error, chunks []string) (terminalBatchOutcome, error) {
				if envelope.Controller != "" {
					return s.terminal.sendControllerBatch(request, envelope.Controller, target.paneID, target.terminalID, validate, chunks)
				}
				return s.terminal.SendBatch(request, target.paneID, target.terminalID, envelope.Takeover, validate, chunks)
			},
			logger: func() *slog.Logger { return s.terminal.logger },
		}, true
	case name == "core" && s.coreTerminal != nil && s.coreFileUploads != nil:
		return uploadSide{
			manager: s.coreFileUploads, state: s.coreProjector, resolve: resolveCoreUploadTarget,
			send: func(request *http.Request, envelope terminalFileRequest, target uploadTarget, validate func() error, chunks []string) (terminalBatchOutcome, error) {
				if envelope.Controller == "" {
					return terminalBatchNotSent, errors.New("Herdr Core files need the controlling connection")
				}
				return s.coreTerminal.sendControllerBatch(request, envelope.Controller, target.paneID, target.terminalID, validate, chunks)
			},
			logger: func() *slog.Logger { return s.coreTerminal.logger },
		}, true
	default:
		return uploadSide{}, false
	}
}

func (s *Server) terminalFileSend(writer http.ResponseWriter, request *http.Request) {
	request, lease, ok := s.bindAccessRequest(request)
	if !ok {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	if lease != nil {
		defer lease.Close()
		defer cleanupBoundRequest(request)
	}
	stopBodyClose := context.AfterFunc(request.Context(), func() { _ = request.Body.Close() })
	defer stopBodyClose()
	side, ok := s.uploadSide(request.URL.Query().Get("herdr"))
	if !ok {
		writeUploadError(writer, http.StatusBadRequest, "Files were not sent because the exact target or file list is invalid")
		return
	}
	if !exactFileSendContentType(request) {
		writeUploadError(writer, http.StatusUnsupportedMediaType, "Files were not sent. Use application/octet-stream.")
		return
	}
	paced := paceUploadBody(writer, request, s.uploadMinRate)
	envelope, files, body, err := readTerminalFileRequest(request.Body, s.fileUploadLimit)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errUploadTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		writeUploadError(writer, status, err.Error())
		return
	}
	initial, ok := side.resolve(side.state.Current(), envelope.WorkspaceID, envelope.PaneID, envelope.TerminalID)
	if !ok {
		writeUploadResult(writer, http.StatusConflict, uploads.NotSent, "Files were not sent because this terminal no longer has a recognized agent.")
		return
	}
	validate := func() error {
		current, ok := side.resolve(side.state.Current(), initial.workspace, initial.paneID, initial.terminalID)
		if !ok || current.tabID != initial.tabID {
			return errors.New("this terminal no longer has a recognized agent")
		}
		return nil
	}
	result, sendErr := side.manager.StageAndForward(request.Context(), initial.workspace, initial.label, files, paced.stop, validate, func(paths []string, validateStaged func() error) (uploads.ForwardResult, error) {
		validateBeforeWrite := func() error {
			if err := validateStaged(); err != nil {
				return err
			}
			return validate()
		}
		if err := requireBodyEnd(body); err != nil {
			return uploads.NotSent, err
		}
		if err := validateBeforeWrite(); err != nil {
			return uploads.NotSent, err
		}
		var chunks []string
		var err error
		if envelope.Intent == "insert" {
			chunks, err = uploadTerminalInsertion(paths)
		} else {
			chunks, err = uploadTerminalSubmission(paths, envelope.Text)
		}
		if err != nil {
			return uploads.NotSent, err
		}
		outcome, err := side.send(request, envelope, initial, validateBeforeWrite, chunks)
		switch outcome {
		case terminalBatchForwarded:
			return uploads.Forwarded, err
		case terminalBatchOccupied:
			return uploads.Occupied, err
		case terminalBatchUnknown:
			return uploads.Unknown, err
		default:
			return uploads.NotSent, err
		}
	})
	status := http.StatusOK
	message := "Terminal input was sent."
	switch result {
	case uploads.Occupied:
		status = http.StatusConflict
		message = "Someone else is controlling this terminal, so the files were not sent."
	case uploads.Unknown:
		status = http.StatusBadGateway
		message = "The input was handed to the connection, but forwarding could not be confirmed. Check the terminal before sending it again."
	case uploads.NotSent:
		status = http.StatusConflict
		message = "Files were not sent. Try again."
		if errors.Is(sendErr, errUploadBody) {
			status = http.StatusBadRequest
			message = "Files were not sent because the request body could not be read."
		}
	}
	if sendErr != nil {
		side.logger().Error("terminal file send stopped", "workspace", initial.workspace, "result", result, "error", sendErr)
	}
	if err := withCommitAuthority(request, func() error {
		writeUploadResult(writer, status, result, message)
		return nil
	}); err != nil {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
	}
}

// A file send's body has uploadBodyGrace to start, then must keep arriving
// at the minimum rate on average, so a large send on a slow link completes
// and a client that trickles its body is cut off.
var uploadBodyGrace = 30 * time.Second

// DefaultUploadMinRate is the minimum rate, in bytes a second, unless
// -upload-min-rate sets another.
const DefaultUploadMinRate = int64(8 << 10)

// paceUploadBody replaces the general body time with the file send's pace,
// or with no read deadline when rate is 0.
func paceUploadBody(writer http.ResponseWriter, request *http.Request, rate int64) *uploadBody {
	body := &uploadBody{ReadCloser: request.Body, controller: http.NewResponseController(writer), rate: rate, start: time.Now()}
	if rate == 0 {
		_ = body.controller.SetReadDeadline(time.Time{})
	}
	request.Body = body
	return body
}

// uploadBody marks read failures as errUploadBody and keeps the pace. Once
// the body has ended it never sets a read deadline again: net/http then
// reads the connection in the background, and a deadline there would cancel
// the request while it forwards.
type uploadBody struct {
	io.ReadCloser
	controller *http.ResponseController
	rate       int64
	start      time.Time

	mu      sync.Mutex
	read    int64
	ended   bool
	stopped bool
}

func (body *uploadBody) Read(buffer []byte) (int, error) {
	body.mu.Lock()
	if body.ended {
		body.mu.Unlock()
		return 0, io.EOF
	}
	if body.stopped {
		body.mu.Unlock()
		return 0, fmt.Errorf("%w: the send was stopped", errUploadBody)
	}
	if body.rate > 0 {
		allowed := uploadBodyGrace + time.Duration(body.read/body.rate)*time.Second
		_ = body.controller.SetReadDeadline(body.start.Add(allowed))
	}
	body.mu.Unlock()
	count, err := body.ReadCloser.Read(buffer)
	body.mu.Lock()
	body.read += int64(count)
	body.ended = errors.Is(err, io.EOF)
	body.mu.Unlock()
	if err != nil && !errors.Is(err, io.EOF) {
		err = fmt.Errorf("%w: %w", errUploadBody, err)
	}
	return count, err
}

// stop makes a waiting read fail at once, and every later one. It leaves a
// body that has ended alone.
func (body *uploadBody) stop() {
	body.mu.Lock()
	defer body.mu.Unlock()
	if body.ended || body.stopped {
		return
	}
	body.stopped = true
	_ = body.controller.SetReadDeadline(time.Now())
}

var (
	errUploadTooLarge = errors.New("The selected files exceed the send limit. Remove files or choose smaller ones.")
	errUploadBody     = errors.New("files were not sent because the request body could not be read")
)

func exactFileSendContentType(request *http.Request) bool {
	values := request.Header.Values("Content-Type")
	return len(values) == 1 && values[0] == "application/octet-stream"
}

// readTerminalFileRequest reads the JSON line that opens a file send and
// returns the files, each to be read in turn from rest, the remaining body.
func readTerminalFileRequest(body io.Reader, limit uploads.Limit) (envelope terminalFileRequest, files []uploads.File, rest io.Reader, err error) {
	metadata := &countingReader{reader: body}
	var source io.Reader = metadata
	if !limit.Unlimited {
		source = io.LimitReader(metadata, uploads.MetadataAllowance)
	}
	decoder := json.NewDecoder(source)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		switch {
		case errors.Is(err, errUploadBody):
			return terminalFileRequest{}, nil, nil, errUploadBody
		case !limit.Unlimited && metadata.read >= uploads.MetadataAllowance:
			return terminalFileRequest{}, nil, nil, errUploadTooLarge
		default:
			return terminalFileRequest{}, nil, nil, errors.New("files were not sent because the request is invalid")
		}
	}
	rest = io.MultiReader(decoder.Buffered(), body)
	var separator [1]byte
	if _, err := io.ReadFull(rest, separator[:]); err != nil || separator[0] != '\n' {
		if errors.Is(err, errUploadBody) {
			return terminalFileRequest{}, nil, nil, errUploadBody
		}
		return terminalFileRequest{}, nil, nil, errors.New("files were not sent because the request is invalid")
	}
	if !validTerminalIdentity(envelope.WorkspaceID) || !validTerminalIdentity(envelope.PaneID) || !validTerminalIdentity(envelope.TerminalID) || len(envelope.Files) == 0 {
		return terminalFileRequest{}, nil, nil, errors.New("files were not sent because the exact target or file list is invalid")
	}
	if envelope.Controller == "" && envelope.Intent != "" || envelope.Controller != "" &&
		(len(envelope.Controller) != 64 || envelope.Takeover || envelope.Intent != "submit" && envelope.Intent != "insert" || envelope.Intent == "insert" && envelope.Text != "") {
		return terminalFileRequest{}, nil, nil, errors.New("files were not sent because the controlling connection or intent is invalid")
	}
	files = make([]uploads.File, 0, len(envelope.Files))
	var total int64
	for _, candidate := range envelope.Files {
		if candidate.Size < 0 {
			return terminalFileRequest{}, nil, nil, errors.New("files were not sent because the exact target or file list is invalid")
		}
		if total > math.MaxInt64-candidate.Size {
			return terminalFileRequest{}, nil, nil, errUploadTooLarge
		}
		total += candidate.Size
		files = append(files, uploads.File{Data: rest, Name: candidate.Name, Size: candidate.Size})
	}
	if !limit.Allows(total) {
		return terminalFileRequest{}, nil, nil, errUploadTooLarge
	}
	return envelope, files, rest, nil
}

type countingReader struct {
	reader io.Reader
	read   int64
}

func (counter *countingReader) Read(buffer []byte) (int, error) {
	count, err := counter.reader.Read(buffer)
	counter.read += int64(count)
	return count, err
}

// requireBodyEnd refuses a body with bytes past its listed files.
func requireBodyEnd(body io.Reader) error {
	var extra [1]byte
	count, err := body.Read(extra[:])
	for count == 0 && err == nil {
		count, err = body.Read(extra[:])
	}
	if count > 0 {
		return errors.New("files were not sent because the request body is longer than its files")
	}
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func resolveUploadTarget(state herdr.State, workspaceID, paneID, terminalID string) (uploadTarget, bool) {
	if state.Connection != herdr.ConnectionLive || state.LastKnown {
		return uploadTarget{}, false
	}
	label := ""
	foundWorkspace := false
	for _, workspace := range state.Snapshot.Workspaces {
		if workspace.WorkspaceID == workspaceID {
			label = workspace.Label
			foundWorkspace = true
			break
		}
	}
	if !foundWorkspace {
		return uploadTarget{}, false
	}
	tabID := ""
	for _, pane := range state.Snapshot.Panes {
		if pane.WorkspaceID == workspaceID && pane.PaneID == paneID && pane.TerminalID == terminalID {
			tabID = pane.TabID
			break
		}
	}
	if tabID == "" {
		return uploadTarget{}, false
	}
	for _, agent := range state.Snapshot.Agents {
		if agent.WorkspaceID == workspaceID && agent.TabID == tabID && agent.PaneID == paneID && agent.TerminalID == terminalID && agent.AgentStatus.Valid() {
			return uploadTarget{label: label, paneID: paneID, tabID: tabID, terminalID: terminalID, workspace: workspaceID}, true
		}
	}
	return uploadTarget{}, false
}

// resolveCoreUploadTarget is the Herdr Core file target rule. An agent is a
// terminal with a current status, whatever the word, including error. Other
// terminals is one group for files while Home lists it.
func resolveCoreUploadTarget(state herdr.State, workspaceID, paneID, terminalID string) (uploadTarget, bool) {
	if state.Connection != herdr.ConnectionLive || state.LastKnown {
		return uploadTarget{}, false
	}
	label, found := "Other terminals", workspaceID == herdr.OtherTerminalsID
	for _, workspace := range state.Snapshot.Workspaces {
		if workspace.WorkspaceID == workspaceID {
			label, found = workspace.Label, true
		}
	}
	if !found {
		return uploadTarget{}, false
	}
	for _, agent := range state.Snapshot.Agents {
		if agent.WorkspaceID == workspaceID && agent.PaneID == paneID && agent.TerminalID == terminalID && agent.AgentStatus != "" {
			for _, pane := range state.Snapshot.Panes {
				if pane.WorkspaceID == workspaceID && pane.PaneID == paneID && pane.TerminalID == terminalID && pane.TabID == agent.TabID {
					return uploadTarget{label: label, paneID: paneID, tabID: pane.TabID, terminalID: terminalID, workspace: workspaceID}, true
				}
			}
		}
	}
	return uploadTarget{}, false
}

func uploadTerminalSubmission(paths []string, text string) ([]string, error) {
	if len(paths) == 0 {
		return nil, errors.New("uploaded path list is empty")
	}
	var builder strings.Builder
	builder.WriteString("User uploaded files:\n")
	for _, path := range paths {
		if err := validateGeneratedPath(path); err != nil {
			return nil, err
		}
		builder.WriteString("- ")
		builder.WriteString(path)
		builder.WriteByte('\n')
	}
	text = strings.ReplaceAll(text, "\x1b", "")
	if text != "" {
		builder.WriteByte('\n')
		builder.WriteString(text)
	}
	value := strings.ReplaceAll(builder.String(), "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.ReplaceAll(value, "\n", "\r")
	return []string{"\x1b[200~" + value + "\x1b[201~", "\r"}, nil
}

func uploadTerminalInsertion(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, errors.New("uploaded path list is empty")
	}
	var value strings.Builder
	for _, path := range paths {
		if err := validateGeneratedPath(path); err != nil {
			return nil, err
		}
		value.WriteString("[User uploaded ")
		value.WriteString(path)
		value.WriteString("] ")
	}
	return []string{"\x1b[200~" + value.String() + "\x1b[201~"}, nil
}

func validateGeneratedPath(path string) error {
	if err := uploads.ValidatePathText(path); err != nil {
		return fmt.Errorf("generated upload path is unsafe: %w", err)
	}
	return nil
}

func writeUploadError(writer http.ResponseWriter, status int, message string) {
	writeUploadResult(writer, status, uploads.NotSent, message)
}

func writeUploadResult(writer http.ResponseWriter, status int, result uploads.ForwardResult, message string) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(terminalFileResponse{Message: message, Result: result})
}

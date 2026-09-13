package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/luiscleto/shepherdr/internal/herdr"
	"github.com/luiscleto/shepherdr/internal/uploads"
)

type terminalFileRequest struct {
	Controller string `json:"controller"`
	Intent     string `json:"intent"`
	Files      []struct {
		Data string `json:"data"`
		Name string `json:"name"`
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

func (s *Server) SetFileUploads(manager *uploads.Manager, limit uploads.Limit) {
	s.fileUploads = manager
	s.fileUploadLimit = limit
	s.fileUploadState = s.projector
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
	if s.access == nil && !sameOrigin(request) {
		writeUploadError(writer, http.StatusForbidden, "Files were not sent. This request cannot use Shepherdr.")
		return
	}
	if !exactJSONContentType(request) {
		writeUploadError(writer, http.StatusUnsupportedMediaType, "Files were not sent. Use application/json.")
		return
	}
	envelope, files, err := readTerminalFileRequest(request, s.fileUploadLimit)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errUploadTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		writeUploadError(writer, status, err.Error())
		return
	}
	initial, ok := resolveUploadTarget(s.fileUploadState.Current(), envelope.WorkspaceID, envelope.PaneID, envelope.TerminalID)
	if !ok {
		writeUploadResult(writer, http.StatusConflict, uploads.NotSent, "Files were not sent because this terminal no longer has a recognized agent.")
		return
	}
	validate := func() error {
		current, ok := resolveUploadTarget(s.fileUploadState.Current(), initial.workspace, initial.paneID, initial.terminalID)
		if !ok || current.tabID != initial.tabID {
			return errors.New("this terminal no longer has a recognized agent")
		}
		return nil
	}
	result, sendErr := s.fileUploads.StageAndForward(request.Context(), initial.workspace, initial.label, files, validate, func(paths []string, validateStaged func() error) (uploads.ForwardResult, error) {
		validateBeforeWrite := func() error {
			if err := validateStaged(); err != nil {
				return err
			}
			return validate()
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
		var outcome terminalBatchOutcome
		if envelope.Controller != "" {
			outcome, err = s.terminal.sendControllerBatch(request, envelope.Controller, initial.paneID, initial.terminalID, validateBeforeWrite, chunks)
		} else {
			outcome, err = s.terminal.SendBatch(request, initial.paneID, initial.terminalID, envelope.Takeover, validateBeforeWrite, chunks)
		}
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
	}
	if sendErr != nil {
		s.terminal.logger.Error("terminal file send stopped", "workspace", initial.workspace, "result", result, "error", sendErr)
	}
	if err := withCommitAuthority(request, func() error {
		writeUploadResult(writer, status, result, message)
		return nil
	}); err != nil {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
	}
}

var errUploadTooLarge = errors.New("The selected files exceed the send limit. Remove files or choose smaller ones.")

func readTerminalFileRequest(request *http.Request, limit uploads.Limit) (terminalFileRequest, []uploads.File, error) {
	var body []byte
	var err error
	if limit.Unlimited {
		body, err = io.ReadAll(request.Body)
	} else {
		body, err = io.ReadAll(io.LimitReader(request.Body, limit.OuterBodyLimit()+1))
		if err == nil && int64(len(body)) > limit.OuterBodyLimit() {
			return terminalFileRequest{}, nil, errUploadTooLarge
		}
	}
	if err != nil {
		return terminalFileRequest{}, nil, errors.New("files were not sent because the request body could not be read")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var envelope terminalFileRequest
	if err := decoder.Decode(&envelope); err != nil || requireJSONEnd(decoder) != nil {
		return terminalFileRequest{}, nil, errors.New("files were not sent because the request JSON is invalid")
	}
	if !validTerminalIdentity(envelope.WorkspaceID) || !validTerminalIdentity(envelope.PaneID) || !validTerminalIdentity(envelope.TerminalID) || len(envelope.Files) == 0 {
		return terminalFileRequest{}, nil, errors.New("files were not sent because the exact target or file list is invalid")
	}
	if envelope.Controller == "" && envelope.Intent != "" || envelope.Controller != "" &&
		(len(envelope.Controller) != 64 || envelope.Takeover || envelope.Intent != "submit" && envelope.Intent != "insert" || envelope.Intent == "insert" && envelope.Text != "") {
		return terminalFileRequest{}, nil, errors.New("files were not sent because the controlling connection or intent is invalid")
	}
	files := make([]uploads.File, 0, len(envelope.Files))
	var decodedTotal int64
	var encodedTotal int64
	for _, candidate := range envelope.Files {
		if request.Context().Err() != nil {
			return terminalFileRequest{}, nil, errors.New("files were not sent because the request ended")
		}
		decoded, err := base64.StdEncoding.DecodeString(candidate.Data)
		if err != nil || base64.StdEncoding.EncodeToString(decoded) != candidate.Data {
			return terminalFileRequest{}, nil, errors.New("files were not sent because file data is not canonical base64")
		}
		size := int64(len(decoded))
		if decodedTotal > int64(^uint64(0)>>1)-size {
			return terminalFileRequest{}, nil, errUploadTooLarge
		}
		decodedTotal += size
		encodedTotal += uploads.EncodedContribution(size)
		files = append(files, uploads.File{Name: candidate.Name, Data: decoded})
	}
	if err := limit.Validate(decodedTotal, encodedTotal, int64(len(body))); err != nil {
		return terminalFileRequest{}, nil, errUploadTooLarge
	}
	return envelope, files, nil
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

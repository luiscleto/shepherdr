package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"

	"github.com/luiscleto/shepherdr/internal/access"
	"github.com/luiscleto/shepherdr/internal/herdr"
)

const maxWorkspaceActionRequest = 16 << 10

type workspaceActionClient interface {
	Snapshot(context.Context) (herdr.Snapshot, error)
	CreateWorkspace(context.Context, string, *string) error
	CreateWorktree(context.Context, herdr.CreateWorktreeSource, *string) error
	CloseWorkspace(context.Context, string, bool) error
	RemoveWorktree(context.Context, string) error
}

type homeRefresher interface {
	RequestRefresh()
}

type workspaceActionCoordinator struct {
	client   workspaceActionClient
	refresh  homeRefresher
	homeDir  func() (string, error)
	mutation chan struct{}
}

func newWorkspaceActionCoordinator(client workspaceActionClient, refresh homeRefresher) *workspaceActionCoordinator {
	return &workspaceActionCoordinator{
		client: client, refresh: refresh, homeDir: os.UserHomeDir, mutation: make(chan struct{}, 1),
	}
}

type interruptionCounts struct {
	Working int `json:"working"`
	Blocked int `json:"blocked"`
	Unknown int `json:"unknown"`
}

type confirmationFacts struct {
	WorkspaceLabel     string             `json:"workspace_label"`
	ScopeWorkspaceIDs  []string           `json:"scope_workspace_ids"`
	AgentTotal         int                `json:"agent_total"`
	InterruptionCounts interruptionCounts `json:"interruption_counts"`
	CheckoutPath       string             `json:"checkout_path,omitempty"`
}

type refusedResponse struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
	Detail  string `json:"detail,omitempty"`
}

func (c *workspaceActionCoordinator) prepare(writer http.ResponseWriter, request *http.Request) {
	fields, ok := readActionObject(writer, request)
	if !ok || !requireObjectKeys(writer, fields, []string{"action", "workspace_id"}, []string{"action", "workspace_id"}) {
		return
	}
	actionName, actionOK := decodeString(fields["action"])
	workspaceID, workspaceOK := decodeString(fields["workspace_id"])
	action, destructive := destructiveAction(actionName)
	if !actionOK || !workspaceOK || workspaceID == "" || !destructive {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}

	var snapshot herdr.Snapshot
	err := withCommitAuthority(request, func() error {
		var snapshotErr error
		snapshot, snapshotErr = c.client.Snapshot(request.Context())
		return snapshotErr
	})
	if err != nil {
		if errors.Is(err, access.ErrUnauthorized) {
			writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
			return
		}
		writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
		return
	}
	facts, status, reason := prepareFacts(snapshot, action, workspaceID)
	if reason != "" {
		writeRefusal(writer, status, reason, "")
		return
	}
	writeActionJSON(writer, http.StatusOK, struct {
		Outcome     string            `json:"outcome"`
		Action      string            `json:"action"`
		WorkspaceID string            `json:"workspace_id"`
		Expected    confirmationFacts `json:"expected"`
	}{Outcome: "prepared", Action: actionName, WorkspaceID: workspaceID, Expected: facts})
}

func (c *workspaceActionCoordinator) run(writer http.ResponseWriter, request *http.Request) {
	fields, ok := readActionObject(writer, request)
	if !ok {
		return
	}
	actionName, actionOK := decodeString(fields["action"])
	if !actionOK {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}

	switch actionName {
	case "create_space":
		c.runCreateSpace(writer, request, fields)
	case string(herdr.WorkspaceActionCreateWorktree):
		c.runCreateWorktree(writer, request, fields)
	default:
		action, destructive := destructiveAction(actionName)
		if !destructive {
			writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
			return
		}
		c.runDestructive(writer, request, fields, action)
	}
}

func (c *workspaceActionCoordinator) runCreateSpace(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	if !requireObjectKeys(writer, fields, []string{"action", "working_directory", "label"}, []string{"action", "working_directory"}) {
		return
	}
	workingDirectory, ok := decodeString(fields["working_directory"])
	if !ok || workingDirectory == "" {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	var label *string
	if raw, present := fields["label"]; present {
		value, valid := decodeString(raw)
		if !valid {
			writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
			return
		}
		if value != "" {
			label = &value
		}
	}
	expanded, err := expandTilde(workingDirectory, c.homeDir)
	if err != nil {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "Could not find the current user's home directory.")
		return
	}
	if !c.acquire() {
		writeRefusal(writer, http.StatusConflict, "busy", "")
		return
	}
	defer c.release()
	c.finishMutation(writer, withCommitAuthority(request, func() error {
		return c.client.CreateWorkspace(context.Background(), expanded, label)
	}))
}

func (c *workspaceActionCoordinator) runCreateWorktree(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	if !requireObjectKeys(writer, fields, []string{"action", "workspace_id", "branch"}, []string{"action", "workspace_id"}) {
		return
	}
	workspaceID, ok := decodeString(fields["workspace_id"])
	if !ok || workspaceID == "" {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	var branch *string
	if raw, present := fields["branch"]; present {
		value, valid := decodeString(raw)
		if !valid {
			writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
			return
		}
		if value != "" {
			branch = &value
		}
	}
	if !c.acquire() {
		writeRefusal(writer, http.StatusConflict, "busy", "")
		return
	}
	defer c.release()

	snapshot, err := c.client.Snapshot(context.Background())
	if err != nil {
		writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
		return
	}
	target, found, applicable := herdr.ResolveWorkspaceAction(snapshot, herdr.WorkspaceActionCreateWorktree, workspaceID)
	if !found {
		writeRefusal(writer, http.StatusNotFound, "not_found", "")
		return
	}
	if !applicable {
		writeRefusal(writer, http.StatusConflict, "not_applicable", "")
		return
	}
	c.finishMutation(writer, withCommitAuthority(request, func() error {
		return c.client.CreateWorktree(context.Background(), target.WorktreeSource, branch)
	}))
}

func (c *workspaceActionCoordinator) runDestructive(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage, action herdr.WorkspaceAction) {
	if !requireObjectKeys(writer, fields, []string{"action", "workspace_id", "expected"}, []string{"action", "workspace_id", "expected"}) {
		return
	}
	workspaceID, ok := decodeString(fields["workspace_id"])
	expected, factsOK := decodeConfirmationFacts(fields["expected"], action == herdr.WorkspaceActionDeleteCheckout)
	if !ok || workspaceID == "" || !factsOK {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if !c.acquire() {
		writeRefusal(writer, http.StatusConflict, "busy", "")
		return
	}
	defer c.release()

	snapshot, err := c.client.Snapshot(context.Background())
	if err != nil {
		writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
		return
	}
	current, status, reason := prepareFacts(snapshot, action, workspaceID)
	if reason != "" {
		writeRefusal(writer, status, reason, "")
		return
	}
	if !reflect.DeepEqual(current, expected) {
		writeRefusal(writer, http.StatusConflict, "stale", "")
		return
	}

	switch action {
	case herdr.WorkspaceActionCloseWorkspace, herdr.WorkspaceActionCloseGroup:
		err = withCommitAuthority(request, func() error {
			return c.client.CloseWorkspace(context.Background(), workspaceID, action == herdr.WorkspaceActionCloseGroup)
		})
	case herdr.WorkspaceActionDeleteCheckout:
		err = withCommitAuthority(request, func() error { return c.client.RemoveWorktree(context.Background(), workspaceID) })
	}
	c.finishMutation(writer, err)
}

func (c *workspaceActionCoordinator) finishMutation(writer http.ResponseWriter, err error) {
	if err == nil {
		if c.refresh != nil {
			c.refresh.RequestRefresh()
		}
		writeActionJSON(writer, http.StatusOK, struct {
			Outcome string `json:"outcome"`
		}{Outcome: "succeeded"})
		return
	}
	if errors.Is(err, access.ErrUnauthorized) {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	var apiError *herdr.APIError
	if errors.As(err, &apiError) {
		reason := "herdr_refused"
		if apiError.Code == "dirty_worktree_requires_force" {
			reason = "checkout_has_changes"
		}
		writeRefusal(writer, http.StatusUnprocessableEntity, reason, apiError.Message)
		return
	}
	if herdr.MutationMayHaveRun(err) {
		status := http.StatusBadGateway
		if herdr.MutationTimedOut(err) {
			status = http.StatusGatewayTimeout
		}
		writeActionJSON(writer, status, struct {
			Outcome string `json:"outcome"`
		}{Outcome: "unknown"})
		return
	}
	writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
}

func (c *workspaceActionCoordinator) acquire() bool {
	select {
	case c.mutation <- struct{}{}:
		return true
	default:
		return false
	}
}

func (c *workspaceActionCoordinator) release() { <-c.mutation }

func prepareFacts(snapshot herdr.Snapshot, action herdr.WorkspaceAction, workspaceID string) (confirmationFacts, int, string) {
	target, found, applicable := herdr.ResolveWorkspaceAction(snapshot, action, workspaceID)
	if !found {
		return confirmationFacts{}, http.StatusNotFound, "not_found"
	}
	if !applicable {
		return confirmationFacts{}, http.StatusConflict, "not_applicable"
	}
	facts := confirmationFacts{
		WorkspaceLabel:    displayWorkspaceLabel(target.Workspace.Label),
		ScopeWorkspaceIDs: append([]string(nil), target.ScopeWorkspaceIDs...),
		CheckoutPath:      target.CheckoutPath,
	}
	scope := make(map[string]struct{}, len(target.ScopeWorkspaceIDs))
	for _, id := range target.ScopeWorkspaceIDs {
		scope[id] = struct{}{}
	}
	for _, agent := range snapshot.Agents {
		if _, affected := scope[agent.WorkspaceID]; !affected {
			continue
		}
		facts.AgentTotal++
		switch agent.AgentStatus {
		case herdr.StatusWorking:
			facts.InterruptionCounts.Working++
		case herdr.StatusBlocked:
			facts.InterruptionCounts.Blocked++
		case herdr.StatusUnknown:
			facts.InterruptionCounts.Unknown++
		}
	}
	return facts, http.StatusOK, ""
}

func destructiveAction(value string) (herdr.WorkspaceAction, bool) {
	action := herdr.WorkspaceAction(value)
	switch action {
	case herdr.WorkspaceActionCloseWorkspace, herdr.WorkspaceActionCloseGroup, herdr.WorkspaceActionDeleteCheckout:
		return action, true
	default:
		return "", false
	}
}

func decodeConfirmationFacts(raw json.RawMessage, checkoutRequired bool) (confirmationFacts, bool) {
	fields, ok := decodeObject(raw)
	if !ok {
		return confirmationFacts{}, false
	}
	allowed := []string{"workspace_label", "scope_workspace_ids", "agent_total", "interruption_counts"}
	if checkoutRequired {
		allowed = append(allowed, "checkout_path")
	}
	if !keysMatch(fields, allowed, allowed) {
		return confirmationFacts{}, false
	}
	label, labelOK := decodeString(fields["workspace_label"])
	var scope []string
	scopeOK := json.Unmarshal(fields["scope_workspace_ids"], &scope) == nil && scope != nil
	for _, workspaceID := range scope {
		if workspaceID == "" {
			scopeOK = false
		}
	}
	var total int
	totalOK := json.Unmarshal(fields["agent_total"], &total) == nil && total >= 0 && string(fields["agent_total"]) != "null"
	countFields, countsOK := decodeObject(fields["interruption_counts"])
	countsOK = countsOK && keysMatch(countFields, []string{"working", "blocked", "unknown"}, []string{"working", "blocked", "unknown"})
	var counts interruptionCounts
	if countsOK {
		countsOK = decodeNonnegativeInt(countFields["working"], &counts.Working) &&
			decodeNonnegativeInt(countFields["blocked"], &counts.Blocked) && decodeNonnegativeInt(countFields["unknown"], &counts.Unknown)
	}
	facts := confirmationFacts{WorkspaceLabel: label, ScopeWorkspaceIDs: scope, AgentTotal: total, InterruptionCounts: counts}
	if checkoutRequired {
		facts.CheckoutPath, ok = decodeString(fields["checkout_path"])
		if !ok {
			return confirmationFacts{}, false
		}
	}
	return facts, labelOK && scopeOK && totalOK && countsOK
}

func expandTilde(value string, homeDir func() (string, error)) (string, error) {
	if value != "~" && !strings.HasPrefix(value, "~/") {
		return value, nil
	}
	home, err := homeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("home directory unavailable")
	}
	return home + value[1:], nil
}

func readActionObject(writer http.ResponseWriter, request *http.Request) (map[string]json.RawMessage, bool) {
	if !sameOrigin(request) {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return nil, false
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return nil, false
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxWorkspaceActionRequest)
	decoder := json.NewDecoder(request.Body)
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil || fields == nil {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return nil, false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return nil, false
	}
	return fields, true
}

func sameOrigin(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host != request.Host || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	} else if forwarded := request.Header.Get("X-Forwarded-Proto"); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	return parsed.Scheme == scheme
}

func requireObjectKeys(writer http.ResponseWriter, fields map[string]json.RawMessage, allowed, required []string) bool {
	if keysMatch(fields, allowed, required) {
		return true
	}
	writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
	return false
}

func keysMatch(fields map[string]json.RawMessage, allowed, required []string) bool {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for key := range fields {
		if _, ok := allowedSet[key]; !ok {
			return false
		}
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return false
		}
	}
	return true
}

func decodeObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	err := json.Unmarshal(raw, &fields)
	return fields, err == nil && fields != nil
}

func decodeString(raw json.RawMessage) (string, bool) {
	var value string
	return value, len(raw) != 0 && string(raw) != "null" && json.Unmarshal(raw, &value) == nil
}

func decodeNonnegativeInt(raw json.RawMessage, target *int) bool {
	return len(raw) != 0 && string(raw) != "null" && json.Unmarshal(raw, target) == nil && *target >= 0
}

func displayWorkspaceLabel(label string) string {
	if label == "" {
		return "Workspace"
	}
	return label
}

func writeRefusal(writer http.ResponseWriter, status int, reason, detail string) {
	writeActionJSON(writer, status, refusedResponse{Outcome: "refused", Reason: reason, Detail: detail})
}

func writeActionJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

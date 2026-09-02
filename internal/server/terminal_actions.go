package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"

	"github.com/luiscleto/shepherdr/internal/access"
	"github.com/luiscleto/shepherdr/internal/herdr"
)

type terminalActionTarget struct {
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	PaneID      string `json:"pane_id"`
	TerminalID  string `json:"terminal_id"`
}

type terminalActionClient interface {
	workspaceActionClient
	SplitTerminal(context.Context, string, string, herdr.SplitDirection) (herdr.PaneInfo, error)
	RenameTerminal(context.Context, string, *string) (herdr.PaneInfo, error)
	CloseTerminal(context.Context, string) error
}

type terminalCloseFacts struct {
	Target            terminalActionTarget  `json:"target"`
	WorkspaceLabel    string                `json:"workspace_label"`
	TabLabel          string                `json:"tab_label"`
	TerminalTitle     string                `json:"terminal_title"`
	Agent             *herdr.Agent          `json:"agent,omitempty"`
	ClosesTab         bool                  `json:"closes_tab"`
	WorkspaceAction   herdr.WorkspaceAction `json:"workspace_action,omitempty"`
	WorkspaceExpected *confirmationFacts    `json:"workspace_expected,omitempty"`
}

type exactTerminalTarget struct {
	Pane      herdr.PaneInfo
	Projected herdr.Terminal
	Tab       herdr.TabInfo
	Workspace herdr.WorkspaceInfo
}

func (c *workspaceActionCoordinator) prepareTerminal(writer http.ResponseWriter, request *http.Request) {
	fields, ok := readActionObject(writer, request)
	if !ok || !requireObjectKeys(
		writer,
		fields,
		[]string{"action", "workspace_id", "tab_id", "pane_id", "terminal_id"},
		[]string{"action", "workspace_id", "tab_id", "pane_id", "terminal_id"},
	) {
		return
	}
	action, actionOK := decodeString(fields["action"])
	target, targetOK := decodeTerminalTarget(fields)
	if !actionOK || action != string(herdr.TerminalActionClose) || !targetOK {
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
	facts, status, reason := prepareTerminalClose(snapshot, target)
	if reason != "" {
		writeRefusal(writer, status, reason, "")
		return
	}
	writeActionJSON(writer, http.StatusOK, struct {
		Outcome  string             `json:"outcome"`
		Action   string             `json:"action"`
		Expected terminalCloseFacts `json:"expected"`
	}{Outcome: "prepared", Action: action, Expected: facts})
}

func (c *workspaceActionCoordinator) runTerminal(writer http.ResponseWriter, request *http.Request) {
	fields, ok := readActionObject(writer, request)
	if !ok {
		return
	}
	action, actionOK := decodeString(fields["action"])
	if !actionOK {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	switch herdr.TerminalAction(action) {
	case herdr.TerminalActionSplit:
		c.runSplitTerminal(writer, request, fields)
	case herdr.TerminalActionRename:
		c.runRenameTerminal(writer, request, fields)
	case herdr.TerminalActionClose:
		c.runCloseTerminal(writer, request, fields)
	default:
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
	}
}

func (c *workspaceActionCoordinator) runSplitTerminal(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	if !requireObjectKeys(
		writer,
		fields,
		[]string{"action", "workspace_id", "tab_id", "pane_id", "terminal_id", "direction"},
		[]string{"action", "workspace_id", "tab_id", "pane_id", "terminal_id", "direction"},
	) {
		return
	}
	target, targetOK := decodeTerminalTarget(fields)
	directionValue, directionOK := decodeString(fields["direction"])
	direction := herdr.SplitDirection(directionValue)
	if !targetOK || !directionOK || (direction != herdr.SplitRight && direction != herdr.SplitDown) {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	client, ok := c.client.(terminalActionClient)
	if !ok {
		writeRefusal(writer, http.StatusConflict, "not_applicable", "")
		return
	}
	if !c.acquire() {
		writeRefusal(writer, http.StatusConflict, "busy", "")
		return
	}
	defer c.release()

	before, exact, ok := c.freshTerminal(writer, target, herdr.TerminalActionSplit)
	if !ok {
		return
	}
	var created herdr.PaneInfo
	err := withCommitAuthority(request, func() error {
		var mutationErr error
		created, mutationErr = client.SplitTerminal(context.Background(), exact.Pane.PaneID, exact.Pane.CWD, direction)
		return mutationErr
	})
	if err != nil {
		c.finishTerminalError(writer, err)
		return
	}
	after, err := c.client.Snapshot(context.Background())
	if err != nil || !splitConfirmed(before, after, exact, created) {
		c.terminalUnknown(writer, http.StatusBadGateway)
		return
	}
	c.refreshTerminalHome()
	writeActionJSON(writer, http.StatusOK, struct {
		Outcome  string               `json:"outcome"`
		Terminal terminalActionTarget `json:"terminal"`
	}{Outcome: "succeeded", Terminal: terminalActionTarget{
		WorkspaceID: created.WorkspaceID, TabID: created.TabID, PaneID: created.PaneID, TerminalID: created.TerminalID,
	}})
}

func (c *workspaceActionCoordinator) runRenameTerminal(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	if !requireObjectKeys(
		writer,
		fields,
		[]string{"action", "workspace_id", "tab_id", "pane_id", "terminal_id", "name"},
		[]string{"action", "workspace_id", "tab_id", "pane_id", "terminal_id", "name"},
	) {
		return
	}
	target, targetOK := decodeTerminalTarget(fields)
	name, nameOK := decodeString(fields["name"])
	if !targetOK || !nameOK {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	client, ok := c.client.(terminalActionClient)
	if !ok {
		writeRefusal(writer, http.StatusConflict, "not_applicable", "")
		return
	}
	if !c.acquire() {
		writeRefusal(writer, http.StatusConflict, "busy", "")
		return
	}
	defer c.release()

	_, exact, ok := c.freshTerminal(writer, target, herdr.TerminalActionRename)
	if !ok {
		return
	}
	var label *string
	if strings.TrimSpace(name) != "" {
		label = &name
	}
	var renamed herdr.PaneInfo
	err := withCommitAuthority(request, func() error {
		var mutationErr error
		renamed, mutationErr = client.RenameTerminal(context.Background(), exact.Pane.PaneID, label)
		return mutationErr
	})
	if err != nil {
		c.finishTerminalError(writer, err)
		return
	}
	after, err := c.client.Snapshot(context.Background())
	if err != nil || !renameConfirmed(after, exact, renamed, label) {
		c.terminalUnknown(writer, http.StatusBadGateway)
		return
	}
	c.refreshTerminalHome()
	writeActionJSON(writer, http.StatusOK, struct {
		Outcome string `json:"outcome"`
	}{Outcome: "succeeded"})
}

func (c *workspaceActionCoordinator) runCloseTerminal(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	if !requireObjectKeys(writer, fields, []string{"action", "expected"}, []string{"action", "expected"}) {
		return
	}
	expected, ok := decodeTerminalCloseFacts(fields["expected"])
	if !ok {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	client, ok := c.client.(terminalActionClient)
	if !ok {
		writeRefusal(writer, http.StatusConflict, "not_applicable", "")
		return
	}
	if !c.acquire() {
		writeRefusal(writer, http.StatusConflict, "busy", "")
		return
	}
	defer c.release()

	before, err := c.client.Snapshot(context.Background())
	if err != nil {
		writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
		return
	}
	current, status, reason := prepareTerminalClose(before, expected.Target)
	if reason != "" {
		writeRefusal(writer, status, reason, "")
		return
	}
	if !reflect.DeepEqual(current, expected) {
		writeRefusal(writer, http.StatusConflict, "stale", "")
		return
	}

	err = withCommitAuthority(request, func() error {
		if current.WorkspaceExpected != nil {
			return c.client.CloseWorkspace(context.Background(), current.Target.WorkspaceID)
		}
		return client.CloseTerminal(context.Background(), current.Target.PaneID)
	})
	if err != nil {
		c.finishTerminalError(writer, err)
		return
	}
	after, err := c.client.Snapshot(context.Background())
	if err != nil || !closeConfirmed(after, current) {
		c.terminalUnknown(writer, http.StatusBadGateway)
		return
	}
	c.refreshTerminalHome()
	writeActionJSON(writer, http.StatusOK, struct {
		Outcome string `json:"outcome"`
	}{Outcome: "succeeded"})
}

func (c *workspaceActionCoordinator) freshTerminal(
	writer http.ResponseWriter,
	target terminalActionTarget,
	action herdr.TerminalAction,
) (herdr.Snapshot, exactTerminalTarget, bool) {
	snapshot, err := c.client.Snapshot(context.Background())
	if err != nil {
		writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
		return herdr.Snapshot{}, exactTerminalTarget{}, false
	}
	exact, found := resolveExactTerminal(snapshot, target)
	if !found {
		writeRefusal(writer, http.StatusNotFound, "not_found", "")
		return herdr.Snapshot{}, exactTerminalTarget{}, false
	}
	if !containsTerminalAction(exact.Projected.Actions, action) {
		writeRefusal(writer, http.StatusConflict, "not_applicable", "")
		return herdr.Snapshot{}, exactTerminalTarget{}, false
	}
	return snapshot, exact, true
}

func (c *workspaceActionCoordinator) finishTerminalError(writer http.ResponseWriter, err error) {
	if errors.Is(err, access.ErrUnauthorized) {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	var apiError *herdr.APIError
	if errors.As(err, &apiError) {
		writeRefusal(writer, http.StatusUnprocessableEntity, "herdr_refused", apiError.Message)
		return
	}
	if herdr.MutationMayHaveRun(err) {
		status := http.StatusBadGateway
		if herdr.MutationTimedOut(err) {
			status = http.StatusGatewayTimeout
		}
		c.terminalUnknown(writer, status)
		return
	}
	writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
}

func (c *workspaceActionCoordinator) terminalUnknown(writer http.ResponseWriter, status int) {
	c.refreshTerminalHome()
	writeActionJSON(writer, status, struct {
		Outcome string `json:"outcome"`
	}{Outcome: "unknown"})
}

func (c *workspaceActionCoordinator) refreshTerminalHome() {
	if c.refresh != nil {
		c.refresh.RequestRefresh()
	}
}

func prepareTerminalClose(snapshot herdr.Snapshot, target terminalActionTarget) (terminalCloseFacts, int, string) {
	exact, found := resolveExactTerminal(snapshot, target)
	if !found {
		return terminalCloseFacts{}, http.StatusNotFound, "not_found"
	}
	if !containsTerminalAction(exact.Projected.Actions, herdr.TerminalActionClose) {
		return terminalCloseFacts{}, http.StatusConflict, "not_applicable"
	}
	facts := terminalCloseFacts{
		Target: target, WorkspaceLabel: displayWorkspaceLabel(exact.Workspace.Label),
		TabLabel: displayTabLabel(exact.Tab.Label), TerminalTitle: exact.Projected.Title,
	}
	if exact.Projected.Agent != nil {
		agent := *exact.Projected.Agent
		facts.Agent = &agent
	}
	workspacePanes, tabPanes := 0, 0
	for _, pane := range snapshot.Panes {
		if pane.WorkspaceID == target.WorkspaceID {
			workspacePanes++
		}
		if pane.TabID == target.TabID {
			tabPanes++
		}
	}
	if workspacePanes == 1 {
		action, applicable := herdr.ResolveWorkspaceCloseAction(snapshot, target.WorkspaceID)
		if !applicable {
			return terminalCloseFacts{}, http.StatusConflict, "not_applicable"
		}
		workspaceFacts, status, reason := prepareFacts(snapshot, action, target.WorkspaceID)
		if reason != "" {
			return terminalCloseFacts{}, status, reason
		}
		facts.WorkspaceAction = action
		facts.WorkspaceExpected = &workspaceFacts
	} else {
		facts.ClosesTab = tabPanes == 1
	}
	return facts, http.StatusOK, ""
}

func resolveExactTerminal(snapshot herdr.Snapshot, target terminalActionTarget) (exactTerminalTarget, bool) {
	if target.WorkspaceID == "" || target.TabID == "" || target.PaneID == "" || target.TerminalID == "" ||
		!herdr.TerminalManagementAvailable(snapshot.Version) {
		return exactTerminalTarget{}, false
	}
	var result exactTerminalTarget
	workspaceMatches, tabMatches, paneMatches := 0, 0, 0
	for _, workspace := range snapshot.Workspaces {
		if workspace.WorkspaceID == target.WorkspaceID {
			result.Workspace = workspace
			workspaceMatches++
		}
	}
	for _, tab := range snapshot.Tabs {
		if tab.TabID == target.TabID && tab.WorkspaceID == target.WorkspaceID {
			result.Tab = tab
			tabMatches++
		}
	}
	for _, pane := range snapshot.Panes {
		if pane.PaneID == target.PaneID && pane.TerminalID == target.TerminalID &&
			pane.TabID == target.TabID && pane.WorkspaceID == target.WorkspaceID {
			result.Pane = pane
			paneMatches++
		}
	}
	if workspaceMatches != 1 || tabMatches != 1 || paneMatches != 1 {
		return exactTerminalTarget{}, false
	}
	home, err := herdr.Project(snapshot)
	if err != nil {
		return exactTerminalTarget{}, false
	}
	projected, found := findProjectedTerminal(home.Workspaces, target.PaneID)
	if !found || projected.TerminalID != target.TerminalID {
		return exactTerminalTarget{}, false
	}
	result.Projected = projected
	return result, true
}

func findProjectedTerminal(workspaces []herdr.Workspace, paneID string) (herdr.Terminal, bool) {
	for _, workspace := range workspaces {
		for _, tab := range workspace.Tabs {
			for _, terminal := range tab.Terminals {
				if terminal.PaneID == paneID {
					return terminal, true
				}
			}
		}
		if terminal, found := findProjectedTerminal(workspace.Worktrees, paneID); found {
			return terminal, true
		}
	}
	return herdr.Terminal{}, false
}

func containsTerminalAction(actions []herdr.TerminalAction, action herdr.TerminalAction) bool {
	for _, candidate := range actions {
		if candidate == action {
			return true
		}
	}
	return false
}

func splitConfirmed(before, after herdr.Snapshot, anchor exactTerminalTarget, created herdr.PaneInfo) bool {
	if _, err := herdr.Project(after); err != nil {
		return false
	}
	if created.PaneID == "" || created.PaneID == anchor.Pane.PaneID || created.TerminalID == "" ||
		created.WorkspaceID != anchor.Pane.WorkspaceID || created.TabID != anchor.Pane.TabID ||
		created.CWD != anchor.Pane.CWD || created.Label != nil ||
		after.FocusedWorkspaceID != before.FocusedWorkspaceID || after.FocusedTabID != before.FocusedTabID ||
		after.FocusedPaneID != before.FocusedPaneID {
		return false
	}
	_, existed := findPaneByIDs(before, created.PaneID, created.TerminalID)
	current, present := findPaneByIDs(after, created.PaneID, created.TerminalID)
	return !existed && present && current.WorkspaceID == created.WorkspaceID && current.TabID == created.TabID &&
		current.CWD == anchor.Pane.CWD && current.Label == nil
}

func renameConfirmed(after herdr.Snapshot, target exactTerminalTarget, renamed herdr.PaneInfo, label *string) bool {
	if _, err := herdr.Project(after); err != nil {
		return false
	}
	if renamed.PaneID != target.Pane.PaneID || renamed.TerminalID != target.Pane.TerminalID ||
		renamed.WorkspaceID != target.Pane.WorkspaceID || renamed.TabID != target.Pane.TabID || !sameLabel(renamed.Label, label) {
		return false
	}
	current, present := findPaneByIDs(after, target.Pane.PaneID, target.Pane.TerminalID)
	return present && current.WorkspaceID == target.Pane.WorkspaceID && current.TabID == target.Pane.TabID && sameLabel(current.Label, label)
}

func closeConfirmed(after herdr.Snapshot, facts terminalCloseFacts) bool {
	if _, err := herdr.Project(after); err != nil {
		return false
	}
	if facts.WorkspaceExpected != nil {
		for _, workspace := range after.Workspaces {
			for _, closedID := range facts.WorkspaceExpected.ScopeWorkspaceIDs {
				if workspace.WorkspaceID == closedID {
					return false
				}
			}
		}
		return true
	}
	for _, pane := range after.Panes {
		if pane.PaneID == facts.Target.PaneID || pane.TerminalID == facts.Target.TerminalID {
			return false
		}
	}
	workspacePresent := false
	for _, workspace := range after.Workspaces {
		workspacePresent = workspacePresent || workspace.WorkspaceID == facts.Target.WorkspaceID
	}
	if !workspacePresent {
		return false
	}
	if facts.ClosesTab {
		for _, tab := range after.Tabs {
			if tab.TabID == facts.Target.TabID {
				return false
			}
		}
	}
	return true
}

func findPaneByIDs(snapshot herdr.Snapshot, paneID, terminalID string) (herdr.PaneInfo, bool) {
	for _, pane := range snapshot.Panes {
		if pane.PaneID == paneID && pane.TerminalID == terminalID {
			return pane, true
		}
	}
	return herdr.PaneInfo{}, false
}

func sameLabel(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func decodeTerminalTarget(fields map[string]json.RawMessage) (terminalActionTarget, bool) {
	workspaceID, workspaceOK := decodeString(fields["workspace_id"])
	tabID, tabOK := decodeString(fields["tab_id"])
	paneID, paneOK := decodeString(fields["pane_id"])
	terminalID, terminalOK := decodeString(fields["terminal_id"])
	target := terminalActionTarget{WorkspaceID: workspaceID, TabID: tabID, PaneID: paneID, TerminalID: terminalID}
	return target, workspaceOK && tabOK && paneOK && terminalOK && workspaceID != "" && tabID != "" && paneID != "" && terminalID != ""
}

func decodeTerminalCloseFacts(raw json.RawMessage) (terminalCloseFacts, bool) {
	fields, ok := decodeObject(raw)
	if !ok || !keysMatch(
		fields,
		[]string{"target", "workspace_label", "tab_label", "terminal_title", "agent", "closes_tab", "workspace_action", "workspace_expected"},
		[]string{"target", "workspace_label", "tab_label", "terminal_title", "closes_tab"},
	) {
		return terminalCloseFacts{}, false
	}
	targetFields, ok := decodeObject(fields["target"])
	if !ok || !keysMatch(
		targetFields,
		[]string{"workspace_id", "tab_id", "pane_id", "terminal_id"},
		[]string{"workspace_id", "tab_id", "pane_id", "terminal_id"},
	) {
		return terminalCloseFacts{}, false
	}
	target, targetOK := decodeTerminalTarget(targetFields)
	workspaceLabel, workspaceOK := decodeString(fields["workspace_label"])
	tabLabel, tabOK := decodeString(fields["tab_label"])
	title, titleOK := decodeString(fields["terminal_title"])
	var closesTab bool
	closesOK := json.Unmarshal(fields["closes_tab"], &closesTab) == nil && string(fields["closes_tab"]) != "null"
	facts := terminalCloseFacts{
		Target: target, WorkspaceLabel: workspaceLabel, TabLabel: tabLabel, TerminalTitle: title, ClosesTab: closesTab,
	}
	if rawAgent, present := fields["agent"]; present {
		agentFields, valid := decodeObject(rawAgent)
		if !valid || !keysMatch(agentFields, []string{"kind", "name", "status"}, []string{"kind", "name", "status"}) ||
			json.Unmarshal(rawAgent, &facts.Agent) != nil || facts.Agent == nil || !facts.Agent.Status.Valid() {
			return terminalCloseFacts{}, false
		}
	}
	workspaceActionRaw, hasWorkspaceAction := fields["workspace_action"]
	workspaceExpectedRaw, hasWorkspaceExpected := fields["workspace_expected"]
	if hasWorkspaceAction != hasWorkspaceExpected || closesTab && hasWorkspaceAction {
		return terminalCloseFacts{}, false
	}
	if hasWorkspaceAction {
		actionValue, valid := decodeString(workspaceActionRaw)
		action := herdr.WorkspaceAction(actionValue)
		if !valid || (action != herdr.WorkspaceActionCloseWorkspace && action != herdr.WorkspaceActionCloseGroup) {
			return terminalCloseFacts{}, false
		}
		expected, valid := decodeConfirmationFacts(workspaceExpectedRaw, false)
		if !valid {
			return terminalCloseFacts{}, false
		}
		facts.WorkspaceAction = action
		facts.WorkspaceExpected = &expected
	}
	return facts, targetOK && workspaceOK && tabOK && titleOK && closesOK
}

func displayTabLabel(label string) string {
	if label == "" {
		return "Tab"
	}
	return label
}

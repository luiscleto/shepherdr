package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"

	"github.com/luiscleto/shepherdr/internal/access"
	"github.com/luiscleto/shepherdr/internal/herdr"
)

// Herdr Core offers Close terminal and Close space, both through terminal.kill.
// Each is prepared from a fresh complete read, confirmed, then checked against
// another fresh complete read before anything is killed. There is no retry.

type coreActionClient interface {
	Read(context.Context) (herdr.CoreRead, error)
	Kill(context.Context, string) (string, error)
}

type coreActionCoordinator struct {
	client     coreActionClient
	homeFolder string
	refresh    homeRefresher
	mutation   chan struct{}
}

func newCoreActionCoordinator(client coreActionClient, homeFolder string, refresh homeRefresher) *coreActionCoordinator {
	return &coreActionCoordinator{client: client, homeFolder: homeFolder, refresh: refresh, mutation: make(chan struct{}, 1)}
}

// coreView is one complete read and the Home it projects to.
type coreView struct {
	read herdr.CoreRead
	home herdr.Home
}

func (c *coreActionCoordinator) view(ctx context.Context) (coreView, error) {
	read, err := c.client.Read(ctx)
	if err != nil {
		return coreView{}, err
	}
	_, home, err := herdr.ProjectCore(read, c.homeFolder)
	if err != nil {
		return coreView{}, err
	}
	return coreView{read: read, home: home}, nil
}

func (c *coreActionCoordinator) prepare(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	if !requireObjectKeys(writer, fields, []string{"action", "workspace_id"}, []string{"action", "workspace_id"}) {
		return
	}
	action, actionOK := decodeString(fields["action"])
	workspaceID, workspaceOK := decodeString(fields["workspace_id"])
	if !actionOK || !workspaceOK || workspaceID == "" || action != string(herdr.WorkspaceActionCloseWorkspace) {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	current, ok := c.preparedView(writer, request)
	if !ok {
		return
	}
	facts, status, reason := coreSpaceFacts(current.home, workspaceID)
	if reason != "" {
		writeRefusal(writer, status, reason, "")
		return
	}
	writeActionJSON(writer, http.StatusOK, struct {
		Outcome     string            `json:"outcome"`
		Action      string            `json:"action"`
		WorkspaceID string            `json:"workspace_id"`
		Expected    confirmationFacts `json:"expected"`
	}{Outcome: "prepared", Action: action, WorkspaceID: workspaceID, Expected: facts})
}

func (c *coreActionCoordinator) run(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	action, actionOK := decodeString(fields["action"])
	if !actionOK || action != string(herdr.WorkspaceActionCloseWorkspace) {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if !requireObjectKeys(writer, fields, []string{"action", "workspace_id", "expected"}, []string{"action", "workspace_id", "expected"}) {
		return
	}
	workspaceID, ok := decodeString(fields["workspace_id"])
	expected, factsOK := decodeConfirmationFacts(fields["expected"], false)
	if !ok || workspaceID == "" || !factsOK {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if !c.acquire() {
		writeRefusal(writer, http.StatusConflict, "busy", "")
		return
	}
	defer c.release()

	current, err := c.view(context.Background())
	if err != nil {
		writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
		return
	}
	facts, status, reason := coreSpaceFacts(current.home, workspaceID)
	if reason != "" {
		writeRefusal(writer, status, reason, "")
		return
	}
	if !reflect.DeepEqual(facts, expected) {
		writeRefusal(writer, http.StatusConflict, "stale", "")
		return
	}
	c.closeSpace(writer, request, current, workspaceID)
}

func (c *coreActionCoordinator) prepareTerminal(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	keys := []string{"action", "workspace_id", "tab_id", "pane_id", "terminal_id"}
	if !requireObjectKeys(writer, fields, keys, keys) {
		return
	}
	action, actionOK := decodeString(fields["action"])
	target, targetOK := decodeTerminalTarget(fields)
	if !actionOK || action != string(herdr.TerminalActionClose) || !targetOK {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	current, ok := c.preparedView(writer, request)
	if !ok {
		return
	}
	facts, status, reason := coreTerminalFacts(current, target)
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

func (c *coreActionCoordinator) runTerminal(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	action, actionOK := decodeString(fields["action"])
	if !actionOK || action != string(herdr.TerminalActionClose) {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if !requireObjectKeys(writer, fields, []string{"action", "expected"}, []string{"action", "expected"}) {
		return
	}
	expected, ok := decodeTerminalCloseFacts(fields["expected"])
	if !ok {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if !c.acquire() {
		writeRefusal(writer, http.StatusConflict, "busy", "")
		return
	}
	defer c.release()

	current, err := c.view(context.Background())
	if err != nil {
		writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
		return
	}
	facts, status, reason := coreTerminalFacts(current, expected.Target)
	if reason != "" {
		writeRefusal(writer, status, reason, "")
		return
	}
	if !reflect.DeepEqual(facts, expected) {
		writeRefusal(writer, http.StatusConflict, "stale", "")
		return
	}
	// The space's last terminal: the person confirmed Close space.
	if facts.WorkspaceExpected != nil {
		c.closeSpace(writer, request, current, facts.Target.WorkspaceID)
		return
	}
	var state string
	err = withCommitAuthority(request, func() error {
		var killErr error
		state, killErr = c.client.Kill(context.Background(), facts.Target.PaneID)
		return killErr
	})
	if err != nil {
		c.finishError(writer, err)
		return
	}
	if state != "finished" {
		c.unknown(writer, http.StatusBadGateway)
		return
	}
	c.succeeded(writer)
}

// closeSpace kills every live terminal Home shows in the space, workers
// included, in Home order. A worker that already ended with its parent is
// skipped. It succeeds only when one complete read afterwards shows nothing
// live left in the space.
func (c *coreActionCoordinator) closeSpace(writer http.ResponseWriter, request *http.Request, current coreView, workspaceID string) {
	workspace, _ := findCoreWorkspace(current.home, workspaceID)
	killed, finished := false, true
	for _, tab := range workspace.Tabs {
		for _, terminal := range tab.Terminals {
			if killed {
				fresh, err := c.client.Read(context.Background())
				if err != nil {
					c.unknown(writer, http.StatusBadGateway)
					return
				}
				if !coreTerminalLive(fresh.Terminals, terminal.PaneID) {
					continue
				}
			}
			var state string
			err := withCommitAuthority(request, func() error {
				var killErr error
				state, killErr = c.client.Kill(context.Background(), terminal.PaneID)
				return killErr
			})
			if err != nil {
				if killed && !errors.Is(err, access.ErrUnauthorized) {
					c.unknown(writer, unknownStatus(err))
					return
				}
				if killed {
					c.refreshHome()
				}
				c.finishError(writer, err)
				return
			}
			killed = true
			finished = finished && state == "finished"
		}
	}
	after, err := c.view(context.Background())
	if _, present := findCoreWorkspace(after.home, workspaceID); err != nil || !finished || present {
		c.unknown(writer, http.StatusBadGateway)
		return
	}
	c.succeeded(writer)
}

// preparedView reads under the session's authority, as preparing writes facts
// the browser shows.
func (c *coreActionCoordinator) preparedView(writer http.ResponseWriter, request *http.Request) (coreView, bool) {
	var current coreView
	err := withCommitAuthority(request, func() error {
		var viewErr error
		current, viewErr = c.view(request.Context())
		return viewErr
	})
	if err != nil {
		if errors.Is(err, access.ErrUnauthorized) {
			writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
			return coreView{}, false
		}
		writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
		return coreView{}, false
	}
	return current, true
}

// finishError answers an error from the first kill, before any kill took effect.
func (c *coreActionCoordinator) finishError(writer http.ResponseWriter, err error) {
	if errors.Is(err, access.ErrUnauthorized) {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	var apiError *herdr.APIError
	if errors.As(err, &apiError) {
		if apiError.Code == "not_found" {
			writeRefusal(writer, http.StatusNotFound, "not_found", "")
			return
		}
		writeRefusal(writer, http.StatusUnprocessableEntity, "herdr_refused", apiError.Message)
		return
	}
	if herdr.MutationMayHaveRun(err) {
		c.unknown(writer, unknownStatus(err))
		return
	}
	writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
}

func unknownStatus(err error) int {
	if herdr.MutationTimedOut(err) {
		return http.StatusGatewayTimeout
	}
	return http.StatusBadGateway
}

func (c *coreActionCoordinator) succeeded(writer http.ResponseWriter) {
	c.refreshHome()
	writeActionJSON(writer, http.StatusOK, struct {
		Outcome string `json:"outcome"`
	}{Outcome: "succeeded"})
}

func (c *coreActionCoordinator) unknown(writer http.ResponseWriter, status int) {
	c.refreshHome()
	writeActionJSON(writer, status, struct {
		Outcome string `json:"outcome"`
	}{Outcome: "unknown"})
}

func (c *coreActionCoordinator) refreshHome() {
	if c.refresh != nil {
		c.refresh.RequestRefresh()
	}
}

func (c *coreActionCoordinator) acquire() bool {
	select {
	case c.mutation <- struct{}{}:
		return true
	default:
		return false
	}
}

func (c *coreActionCoordinator) release() { <-c.mutation }

// coreSpaceFacts describes closing a space Home shows. Other terminals is not
// a space and has no actions.
func coreSpaceFacts(home herdr.Home, workspaceID string) (confirmationFacts, int, string) {
	workspace, found := findCoreWorkspace(home, workspaceID)
	if !found {
		return confirmationFacts{}, http.StatusNotFound, "not_found"
	}
	if !slices.Contains(workspace.Actions, herdr.WorkspaceActionCloseWorkspace) {
		return confirmationFacts{}, http.StatusConflict, "not_applicable"
	}
	facts := confirmationFacts{WorkspaceLabel: displayWorkspaceLabel(workspace.Label), ScopeWorkspaceIDs: []string{workspaceID}}
	for _, tab := range workspace.Tabs {
		for _, terminal := range tab.Terminals {
			if terminal.Agent == nil {
				continue
			}
			facts.AgentTotal++
			switch terminal.Agent.Status {
			case herdr.StatusWorking:
				facts.InterruptionCounts.Working++
			case herdr.StatusBlocked:
				facts.InterruptionCounts.Blocked++
			case herdr.StatusError:
				facts.InterruptionCounts.Error++
			}
		}
	}
	return facts, http.StatusOK, ""
}

// coreTerminalFacts describes closing one exact terminal. Workers that die
// with it are counted. When nothing would remain in its space, the facts are
// the space's, as for the TUI's last pane.
func coreTerminalFacts(current coreView, target terminalActionTarget) (terminalCloseFacts, int, string) {
	workspace, found := findCoreWorkspace(current.home, target.WorkspaceID)
	var tab herdr.Tab
	var terminal herdr.Terminal
	terminalFound := false
	for _, candidateTab := range workspace.Tabs {
		if candidateTab.ID != target.TabID {
			continue
		}
		for _, candidate := range candidateTab.Terminals {
			if candidate.PaneID == target.PaneID && candidate.TerminalID == target.TerminalID {
				tab, terminal, terminalFound = candidateTab, candidate, true
			}
		}
	}
	if !found || !terminalFound {
		return terminalCloseFacts{}, http.StatusNotFound, "not_found"
	}
	if !containsTerminalAction(terminal.Actions, herdr.TerminalActionClose) {
		return terminalCloseFacts{}, http.StatusConflict, "not_applicable"
	}
	facts := terminalCloseFacts{
		Target: target, WorkspaceLabel: displayWorkspaceLabel(workspace.Label),
		TabLabel: displayTabLabel(tab.Label), TerminalTitle: terminal.Title,
	}
	if terminal.Agent != nil {
		agent := *terminal.Agent
		facts.Agent = &agent
	}
	ending := herdr.CoreEndsWith(current.read.Terminals, target.PaneID)
	for _, shown := range current.home.Workspaces {
		for _, shownTab := range shown.Tabs {
			for _, candidate := range shownTab.Terminals {
				if _, ends := ending[candidate.PaneID]; ends && candidate.PaneID != target.PaneID {
					facts.ClosesWorkers++
				}
			}
		}
	}
	if workspace.ID == herdr.OtherTerminalsID {
		return facts, http.StatusOK, ""
	}
	spaceLeft, tabLeft := 0, 0
	for _, spaceTab := range workspace.Tabs {
		for _, candidate := range spaceTab.Terminals {
			if _, ends := ending[candidate.PaneID]; ends {
				continue
			}
			spaceLeft++
			if spaceTab.ID == target.TabID {
				tabLeft++
			}
		}
	}
	if spaceLeft == 0 {
		spaceFacts, status, reason := coreSpaceFacts(current.home, workspace.ID)
		if reason != "" {
			return terminalCloseFacts{}, status, reason
		}
		facts.ClosesWorkers = 0
		facts.WorkspaceAction = herdr.WorkspaceActionCloseWorkspace
		facts.WorkspaceExpected = &spaceFacts
		return facts, http.StatusOK, ""
	}
	facts.ClosesTab = tabLeft == 0
	return facts, http.StatusOK, ""
}

func findCoreWorkspace(home herdr.Home, workspaceID string) (herdr.Workspace, bool) {
	for _, workspace := range home.Workspaces {
		if workspace.ID == workspaceID {
			return workspace, true
		}
	}
	return herdr.Workspace{}, false
}

func coreTerminalLive(terminals []herdr.CoreTerminal, address string) bool {
	for _, terminal := range terminals {
		if terminal.Address == address {
			return true
		}
	}
	return false
}

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/luiscleto/shepherdr/internal/access"
	"github.com/luiscleto/shepherdr/internal/herdr"
)

// Herdr Core offers Close terminal and Close space, both through terminal.kill.
// Each is prepared from a fresh complete read, confirmed, then checked against
// another fresh complete read before anything is killed. While the Herdr app
// window is open, it also offers New space, New terminal and Rename space,
// which the window does. There is no retry.

type coreActionClient interface {
	Read(context.Context) (herdr.CoreRead, error)
	Kill(context.Context, string) (string, error)
}

// appActionClient runs commands in the Herdr app window.
type appActionClient interface {
	WindowOpen(context.Context) bool
	Do(context.Context, string, map[string]any, any) error
}

// homeWatcher is the published core Home.
type homeWatcher interface {
	Subscribe() (<-chan herdr.State, func())
}

// splitWait bounds how long a split waits for Home to show the new terminal.
const splitWait = 5 * time.Second

type coreActionCoordinator struct {
	client     coreActionClient
	app        appActionClient
	published  homeWatcher
	homeFolder string
	homeDir    func() (string, error)
	refresh    homeRefresher
	mutation   chan struct{}
}

func newCoreActionCoordinator(client coreActionClient, homeFolder string, refresh homeRefresher) *coreActionCoordinator {
	return &coreActionCoordinator{
		client: client, homeFolder: homeFolder, homeDir: os.UserHomeDir, refresh: refresh, mutation: make(chan struct{}, 1),
	}
}

// coreView is one complete read and the Home it projects to.
type coreView struct {
	read herdr.CoreRead
	home herdr.Home
}

// view reads and projects Home; appWindowOpen projects the app window's
// actions too.
func (c *coreActionCoordinator) view(ctx context.Context, appWindowOpen bool) (coreView, error) {
	read, err := c.client.Read(ctx)
	if err != nil {
		return coreView{}, err
	}
	read.AppWindowOpen = appWindowOpen
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
	switch {
	case actionOK && action == "create_space":
		c.runCreateSpace(writer, request, fields)
		return
	case actionOK && action == string(herdr.WorkspaceActionRenameSpace):
		c.runRenameSpace(writer, request, fields)
		return
	case !actionOK || action != string(herdr.WorkspaceActionCloseWorkspace):
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

	current, err := c.view(context.Background(), false)
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
	if actionOK && action == string(herdr.TerminalActionSplit) {
		c.runSplitTerminal(writer, request, fields)
		return
	}
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

	current, err := c.view(context.Background(), false)
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

// closeSpace kills every live terminal Home shows in the space, in
// coreSpaceCloseOrder. A terminal no longer live is skipped. It succeeds only
// when one complete read afterwards shows nothing live left in the space.
func (c *coreActionCoordinator) closeSpace(writer http.ResponseWriter, request *http.Request, current coreView, workspaceID string) {
	workspace, _ := findCoreWorkspace(current.home, workspaceID)
	killed, finished := false, true
	for _, address := range coreSpaceCloseOrder(current.read.Terminals, workspace) {
		if killed {
			fresh, err := c.client.Read(context.Background())
			if err != nil {
				c.unknown(writer, http.StatusBadGateway)
				return
			}
			if !coreTerminalLive(fresh.Terminals, address) {
				continue
			}
		}
		var state string
		err := withCommitAuthority(request, func() error {
			var killErr error
			state, killErr = c.client.Kill(context.Background(), address)
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
	after, err := c.view(context.Background(), false)
	if _, present := findCoreWorkspace(after.home, workspaceID); err != nil || !finished || present {
		c.unknown(writer, http.StatusBadGateway)
		return
	}
	c.succeeded(writer)
}

// runCreateSpace makes a space in the app window for an existing folder, with
// the label as its name. The window shows the new space.
func (c *coreActionCoordinator) runCreateSpace(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	if !requireObjectKeys(writer, fields, []string{"action", "working_directory", "label"}, []string{"action", "working_directory"}) {
		return
	}
	workingDirectory, ok := decodeString(fields["working_directory"])
	if !ok || workingDirectory == "" {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	args := map[string]any{}
	if raw, present := fields["label"]; present {
		label, valid := decodeString(raw)
		if !valid {
			writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
			return
		}
		if strings.TrimSpace(label) != "" {
			args["name"] = label
		}
	}
	// The app does not expand "~", and a relative folder would be the app's.
	folder, err := expandTilde(workingDirectory, c.homeDir)
	if err != nil {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "Could not find the current user's home directory.")
		return
	}
	if !filepath.IsAbs(folder) {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	args["folder"] = folder
	if !c.startAppAction(writer) {
		return
	}
	defer c.release()
	var created struct {
		Space    string `json:"space"`
		Tab      string `json:"tab"`
		Terminal string `json:"terminal"`
	}
	if err := c.doApp(request, "space.new", args, &created); err != nil {
		c.finishAppError(writer, err)
		return
	}
	if created.Space == "" || created.Tab == "" || created.Terminal == "" {
		c.unknown(writer, http.StatusBadGateway)
		return
	}
	c.succeeded(writer)
}

// runRenameSpace names a space in the app window; an empty name returns it to
// the automatic name. The app refuses an id it no longer has.
func (c *coreActionCoordinator) runRenameSpace(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	keys := []string{"action", "workspace_id", "name"}
	if !requireObjectKeys(writer, fields, keys, keys) {
		return
	}
	workspaceID, workspaceOK := decodeString(fields["workspace_id"])
	name, nameOK := decodeString(fields["name"])
	if !workspaceOK || !nameOK || workspaceID == "" {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if workspaceID == herdr.OtherTerminalsID {
		writeRefusal(writer, http.StatusConflict, "not_applicable", "")
		return
	}
	if strings.TrimSpace(name) == "" {
		name = ""
	}
	if !c.startAppAction(writer) {
		return
	}
	defer c.release()
	var renamed struct {
		OK bool `json:"ok"`
	}
	if err := c.doApp(request, "space.rename", map[string]any{"id": workspaceID, "name": name}, &renamed); err != nil {
		c.finishAppError(writer, err)
		return
	}
	if !renamed.OK {
		c.unknown(writer, http.StatusBadGateway)
		return
	}
	c.succeeded(writer)
}

// runSplitTerminal splits the exact terminal's pane in the app window, then
// waits for the published Home to show the new terminal in the same tab so
// the browser can open it.
func (c *coreActionCoordinator) runSplitTerminal(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	keys := []string{"action", "workspace_id", "tab_id", "pane_id", "terminal_id", "direction"}
	if !requireObjectKeys(writer, fields, keys, keys) {
		return
	}
	target, targetOK := decodeTerminalTarget(fields)
	direction, directionOK := decodeString(fields["direction"])
	if !targetOK || !directionOK || (direction != string(herdr.SplitRight) && direction != string(herdr.SplitDown)) {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if !c.startAppAction(writer) {
		return
	}
	defer c.release()
	current, err := c.view(context.Background(), true)
	if err != nil {
		writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
		return
	}
	terminal, found := findCoreTerminal(current.home, target)
	if !found {
		writeRefusal(writer, http.StatusNotFound, "not_found", "")
		return
	}
	block, placed := herdr.CorePaneBlock(current.read, target.WorkspaceID, target.TabID, target.PaneID)
	if !containsTerminalAction(terminal.Actions, herdr.TerminalActionSplit) || !placed {
		writeRefusal(writer, http.StatusConflict, "not_applicable", "")
		return
	}
	// Subscribe first, so no Home published after the split is missed.
	var updates <-chan herdr.State
	if c.published != nil {
		var unsubscribe func()
		updates, unsubscribe = c.published.Subscribe()
		defer unsubscribe()
	}
	var created struct {
		Pane     string `json:"pane"`
		Terminal string `json:"terminal"`
	}
	if err := c.doApp(request, "pane.split", map[string]any{"direction": direction, "pane": block}, &created); err != nil {
		c.finishAppError(writer, err)
		return
	}
	if created.Pane == "" || created.Terminal == "" || updates == nil {
		c.unknown(writer, http.StatusBadGateway)
		return
	}
	c.refreshHome()
	timeout := time.NewTimer(splitWait)
	defer timeout.Stop()
	for {
		select {
		case state := <-updates:
			shown, ok := findCoreTerminal(state.Home, terminalActionTarget{WorkspaceID: target.WorkspaceID, TabID: target.TabID, PaneID: created.Terminal})
			if !ok || state.Connection != herdr.ConnectionLive {
				continue
			}
			writeActionJSON(writer, http.StatusOK, struct {
				Outcome  string               `json:"outcome"`
				Terminal terminalActionTarget `json:"terminal"`
			}{Outcome: "succeeded", Terminal: terminalActionTarget{
				WorkspaceID: target.WorkspaceID, TabID: target.TabID, PaneID: shown.PaneID, TerminalID: shown.TerminalID,
			}})
			return
		case <-timeout.C:
			c.unknown(writer, http.StatusGatewayTimeout)
			return
		}
	}
}

// startAppAction takes the one-action latch and checks the app window is
// open. When it is closed nothing is sent, and Home asks the window again.
// Without the app, core has no such action.
func (c *coreActionCoordinator) startAppAction(writer http.ResponseWriter) bool {
	if c.app == nil {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return false
	}
	if !c.acquire() {
		writeRefusal(writer, http.StatusConflict, "busy", "")
		return false
	}
	if !c.app.WindowOpen(context.Background()) {
		c.release()
		c.refreshHome()
		writeRefusal(writer, http.StatusServiceUnavailable, "app_closed", "")
		return false
	}
	return true
}

func (c *coreActionCoordinator) doApp(request *http.Request, command string, args map[string]any, result any) error {
	return withCommitAuthority(request, func() error {
		return c.app.Do(context.Background(), command, args, result)
	})
}

// finishAppError answers an app command that did not succeed.
func (c *coreActionCoordinator) finishAppError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, access.ErrUnauthorized):
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
	case errors.Is(err, herdr.ErrAppWindowClosed):
		c.refreshHome()
		writeRefusal(writer, http.StatusServiceUnavailable, "app_closed", "")
	default:
		var apiError *herdr.APIError
		if errors.As(err, &apiError) {
			writeRefusal(writer, http.StatusUnprocessableEntity, "herdr_refused", apiError.Message)
			return
		}
		if herdr.MutationMayHaveRun(err) {
			c.unknown(writer, unknownStatus(err))
			return
		}
		writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
	}
}

// findCoreTerminal finds the terminal at target's space, tab and address, and
// matches its terminal id when target names one.
func findCoreTerminal(home herdr.Home, target terminalActionTarget) (herdr.Terminal, bool) {
	workspace, _ := findCoreWorkspace(home, target.WorkspaceID)
	for _, tab := range workspace.Tabs {
		if tab.ID != target.TabID {
			continue
		}
		for _, terminal := range tab.Terminals {
			if terminal.PaneID == target.PaneID && (target.TerminalID == "" || terminal.TerminalID == target.TerminalID) {
				return terminal, true
			}
		}
	}
	return herdr.Terminal{}, false
}

// preparedView reads under the session's authority, as preparing writes facts
// the browser shows.
func (c *coreActionCoordinator) preparedView(writer http.ResponseWriter, request *http.Request) (coreView, bool) {
	var current coreView
	err := withCommitAuthority(request, func() error {
		var viewErr error
		current, viewErr = c.view(request.Context(), false)
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
	// What remains is what Home shows once the kill lands: a worker that
	// outlives its placed parent moves to Other terminals and keeps nothing here.
	after := current.read
	after.Terminals = slices.DeleteFunc(slices.Clone(current.read.Terminals), func(terminal herdr.CoreTerminal) bool {
		_, ends := ending[terminal.Address]
		return ends
	})
	_, afterHome, err := herdr.ProjectCore(after, "")
	if err != nil {
		return terminalCloseFacts{}, http.StatusServiceUnavailable, "herdr_unavailable"
	}
	remaining, spaceRemains := findCoreWorkspace(afterHome, workspace.ID)
	if !spaceRemains {
		spaceFacts, status, reason := coreSpaceFacts(current.home, workspace.ID)
		if reason != "" {
			return terminalCloseFacts{}, status, reason
		}
		facts.ClosesWorkers = 0
		facts.WorkspaceAction = herdr.WorkspaceActionCloseWorkspace
		facts.WorkspaceExpected = &spaceFacts
		return facts, http.StatusOK, ""
	}
	facts.ClosesTab = !slices.ContainsFunc(remaining.Tabs, func(tab herdr.Tab) bool { return tab.ID == target.TabID })
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

// coreSpaceCloseOrder lists the terminals Home shows in the space so that each
// ends after the workers it started, deepest first, and otherwise in Home
// order. Only terminals shown in this space count: a sub-agent the app placed
// elsewhere, or one under Other terminals, is never listed.
func coreSpaceCloseOrder(terminals []herdr.CoreTerminal, workspace herdr.Workspace) []string {
	parents := make(map[string]string, len(terminals))
	for _, terminal := range terminals {
		if terminal.Parent != nil {
			parents[terminal.Address] = *terminal.Parent
		}
	}
	var order []string
	shown := map[string]struct{}{}
	for _, tab := range workspace.Tabs {
		for _, terminal := range tab.Terminals {
			order = append(order, terminal.PaneID)
			shown[terminal.PaneID] = struct{}{}
		}
	}
	// A terminal's depth counts its ancestors shown in this space, so a worker
	// is always deeper than the terminal that started it.
	depth := make(map[string]int, len(order))
	for _, address := range order {
		seen := map[string]struct{}{address: {}}
		for parent, ok := parents[address]; ok; parent, ok = parents[parent] {
			if _, loop := seen[parent]; loop {
				break
			}
			seen[parent] = struct{}{}
			if _, inSpace := shown[parent]; inSpace {
				depth[address]++
			}
		}
	}
	slices.SortStableFunc(order, func(a, b string) int { return depth[b] - depth[a] })
	return order
}

func coreTerminalLive(terminals []herdr.CoreTerminal, address string) bool {
	for _, terminal := range terminals {
		if terminal.Address == address {
			return true
		}
	}
	return false
}

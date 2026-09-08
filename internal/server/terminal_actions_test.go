package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/luiscleto/shepherdr/internal/herdr"
)

func TestSplitTerminalUsesFreshExactPaneAndConfirmsNewNonfocusedTerminal(t *testing.T) {
	before := terminalActionSnapshot()
	after := terminalActionSnapshot()
	created := herdr.PaneInfo{
		CWD: "/fresh/anchor", PaneID: "pane-created", TabID: "tab-one", TerminalID: "terminal-created", WorkspaceID: "workspace",
	}
	after.Panes[0].CWD = "/fresh/anchor"
	after.Panes = append(after.Panes, created)
	snapshots := []herdr.Snapshot{before, after}
	var splitPane, splitCWD string
	var splitDirection herdr.SplitDirection
	client := &fakeTerminalActionClient{fakeWorkspaceActionClient: &fakeWorkspaceActionClient{
		snapshot: func(context.Context) (herdr.Snapshot, error) {
			value := snapshots[0]
			snapshots = snapshots[1:]
			return value, nil
		},
	}}
	client.split = func(_ context.Context, paneID, cwd string, direction herdr.SplitDirection) (herdr.PaneInfo, error) {
		splitPane, splitCWD, splitDirection = paneID, cwd, direction
		return created, nil
	}
	refresh := &countingHomeRefresher{}
	response := performActionRequest(t, actionHandler(client, refresh), "/api/terminal-actions", `{
		"action":"split_terminal","workspace_id":"workspace","tab_id":"tab-one",
		"pane_id":"pane-one","terminal_id":"terminal-one","direction":"right"
	}`)
	if response.Code != http.StatusOK {
		t.Fatalf("split status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Outcome  string               `json:"outcome"`
		Terminal terminalActionTarget `json:"terminal"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "succeeded" || result.Terminal != (terminalActionTarget{
		WorkspaceID: "workspace", TabID: "tab-one", PaneID: "pane-created", TerminalID: "terminal-created",
	}) {
		t.Fatalf("split result = %+v", result)
	}
	if splitPane != "pane-one" || splitCWD != "/fresh/anchor" || splitDirection != herdr.SplitRight {
		t.Fatalf("split call = pane %q cwd %q direction %q", splitPane, splitCWD, splitDirection)
	}
	if refresh.count.Load() != 1 || len(snapshots) != 0 {
		t.Fatalf("refreshes=%d remaining snapshots=%d", refresh.count.Load(), len(snapshots))
	}
}

func TestRenameTerminalBlankClearsManualPaneLabelAndConfirmsSnapshot(t *testing.T) {
	current := terminalActionSnapshot()
	manual := "Old name"
	current.Panes[0].Label = &manual
	var renamedPane string
	var renamedLabel *string
	client := &fakeTerminalActionClient{fakeWorkspaceActionClient: &fakeWorkspaceActionClient{
		snapshot: func(context.Context) (herdr.Snapshot, error) { return current, nil },
	}}
	client.rename = func(_ context.Context, paneID string, label *string) (herdr.PaneInfo, error) {
		renamedPane, renamedLabel = paneID, label
		current.Panes[0].Label = nil
		return current.Panes[0], nil
	}
	refresh := &countingHomeRefresher{}
	response := performActionRequest(t, actionHandler(client, refresh), "/api/terminal-actions", `{
		"action":"rename_terminal","workspace_id":"workspace","tab_id":"tab-one",
		"pane_id":"pane-one","terminal_id":"terminal-one","name":"   "
	}`)
	assertOutcome(t, response, http.StatusOK, "succeeded", "")
	if renamedPane != "pane-one" || renamedLabel != nil || refresh.count.Load() != 1 {
		t.Fatalf("rename pane=%q label=%v refreshes=%d", renamedPane, renamedLabel, refresh.count.Load())
	}
}

func TestCloseTerminalUsesPreparedExactImpactAndRefusesStaleFacts(t *testing.T) {
	current := terminalActionSnapshot()
	client := &fakeTerminalActionClient{fakeWorkspaceActionClient: &fakeWorkspaceActionClient{
		snapshot: func(context.Context) (herdr.Snapshot, error) { return current, nil },
	}}
	var closedPane string
	client.close = func(_ context.Context, paneID string) error {
		closedPane = paneID
		current.Panes = current.Panes[1:]
		current.Tabs = current.Tabs[1:]
		current.Workspaces[0].ActiveTabID = "tab-two"
		current.FocusedTabID = "tab-two"
		current.FocusedPaneID = "pane-two"
		return nil
	}
	refresh := &countingHomeRefresher{}
	handler := actionHandler(client, refresh)
	prepare := performActionRequest(t, handler, "/api/terminal-actions/prepare", `{
		"action":"close_terminal","workspace_id":"workspace","tab_id":"tab-one",
		"pane_id":"pane-one","terminal_id":"terminal-one"
	}`)
	if prepare.Code != http.StatusOK {
		t.Fatalf("prepare status=%d body=%s", prepare.Code, prepare.Body.String())
	}
	var prepared struct {
		Expected terminalCloseFacts `json:"expected"`
	}
	if err := json.Unmarshal(prepare.Body.Bytes(), &prepared); err != nil {
		t.Fatal(err)
	}
	if !prepared.Expected.ClosesTab || prepared.Expected.WorkspaceExpected != nil || prepared.Expected.TerminalTitle != "Anchor" {
		t.Fatalf("prepared terminal facts = %+v", prepared.Expected)
	}
	run, err := json.Marshal(map[string]any{"action": "close_terminal", "expected": prepared.Expected})
	if err != nil {
		t.Fatal(err)
	}
	changedLabel := "Changed after prepare"
	current.Panes[0].Label = &changedLabel
	stale := performActionRequest(t, handler, "/api/terminal-actions", string(run))
	assertOutcome(t, stale, http.StatusConflict, "refused", "stale")
	if closedPane != "" {
		t.Fatalf("stale close reached pane.close for %q", closedPane)
	}
	current.Panes[0].Label = nil
	succeeded := performActionRequest(t, handler, "/api/terminal-actions", string(run))
	assertOutcome(t, succeeded, http.StatusOK, "succeeded", "")
	if closedPane != "pane-one" || refresh.count.Load() != 1 {
		t.Fatalf("closed pane=%q refreshes=%d", closedPane, refresh.count.Load())
	}
}

func TestClosingFinalTerminalUsesExistingWorkspaceCloseScope(t *testing.T) {
	current := terminalActionSnapshot()
	current.Tabs = current.Tabs[:1]
	current.Panes = current.Panes[:1]
	var closedWorkspace string
	client := &fakeTerminalActionClient{fakeWorkspaceActionClient: &fakeWorkspaceActionClient{
		snapshot: func(context.Context) (herdr.Snapshot, error) { return current, nil },
		closeWorkspace: func(_ context.Context, workspaceID string, closeGroup bool) error {
			if closeGroup {
				t.Fatal("ordinary final close authorized group close")
			}
			closedWorkspace = workspaceID
			current.Workspaces, current.Tabs, current.Panes = nil, nil, nil
			current.FocusedWorkspaceID, current.FocusedTabID, current.FocusedPaneID = "", "", ""
			return nil
		},
	}}
	client.close = func(context.Context, string) error {
		t.Fatal("final terminal must not call pane.close")
		return nil
	}
	handler := actionHandler(client, &countingHomeRefresher{})
	prepare := performActionRequest(t, handler, "/api/terminal-actions/prepare", `{
		"action":"close_terminal","workspace_id":"workspace","tab_id":"tab-one",
		"pane_id":"pane-one","terminal_id":"terminal-one"
	}`)
	if prepare.Code != http.StatusOK {
		t.Fatalf("prepare status=%d body=%s", prepare.Code, prepare.Body.String())
	}
	var prepared struct {
		Expected terminalCloseFacts `json:"expected"`
	}
	if err := json.Unmarshal(prepare.Body.Bytes(), &prepared); err != nil {
		t.Fatal(err)
	}
	if prepared.Expected.WorkspaceAction != herdr.WorkspaceActionCloseWorkspace || prepared.Expected.WorkspaceExpected == nil {
		t.Fatalf("final terminal did not resolve workspace close: %+v", prepared.Expected)
	}
	body, err := json.Marshal(map[string]any{"action": "close_terminal", "expected": prepared.Expected})
	if err != nil {
		t.Fatal(err)
	}
	response := performActionRequest(t, handler, "/api/terminal-actions", string(body))
	assertOutcome(t, response, http.StatusOK, "succeeded", "")
	if closedWorkspace != "workspace" {
		t.Fatalf("workspace.close target=%q", closedWorkspace)
	}
}

func TestTerminalActionsStayHiddenAndUncallableOnUnconfirmedHerdrRelease(t *testing.T) {
	snapshot := terminalActionSnapshot()
	snapshot.Version = "0.8.1"
	home, err := herdr.Project(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, tab := range home.Workspaces[0].Tabs {
		for _, terminal := range tab.Terminals {
			if len(terminal.Actions) != 0 {
				t.Fatalf("0.8.1 terminal actions = %v", terminal.Actions)
			}
		}
	}
	client := &fakeTerminalActionClient{fakeWorkspaceActionClient: &fakeWorkspaceActionClient{
		snapshot: func(context.Context) (herdr.Snapshot, error) { return snapshot, nil },
	}}
	response := performActionRequest(t, actionHandler(client, nil), "/api/terminal-actions", `{
		"action":"rename_terminal","workspace_id":"workspace","tab_id":"tab-one",
		"pane_id":"pane-one","terminal_id":"terminal-one","name":"No"
	}`)
	if response.Code != http.StatusNotFound {
		t.Fatalf("0.8.1 action status=%d body=%s", response.Code, response.Body.String())
	}
}

type fakeTerminalActionClient struct {
	*fakeWorkspaceActionClient
	split  func(context.Context, string, string, herdr.SplitDirection) (herdr.PaneInfo, error)
	rename func(context.Context, string, *string) (herdr.PaneInfo, error)
	close  func(context.Context, string) error
}

func (c *fakeTerminalActionClient) SplitTerminal(
	ctx context.Context,
	paneID, cwd string,
	direction herdr.SplitDirection,
) (herdr.PaneInfo, error) {
	return c.split(ctx, paneID, cwd, direction)
}

func (c *fakeTerminalActionClient) RenameTerminal(ctx context.Context, paneID string, label *string) (herdr.PaneInfo, error) {
	return c.rename(ctx, paneID, label)
}

func (c *fakeTerminalActionClient) CloseTerminal(ctx context.Context, paneID string) error {
	return c.close(ctx, paneID)
}

func terminalActionSnapshot() herdr.Snapshot {
	return herdr.Snapshot{
		Version: "0.8.2", FocusedWorkspaceID: "workspace", FocusedTabID: "tab-one", FocusedPaneID: "pane-one",
		Workspaces: []herdr.WorkspaceInfo{{ActiveTabID: "tab-one", WorkspaceID: "workspace", Label: "Repository"}},
		Tabs: []herdr.TabInfo{
			{TabID: "tab-one", WorkspaceID: "workspace", Label: "First"},
			{TabID: "tab-two", WorkspaceID: "workspace", Label: "Second"},
		},
		Panes: []herdr.PaneInfo{
			{CWD: "/fresh/anchor", Focused: true, PaneID: "pane-one", TabID: "tab-one", TerminalID: "terminal-one", TerminalTitleStripped: "Anchor", WorkspaceID: "workspace"},
			{CWD: "/work/other", PaneID: "pane-two", TabID: "tab-two", TerminalID: "terminal-two", TerminalTitleStripped: "Other", WorkspaceID: "workspace"},
		},
	}
}

func TestTerminalCloseFactsRoundTripUsesPrimitiveExactValues(t *testing.T) {
	facts, status, reason := prepareTerminalClose(terminalActionSnapshot(), terminalActionTarget{
		WorkspaceID: "workspace", TabID: "tab-one", PaneID: "pane-one", TerminalID: "terminal-one",
	})
	if status != http.StatusOK || reason != "" {
		t.Fatalf("prepare status=%d reason=%q", status, reason)
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	decoded, ok := decodeTerminalCloseFacts(raw)
	if !ok || !reflect.DeepEqual(decoded, facts) {
		t.Fatalf("round trip decoded=%+v ok=%t want=%+v", decoded, ok, facts)
	}
}

// Both entry points must recheck the confirmed scope before forwarding group intent.
func TestCloseIntentAcrossHomeAndFinalTerminal(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		for _, group := range []bool{false, true} {
			t.Run(fmt.Sprintf("terminal=%t/group=%t", terminal, group), func(t *testing.T) {
				base := terminalActionSnapshot()
				base.Version, base.Protocol = "0.9.0", 22
				base.Tabs, base.Panes = base.Tabs[:1], base.Panes[:1]
				if group {
					base.Workspaces[0].Worktree = &herdr.WorktreeInfo{Valid: true, RepoKey: "repo", CheckoutPath: "/repo"}
					base.Workspaces = append(base.Workspaces, herdr.WorkspaceInfo{WorkspaceID: "child", Label: "Child", Worktree: &herdr.WorktreeInfo{Valid: true, RepoKey: "repo", CheckoutPath: "/repo/child", IsLinkedWorktree: true}})
				}
				current := base
				calls := 0
				client := &fakeTerminalActionClient{fakeWorkspaceActionClient: &fakeWorkspaceActionClient{
					snapshot: func(context.Context) (herdr.Snapshot, error) { return current, nil },
					closeWorkspace: func(_ context.Context, id string, closeGroup bool) error {
						calls++
						if id != "workspace" || closeGroup != group {
							t.Fatalf("close target=%q group=%t", id, closeGroup)
						}
						current.Workspaces, current.Tabs, current.Panes = nil, nil, nil
						current.FocusedWorkspaceID, current.FocusedTabID, current.FocusedPaneID = "", "", ""
						return nil
					},
				}, close: func(context.Context, string) error { t.Fatal("unexpected pane.close"); return nil }}
				handler := actionHandler(client, &countingHomeRefresher{})
				path, action := "/api/workspace-actions", "close_workspace"
				if group {
					action = "close_group"
				}
				if terminal {
					path, action = "/api/terminal-actions", "close_terminal"
				}
				body := map[string]any{"action": action, "workspace_id": "workspace"}
				if terminal {
					body["tab_id"], body["pane_id"], body["terminal_id"] = "tab-one", "pane-one", "terminal-one"
				}
				raw, _ := json.Marshal(body)
				prepare := performActionRequest(t, handler, path+"/prepare", string(raw))
				assertOutcome(t, prepare, http.StatusOK, "prepared", "")
				var prepared struct {
					Expected json.RawMessage `json:"expected"`
				}
				if err := json.Unmarshal(prepare.Body.Bytes(), &prepared); err != nil {
					t.Fatal(err)
				}
				body = map[string]any{"action": action, "expected": prepared.Expected}
				if !terminal {
					body["workspace_id"] = "workspace"
				}
				raw, _ = json.Marshal(body)
				// A changed group member or ordinary workspace label invalidates confirmation.
				current.Workspaces = append([]herdr.WorkspaceInfo(nil), base.Workspaces...)
				if group {
					current.Workspaces = append(current.Workspaces, herdr.WorkspaceInfo{WorkspaceID: "new-child", Worktree: &herdr.WorktreeInfo{Valid: true, RepoKey: "repo", CheckoutPath: "/repo/new", IsLinkedWorktree: true}})
				} else {
					current.Workspaces[0].Label = "Changed"
				}
				assertOutcome(t, performActionRequest(t, handler, path, string(raw)), http.StatusConflict, "refused", "stale")
				if calls != 0 {
					t.Fatalf("stale close made %d calls", calls)
				}
				current = base
				assertOutcome(t, performActionRequest(t, handler, path, string(raw)), http.StatusOK, "succeeded", "")
				if calls != 1 {
					t.Fatalf("confirmed close made %d calls", calls)
				}
			})
		}
	}
}

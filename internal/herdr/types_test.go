package herdr

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestProjectUsesCanonicalPanesAndExactAgentDecoration(t *testing.T) {
	name := "worker"
	kind := "codex"
	snapshot := Snapshot{
		Protocol: Protocol,
		Workspaces: []WorkspaceInfo{
			{ActiveTabID: "w2:t1", WorkspaceID: "w2", Label: "Second", Number: 2},
			{ActiveTabID: "w1:t1", WorkspaceID: "w1", Label: "First", Number: 1},
		},
		Tabs: []TabInfo{
			{TabID: "w2:t1", WorkspaceID: "w2", Label: "1", Number: 1},
			{TabID: "w1:t1", WorkspaceID: "w1", Label: "1", Number: 1},
		},
		Panes: []PaneInfo{
			{PaneID: "w2:p2", TabID: "w2:t1", TerminalID: "term-2", TerminalTitleStripped: "Shell", WorkspaceID: "w2"},
			{PaneID: "w1:p1", TabID: "w1:t1", TerminalID: "term-1", TerminalTitleStripped: "Worker", WorkspaceID: "w1"},
			{PaneID: "w2:p1", TabID: "w2:t1", TerminalID: "term-3", TerminalTitleStripped: "Blocked", WorkspaceID: "w2"},
		},
		Agents: []AgentInfo{
			{Agent: &kind, Name: &name, AgentStatus: StatusWorking, PaneID: "w1:p1", TabID: "w1:t1", TerminalID: "term-1", WorkspaceID: "w1"},
			{Agent: &kind, AgentStatus: StatusBlocked, PaneID: "w2:p1", TabID: "w2:t1", TerminalID: "term-3", WorkspaceID: "w2"},
		},
	}

	home, err := Project(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if home.WorkingCount != 1 || home.BlockedCount != 1 {
		t.Fatalf("counts = working %d blocked %d, want 1 and 1", home.WorkingCount, home.BlockedCount)
	}
	if got := home.Workspaces[0].ID; got != "w2" {
		t.Fatalf("first workspace = %q, want Herdr's first workspace w2", got)
	}
	terminals := projectedTerminals(home)
	if len(terminals) != len(snapshot.Panes) {
		t.Fatalf("projected %d terminals from %d canonical panes", len(terminals), len(snapshot.Panes))
	}
	ordinary := findProjectedTerminal(home, "w2:p2")
	if ordinary == nil || ordinary.Agent != nil {
		t.Fatalf("ordinary canonical pane was not preserved without agent decoration: %+v", ordinary)
	}
	blocked := findProjectedTerminal(home, "w2:p1")
	if blocked == nil || blocked.Agent == nil || blocked.Agent.Status != StatusBlocked {
		t.Fatalf("exact agent decoration was not preserved: %+v", blocked)
	}
}

func TestProjectGroupsOnlyExactWorktreeProvenanceAtTheParentPlace(t *testing.T) {
	kind := "codex"
	statusByWorkspace := map[string]Status{
		"child-before": StatusBlocked,
		"flat":         StatusUnknown,
		"parent":       StatusWorking,
		"child-after":  StatusDone,
		"malformed":    StatusIdle,
	}
	workspaceSources := []WorkspaceInfo{
		{WorkspaceID: "child-before", Label: "Before", Worktree: worktree("repo", true)},
		{WorkspaceID: "flat", Label: "Flat"},
		{WorkspaceID: "parent", Label: "Parent", Worktree: worktree("repo", false)},
		{WorkspaceID: "child-after", Label: "After", Worktree: worktree("repo", true)},
		{WorkspaceID: "malformed", Label: "Malformed", Worktree: &WorktreeInfo{RepoKey: "repo", IsLinkedWorktree: true}},
	}
	snapshot := Snapshot{Workspaces: workspaceSources}
	for index := range workspaceSources {
		workspaceID := workspaceSources[index].WorkspaceID
		tabID := workspaceID + ":tab"
		paneID := workspaceID + ":pane"
		terminalID := workspaceID + ":terminal"
		snapshot.Workspaces[index].ActiveTabID = tabID
		snapshot.Tabs = append(snapshot.Tabs, TabInfo{TabID: tabID, WorkspaceID: workspaceID})
		snapshot.Panes = append(snapshot.Panes, PaneInfo{PaneID: paneID, TabID: tabID, TerminalID: terminalID, WorkspaceID: workspaceID})
		snapshot.Agents = append(snapshot.Agents, AgentInfo{
			Agent: &kind, AgentStatus: statusByWorkspace[workspaceID], PaneID: paneID, TabID: tabID,
			TerminalID: terminalID, WorkspaceID: workspaceID,
		})
	}

	home, err := Project(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := workspaceIDs(home.Workspaces), []string{"flat", "parent", "malformed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("top-level workspaces = %v, want %v", got, want)
	}
	parent := home.Workspaces[1]
	if got, want := workspaceIDs(parent.Worktrees), []string{"child-before", "child-after"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("nested workspaces = %v, want Herdr order %v", got, want)
	}
	if parent.AgentCounts == nil || *parent.AgentCounts != (AgentCounts{Working: 1}) {
		t.Fatalf("workspace agent counts = %+v", parent.AgentCounts)
	}
	if parent.GroupAgentCounts == nil || *parent.GroupAgentCounts != (AgentCounts{Working: 1, Blocked: 1, Done: 1}) {
		t.Fatalf("group agent counts = %+v", parent.GroupAgentCounts)
	}
	if len(projectedTerminals(home)) != len(snapshot.Panes) {
		t.Fatalf("projected %d terminals, want %d exactly once", len(projectedTerminals(home)), len(snapshot.Panes))
	}
}

func TestProjectLeavesAmbiguousAndLinkedOnlyProvenanceFlat(t *testing.T) {
	snapshot := Snapshot{Workspaces: []WorkspaceInfo{
		{WorkspaceID: "ordinary-one", Worktree: worktree("ambiguous", false)},
		{WorkspaceID: "ordinary-two", Worktree: worktree("ambiguous", false)},
		{WorkspaceID: "ambiguous-child", Worktree: worktree("ambiguous", true)},
		{WorkspaceID: "linked-only", Worktree: worktree("linked", true)},
	}}
	home, err := Project(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := workspaceIDs(home.Workspaces), []string{"ordinary-one", "ordinary-two", "ambiguous-child", "linked-only"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("flat workspaces = %v, want %v", got, want)
	}
	for _, workspace := range home.Workspaces {
		if len(workspace.Worktrees) != 0 || workspace.AgentCounts != nil {
			t.Fatalf("workspace %q was unexpectedly grouped", workspace.ID)
		}
	}
}

func TestMalformedWorktreeProvenanceStaysFlatWithoutRejectingHome(t *testing.T) {
	var snapshot Snapshot
	err := json.Unmarshal([]byte(`{
		"workspaces": [
			{"workspace_id":"ordinary","worktree":{"repo_key":"repo","is_linked_worktree":false}},
			{"workspace_id":"missing-flag","worktree":{"repo_key":"repo"}},
			{"workspace_id":"wrong-type","worktree":{"repo_key":"repo","is_linked_worktree":"yes"}}
		]
	}`), &snapshot)
	if err != nil {
		t.Fatal(err)
	}
	home, err := Project(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := workspaceIDs(home.Workspaces), []string{"ordinary", "missing-flag", "wrong-type"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("workspaces = %v, want malformed provenance flat in %v", got, want)
	}
}

func TestProjectUsesCompleteUnzoomedLayoutOnlyForOrder(t *testing.T) {
	base := layoutSnapshot()
	home, err := Project(base)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := projectedPaneIDs(home), []string{"w1:p2", "w1:p1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("layout order = %v, want %v", got, want)
	}

	tests := map[string]func(*Snapshot){
		"absent":    func(snapshot *Snapshot) { snapshot.Layouts = nil },
		"partial":   func(snapshot *Snapshot) { snapshot.Layouts[0].Panes = snapshot.Layouts[0].Panes[:1] },
		"duplicate": func(snapshot *Snapshot) { snapshot.Layouts[0].Panes[1].PaneID = snapshot.Layouts[0].Panes[0].PaneID },
		"zoomed":    func(snapshot *Snapshot) { snapshot.Layouts[0].Zoomed = true },
		"ambiguous": func(snapshot *Snapshot) { snapshot.Layouts = append(snapshot.Layouts, snapshot.Layouts[0]) },
		"unusable":  func(snapshot *Snapshot) { snapshot.Layouts[0].Panes[0].Rect.Width = 0 },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			snapshot := layoutSnapshot()
			change(&snapshot)
			home, err := Project(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := projectedPaneIDs(home), []string{"w1:p1", "w1:p2"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("fallback order = %v, want every canonical pane in %v", got, want)
			}
		})
	}
}

func TestProjectAssignsOnlyContextualCollisionTitles(t *testing.T) {
	snapshot := Snapshot{
		Workspaces: []WorkspaceInfo{
			{ActiveTabID: "w1:t1", WorkspaceID: "w1", Label: "Same", Number: 1},
			{ActiveTabID: "w2:t1", WorkspaceID: "w2", Label: "Same", Number: 2},
			{ActiveTabID: "w3:t1", WorkspaceID: "w3", Label: "Multi", Number: 3},
		},
		Tabs: []TabInfo{
			{TabID: "w1:t1", WorkspaceID: "w1", Label: "1", Number: 1},
			{TabID: "w2:t1", WorkspaceID: "w2", Label: "1", Number: 1},
			{TabID: "w3:t1", WorkspaceID: "w3", Label: "First", Number: 1},
			{TabID: "w3:t2", WorkspaceID: "w3", Label: "Second", Number: 2},
		},
		Panes: []PaneInfo{
			{PaneID: "w1:p1", TabID: "w1:t1", TerminalID: "term-1", TerminalTitleStripped: "Ignored", WorkspaceID: "w1"},
			{PaneID: "w2:p1", TabID: "w2:t1", TerminalID: "term-2", TerminalTitleStripped: "Ignored", WorkspaceID: "w2"},
			{PaneID: "w3:p1", TabID: "w3:t1", TerminalID: "term-3", TerminalTitleStripped: "Build", WorkspaceID: "w3"},
			{PaneID: "w3:p2", TabID: "w3:t1", TerminalID: "term-4", TerminalTitleStripped: "Build", WorkspaceID: "w3"},
			{PaneID: "w3:p3", TabID: "w3:t2", TerminalID: "term-5", TerminalTitleStripped: "Build", WorkspaceID: "w3"},
		},
	}
	home, err := Project(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"w1:p1": "Same",
		"w2:p1": "Same",
		"w3:p1": "Build 1",
		"w3:p2": "Build 2",
		"w3:p3": "Build",
	}
	for paneID, title := range want {
		if terminal := findProjectedTerminal(home, paneID); terminal == nil || terminal.Title != title {
			t.Errorf("pane %s title = %+v, want %q", paneID, terminal, title)
		}
	}
	if !home.Workspaces[2].Tabs[0].Current || home.Workspaces[2].Tabs[1].Current {
		t.Fatalf("current tab did not follow active_tab_id: %+v", home.Workspaces[2].Tabs)
	}
}

func TestProjectUsesManualAgentAndAutomaticTerminalNamesWithoutRenumberingManualLabels(t *testing.T) {
	manual := "Build"
	agentKind := "codex"
	agentName := "Planner"
	snapshot := Snapshot{
		Version:    "0.8.2",
		Workspaces: []WorkspaceInfo{{ActiveTabID: "tab", WorkspaceID: "workspace", Label: "Repository"}},
		Tabs:       []TabInfo{{TabID: "tab", WorkspaceID: "workspace"}},
		Panes: []PaneInfo{
			{CWD: "/work", Label: &manual, PaneID: "manual", TabID: "tab", TerminalID: "terminal-manual", TerminalTitleStripped: "Ignored", WorkspaceID: "workspace"},
			{CWD: "/work", PaneID: "agent", TabID: "tab", TerminalID: "terminal-agent", TerminalTitleStripped: "Ignored", WorkspaceID: "workspace"},
			{CWD: "/work", PaneID: "automatic-one", TabID: "tab", TerminalID: "terminal-one", TerminalTitleStripped: "Build", WorkspaceID: "workspace"},
			{CWD: "/work", PaneID: "automatic-two", TabID: "tab", TerminalID: "terminal-two", TerminalTitleStripped: "Build", WorkspaceID: "workspace"},
			{CWD: "/work", PaneID: "fallback", TabID: "tab", TerminalID: "terminal-fallback", WorkspaceID: "workspace"},
		},
		Agents: []AgentInfo{{
			Agent: &agentKind, Name: &agentName, AgentStatus: StatusWorking, PaneID: "agent", TabID: "tab",
			TerminalID: "terminal-agent", WorkspaceID: "workspace",
		}},
	}

	home, err := Project(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"manual":        "Build",
		"agent":         "Planner",
		"automatic-one": "Build 1",
		"automatic-two": "Build 2",
		"fallback":      "Terminal",
	}
	for paneID, title := range want {
		terminal := findProjectedTerminal(home, paneID)
		if terminal == nil || terminal.Title != title {
			t.Errorf("pane %s title = %+v, want %q", paneID, terminal, title)
		}
	}
	terminal := findProjectedTerminal(home, "manual")
	if terminal == nil || terminal.ManualName == nil || *terminal.ManualName != manual {
		t.Fatalf("manual pane label was not preserved: %+v", terminal)
	}
	if got, want := terminal.Actions, []TerminalAction{TerminalActionSplit, TerminalActionRename, TerminalActionClose}; !reflect.DeepEqual(got, want) {
		t.Fatalf("terminal actions = %v, want %v", got, want)
	}
}

func TestProjectAvoidsSecondAutomaticNameCollisionsWithinTab(t *testing.T) {
	snapshot := Snapshot{
		Version:    "0.8.2",
		Workspaces: []WorkspaceInfo{{ActiveTabID: "tab", WorkspaceID: "workspace", Label: "Repository"}},
		Tabs:       []TabInfo{{TabID: "tab", WorkspaceID: "workspace"}},
		Panes: []PaneInfo{
			{CWD: "/work", PaneID: "build-one", TabID: "tab", TerminalID: "terminal-one", TerminalTitleStripped: "Build", WorkspaceID: "workspace"},
			{CWD: "/work", PaneID: "build-two", TabID: "tab", TerminalID: "terminal-two", TerminalTitleStripped: "Build", WorkspaceID: "workspace"},
			{CWD: "/work", PaneID: "build-numbered", TabID: "tab", TerminalID: "terminal-numbered", TerminalTitleStripped: "Build 1", WorkspaceID: "workspace"},
		},
	}

	home, err := Project(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, 3)
	for _, paneID := range []string{"build-one", "build-two", "build-numbered"} {
		terminal := findProjectedTerminal(home, paneID)
		if terminal == nil {
			t.Fatalf("pane %s was not projected", paneID)
		}
		got = append(got, terminal.Title)
	}
	want := []string{"Build 2", "Build 3", "Build 1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("terminal titles = %v, want %v", got, want)
	}
}

func TestProjectDoesNotOfferSplitWithoutAFreshPaneWorkingDirectory(t *testing.T) {
	snapshot := Snapshot{
		Version:    "0.8.2",
		Workspaces: []WorkspaceInfo{{ActiveTabID: "tab", WorkspaceID: "workspace"}},
		Tabs:       []TabInfo{{TabID: "tab", WorkspaceID: "workspace"}},
		Panes:      []PaneInfo{{PaneID: "pane", TabID: "tab", TerminalID: "terminal", WorkspaceID: "workspace"}},
	}
	home, err := Project(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	terminal := findProjectedTerminal(home, "pane")
	if terminal == nil {
		t.Fatal("terminal was not projected")
	}
	if got, want := terminal.Actions, []TerminalAction{TerminalActionRename, TerminalActionClose}; !reflect.DeepEqual(got, want) {
		t.Fatalf("actions without cwd = %v, want %v", got, want)
	}
}

func TestProjectExcludesAgentSessionRevisionAndTerminalTitleChurn(t *testing.T) {
	name := "Stable worker"
	kind := "codex"
	snapshot := layoutSnapshot()
	snapshot.Agents = []AgentInfo{{
		Agent: &kind, Name: &name, AgentStatus: StatusWorking, PaneID: "w1:p1", Revision: 1, StateChangeSeq: 2,
		TabID: "w1:t1", TerminalID: "term-1", WorkspaceID: "w1",
		AgentSession: &AgentSession{Agent: "codex", Kind: "id", Source: "one", Value: "one"},
	}}
	first, err := Project(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Agents[0].Revision = 999
	snapshot.Agents[0].StateChangeSeq = 999
	snapshot.Agents[0].AgentSession = &AgentSession{Agent: "codex", Kind: "id", Source: "two", Value: "two"}
	snapshot.Panes[0].TerminalTitleStripped = "rapidly changing terminal title"
	second, err := Project(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("non-semantic agent churn changed Home:\nfirst=%+v\nsecond=%+v", first, second)
	}
}

func TestProjectRejectsBrokenCanonicalJoinsAndDuplicates(t *testing.T) {
	kind := "codex"
	base := func() Snapshot {
		return Snapshot{
			Workspaces: []WorkspaceInfo{{ActiveTabID: "w1:t1", WorkspaceID: "w1", Label: "One"}},
			Tabs:       []TabInfo{{TabID: "w1:t1", WorkspaceID: "w1", Label: "1"}},
			Panes:      []PaneInfo{{PaneID: "w1:p1", TabID: "w1:t1", TerminalID: "term-1", WorkspaceID: "w1"}},
			Agents:     []AgentInfo{{Agent: &kind, AgentStatus: StatusIdle, PaneID: "w1:p1", TabID: "w1:t1", TerminalID: "term-1", WorkspaceID: "w1"}},
		}
	}
	tests := map[string]func(*Snapshot){
		"duplicate workspace": func(snapshot *Snapshot) { snapshot.Workspaces = append(snapshot.Workspaces, snapshot.Workspaces[0]) },
		"duplicate tab":       func(snapshot *Snapshot) { snapshot.Tabs = append(snapshot.Tabs, snapshot.Tabs[0]) },
		"duplicate pane":      func(snapshot *Snapshot) { snapshot.Panes = append(snapshot.Panes, snapshot.Panes[0]) },
		"duplicate terminal": func(snapshot *Snapshot) {
			snapshot.Panes = append(snapshot.Panes, PaneInfo{PaneID: "w1:p2", TabID: "w1:t1", TerminalID: "term-1", WorkspaceID: "w1"})
		},
		"mismatched pane tab":       func(snapshot *Snapshot) { snapshot.Panes[0].TabID = "missing" },
		"mismatched active tab":     func(snapshot *Snapshot) { snapshot.Workspaces[0].ActiveTabID = "missing" },
		"mismatched agent tab":      func(snapshot *Snapshot) { snapshot.Agents[0].TabID = "missing" },
		"mismatched agent terminal": func(snapshot *Snapshot) { snapshot.Agents[0].TerminalID = "replacement" },
		"duplicate agent":           func(snapshot *Snapshot) { snapshot.Agents = append(snapshot.Agents, snapshot.Agents[0]) },
		"unknown focus":             func(snapshot *Snapshot) { snapshot.FocusedPaneID = "missing" },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			snapshot := base()
			change(&snapshot)
			_, err := Project(snapshot)
			if _, ok := err.(*IncoherentSnapshotError); !ok {
				t.Fatalf("error = %T %v, want IncoherentSnapshotError", err, err)
			}
		})
	}
}

func TestProjectRejectsUnknownAgentStatus(t *testing.T) {
	snapshot := layoutSnapshot()
	snapshot.Agents = []AgentInfo{{AgentStatus: "waiting", PaneID: "w1:p1", TabID: "w1:t1", TerminalID: "term-1", WorkspaceID: "w1"}}
	_, err := Project(snapshot)
	if _, ok := err.(*InvalidStatusError); !ok {
		t.Fatalf("error = %T %v, want InvalidStatusError", err, err)
	}
}

func layoutSnapshot() Snapshot {
	return Snapshot{
		Workspaces: []WorkspaceInfo{{ActiveTabID: "w1:t1", WorkspaceID: "w1", Label: "One"}},
		Tabs:       []TabInfo{{TabID: "w1:t1", WorkspaceID: "w1", Label: "1"}},
		Panes: []PaneInfo{
			{PaneID: "w1:p1", TabID: "w1:t1", TerminalID: "term-1", TerminalTitleStripped: "One", WorkspaceID: "w1"},
			{PaneID: "w1:p2", TabID: "w1:t1", TerminalID: "term-2", TerminalTitleStripped: "Two", WorkspaceID: "w1"},
		},
		Layouts: []LayoutInfo{{
			WorkspaceID: "w1", TabID: "w1:t1", Panes: []LayoutPane{
				{PaneID: "w1:p1", Rect: Rectangle{X: 20, Y: 1, Width: 10, Height: 10}},
				{PaneID: "w1:p2", Rect: Rectangle{X: 1, Y: 1, Width: 10, Height: 10}},
			},
		}},
	}
}

func projectedTerminals(home Home) []Terminal {
	var terminals []Terminal
	var appendWorkspace func(Workspace)
	appendWorkspace = func(workspace Workspace) {
		for _, tab := range workspace.Tabs {
			terminals = append(terminals, tab.Terminals...)
		}
		for _, worktree := range workspace.Worktrees {
			appendWorkspace(worktree)
		}
	}
	for _, workspace := range home.Workspaces {
		appendWorkspace(workspace)
	}
	return terminals
}

func workspaceIDs(workspaces []Workspace) []string {
	ids := make([]string, 0, len(workspaces))
	for _, workspace := range workspaces {
		ids = append(ids, workspace.ID)
	}
	return ids
}

func worktree(repoKey string, linked bool) *WorktreeInfo {
	return &WorktreeInfo{RepoKey: repoKey, IsLinkedWorktree: linked, Valid: true}
}

func projectedPaneIDs(home Home) []string {
	terminals := projectedTerminals(home)
	result := make([]string, 0, len(terminals))
	for _, terminal := range terminals {
		result = append(result, terminal.PaneID)
	}
	return result
}

func findProjectedTerminal(home Home, paneID string) *Terminal {
	for _, terminal := range projectedTerminals(home) {
		if terminal.PaneID == paneID {
			copy := terminal
			return &copy
		}
	}
	return nil
}

func TestProjectTerminalActionsAcrossSupportedVersions(t *testing.T) {
	for _, version := range []string{"0.9.0", "0.9.1", "0.9.1+build.1", "0.9.2", "0.9.1-rc.1", "unknown"} {
		t.Run(version, func(t *testing.T) {
			snapshot := Snapshot{
				Version: version, Protocol: 22,
				Workspaces: []WorkspaceInfo{{ActiveTabID: "tab", WorkspaceID: "workspace"}},
				Tabs:       []TabInfo{{TabID: "tab", WorkspaceID: "workspace"}},
				Panes:      []PaneInfo{{CWD: "/work", PaneID: "pane", TabID: "tab", TerminalID: "terminal", WorkspaceID: "workspace"}},
			}
			home, err := Project(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			terminal := findProjectedTerminal(home, "pane")
			if terminal == nil {
				t.Fatal("terminal was not projected")
			}
			want := []TerminalAction{}
			if version == "0.9.0" || version == "0.9.1" || version == "0.9.1+build.1" {
				want = []TerminalAction{TerminalActionSplit, TerminalActionRename, TerminalActionClose}
			}
			if !reflect.DeepEqual(terminal.Actions, want) {
				t.Fatalf("actions = %v, want %v", terminal.Actions, want)
			}
		})
	}
}

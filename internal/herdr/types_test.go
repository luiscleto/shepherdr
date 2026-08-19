package herdr

import (
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
	if got := home.Workspaces[0].ID; got != "w1" {
		t.Fatalf("first workspace = %q, want w1", got)
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
		"w1:p1": "Same 1",
		"w2:p1": "Same 2",
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
	for _, workspace := range home.Workspaces {
		for _, tab := range workspace.Tabs {
			terminals = append(terminals, tab.Terminals...)
		}
	}
	return terminals
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

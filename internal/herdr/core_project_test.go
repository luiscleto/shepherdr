package herdr

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The fixtures are complete reads captured from herdr-core 0.1.15 driven by
// the Herdr app 0.1.10 (paths and host names anonymized).
func loadCoreRead(t *testing.T, name string) CoreRead {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var captured struct {
		Hello struct {
			Core    struct{ ID string } `json:"core"`
			Version string              `json:"version"`
		} `json:"hello"`
		List struct {
			Terminals []CoreTerminal `json:"terminals"`
		} `json:"terminal.list"`
		Document struct {
			Value json.RawMessage `json:"value"`
		} `json:"doc.get"`
	}
	if err := json.Unmarshal(data, &captured); err != nil {
		t.Fatal(err)
	}
	return CoreRead{CoreID: captured.Hello.Core.ID, Version: captured.Hello.Version, Terminals: captured.List.Terminals, Document: captured.Document.Value}
}

func homeRows(home Home) []string {
	rows := []string{}
	for _, workspace := range home.Workspaces {
		for _, tab := range workspace.Tabs {
			for _, terminal := range tab.Terminals {
				row := fmt.Sprintf("%s | %s | %s %s | %s", workspace.Label, tab.Label, terminal.PaneID, terminal.TerminalID, terminal.Title)
				if terminal.Agent != nil {
					row += fmt.Sprintf(" | %s %s %s", terminal.Agent.Kind, terminal.Agent.Name, terminal.Agent.Status)
				}
				rows = append(rows, row)
			}
		}
	}
	return rows
}

func TestProjectCoreMatchesTheAppAndPlacesEveryTerminalOnce(t *testing.T) {
	read := loadCoreRead(t, "core-read-app.json")
	snapshot, home, err := ProjectCore(read, "/home/user")
	if err != nil {
		t.Fatal(err)
	}
	id := read.CoreID + ":"
	want := []string{
		// Named space and tab; a split in tree order; the worker started from
		// t3 right after it; an unnamed second tab named by its position.
		"alpha | main | t3 " + id + "t3 | user@host:/tmp/w1/work/repo 1",
		"alpha | main | t9 " + id + "t9 | worker1",
		"alpha | main | t4 " + id + "t4 | user@host:/tmp/w1/work/repo 2",
		"alpha | 2 | t5 " + id + "t5 | user@host:/tmp/w1/work/repo",
		// An unnamed space in a git subfolder takes the git top from "where";
		// one in home is "~". A lone terminal takes its space's name.
		"repo | 1 | t6 " + id + "t6 | repo",
		"~ | 1 | t7 " + id + "t7 | ~",
		// A terminal started outside the app.
		"Other terminals | Other terminals | t8 " + id + "t8 | outside",
	}
	if got := homeRows(home); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if home.Workspaces[0].Number != 1 || home.Workspaces[2].Number != 3 || home.Workspaces[3].ID != OtherTerminalsID {
		t.Fatalf("space order = %+v", home.Workspaces)
	}
	for _, workspace := range home.Workspaces {
		// A space can be closed; Other terminals is not a space.
		wantActions := []WorkspaceAction{WorkspaceActionCloseWorkspace}
		if workspace.ID == OtherTerminalsID {
			wantActions = []WorkspaceAction{}
		}
		if !reflect.DeepEqual(workspace.Actions, wantActions) || workspace.Worktrees != nil || workspace.CheckoutPath != "" {
			t.Fatalf("core space has other actions or worktrees: %+v", workspace)
		}
		for _, tab := range workspace.Tabs {
			if tab.Current {
				t.Fatalf("core tab claims app focus: %+v", tab)
			}
			for _, terminal := range tab.Terminals {
				// Every terminal can be closed, including under Other terminals.
				if !reflect.DeepEqual(terminal.Actions, []TerminalAction{TerminalActionClose}) || terminal.ManualName != nil {
					t.Fatalf("core terminal has other actions or a manual name: %+v", terminal)
				}
			}
		}
	}
	// Space notices come from the app's spaces, never from Other terminals.
	spaces := []string{}
	for _, workspace := range snapshot.Workspaces {
		spaces = append(spaces, workspace.Label)
	}
	if !reflect.DeepEqual(spaces, []string{"alpha", "repo", "~"}) || len(snapshot.Panes) != 7 || snapshot.Version != "0.1.15" {
		t.Fatalf("snapshot spaces %v, %d panes, version %q", spaces, len(snapshot.Panes), snapshot.Version)
	}
	// Notices may name a tab, so the snapshot keeps only a name the person
	// gave it: never the number Home shows, nor Other terminals.
	tabNames := []string{}
	for _, tab := range snapshot.Tabs {
		tabNames = append(tabNames, tab.Label)
	}
	if !reflect.DeepEqual(tabNames, []string{"main", "", "", "", ""}) || home.Workspaces[0].Tabs[1].Label != "2" {
		t.Fatalf("snapshot tab names %q, Home's second tab %q", tabNames, home.Workspaces[0].Tabs[1].Label)
	}
}

func TestProjectCoreKeepsHostileNamesAsData(t *testing.T) {
	read := loadCoreRead(t, "core-read-hostile.json")
	_, home, err := ProjectCore(read, "/home/user")
	if err != nil {
		t.Fatal(err)
	}
	first := home.Workspaces[0]
	if first.Label != "<img src=x onerror=alert(1)>" || first.Tabs[1].Label != `"><script>alert(2)</script>` {
		t.Fatalf("hostile names changed: %q / %q", first.Label, first.Tabs[1].Label)
	}
	// A worker started while the app was closed has no "where" label and
	// still follows its parent; status from a program's own report shows.
	if got := homeRows(Home{Workspaces: home.Workspaces[:1]})[:2]; got[0] != "<img src=x onerror=alert(1)> | main | t3 "+read.CoreID+":t3 | demo-agent | demo-agent demo-agent working" ||
		!strings.HasPrefix(got[1], "<img src=x onerror=alert(1)> | main | t11 ") {
		t.Fatalf("rows = %q", got)
	}
	if home.WorkingCount != 1 || home.Workspaces[0].AgentCounts == nil || home.Workspaces[0].AgentCounts.Working != 1 {
		t.Fatalf("counts = %+v / %+v", home, home.Workspaces[0].AgentCounts)
	}
	other := home.Workspaces[len(home.Workspaces)-1]
	if other.ID != OtherTerminalsID || len(other.Tabs[0].Terminals) != 2 || other.Tabs[0].Terminals[0].Title != "outside2" {
		t.Fatalf("Other terminals = %+v", other)
	}
}

// statusFromEvent reads a real status event, as core sends it on a change.
func statusFromEvent(t *testing.T, state string) *CoreStatus {
	t.Helper()
	data, err := os.ReadFile("testdata/core-events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var message struct {
			Params struct {
				Type string `json:"type"`
				Data struct {
					Status *CoreStatus `json:"status"`
				} `json:"data"`
			} `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			t.Fatal(err)
		}
		if message.Params.Type == "status" && message.Params.Data.Status != nil && message.Params.Data.Status.State == state {
			return message.Params.Data.Status
		}
	}
	t.Fatalf("no captured %s status event", state)
	return nil
}

func TestProjectCoreCountsBlockedAndErrorAgents(t *testing.T) {
	read := loadCoreRead(t, "core-read-app.json")
	for index := range read.Terminals {
		switch read.Terminals[index].Address {
		case "t4":
			read.Terminals[index].Status = statusFromEvent(t, "blocked")
			read.Terminals[index].Title = "Greeting question"
		case "t9":
			// A worker reports like any agent.
			read.Terminals[index].Status = statusFromEvent(t, "error")
			read.Terminals[index].Title = ""
		}
	}
	snapshot, home, err := ProjectCore(read, "/home/user")
	if err != nil {
		t.Fatal(err)
	}
	if home.BlockedCount != 1 || home.ErrorCount != 1 || home.WorkingCount != 0 {
		t.Fatalf("home counts = %d blocked, %d error, %d working", home.BlockedCount, home.ErrorCount, home.WorkingCount)
	}
	if counts := home.Workspaces[0].AgentCounts; counts == nil || *counts != (AgentCounts{Blocked: 1, Error: 1}) {
		t.Fatalf("space counts = %+v", counts)
	}
	rows := homeRows(home)
	if rows[1] != "alpha | main | t9 "+read.CoreID+":t9 | demo-agent | demo-agent demo-agent error" ||
		rows[2] != "alpha | main | t4 "+read.CoreID+":t4 | claude-code | claude-code claude-code blocked" {
		t.Fatalf("rows = %q", rows)
	}
	if len(snapshot.Agents) != 2 || snapshot.Agents[0].AgentStatus != StatusError || snapshot.Agents[0].WorkspaceID != home.Workspaces[0].ID {
		t.Fatalf("agents = %+v", snapshot.Agents)
	}
}

func TestProjectCoreRestartedTerminalKeepsItsRowAndLosesItsStatus(t *testing.T) {
	read := loadCoreRead(t, "core-read-app.json")
	read.Terminals[2].Status = statusFromEvent(t, "error")
	_, before, err := ProjectCore(read, "/home/user")
	if err != nil {
		t.Fatal(err)
	}
	// After lost then restarted, core keeps the address and labels; the new
	// shell has not reported, so its status is null.
	read.Terminals[2].Status = nil
	_, after, err := ProjectCore(read, "/home/user")
	if err != nil {
		t.Fatal(err)
	}
	beforeTerminal, afterTerminal := before.Workspaces[0].Tabs[1].Terminals[0], after.Workspaces[0].Tabs[1].Terminals[0]
	if beforeTerminal.PaneID != "t5" || beforeTerminal.PaneID != afterTerminal.PaneID || beforeTerminal.TerminalID != afterTerminal.TerminalID ||
		beforeTerminal.Agent == nil || afterTerminal.Agent != nil || after.ErrorCount != 0 {
		t.Fatalf("restart changed the row: %+v -> %+v", beforeTerminal, afterTerminal)
	}
}

func TestProjectCoreDocumentEdges(t *testing.T) {
	read := loadCoreRead(t, "core-read-app.json")
	project := func(document string, change func(*CoreRead)) (Home, error) {
		candidate := read
		candidate.Terminals = append([]CoreTerminal(nil), read.Terminals...)
		candidate.Document = json.RawMessage(document)
		if change != nil {
			change(&candidate)
		}
		_, home, err := ProjectCore(candidate, "/home/user")
		return home, err
	}

	// No document: the app adds every labelled terminal when it opens (rule
	// 1b), and the rest are Other terminals.
	for _, document := range []string{"null", ""} {
		home, err := project(document, nil)
		if err != nil || len(home.Workspaces) != 4 || home.Workspaces[3].ID != OtherTerminalsID || len(homeRows(home)) != 7 {
			t.Fatalf("document %q: %q, %v", document, homeRows(home), err)
		}
	}
	unlabelled := func(candidate *CoreRead) {
		for index, terminal := range candidate.Terminals {
			candidate.Terminals[index].Labels = map[string]string{"where": terminal.Labels["where"]}
		}
	}
	home, err := project("null", unlabelled)
	if err != nil || len(home.Workspaces) != 1 || home.Workspaces[0].ID != OtherTerminalsID || len(home.Workspaces[0].Tabs[0].Terminals) != 7 {
		t.Fatalf("unlabelled: %q, %v", homeRows(home), err)
	}
	for _, document := range []string{`[]`, `{}`, `{"spaces":{}}`, `{"spaces":[1]}`, `{"spaces":[{"tabs":[]}]}`, `{"spaces":[{"id":"sp1"}]}`, `{"spaces":[{"id":"sp1","tabs":[{"name":"x"}]}]}`} {
		if _, err := project(document, nil); !errors.Is(err, ErrUnreadableAppLayout) {
			t.Fatalf("document %s: err = %v", document, err)
		}
	}
	// Unknown keys and node types are ignored; the terminal they hold still
	// appears once, under Other terminals when it has no app labels. A dead
	// block and an empty tab or space are left out.
	home, err = project(`{"spaces":[{"id":"sp1","name":"","folder":"/srv/api","extra":1,"tabs":[
		{"id":"tb1","name":"","root":{"type":"grid","children":[{"type":"block","content":{"terminal":"t3"}}]}},
		{"id":"tb2","name":"","root":{"type":"column","children":[{"type":"block","content":{"terminal":"t99"}},{"type":"block","content":{"mod":"x"}},{"type":"stack","children":[{"type":"block","content":{"terminal":"t4"}}]}]}}]},
		{"id":"sp2","name":"empty","tabs":[{"id":"tb3","name":"","root":{"type":"block","content":{"terminal":"t98"}}}]}]}`, unlabelled)
	if err != nil {
		t.Fatal(err)
	}
	if len(home.Workspaces) != 2 || home.Workspaces[0].Label != "alpha" || home.Workspaces[0].Tabs[0].Label != "2" ||
		home.Workspaces[1].ID != OtherTerminalsID || len(home.Workspaces[1].Tabs[0].Terminals) != 6 {
		t.Fatalf("tolerant layout = %q", homeRows(home))
	}
	// The space label falls back to the folder when the first live terminal
	// has no "where" label.
	home, err = project(`{"spaces":[{"id":"sp1","name":"","folder":"/srv/api","tabs":[{"id":"tb1","name":"","root":{"type":"block","content":{"terminal":"t8"}}}]}]}`, nil)
	if err != nil || home.Workspaces[0].Label != "api" {
		t.Fatalf("folder label = %+v, %v", home.Workspaces, err)
	}
	if _, err := project(`{"spaces":[{"id":"sp1","tabs":[{"id":"tb1","root":{"type":"row","children":[{"type":"block","content":{"terminal":"t3"}},{"type":"block","content":{"terminal":"t3"}}]}}]}]}`, nil); err == nil {
		t.Fatal("a terminal placed twice was accepted")
	}
	if _, err := project(`{"spaces":[{"id":"sp1","tabs":[]},{"id":"sp1","tabs":[]}]}`, nil); err == nil {
		t.Fatal("a duplicated space id was accepted")
	}
	// Only this machine's terminals are shown.
	home, err = project("null", func(candidate *CoreRead) {
		unlabelled(candidate)
		candidate.Terminals = append(candidate.Terminals, CoreTerminal{Address: "vps/t3", Title: "remote"})
	})
	if err != nil || len(home.Workspaces[0].Tabs[0].Terminals) != 7 {
		t.Fatalf("remote terminal shown: %q, %v", homeRows(home), err)
	}
	// Any status word other than Herdr Core's five fails the read.
	for _, word := range []string{"unknown", "Blocked", "waiting"} {
		_, err := project("null", func(candidate *CoreRead) {
			candidate.Terminals[0].Status = &CoreStatus{App: "x", State: word}
		})
		var invalid *InvalidStatusError
		if !errors.As(err, &invalid) {
			t.Fatalf("status %q: err = %v", word, err)
		}
	}
}

func TestProjectCoreNestedWorkersFollowTheirTopParentInStartOrder(t *testing.T) {
	read := loadCoreRead(t, "core-read-app.json")
	parent, child := "t9", "t3"
	var started uint64
	for _, terminal := range read.Terminals {
		if terminal.Address == "t9" {
			started = terminal.CreatedMS
		}
	}
	read.Terminals = append(read.Terminals,
		CoreTerminal{Address: "t21", CreatedMS: started + 30, Parent: &parent, Title: "grandchild"},
		CoreTerminal{Address: "t20", CreatedMS: started - 20, Parent: &child, Title: "earlier"},
	)
	_, home, err := ProjectCore(read, "/home/user")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, terminal := range home.Workspaces[0].Tabs[0].Terminals {
		got = append(got, terminal.PaneID)
	}
	if want := []string{"t3", "t20", "t9", "t21", "t4"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestCoreTerminalNamesFollowTheAgentThenTheTerminal(t *testing.T) {
	name := func(value string) *string { return &value }
	for _, test := range []struct {
		label     string
		source    CoreTerminal
		agentName string
		title     string
	}{
		// The live title changes while the agent works; the name doesn't.
		{label: "agent title", source: CoreTerminal{Name: name("build"), Title: "⠹ Run shell commands", Status: &CoreStatus{App: "claude", State: "idle"}}, agentName: "claude", title: "claude"},
		{label: "agent app", source: CoreTerminal{Name: name("build"), Status: &CoreStatus{App: "claude", State: "idle"}}, agentName: "claude", title: "claude"},
		{label: "agent without a name", source: CoreTerminal{Name: name("build"), Status: &CoreStatus{State: "idle"}}, agentName: "", title: "build"},
		{label: "core name", source: CoreTerminal{Name: name("build"), Title: "zsh"}, title: "build"},
		{label: "terminal title", source: CoreTerminal{Title: "zsh"}, title: "zsh"},
		{label: "nothing", source: CoreTerminal{}, title: "Terminal"},
	} {
		t.Run(test.label, func(t *testing.T) {
			test.source.Address = "t1"
			terminal := coreTerminal("core", test.source)
			agentName := ""
			if terminal.Agent != nil {
				agentName = terminal.Agent.Name
			}
			if (terminal.Agent != nil) != (test.source.Status != nil) || agentName != test.agentName || terminal.Title != test.title {
				t.Fatalf("agent = %v %q, title = %q; want %q, %q", terminal.Agent != nil, agentName, terminal.Title, test.agentName, test.title)
			}
		})
	}
}

func TestCoreEndsWithFollowsWorkersThatDieWithTheirParent(t *testing.T) {
	parent, child := "t1", "t2"
	terminals := []CoreTerminal{
		{Address: "t1"},
		{Address: "t2", Parent: &parent, DieWithParent: true},
		{Address: "t3", Parent: &child, DieWithParent: true},
		// Started by t1 but outlives it.
		{Address: "t4", Parent: &parent},
		{Address: "t5"},
	}
	got := CoreEndsWith(terminals, "t1")
	want := map[string]struct{}{"t1": {}, "t2": {}, "t3": {}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ending = %v", got)
	}
	if got := CoreEndsWith(terminals, "t9"); len(got) != 0 {
		t.Fatalf("a terminal that isn't live ends %v", got)
	}
}

func TestProjectCoreOffersTheAppWindowActionsOnlyWhileItIsOpen(t *testing.T) {
	read := loadCoreRead(t, "core-read-app.json")
	read.AppWindow = AppWindowOpen
	_, home, err := ProjectCore(read, "/home/user")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(home.Actions, []HomeAction{HomeActionCreateSpace}) {
		t.Fatalf("home actions = %v", home.Actions)
	}
	got := []string{}
	for _, workspace := range home.Workspaces {
		name := "-"
		if workspace.ManualName != nil {
			name = *workspace.ManualName
		}
		got = append(got, fmt.Sprintf("%s %s %v", workspace.Label, name, workspace.Actions))
		for _, tab := range workspace.Tabs {
			for _, terminal := range tab.Terminals {
				got = append(got, fmt.Sprintf("  %s %v", terminal.PaneID, terminal.Actions))
			}
		}
	}
	want := []string{
		// Only the named space has a manual name. A worker has no pane, so it
		// can't be split; neither can a terminal under Other terminals.
		"alpha alpha [new_tab rename_space rename_tab close_workspace]",
		"  t3 [split_terminal close_terminal]",
		"  t9 [close_terminal]",
		"  t4 [split_terminal close_terminal]",
		"  t5 [split_terminal close_terminal]",
		"repo - [new_tab rename_space close_workspace]",
		"  t6 [split_terminal close_terminal]",
		"~ - [new_tab rename_space close_workspace]",
		"  t7 [split_terminal close_terminal]",
		"Other terminals - []",
		"  t8 [close_terminal]",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("actions:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Rename tab starts from the name the person gave the tab, if any.
	if tabs := home.Workspaces[0].Tabs; tabs[0].ManualName == nil || *tabs[0].ManualName != "main" || tabs[1].ManualName != nil || tabs[1].Label != "2" {
		t.Fatalf("alpha tabs = %+v", tabs)
	}

	// Without the app, or before its window answers, there are no app actions.
	read.AppWindow = AppWindowUnpaired
	_, unpaired, err := ProjectCore(read, "/home/user")
	if err != nil || unpaired.Actions != nil || unpaired.AppWindow != "" || !reflect.DeepEqual(unpaired.Workspaces[0].Actions, []WorkspaceAction{WorkspaceActionCloseWorkspace}) ||
		!reflect.DeepEqual(unpaired.Workspaces[0].Tabs[0].Terminals[0].Actions, []TerminalAction{TerminalActionClose}) {
		t.Fatalf("no app: %v %+v", err, unpaired)
	}

	// While the window is closed, New space, New terminal and New tab start in
	// core; Rename space needs the window.
	read.AppWindow = AppWindowClosed
	_, closed, err := ProjectCore(read, "/home/user")
	if err != nil || !reflect.DeepEqual(closed.Actions, []HomeAction{HomeActionCreateSpace}) || closed.AppWindow != "closed" {
		t.Fatalf("closed window: %v %v %q", err, closed.Actions, closed.AppWindow)
	}
	if !reflect.DeepEqual(closed.Workspaces[0].Actions, []WorkspaceAction{WorkspaceActionNewTab, WorkspaceActionCloseWorkspace}) ||
		!reflect.DeepEqual(closed.Workspaces[0].Tabs[0].Terminals[0].Actions, []TerminalAction{TerminalActionSplit, TerminalActionClose}) ||
		!reflect.DeepEqual(closed.Workspaces[0].Tabs[0].Terminals[1].Actions, []TerminalAction{TerminalActionClose}) {
		t.Fatalf("closed window actions: %+v", closed.Workspaces[0])
	}
}

func TestCorePaneBlockNamesTheAppPaneOfAPlacedTerminal(t *testing.T) {
	read := loadCoreRead(t, "core-read-app.json")
	const alpha, main, second = "sp4d15c560d9928dee4d7842adfe949cb6", "tba5ed0454645e0dac10d29a4b83da7df7", "tb7c3a8ed2988a3811e31c1b41c81d4ba0"
	if block, ok := CorePaneBlock(read, alpha, main, "t4"); !ok || block != "p20daff1ca339f27e18f44423f49f4290" {
		t.Fatalf("t4 block = %q %v", block, ok)
	}
	// The worker has no pane, and a terminal is found only in its own tab.
	for _, address := range []string{"t9", "t5", "t8"} {
		if block, ok := CorePaneBlock(read, alpha, main, address); ok {
			t.Fatalf("%s block = %q", address, block)
		}
	}
	if _, ok := CorePaneBlock(read, alpha, second, "t5"); !ok {
		t.Fatal("t5 has no block in its own tab")
	}
}

// loadCoreReads reads complete reads captured one per line, in the same shape
// as loadCoreRead.
func loadCoreReads(t *testing.T, name string) []CoreRead {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	reads := []CoreRead{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var captured struct {
			Hello struct {
				Core    struct{ ID string } `json:"core"`
				Version string              `json:"version"`
			} `json:"hello"`
			List struct {
				Terminals []CoreTerminal `json:"terminals"`
			} `json:"terminal.list"`
			Document struct {
				Value json.RawMessage `json:"value"`
			} `json:"doc.get"`
		}
		if err := json.Unmarshal([]byte(line), &captured); err != nil {
			t.Fatal(err)
		}
		reads = append(reads, CoreRead{CoreID: captured.Hello.Core.ID, Version: captured.Hello.Version, Terminals: captured.List.Terminals, Document: captured.Document.Value})
	}
	return reads
}

func spaceRows(home Home) []string {
	rows := []string{}
	for _, workspace := range home.Workspaces {
		tabs := []string{}
		for _, tab := range workspace.Tabs {
			addresses := []string{}
			for _, terminal := range tab.Terminals {
				addresses = append(addresses, terminal.PaneID)
			}
			tabs = append(tabs, tab.Label+": "+strings.Join(addresses, " "))
		}
		rows = append(rows, workspace.Label+" | "+strings.Join(tabs, " | "))
	}
	return rows
}

// The reads are every distinct complete read from herdr-core 0.1.15 while the
// Herdr app 0.1.10 window was closed and then opened: a New space in
// repo/sub (t19), a New terminal in alpha / main (t1b), and a worker started
// from t19 (t1c).
func TestProjectCoreShowsWaitingTerminalsWhereTheAppPutsThem(t *testing.T) {
	reads := loadCoreReads(t, "core-reads-closed-reopen.jsonl")
	const newSpace = "sp98f4d85f85743896bd294723bd30de49"
	before := []string{
		"alpha | main: t1 t18 t2 t6 t17 | third: t14 | 3: t15",
		"plain | 1: t4", "plain | 1: t10", "repo | 1: t8 t9", "plain | 1: t11", "plain | 1: t16",
	}
	// After the runs, while the window is closed: the new terminal ends its
	// tab, and the new space comes last, named after its folder, with its
	// worker. On open the app writes "where" first, so the name becomes the git
	// top's once, and then it places both. Nothing else moves.
	want := [][]string{
		before,
		{"alpha | main: t1 t18 t2 t6 t17 t1b | third: t14 | 3: t15", "plain | 1: t4", "plain | 1: t10", "repo | 1: t8 t9", "plain | 1: t11", "plain | 1: t16", "sub | 1: t19 t1c"},
		{"alpha | main: t1 t18 t2 t6 t17 t1b | third: t14 | 3: t15", "plain | 1: t4", "plain | 1: t10", "repo | 1: t8 t9", "plain | 1: t11", "plain | 1: t16", "sub | 1: t19 t1c"},
		{"alpha | main: t1 t18 t2 t6 t17 t1b | third: t14 | 3: t15", "plain | 1: t4", "plain | 1: t10", "repo | 1: t8 t9", "plain | 1: t11", "plain | 1: t16", "repo | 1: t19 t1c"},
		{"alpha | main: t1 t18 t2 t6 t17 t1b | third: t14 | 3: t15", "plain | 1: t4", "plain | 1: t10", "repo | 1: t8 t9", "plain | 1: t11", "plain | 1: t16", "repo | 1: t19 t1c"},
	}
	for index, read := range reads {
		read.AppWindow = AppWindowClosed
		snapshot, home, err := ProjectCore(read, "/home/user")
		if err != nil {
			t.Fatalf("read %d: %v", index, err)
		}
		if got := spaceRows(home); !reflect.DeepEqual(got, want[index]) {
			t.Fatalf("read %d:\n%s\nwant:\n%s", index, strings.Join(got, "\n"), strings.Join(want[index], "\n"))
		}
		// Every live terminal shows exactly once, in every read.
		if len(homeRows(home)) != len(read.Terminals) {
			t.Fatalf("read %d shows %d of %d terminals", index, len(homeRows(home)), len(read.Terminals))
		}
		if index == 0 {
			continue
		}
		// The new space keeps its id through adoption, so Space opened fires once.
		last := snapshot.Workspaces[len(snapshot.Workspaces)-1]
		space := home.Workspaces[len(home.Workspaces)-1]
		if last.WorkspaceID != newSpace || space.ID != newSpace {
			t.Fatalf("read %d: new space %+v %q", index, last, space.ID)
		}
		alpha := home.Workspaces[0].Tabs[0].Terminals
		newTerminal, newSpaceFirst := alpha[len(alpha)-1], space.Tabs[0].Terminals[0]
		// While waiting, a terminal has no pane to start a terminal next to,
		// and a waiting space can only be closed; once placed, it takes New tab.
		placed := index == 4
		wantFirst := []TerminalAction{TerminalActionClose}
		if placed {
			wantFirst = []TerminalAction{TerminalActionSplit, TerminalActionClose}
		}
		wantNew := []TerminalAction{TerminalActionClose}
		if index >= 3 {
			wantNew = []TerminalAction{TerminalActionSplit, TerminalActionClose}
		}
		wantClosedSpace := []WorkspaceAction{WorkspaceActionCloseWorkspace}
		if placed {
			wantClosedSpace = []WorkspaceAction{WorkspaceActionNewTab, WorkspaceActionCloseWorkspace}
		}
		if newTerminal.PaneID != "t1b" || !reflect.DeepEqual(newTerminal.Actions, wantNew) || !reflect.DeepEqual(newSpaceFirst.Actions, wantFirst) ||
			!reflect.DeepEqual(space.Actions, wantClosedSpace) {
			t.Fatalf("read %d: actions t1b %v, t19 %v, space %v", index, newTerminal.Actions, newSpaceFirst.Actions, space.Actions)
		}
		// With the window open, a waiting space still can't be renamed.
		read.AppWindow = AppWindowOpen
		_, open, _ := ProjectCore(read, "/home/user")
		wantSpace := []WorkspaceAction{WorkspaceActionCloseWorkspace}
		if placed {
			wantSpace = []WorkspaceAction{WorkspaceActionNewTab, WorkspaceActionRenameSpace, WorkspaceActionCloseWorkspace}
		}
		if got := open.Workspaces[len(open.Workspaces)-1].Actions; !reflect.DeepEqual(got, wantSpace) || open.AppWindow != "" {
			t.Fatalf("read %d open: space actions %v, window %q", index, got, open.AppWindow)
		}
	}
}

// With the window open, the app writes a space it starts into the document a
// moment after its terminal, with the space's name. A terminal started after
// Shepherdr saw the window open waits for that write under Other terminals, so
// the space first shows, and is announced, under its own name. Terminals
// started before, which the app adopts on open, wait as before.
func TestProjectCoreLeavesTheAppTheSpacesItStarts(t *testing.T) {
	read := loadCoreReads(t, "core-reads-closed-reopen.jsonl")[1]
	read.AppWindow = AppWindowOpen
	rows := func(opened time.Time) []string {
		read.AppWindowOpened = opened
		_, home, err := ProjectCore(read, "/home/user")
		if err != nil {
			t.Fatal(err)
		}
		return spaceRows(home)
	}
	// t19 starts the new space at 1793483004368, t1b ends alpha / main just
	// after, and t1c is t19's worker.
	placed := []string{
		"alpha | main: t1 t18 t2 t6 t17 t1b | third: t14 | 3: t15",
		"plain | 1: t4", "plain | 1: t10", "repo | 1: t8 t9", "plain | 1: t11", "plain | 1: t16",
	}
	if got, want := rows(time.UnixMilli(1793483005000)), append(slices.Clone(placed), "sub | 1: t19 t1c"); !reflect.DeepEqual(got, want) {
		t.Fatalf("opened after the runs:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if got, want := rows(time.UnixMilli(1793483004368)), append(slices.Clone(placed), "Other terminals | Other terminals: t19 t1c"); !reflect.DeepEqual(got, want) {
		t.Fatalf("opened before the runs:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestProjectCoreLeavesOddAppLabelsUnderOtherTerminals(t *testing.T) {
	read := loadCoreReads(t, "core-reads-closed-reopen.jsonl")[0]
	const alpha, third = "sp034c04caa6b61d2a58e5a07df15f9f97", "tbc2b3d86003d0cee53a0395d6272dea94"
	labelled := func(address string, labels map[string]string) CoreTerminal {
		return CoreTerminal{Address: address, CWD: "/tmp/w3/work/plain", CreatedMS: 1793483100000, Labels: labels, Title: address}
	}
	read.Terminals = append(read.Terminals,
		// A tab id that belongs to another space, the id "other", or only one
		// label: the app wouldn't place these where Home could say.
		labelled("t30", map[string]string{"herdr.space": "spffff", "herdr.tab": third}),
		labelled("t31", map[string]string{"herdr.space": OtherTerminalsID, "herdr.tab": "tbffff"}),
		labelled("t32", map[string]string{"herdr.space": alpha}),
		// An unknown tab in a known space goes at the end of that space.
		labelled("t33", map[string]string{"herdr.space": alpha, "herdr.tab": "tbeeee"}),
	)
	_, home, err := ProjectCore(read, "/home/user")
	if err != nil {
		t.Fatal(err)
	}
	got := spaceRows(home)
	want := []string{
		"alpha | main: t1 t18 t2 t6 t17 | third: t14 | 3: t15 | 4: t33",
		"plain | 1: t4", "plain | 1: t10", "repo | 1: t8 t9", "plain | 1: t11", "plain | 1: t16",
		"Other terminals | Other terminals: t30 t31 t32",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestProjectCoreSuggestsTheRepositoryFolderOfEachShownSpace(t *testing.T) {
	read := loadCoreRead(t, "core-read-app.json")
	// Without app labels, the terminals the document doesn't place are Other
	// terminals rather than waiting spaces.
	for index := range read.Terminals {
		read.Terminals[index].Labels = nil
	}
	space := func(id, folder, terminal string, extra string) string {
		return `{"id":"` + id + `","name":"","folder":` + strconvQuote(folder) + extra + `,"tabs":[{"id":"tb` + id + `","name":"","root":{"type":"block","content":{"terminal":"` + terminal + `"}}}]}`
	}
	read.Document = json.RawMessage(`{"spaces":[` + strings.Join([]string{
		space("sp1", "/w/repo/sub", "t3", ""),
		space("sp2", "/w/plain", "t4", ""),
		space("sp3", "/w/repo", "t5", `,"machine":"vps"`),
		space("sp4", "/w/repo", "t99", ""),
	}, ",") + `]}`)
	asked := []string{}
	repositories := func(folder string) string {
		asked = append(asked, folder)
		return map[string]string{"/w/repo/sub": "/w/repo", "/w/repo": "/w/repo"}[folder]
	}
	_, home, err := projectCore(read, "/home/user", repositories)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, workspace := range home.Workspaces {
		got[workspace.ID] = workspace.CheckoutPath
	}
	// Another machine's space (sp3) and a space with no live terminal (sp4)
	// ask for nothing and suggest nothing.
	if want := map[string]string{"sp1": "/w/repo", "sp2": "", "sp3": "", OtherTerminalsID: ""}; !reflect.DeepEqual(got, want) {
		t.Fatalf("checkout paths = %q, want %q", got, want)
	}
	if want := []string{"/w/repo/sub", "/w/plain"}; !reflect.DeepEqual(asked, want) {
		t.Fatalf("asked = %q, want %q", asked, want)
	}
	// The action views project without suggestions.
	_, plain, err := ProjectCore(read, "/home/user")
	if err != nil || plain.Workspaces[0].CheckoutPath != "" {
		t.Fatalf("ProjectCore checkout path = %q, %v", plain.Workspaces[0].CheckoutPath, err)
	}
}

func TestRepositoryFolderCountsOnlyTopLevelRepositories(t *testing.T) {
	root := t.TempDir()
	for _, folder := range []string{"repo/.git", "repo/sub/deep", "repo/module/src", "repo/inner/.git", "plain", "stray/.git", "linked", "separate", "real/.git", "symlinked"} {
		if err := os.MkdirAll(root+"/"+folder, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		"repo/.git/HEAD":   "ref: refs/heads/main\n",
		"real/.git/HEAD":   "ref: refs/heads/main\n",
		"linked/.git":      "gitdir: " + root + "/repo/.git/worktrees/linked\n",
		"repo/module/.git": "gitdir: ../.git/modules/module\n",
		"separate/.git":    "gitdir: " + root + "/real/.git\n",
	} {
		if err := os.WriteFile(root+"/"+path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for link, target := range map[string]string{"shortcut": root + "/repo/sub", "symlinked/.git": root + "/real/.git"} {
		if err := os.Symlink(target, root+"/"+link); err != nil {
			t.Fatal(err)
		}
	}
	// The answer has symlinks resolved, including any above the temporary
	// folder, such as /var on macOS.
	top, err := filepath.EvalSymlinks(root + "/repo")
	if err != nil {
		t.Fatal(err)
	}
	// Any .git entry that isn't a real folder holding HEAD ends the search,
	// so a parent repository is never suggested for it.
	for folder, want := range map[string]string{
		"repo/sub/deep":   top,
		"repo":            top,
		"shortcut":        top,
		"repo/module/src": "",
		"repo/inner":      "",
		"plain":           "",
		"stray":           "",
		"linked":          "",
		"separate":        "",
		"symlinked":       "",
		"missing":         "",
	} {
		if got := repositoryFolder(root + "/" + folder); got != want {
			t.Errorf("%s: %q, want %q", folder, got, want)
		}
	}
	if got := repositoryFolder("repo"); got != "" {
		t.Errorf("relative folder: %q", got)
	}
}

func TestRepositoryFoldersCheckEachFolderOnceOffTheProjection(t *testing.T) {
	release := make(chan struct{})
	var checks atomic.Int32
	folders := newRepositoryFolders(func(folder string) string {
		checks.Add(1)
		<-release
		return folder + "-repo"
	})
	// A new folder suggests nothing yet; its check runs in the background,
	// and a stalled check is never started twice.
	for range 3 {
		if got := folders.lookup("/a"); got != "" {
			t.Fatalf("unchecked lookup = %q", got)
		}
		folders.settle()
	}
	close(release)
	<-folders.resolved
	if got := folders.lookup("/a"); got != "/a-repo" || checks.Load() != 1 {
		t.Fatalf("lookup = %q after %d checks", got, checks.Load())
	}
	folders.settle()
	// A folder no shown space uses is forgotten and checked again when it
	// comes back.
	folders.settle()
	if got := folders.lookup("/a"); got != "" {
		t.Fatalf("forgotten lookup = %q", got)
	}
	folders.settle()
	<-folders.resolved
	if got := folders.lookup("/a"); got != "/a-repo" || checks.Load() != 2 {
		t.Fatalf("lookup = %q after %d checks", got, checks.Load())
	}
}

func strconvQuote(value string) string {
	quoted, _ := json.Marshal(value)
	return string(quoted)
}

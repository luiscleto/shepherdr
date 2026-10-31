package herdr

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
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
		if len(workspace.Actions) != 0 || workspace.Worktrees != nil || workspace.CheckoutPath != "" {
			t.Fatalf("core space has actions or worktrees: %+v", workspace)
		}
		for _, tab := range workspace.Tabs {
			if tab.Current {
				t.Fatalf("core tab claims app focus: %+v", tab)
			}
			for _, terminal := range tab.Terminals {
				if len(terminal.Actions) != 0 || terminal.ManualName != nil {
					t.Fatalf("core terminal has actions or a manual name: %+v", terminal)
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
	if got := homeRows(Home{Workspaces: home.Workspaces[:1]})[:2]; got[0] != "<img src=x onerror=alert(1)> | main | t3 "+read.CoreID+":t3 | user@host:/tmp/w1/work/repo 1 | demo-agent user@host:/tmp/w1/work/repo working" ||
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
		rows[2] != "alpha | main | t4 "+read.CoreID+":t4 | Greeting question | claude-code Greeting question blocked" {
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

	// The app has never run: every terminal is Other terminals.
	for _, document := range []string{"null", ""} {
		home, err := project(document, nil)
		if err != nil || len(home.Workspaces) != 1 || home.Workspaces[0].ID != OtherTerminalsID || len(home.Workspaces[0].Tabs[0].Terminals) != 7 {
			t.Fatalf("document %q: %+v, %v", document, home, err)
		}
	}
	for _, document := range []string{`[]`, `{}`, `{"spaces":{}}`, `{"spaces":[1]}`, `{"spaces":[{"tabs":[]}]}`, `{"spaces":[{"id":"sp1"}]}`, `{"spaces":[{"id":"sp1","tabs":[{"name":"x"}]}]}`} {
		if _, err := project(document, nil); !errors.Is(err, ErrUnreadableAppLayout) {
			t.Fatalf("document %s: err = %v", document, err)
		}
	}
	// Unknown keys and node types are ignored; the terminal they hold still
	// appears once, under Other terminals. A dead block and an empty tab or
	// space are left out.
	home, err := project(`{"spaces":[{"id":"sp1","name":"","folder":"/srv/api","extra":1,"tabs":[
		{"id":"tb1","name":"","root":{"type":"grid","children":[{"type":"block","content":{"terminal":"t3"}}]}},
		{"id":"tb2","name":"","root":{"type":"column","children":[{"type":"block","content":{"terminal":"t99"}},{"type":"block","content":{"mod":"x"}},{"type":"stack","children":[{"type":"block","content":{"terminal":"t4"}}]}]}}]},
		{"id":"sp2","name":"empty","tabs":[{"id":"tb3","name":"","root":{"type":"block","content":{"terminal":"t98"}}}]}]}`, nil)
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
		{label: "agent title", source: CoreTerminal{Name: name("build"), Title: "Agent", Status: &CoreStatus{App: "claude", State: "idle"}}, agentName: "Agent", title: "Agent"},
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

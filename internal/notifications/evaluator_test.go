package notifications

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/luiscleto/shepherdr/internal/herdr"
)

func TestSnapshotEvaluatorUsesOnlyContiguousCompleteDifferences(t *testing.T) {
	evaluator := snapshotEvaluator{herdr: herdr.BackendTUI}
	initial := notificationSnapshot("working", "w1")
	if events := evaluator.Observe(initial, true); len(events) != 0 {
		t.Fatalf("initial baseline produced %+v", events)
	}

	changed := notificationSnapshot("blocked", "w1", "w2", "w3")
	events := evaluator.Observe(changed, false)
	if len(events) != 2 {
		t.Fatalf("status plus workspace-open burst produced %d events, want 2: %+v", len(events), events)
	}
	status := events[0]
	if status.Kind != EventStatus || status.Status != herdr.StatusBlocked || status.WorkspaceName != "Workspace w1" || status.PaneID != "w1:p1" || status.TerminalID != "term-1" {
		t.Fatalf("status event = %+v", status)
	}
	if status.Destination != "/#terminal=w1%3Ap1\u0026terminal_id=term-1\u0026herdr=tui" {
		t.Fatalf("status destination = %q", status.Destination)
	}
	if events[1] != (Event{Kind: EventWorkspaceOpened, Destination: "/"}) {
		t.Fatalf("workspace-open summary = %+v", events[1])
	}

	gapSnapshot := notificationSnapshot("done", "w1")
	if events := evaluator.Observe(gapSnapshot, true); len(events) != 0 {
		t.Fatalf("post-gap baseline produced %+v", events)
	}
	afterGap := notificationSnapshot("idle", "w1")
	if events := evaluator.Observe(afterGap, false); len(events) != 1 || events[0].Status != herdr.StatusIdle {
		t.Fatalf("first contiguous post-gap transition = %+v", events)
	}
}

func TestSnapshotEvaluatorEmitsOneNamedWorkspaceLifecyclePairAcrossSilentBaselines(t *testing.T) {
	evaluator := snapshotEvaluator{herdr: herdr.BackendTUI}
	initial := notificationSnapshot("working", "w1")
	if events := evaluator.Observe(initial, true); len(events) != 0 {
		t.Fatalf("initial baseline produced %+v", events)
	}

	opened := notificationSnapshot("working", "w1", "temporary")
	events := evaluator.Observe(opened, false)
	wantOpened := Event{Kind: EventWorkspaceOpened, WorkspaceName: "Workspace temporary", Destination: "/"}
	if len(events) != 1 || events[0] != wantOpened {
		t.Fatalf("workspace opened events = %+v, want [%+v]", events, wantOpened)
	}
	if events := evaluator.Observe(opened, true); len(events) != 0 {
		t.Fatalf("replacement-subscription baseline produced %+v", events)
	}
	if events := evaluator.Observe(opened, false); len(events) != 0 {
		t.Fatalf("unchanged post-baseline snapshot produced %+v", events)
	}

	closed := notificationSnapshot("working", "w1")
	events = evaluator.Observe(closed, false)
	wantClosed := Event{Kind: EventWorkspaceClosed, WorkspaceName: "Workspace temporary", Destination: "/"}
	if len(events) != 1 || events[0] != wantClosed {
		t.Fatalf("workspace closed events = %+v, want [%+v]", events, wantClosed)
	}

	reconnect := notificationSnapshot("working", "w1", "reconnected")
	if events := evaluator.Observe(reconnect, true); len(events) != 0 {
		t.Fatalf("reconnect baseline produced %+v", events)
	}
	if events := evaluator.Observe(reconnect, false); len(events) != 0 {
		t.Fatalf("unchanged reconnect snapshot produced %+v", events)
	}
}

func TestSnapshotEvaluatorDoesNotTreatNewAgentAsTransition(t *testing.T) {
	evaluator := snapshotEvaluator{herdr: herdr.BackendTUI}
	evaluator.Observe(herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{{WorkspaceID: "w1"}}}, true)
	current := notificationSnapshot("blocked", "w1")
	events := evaluator.Observe(current, false)
	if len(events) != 0 {
		t.Fatalf("newly observed agent produced %+v", events)
	}
}

func TestDefaultSelectionsIncludeAccessChanges(t *testing.T) {
	want := EventSettings{
		Blocked: true, Error: true, Done: true, TrustedSignInAdded: true, TrustedSignInRemoved: true,
	}
	if got := DefaultEventSettings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults = %+v, want %+v", got, want)
	}
}

func TestNotificationWorkspaceNameUsesOnlyShortNonBlankDisplayText(t *testing.T) {
	if got := notificationWorkspaceName("  Review workspace  "); got != "Review workspace" {
		t.Fatalf("trimmed workspace name = %q", got)
	}
	if got := notificationWorkspaceName(strings.Repeat("x", 161)); got != "" {
		t.Fatalf("overlong workspace name = %q, want generic fallback", got)
	}
}

func notificationSnapshot(status string, workspaceIDs ...string) herdr.Snapshot {
	workspaces := make([]herdr.WorkspaceInfo, len(workspaceIDs))
	for index, id := range workspaceIDs {
		workspaces[index].WorkspaceID = id
		workspaces[index].Label = "Workspace " + id
	}
	return herdr.Snapshot{
		Agents: []herdr.AgentInfo{{
			AgentStatus: herdr.Status(status), PaneID: "w1:p1", TabID: "w1:t1", TerminalID: "term-1", WorkspaceID: "w1",
		}},
		Workspaces: workspaces,
	}
}

func TestErrorFromTheHerdrAppStartsOnAndIsItsOwnChoice(t *testing.T) {
	var older EventSettings
	if err := json.Unmarshal([]byte(`{"blocked":false,"done":true}`), &older); err != nil {
		t.Fatal(err)
	}
	if !older.Error {
		t.Fatalf("settings saved before error existed did not start with error on: %+v", older)
	}
	var off EventSettings
	if err := json.Unmarshal([]byte(`{"blocked":true,"error":false}`), &off); err != nil || off.Error {
		t.Fatalf("explicit error choice = %+v, %v", off, err)
	}
	event := Event{Kind: EventStatus, Status: herdr.StatusError}
	if !event.selected(EventSettings{Error: true}) || event.selected(EventSettings{Blocked: true}) {
		t.Fatal("error notices do not follow the error choice")
	}

	evaluator := snapshotEvaluator{herdr: herdr.BackendTUI}
	evaluator.Observe(notificationSnapshot("working", "w1"), true)
	events := evaluator.Observe(notificationSnapshot("error", "w1"), false)
	if len(events) != 1 || events[0].Status != herdr.StatusError || events[0].WorkspaceName != "Workspace w1" {
		t.Fatalf("working to error produced %+v", events)
	}
}

func TestManagerMarksNoticesWithTheirHerdr(t *testing.T) {
	manager := NewManager(UnavailableStore(errors.New("not needed")), nil)
	core := manager.Observer(herdr.BackendCore)
	core.ObserveSnapshot(notificationSnapshot("working", "w1"), true)
	core.ObserveSnapshot(notificationSnapshot("blocked", "w1", "w2"), false)
	for range 2 {
		event := <-manager.events
		if event.Herdr != herdr.BackendCore {
			t.Fatalf("event = %+v", event)
		}
	}
	payload, err := eventPayload(Event{Kind: EventWorkspaceOpened, Herdr: herdr.BackendCore, WorkspaceName: "alpha", Destination: "/"})
	if err != nil || string(payload) != `{"destination":"/","herdr":"core","kind":"workspace_opened","workspace_name":"alpha"}` {
		t.Fatalf("payload = %s, %v", payload, err)
	}
}

// Each Herdr's reads are compared only with that Herdr's previous read. A
// shared comparison would see the other Herdr's workspaces and agents appear
// and disappear on every read and raise false notices.
func TestEachHerdrIsComparedOnlyWithItself(t *testing.T) {
	tuiSnapshot := func(status string) herdr.Snapshot {
		return herdr.Snapshot{
			Agents: []herdr.AgentInfo{{
				AgentStatus: herdr.Status(status), PaneID: "w1:p1", TabID: "w1:t1", TerminalID: "term-1", WorkspaceID: "w1",
			}},
			Workspaces: []herdr.WorkspaceInfo{{WorkspaceID: "w1", Label: "api"}},
		}
	}
	coreSnapshot := func(status string) herdr.Snapshot {
		return herdr.Snapshot{
			Agents: []herdr.AgentInfo{{
				AgentStatus: herdr.Status(status), PaneID: "t3", TabID: "tb1", TerminalID: "core-1:t3", WorkspaceID: "sp1",
			}},
			Workspaces: []herdr.WorkspaceInfo{{WorkspaceID: "sp1", Label: "alpha"}},
		}
	}
	manager := NewManager(UnavailableStore(errors.New("not needed")), nil)
	tui := manager.Observer(herdr.BackendTUI)
	core := manager.Observer(herdr.BackendCore)
	tui.ObserveSnapshot(tuiSnapshot("working"), true)
	core.ObserveSnapshot(coreSnapshot("working"), true)
	for range 3 {
		tui.ObserveSnapshot(tuiSnapshot("working"), false)
		core.ObserveSnapshot(coreSnapshot("working"), false)
	}
	if got := len(manager.events); got != 0 {
		t.Fatalf("unchanged reads from both Herdrs raised %d notices: %+v", got, <-manager.events)
	}

	tui.ObserveSnapshot(tuiSnapshot("blocked"), false)
	core.ObserveSnapshot(coreSnapshot("error"), false)
	if got := len(manager.events); got != 2 {
		t.Fatalf("one change on each Herdr raised %d notices, want 2", got)
	}
	for _, want := range []string{
		`{"destination":"/#terminal=w1%3Ap1\u0026terminal_id=term-1\u0026herdr=tui","herdr":"tui","kind":"status","workspace_name":"api","pane_id":"w1:p1","status":"blocked","terminal_id":"term-1"}`,
		`{"destination":"/#terminal=t3\u0026terminal_id=core-1%3At3\u0026herdr=core","herdr":"core","kind":"status","workspace_name":"alpha","pane_id":"t3","status":"error","terminal_id":"core-1:t3"}`,
	} {
		payload, err := eventPayload(<-manager.events)
		if err != nil || string(payload) != want {
			t.Fatalf("payload = %s, %v; want %s", payload, err, want)
		}
	}
}

func TestStatusNoticesMarkHerdrAppTerminalsInNoSpace(t *testing.T) {
	inOther := func(status string) herdr.Snapshot {
		snapshot := notificationSnapshot(status, "sp1")
		snapshot.Agents[0].WorkspaceID = herdr.OtherTerminalsID
		return snapshot
	}
	for _, test := range []struct {
		name    string
		backend herdr.Backend
		want    string
	}{
		{name: "Herdr app", backend: herdr.BackendCore, want: `{"destination":"/#terminal=w1%3Ap1\u0026terminal_id=term-1\u0026herdr=core","herdr":"core","kind":"status","other_terminals":true,"pane_id":"w1:p1","status":"blocked","terminal_id":"term-1"}`},
		{name: "Herdr TUI", backend: herdr.BackendTUI, want: `{"destination":"/#terminal=w1%3Ap1\u0026terminal_id=term-1\u0026herdr=tui","herdr":"tui","kind":"status","pane_id":"w1:p1","status":"blocked","terminal_id":"term-1"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := NewManager(UnavailableStore(errors.New("not needed")), nil)
			observer := manager.Observer(test.backend)
			observer.ObserveSnapshot(inOther("working"), true)
			observer.ObserveSnapshot(inOther("blocked"), false)
			payload, err := eventPayload(<-manager.events)
			if err != nil || string(payload) != test.want {
				t.Fatalf("payload = %s, %v; want %s", payload, err, test.want)
			}
		})
	}

	manager := NewManager(UnavailableStore(errors.New("not needed")), nil)
	core := manager.Observer(herdr.BackendCore)
	core.ObserveSnapshot(notificationSnapshot("working", "w1"), true)
	core.ObserveSnapshot(notificationSnapshot("blocked", "w1"), false)
	if event := <-manager.events; event.OtherTerminals || event.WorkspaceName != "Workspace w1" {
		t.Fatalf("space notice = %+v", event)
	}
}

// A status notice adds only a name the person gave the terminal or its tab.
func TestStatusNoticesAddOnlyANameThePersonGave(t *testing.T) {
	const terminalTitle = "luisc@omarazer:~/Work/shepherdr"
	named := func(pane *string, tab string, status string) herdr.Snapshot {
		agentName, agentTitle, displayAgent := "claude", "Claude Code session", "Claude"
		return herdr.Snapshot{
			Agents: []herdr.AgentInfo{{
				Agent: &displayAgent, AgentStatus: herdr.Status(status), DisplayAgent: &displayAgent, Name: &agentName,
				PaneID: "w1:p1", TabID: "w1:t4", TerminalID: "term-1", Title: &agentTitle, WorkspaceID: "w1",
			}},
			Panes: []herdr.PaneInfo{{
				CWD: "/home/luisc/Work/shepherdr", Label: pane, PaneID: "w1:p1", TabID: "w1:t4", TerminalID: "term-1",
				TerminalTitle: terminalTitle, TerminalTitleStripped: terminalTitle, WorkspaceID: "w1",
			}},
			Tabs:       []herdr.TabInfo{{Label: tab, Number: 4, TabID: "w1:t4", WorkspaceID: "w1"}},
			Workspaces: []herdr.WorkspaceInfo{{Label: "Douro", WorkspaceID: "w1"}},
		}
	}
	text := func(value string) *string { return &value }
	for _, test := range []struct {
		name    string
		backend herdr.Backend
		pane    *string
		tab     string
		want    string
	}{
		{name: "named Herdr app tab", backend: herdr.BackendCore, tab: "Review PR17 CI", want: "Review PR17 CI"},
		{name: "unnamed Herdr app tab", backend: herdr.BackendCore, tab: "", want: ""},
		{name: "Herdr app tab named with digits", backend: herdr.BackendCore, tab: "17", want: "17"},
		{name: "named Herdr TUI terminal", backend: herdr.BackendTUI, pane: text("Scout P3 Queiroz cite"), tab: "Review PR17 CI", want: "Scout P3 Queiroz cite"},
		{name: "named Herdr TUI tab", backend: herdr.BackendTUI, tab: "Review PR17 CI", want: "Review PR17 CI"},
		{name: "default-numbered Herdr TUI tab", backend: herdr.BackendTUI, tab: "3", want: ""},
		{name: "blank Herdr TUI names", backend: herdr.BackendTUI, pane: text("  "), tab: "", want: ""},
		{name: "hostile name", backend: herdr.BackendCore, tab: "<img src=x onerror=alert(1)>\x1b]0;evil\a\u202e\r\n" + strings.Repeat("y", 80), want: "<img src=x onerror=alert(1)>]0;evil " + strings.Repeat("y", 23) + "…"},
	} {
		t.Run(test.name, func(t *testing.T) {
			evaluator := snapshotEvaluator{herdr: test.backend}
			evaluator.Observe(named(test.pane, test.tab, "working"), true)
			events := evaluator.Observe(named(test.pane, test.tab, "done"), false)
			if len(events) != 1 || events[0].GivenName != test.want || events[0].WorkspaceName != "Douro" {
				t.Fatalf("events = %+v, want name %q", events, test.want)
			}
			event := events[0]
			event.Herdr = test.backend
			payload, err := eventPayload(event)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{terminalTitle, "omarazer", "luisc", "claude", "Claude", "\x1b", "\a", "\u202e"} {
				if strings.Contains(string(payload), forbidden) {
					t.Fatalf("payload exposed %q: %s", forbidden, payload)
				}
			}
			if utf8.RuneCountInString(event.GivenName) > maxGivenNameRunes {
				t.Fatalf("name %q is longer than %d", event.GivenName, maxGivenNameRunes)
			}
		})
	}

	evaluator := snapshotEvaluator{herdr: herdr.BackendCore}
	evaluator.Observe(named(nil, "Review PR17 CI", "working"), true)
	events := evaluator.Observe(named(nil, "Review PR17 CI", "blocked"), false)
	payload, err := eventPayload(Event{
		Kind: EventStatus, Herdr: herdr.BackendCore, Status: events[0].Status, GivenName: events[0].GivenName,
		WorkspaceName: events[0].WorkspaceName, PaneID: events[0].PaneID, TerminalID: events[0].TerminalID, Destination: events[0].Destination,
	})
	want := `{"destination":"/#terminal=w1%3Ap1\u0026terminal_id=term-1\u0026herdr=core","herdr":"core","kind":"status","name":"Review PR17 CI","workspace_name":"Douro","pane_id":"w1:p1","status":"blocked","terminal_id":"term-1"}`
	if err != nil || string(payload) != want {
		t.Fatalf("payload = %s, %v; want %s", payload, err, want)
	}
	lifecycle, _ := eventPayload(Event{Kind: EventWorkspaceOpened, Herdr: herdr.BackendCore, GivenName: "Review PR17 CI", WorkspaceName: "Douro", Destination: "/"})
	if strings.Contains(string(lifecycle), "Review") {
		t.Fatalf("a space notice carried a tab name: %s", lifecycle)
	}
}

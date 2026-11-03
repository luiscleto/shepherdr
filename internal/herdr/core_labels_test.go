package herdr

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// The rules follow the Herdr app 0.1.12's docs/labels.md and what its agents
// list showed for each value on a real core.
func TestCoreShownStatusFollowsTheAppsSeenRule(t *testing.T) {
	for _, test := range []struct {
		state string
		seen  *string
		turn  uint64
		want  Status
	}{
		{"done", nil, 3, StatusDone},
		{"done", text("2"), 3, StatusDone},
		{"done", text("3"), 3, StatusIdle},
		{"done", text("9"), 3, StatusIdle},
		{"done", text("+3"), 3, StatusIdle},
		{"done", text("03"), 3, StatusIdle},
		{"done", text(" 3"), 3, StatusDone},
		{"done", text("3 "), 3, StatusDone},
		{"done", text("++3"), 3, StatusDone},
		{"done", text("+"), 0, StatusDone},
		{"done", text(""), 0, StatusDone},
		{"done", text("-1"), 0, StatusDone},
		{"done", text("abc"), 0, StatusDone},
		{"done", text("18446744073709551616"), 1, StatusDone},
		{"done", text("18446744073709551615"), 1, StatusIdle},
		// An idle report is done while its turn is unseen, as in the app.
		{"idle", text("1"), 3, StatusDone},
		{"idle", text("3"), 3, StatusIdle},
		{"idle", text("0"), 0, StatusIdle},
		{"idle", nil, 3, StatusIdle},
		{"idle", text("junk"), 3, StatusIdle},
		// Every other word stays core's.
		{"working", text("0"), 3, StatusWorking},
		{"working", text("9"), 3, StatusWorking},
		{"blocked", text("0"), 3, StatusBlocked},
		{"blocked", text("9"), 3, StatusBlocked},
		{"error", text("0"), 3, StatusError},
		{"error", text("9"), 3, StatusError},
	} {
		terminal := CoreTerminal{Status: &CoreStatus{State: test.state}, Turn: test.turn, Labels: map[string]string{}}
		if test.seen != nil {
			terminal.Labels[CoreSeenLabel] = *test.seen
		}
		if got := CoreShownStatus(terminal); got != test.want {
			t.Errorf("%s seen=%v turn=%d: shown %q, want %q", test.state, test.seen, test.turn, got, test.want)
		}
	}
	if got := CoreShownStatus(CoreTerminal{Labels: map[string]string{CoreSeenLabel: "9"}}); got != "" {
		t.Fatalf("a terminal without a status shows %q", got)
	}
}

func TestCoreSeenIsOnlyRaisedOrWrittenWhenMissing(t *testing.T) {
	for _, test := range []struct {
		seen  *string
		turn  uint64
		raise bool
	}{
		{nil, 0, true},
		{text("junk"), 2, true},
		{text("1"), 2, true},
		{text("2"), 2, false},
		{text("5"), 2, false},
	} {
		terminal := CoreTerminal{Turn: test.turn, Labels: map[string]string{}}
		if test.seen != nil {
			terminal.Labels[CoreSeenLabel] = *test.seen
		}
		if got := CoreSeenNeedsRaise(terminal); got != test.raise {
			t.Errorf("seen=%v turn=%d: raise = %t", test.seen, test.turn, got)
		}
	}
}

// Home shows the seen state; notifications keep core's word.
func TestCoreHomeShowsSeenAgentsAndNoticesKeepCoresWord(t *testing.T) {
	read := loadCoreRead(t, "core-read-app.json")
	for index := range read.Terminals {
		terminal := &read.Terminals[index]
		switch terminal.Address {
		case "t3": // seen since its turn ended
			terminal.Status, terminal.Turn = &CoreStatus{App: "claude-code", State: "done"}, 4
			terminal.Labels[CoreSeenLabel] = "4"
		case "t4": // a turn ended after it was seen
			terminal.Status, terminal.Turn = &CoreStatus{App: "codex", State: "done"}, 5
			terminal.Labels[CoreSeenLabel] = "4"
		case "t5": // reported idle after a turn nobody has seen
			terminal.Status, terminal.Turn = &CoreStatus{App: "pi", State: "idle"}, 2
			terminal.Labels[CoreSeenLabel] = "1"
		case "t8": // never seen by any client
			terminal.Status, terminal.Turn = &CoreStatus{App: "pi", State: "done"}, 1
		}
	}
	snapshot, home, err := ProjectCore(read, "/home/me")
	if err != nil {
		t.Fatal(err)
	}
	shown := map[string]Status{}
	for _, workspace := range home.Workspaces {
		for _, tab := range workspace.Tabs {
			for _, terminal := range tab.Terminals {
				if terminal.Agent != nil {
					shown[terminal.PaneID] = terminal.Agent.Status
				}
			}
		}
	}
	if shown["t3"] != StatusIdle || shown["t4"] != StatusDone || shown["t5"] != StatusDone || shown["t8"] != StatusDone {
		t.Fatalf("shown = %v", shown)
	}
	alpha := home.Workspaces[0]
	if alpha.AgentCounts == nil || alpha.AgentCounts.Idle != 1 || alpha.AgentCounts.Done != 2 {
		t.Fatalf("alpha counts = %+v", alpha.AgentCounts)
	}
	notices := map[string]Status{}
	for _, agent := range snapshot.Agents {
		notices[agent.PaneID] = agent.AgentStatus
	}
	if notices["t3"] != StatusDone || notices["t4"] != StatusDone || notices["t5"] != StatusIdle || notices["t8"] != StatusDone {
		t.Fatalf("notice statuses = %v", notices)
	}
}

func TestCoreTerminalFollowTracksTurnsAndLabels(t *testing.T) {
	core, path := newTestCore(t)
	core.set(func(core *testCore) {
		core.read.Terminals[0].Status = &CoreStatus{App: "claude-code", State: "working"}
		core.read.Terminals[0].Turn = 2
	})
	_, otherPath := newTestCore(t)
	if _, err := NewCoreClient(otherPath, discardLogger()).FollowTerminal(context.Background(), "t99"); err == nil {
		t.Fatal("followed a terminal core doesn't have")
	}
	follow, err := NewCoreClient(path, discardLogger()).FollowTerminal(context.Background(), "t3")
	if err != nil {
		t.Fatal(err)
	}
	defer follow.Close()
	if first := follow.Terminal(); first.Turn != 2 || first.Status.State != "working" {
		t.Fatalf("first = %+v", first)
	}
	for _, line := range []string{
		`{"jsonrpc":"2.0","method":"heartbeat","params":{"seq":4}}`,
		`{"jsonrpc":"2.0","method":"event","params":{"data":{"records":{},"reported":true,"status":{"app":"claude-code","state":"done"}},"seq":5,"terminal":"t3","type":"status"}}`,
		`{"jsonrpc":"2.0","method":"event","params":{"data":{"state":"done","turn":3},"seq":6,"terminal":"t3","type":"turn"}}`,
		`{"jsonrpc":"2.0","method":"event","params":{"data":{"labels":{"herdr.seen":"3"}},"seq":7,"terminal":"t3","type":"labels"}}`,
		`{"jsonrpc":"2.0","method":"event","params":{"data":{"state":"idle","turn":4},"seq":8,"terminal":"t4","type":"turn"}}`,
		`{"jsonrpc":"2.0","method":"event","params":{"data":{"exit_code":0},"seq":9,"terminal":"t3","type":"finished"}}`,
	} {
		core.events <- line
	}
	var ended []bool
	for {
		turnEnded, err := follow.Next()
		if errors.Is(err, ErrCoreTerminalGone) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		ended = append(ended, turnEnded)
	}
	last := follow.Terminal()
	if !slices.Equal(ended, []bool{false, false, true, false, false}) || last.Turn != 3 || last.Status.State != "done" || last.Labels[CoreSeenLabel] != "3" {
		t.Fatalf("ended = %v, last = %+v", ended, last)
	}
}

func text(value string) *string { return &value }

// A pinned agent shows its pin and offers Unpin; another agent offers Pin. A
// plain terminal is never pinned, whatever its labels say.
func TestCoreHomeShowsPinnedAgentsWithPinOrUnpin(t *testing.T) {
	read := loadCoreRead(t, "core-read-app.json")
	read.AppWindow = AppWindowOpen
	for index := range read.Terminals {
		terminal := &read.Terminals[index]
		switch terminal.Address {
		case "t3":
			terminal.Status = &CoreStatus{App: "claude-code", State: "done"}
			terminal.Labels[CorePinnedLabel] = "1"
		case "t4":
			terminal.Status = &CoreStatus{App: "codex", State: "working"}
			terminal.Labels[CorePinnedLabel] = ""
		case "t8": // under Other terminals; any non-empty value pins
			terminal.Status = &CoreStatus{App: "pi", State: "blocked"}
			terminal.Labels = map[string]string{CorePinnedLabel: "yes"}
		case "t9":
			terminal.Labels[CorePinnedLabel] = "1"
		}
	}
	_, home, err := ProjectCore(read, "/home/me")
	if err != nil {
		t.Fatal(err)
	}
	shown := map[string]Terminal{}
	for _, workspace := range home.Workspaces {
		for _, tab := range workspace.Tabs {
			for _, terminal := range tab.Terminals {
				shown[terminal.PaneID] = terminal
			}
		}
	}
	check := func(address string, pinned bool, actions ...TerminalAction) {
		t.Helper()
		terminal := shown[address]
		if terminal.Agent == nil || terminal.Agent.Pinned != pinned || !slices.Equal(terminal.Actions, actions) {
			t.Fatalf("%s = %+v %+v", address, terminal.Agent, terminal.Actions)
		}
	}
	check("t3", true, TerminalActionUnpin, TerminalActionSplit, TerminalActionClose)
	check("t4", false, TerminalActionPin, TerminalActionSplit, TerminalActionClose)
	check("t8", true, TerminalActionUnpin, TerminalActionClose)
	if plain := shown["t9"]; plain.Agent != nil || slices.Contains(plain.Actions, TerminalActionPin) || slices.Contains(plain.Actions, TerminalActionUnpin) {
		t.Fatalf("plain terminal = %+v", plain)
	}
}

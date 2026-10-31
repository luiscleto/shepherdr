package notifications

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/luiscleto/shepherdr/internal/herdr"
)

func TestSnapshotEvaluatorUsesOnlyContiguousCompleteDifferences(t *testing.T) {
	evaluator := snapshotEvaluator{}
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
	if status.Destination != "/#terminal=w1%3Ap1&terminal_id=term-1" {
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
	evaluator := snapshotEvaluator{}
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
	evaluator := snapshotEvaluator{}
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

	evaluator := snapshotEvaluator{}
	evaluator.Observe(notificationSnapshot("working", "w1"), true)
	events := evaluator.Observe(notificationSnapshot("error", "w1"), false)
	if len(events) != 1 || events[0].Status != herdr.StatusError || events[0].WorkspaceName != "Workspace w1" {
		t.Fatalf("working to error produced %+v", events)
	}
}

func TestManagerMarksNoticesWithTheHerdrApp(t *testing.T) {
	manager := NewManager(UnavailableStore(errors.New("not needed")), nil)
	manager.SetHerdr(herdr.BackendCore)
	manager.ObserveSnapshot(notificationSnapshot("working", "w1"), true)
	manager.ObserveSnapshot(notificationSnapshot("blocked", "w1", "w2"), false)
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

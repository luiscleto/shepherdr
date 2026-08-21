package notifications

import (
	"reflect"
	"testing"

	"github.com/luisc/shepherdr/internal/herdr"
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
	if status.Kind != EventStatus || status.Status != herdr.StatusBlocked || status.PaneID != "w1:p1" || status.TerminalID != "term-1" {
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

func TestSnapshotEvaluatorDoesNotTreatNewAgentAsTransition(t *testing.T) {
	evaluator := snapshotEvaluator{}
	evaluator.Observe(herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{{WorkspaceID: "w1"}}}, true)
	current := notificationSnapshot("blocked", "w1")
	events := evaluator.Observe(current, false)
	if len(events) != 0 {
		t.Fatalf("newly observed agent produced %+v", events)
	}
}

func TestDefaultSelectionsAreBlockedAndDoneOnly(t *testing.T) {
	want := EventSettings{Blocked: true, Done: true}
	if got := DefaultEventSettings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults = %+v, want %+v", got, want)
	}
}

func notificationSnapshot(status string, workspaceIDs ...string) herdr.Snapshot {
	workspaces := make([]herdr.WorkspaceInfo, len(workspaceIDs))
	for index, id := range workspaceIDs {
		workspaces[index].WorkspaceID = id
	}
	return herdr.Snapshot{
		Agents: []herdr.AgentInfo{{
			AgentStatus: herdr.Status(status), PaneID: "w1:p1", TabID: "w1:t1", TerminalID: "term-1", WorkspaceID: "w1",
		}},
		Workspaces: workspaces,
	}
}

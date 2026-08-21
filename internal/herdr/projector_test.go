package herdr

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRepeatedTransportFailureDoesNotRepublish(t *testing.T) {
	projector := NewProjector(nil)
	projector.state = State{
		Connection: ConnectionLive,
		Gap:        7,
		HasHome:    true,
		Home:       Home{Workspaces: []Workspace{}},
	}
	updates, unsubscribe := projector.Subscribe()
	defer unsubscribe()
	<-updates
	projector.publishError(io.EOF)
	first := <-updates
	if first.Connection != ConnectionNotRunning || first.Gap != 8 || !first.LastKnown {
		t.Fatalf("first transport failure = %+v", first)
	}
	projector.publishError(io.EOF)
	select {
	case repeated := <-updates:
		t.Fatalf("repeated identical transport failure published %+v", repeated)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestProjectorUsesOneReadAndOneDirtyFollowUpForAnEventBurst(t *testing.T) {
	fixture := newLoopFixture(t, stableProjectorSnapshot())
	fixture.blockSnapshot.Store(2)

	projector := NewProjector(NewClient(fixture.socketPath))
	updates, unsubscribe := projector.Subscribe()
	defer unsubscribe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go projector.Run(ctx)
	<-updates

	waitSignal(t, fixture.subscriptionReady, "subscription acknowledgement")
	waitSignal(t, fixture.readStarted, "post-subscription snapshot")
	written := make(chan struct{})
	for index := 0; index < 24; index++ {
		request := fixtureEvent{kind: "pane_updated"}
		if index == 23 {
			request.written = written
		}
		fixture.events <- request
	}
	waitSignal(t, written, "event burst")
	close(fixture.releaseRead)

	live := waitForProjectorState(t, updates, func(state State) bool { return state.Connection == ConnectionLive })
	if !live.HasHome {
		t.Fatal("post-subscription snapshot did not publish a complete Home")
	}
	waitForCount(t, &fixture.snapshots, 3)
	time.Sleep(100 * time.Millisecond)
	if got := fixture.snapshots.Load(); got != 3 {
		t.Fatalf("event burst caused %d snapshots, want setup, post-subscription, and one follow-up", got)
	}
	if got := fixture.maxActiveSnapshots.Load(); got != 1 {
		t.Fatalf("maximum active snapshot reads = %d, want 1", got)
	}
	select {
	case state := <-updates:
		t.Fatalf("unchanged follow-up Home unexpectedly published %+v", state)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestOrdinaryTitleChangePublishesCurrentHomeWithoutLifecycle(t *testing.T) {
	projector := NewProjector(nil)
	updates, unsubscribe := projector.Subscribe()
	defer unsubscribe()
	<-updates

	snapshot := ordinaryTitleSnapshot()
	projector.publishLive(snapshot)
	first := <-updates
	if title := findProjectedTerminal(first.Home, "w1:p1").Title; title != "Shell" {
		t.Fatalf("initial ordinary title = %q, want Shell", title)
	}

	snapshot.Panes[0].TerminalTitleStripped = "rapid output title"
	projector.publishLive(snapshot)
	changed := <-updates
	if changed.Connection != ConnectionLive || changed.Gap != first.Gap || changed.LastKnown {
		t.Fatalf("title change published lifecycle state: connection=%s gap=%d last_known=%t", changed.Connection, changed.Gap, changed.LastKnown)
	}
	if title := findProjectedTerminal(changed.Home, "w1:p1").Title; title != "rapid output title" {
		t.Fatalf("published Home title = %q, want rapid output title", title)
	}
	if changed.Snapshot.Panes[0].TerminalTitleStripped != "rapid output title" {
		t.Fatal("Terminal-facing current raw snapshot was not replaced")
	}
}

type snapshotObservation struct {
	baseline bool
	snapshot Snapshot
}

type recordingSnapshotObserver struct {
	observations []snapshotObservation
}

func (o *recordingSnapshotObserver) ObserveSnapshot(snapshot Snapshot, baseline bool) {
	o.observations = append(o.observations, snapshotObservation{baseline: baseline, snapshot: snapshot})
}

func TestProjectorObservesOnlyValidatedPublicationsWithBaselineBoundary(t *testing.T) {
	projector := NewProjector(nil)
	observer := &recordingSnapshotObserver{}
	projector.SetSnapshotObserver(observer)
	snapshot := ordinaryTitleSnapshot()
	if !projector.publishLiveObserved(snapshot, true) {
		t.Fatal("valid baseline snapshot was rejected")
	}
	snapshot.Panes[0].TerminalTitleStripped = "changed"
	if !projector.publishLiveObserved(snapshot, false) {
		t.Fatal("valid contiguous snapshot was rejected")
	}
	invalid := snapshot
	invalid.Panes[0].PaneID = ""
	if projector.publishLiveObserved(invalid, false) {
		t.Fatal("invalid snapshot was observed as a publication")
	}
	if len(observer.observations) != 2 || !observer.observations[0].baseline || observer.observations[1].baseline {
		t.Fatalf("observations = %+v", observer.observations)
	}
}

func TestWorktreeEventRefreshesOnceWithoutGapOrRetry(t *testing.T) {
	fixture := newLoopFixture(t, stableProjectorSnapshot())
	projector := NewProjector(NewClient(fixture.socketPath))
	updates, unsubscribe := projector.Subscribe()
	defer unsubscribe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go projector.Run(ctx)
	<-updates

	first := waitForProjectorState(t, updates, func(state State) bool { return state.Connection == ConnectionLive })
	written := make(chan struct{})
	fixture.events <- fixtureEvent{kind: "worktree_created", written: written}
	waitSignal(t, written, "worktree event")
	waitForCount(t, &fixture.snapshots, 3)

	select {
	case state := <-updates:
		t.Fatalf("worktree event published lifecycle state: connection=%s gap=%d", state.Connection, state.Gap)
	case <-time.After(50 * time.Millisecond):
	}
	if got := fixture.snapshots.Load(); got != 3 {
		t.Fatalf("worktree event caused %d snapshots, want setup, post-subscription, and one refresh", got)
	}
	if got := fixture.subscriptions.Load(); got != 1 {
		t.Fatalf("worktree event caused %d subscriptions, want 1", got)
	}
	current := projector.Current()
	if current.Connection != ConnectionLive || current.Gap != first.Gap || current.LastKnown {
		t.Fatalf("worktree event changed lifecycle state: connection=%s gap=%d last_known=%t", current.Connection, current.Gap, current.LastKnown)
	}
}

func TestRequestedHomeRefreshUsesOneCompleteRead(t *testing.T) {
	fixture := newLoopFixture(t, stableProjectorSnapshot())
	projector := NewProjector(NewClient(fixture.socketPath))
	updates, unsubscribe := projector.Subscribe()
	defer unsubscribe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go projector.Run(ctx)
	<-updates
	waitForProjectorState(t, updates, func(state State) bool { return state.Connection == ConnectionLive })

	projector.RequestRefresh()
	waitForCount(t, &fixture.snapshots, 3)
	time.Sleep(100 * time.Millisecond)
	if got := fixture.snapshots.Load(); got != 3 {
		t.Fatalf("one requested refresh caused %d total snapshots, want setup, post-subscription, and one refresh", got)
	}
	select {
	case state := <-updates:
		t.Fatalf("unchanged requested refresh unexpectedly published %+v", state)
	default:
	}
}

func TestProjectorResubscribesBeforePublishingANewPane(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "herdr.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancelServer := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancelServer()
		listener.Close()
	})
	var snapshots atomic.Int32
	var subscriptions atomic.Int32
	go serveNewPaneFixture(ctx, listener, &snapshots, &subscriptions)

	projector := NewProjector(NewClient(socketPath))
	updates, unsubscribe := projector.Subscribe()
	defer unsubscribe()
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	go projector.Run(runCtx)

	live := waitForProjectorState(t, updates, func(state State) bool { return state.Connection == ConnectionLive })
	if got := subscriptions.Load(); got != 2 {
		t.Fatalf("published after %d subscriptions, want the replacement covering the new pane", got)
	}
	if got := projectedTerminals(live.Home); len(got) != 1 || got[0].PaneID != "w1:p1" {
		t.Fatalf("published Home = %+v", live.Home)
	}
}

func TestSubscriptionLossReconnectsAndUsesAPostSubscriptionSnapshot(t *testing.T) {
	fixture := newLoopFixture(t, stableProjectorSnapshot())
	projector := NewProjector(NewClient(fixture.socketPath))
	updates, unsubscribe := projector.Subscribe()
	defer unsubscribe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go projector.Run(ctx)

	firstLive := waitForProjectorState(t, updates, func(state State) bool { return state.Connection == ConnectionLive })
	fixture.events <- fixtureEvent{drop: true}
	reconnecting := waitForProjectorState(t, updates, func(state State) bool {
		return state.Connection == ConnectionReconnecting && state.LastKnown
	})
	if reconnecting.Gap != firstLive.Gap+1 {
		t.Fatalf("reconnecting gap = %d, want %d", reconnecting.Gap, firstLive.Gap+1)
	}
	recovered := waitForProjectorState(t, updates, func(state State) bool { return state.Connection == ConnectionLive })
	if recovered.LastKnown || !reflect.DeepEqual(recovered.Home, firstLive.Home) {
		t.Fatalf("unexpected recovered state: %+v", recovered)
	}
	if got := fixture.subscriptions.Load(); got < 2 {
		t.Fatalf("recovery established %d subscriptions, want at least 2", got)
	}
	if got := fixture.snapshots.Load(); got < 4 {
		t.Fatalf("recovery used %d snapshots, want setup and post-subscription reads on both connections", got)
	}
}

func TestSubscriptionCoverageRejectsPaneAddedBetweenSnapshots(t *testing.T) {
	first := Snapshot{Panes: []PaneInfo{{PaneID: "w1:p1"}}}
	second := Snapshot{Panes: []PaneInfo{{PaneID: "w1:p1"}, {PaneID: "w1:p2"}}}
	if subscriptionCoversSnapshot(first, second) {
		t.Fatal("subscription basis unexpectedly covered a pane that appeared later")
	}
	if !subscriptionCoversSnapshot(second, first) {
		t.Fatal("subscriptions for a removed pane should cover the current snapshot")
	}
}

type fixtureEvent struct {
	drop    bool
	kind    string
	written chan struct{}
}

type loopFixture struct {
	socketPath         string
	snapshot           Snapshot
	events             chan fixtureEvent
	subscriptionReady  chan struct{}
	readStarted        chan struct{}
	releaseRead        chan struct{}
	blockSnapshot      atomic.Int32
	snapshots          atomic.Int32
	subscriptions      atomic.Int32
	activeSnapshots    atomic.Int32
	maxActiveSnapshots atomic.Int32
	readStartedOnce    sync.Once
}

func newLoopFixture(t *testing.T, snapshot Snapshot) *loopFixture {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "herdr.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	fixture := &loopFixture{
		socketPath:        socketPath,
		snapshot:          snapshot,
		events:            make(chan fixtureEvent, 64),
		subscriptionReady: make(chan struct{}, 8),
		readStarted:       make(chan struct{}),
		releaseRead:       make(chan struct{}),
	}
	t.Cleanup(func() {
		cancel()
		listener.Close()
	})
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go fixture.serve(ctx, connection)
		}
	}()
	return fixture
}

func (fixture *loopFixture) serve(ctx context.Context, connection net.Conn) {
	defer connection.Close()
	var request struct {
		ID     string `json:"id"`
		Method string `json:"method"`
	}
	if json.NewDecoder(connection).Decode(&request) != nil {
		return
	}
	encoder := json.NewEncoder(connection)
	switch request.Method {
	case "session.snapshot":
		number := fixture.snapshots.Add(1)
		active := fixture.activeSnapshots.Add(1)
		for {
			maximum := fixture.maxActiveSnapshots.Load()
			if active <= maximum || fixture.maxActiveSnapshots.CompareAndSwap(maximum, active) {
				break
			}
		}
		defer fixture.activeSnapshots.Add(-1)
		if number == fixture.blockSnapshot.Load() {
			fixture.readStartedOnce.Do(func() { close(fixture.readStarted) })
			select {
			case <-ctx.Done():
				return
			case <-fixture.releaseRead:
			}
		}
		_ = encoder.Encode(map[string]any{
			"id": request.ID, "result": map[string]any{"type": "session_snapshot", "snapshot": fixture.snapshot},
		})
	case "events.subscribe":
		fixture.subscriptions.Add(1)
		if encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"type": "subscription_started"}}) != nil {
			return
		}
		fixture.subscriptionReady <- struct{}{}
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-fixture.events:
				if event.drop {
					return
				}
				if encoder.Encode(map[string]any{"event": event.kind, "data": map[string]any{"type": event.kind}}) != nil {
					return
				}
				if event.written != nil {
					close(event.written)
				}
			}
		}
	}
}

func stableProjectorSnapshot() Snapshot {
	return Snapshot{
		Version:    "0.8.0",
		Protocol:   Protocol,
		Workspaces: []WorkspaceInfo{{ActiveTabID: "w1:t1", WorkspaceID: "w1", Number: 1, Label: "One"}},
		Tabs:       []TabInfo{{TabID: "w1:t1", WorkspaceID: "w1", Number: 1, Label: "1"}},
		Panes: []PaneInfo{{
			PaneID: "w1:p1", TabID: "w1:t1", TerminalID: "term-1", TerminalTitleStripped: "One", WorkspaceID: "w1",
		}},
	}
}

func ordinaryTitleSnapshot() Snapshot {
	return Snapshot{
		Version:    "0.8.0",
		Protocol:   Protocol,
		Workspaces: []WorkspaceInfo{{ActiveTabID: "w1:t1", WorkspaceID: "w1", Number: 1, Label: "One"}},
		Tabs:       []TabInfo{{TabID: "w1:t1", WorkspaceID: "w1", Number: 1, Label: "1"}},
		Panes: []PaneInfo{
			{PaneID: "w1:p1", TabID: "w1:t1", TerminalID: "term-1", TerminalTitleStripped: "Shell", WorkspaceID: "w1"},
			{PaneID: "w1:p2", TabID: "w1:t1", TerminalID: "term-2", TerminalTitleStripped: "Logs", WorkspaceID: "w1"},
		},
	}
}

func waitForProjectorState(t *testing.T, updates <-chan State, matches func(State) bool) State {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case state := <-updates:
			if matches(state) {
				return state
			}
		case <-timeout.C:
			t.Fatal("timed out waiting for projector state")
		}
	}
}

func waitSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitForCount(t *testing.T, value *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if value.Load() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("count = %d, want at least %d", value.Load(), want)
}

func serveNewPaneFixture(ctx context.Context, listener net.Listener, snapshots, subscriptions *atomic.Int32) {
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer connection.Close()
			var request struct {
				ID     string `json:"id"`
				Method string `json:"method"`
			}
			if json.NewDecoder(connection).Decode(&request) != nil {
				return
			}
			encoder := json.NewEncoder(connection)
			switch request.Method {
			case "session.snapshot":
				number := snapshots.Add(1)
				snapshot := Snapshot{Version: "0.8.0", Protocol: Protocol}
				if number > 1 {
					snapshot = stableProjectorSnapshot()
				}
				_ = encoder.Encode(map[string]any{
					"id": request.ID, "result": map[string]any{"type": "session_snapshot", "snapshot": snapshot},
				})
			case "events.subscribe":
				subscriptions.Add(1)
				_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"type": "subscription_started"}})
				<-ctx.Done()
			}
		}()
	}
}

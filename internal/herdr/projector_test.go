package herdr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
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

func TestDirtyLatchIsOneSlotAndNonblocking(t *testing.T) {
	dirty := make(chan struct{}, 1)
	latchDirty(dirty)
	latchDirty(dirty)

	select {
	case <-dirty:
	default:
		t.Fatal("dirty latch was empty after synchronous enqueue attempts")
	}
	select {
	case <-dirty:
		t.Fatal("dirty latch queued more than one signal")
	default:
	}

	latchDirty(dirty)
	select {
	case <-dirty:
	default:
		t.Fatal("dirty latch could not be reused after draining")
	}
}

func TestProjectorUsesOneReadAndOneDirtyFollowUpForEventDuringBlockedRead(t *testing.T) {
	fixture := newLoopFixture(t, stableProjectorSnapshot())
	fixture.blockSnapshot.Store(2)
	fixture.snapshotForRead = func(number int32) Snapshot {
		snapshot := stableProjectorSnapshot()
		snapshot.Workspaces[0].Label = fmt.Sprintf("read %d", number)
		return snapshot
	}

	projector := NewProjector(NewClient(fixture.socketPath, discardLogger()))
	updates, unsubscribe := projector.Subscribe()
	defer unsubscribe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan struct{})
	go func() {
		projector.Run(ctx)
		close(runDone)
	}()
	<-updates

	waitSignal(t, fixture.subscriptionReady, "subscription acknowledgement")
	waitSignal(t, fixture.readStarted, "post-subscription snapshot")
	fixture.events <- fixtureEvent{kind: "pane_updated"}
	close(fixture.releaseRead)

	live := waitForProjectorState(t, updates, func(state State) bool {
		return state.Connection == ConnectionLive && len(state.Snapshot.Workspaces) == 1 && state.Snapshot.Workspaces[0].Label == "read 3"
	})
	if !live.HasHome {
		t.Fatal("follow-up snapshot did not publish a complete Home")
	}
	if live.HerdrVersion != "0.9.0" {
		t.Fatalf("published Herdr version = %q, want 0.9.0", live.HerdrVersion)
	}
	cancel()
	waitSignal(t, runDone, "projector shutdown")
	if got := fixture.snapshots.Load(); got != 3 {
		t.Fatalf("one event caused %d snapshots, want setup, post-subscription, and one follow-up", got)
	}
	if got := fixture.maxActiveSnapshots.Load(); got != 1 {
		t.Fatalf("maximum active snapshot reads = %d, want 1", got)
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
	mu           sync.Mutex
	observations []snapshotObservation
}

type publishedSnapshotCheck struct {
	projector *Projector
	called    bool
	current   Snapshot
}

func (o *publishedSnapshotCheck) ObservePublishedSnapshot(snapshot Snapshot, _ bool) {
	o.called = true
	o.current = o.projector.Current().Snapshot
	if !reflect.DeepEqual(o.current, snapshot) {
		panic("published snapshot observer ran before current state was replaced")
	}
}

func (o *recordingSnapshotObserver) ObserveSnapshot(snapshot Snapshot, baseline bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.observations = append(o.observations, snapshotObservation{baseline: baseline, snapshot: snapshot})
}

func (o *recordingSnapshotObserver) all() []snapshotObservation {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]snapshotObservation(nil), o.observations...)
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
	observations := observer.all()
	if len(observations) != 2 || !observations[0].baseline || observations[1].baseline {
		t.Fatalf("observations = %+v", observations)
	}
}

func TestPublishedSnapshotObserverRunsAfterCurrentStatePublication(t *testing.T) {
	projector := NewProjector(nil)
	observer := &publishedSnapshotCheck{projector: projector}
	projector.SetPublishedSnapshotObserver(observer)
	snapshot := stableProjectorSnapshot()
	if !projector.publishLiveObserved(snapshot, true) {
		t.Fatal("valid snapshot was rejected")
	}
	if !observer.called || !reflect.DeepEqual(observer.current, snapshot) {
		t.Fatal("post-publication observer did not see the complete current snapshot")
	}
}

func TestProjectorObservesDottedStatusAndNewPaneBeforeSilentReplacementBaseline(t *testing.T) {
	socketPath := shortSocketPath(t)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancelServer := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancelServer()
		listener.Close()
	})
	triggerStatusAndNewPane := make(chan struct{})
	var snapshots atomic.Int32
	var subscriptions atomic.Int32
	go serveStatusResubscriptionBoundaryFixture(ctx, listener, triggerStatusAndNewPane, &snapshots, &subscriptions)

	projector := NewProjector(NewClient(socketPath, discardLogger()))
	observer := &recordingSnapshotObserver{}
	projector.SetSnapshotObserver(observer)
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	go projector.Run(runCtx)

	first := waitForSnapshotObservations(t, observer, 1)[0]
	if !first.baseline || len(first.snapshot.Panes) != 1 || len(first.snapshot.Agents) != 1 || first.snapshot.Agents[0].AgentStatus != StatusWorking {
		t.Fatalf("initial observation = %+v", first)
	}
	close(triggerStatusAndNewPane)
	observations := waitForSnapshotObservations(t, observer, 3)
	if observations[1].baseline || len(observations[1].snapshot.Workspaces) != 2 || len(observations[1].snapshot.Panes) != 2 ||
		len(observations[1].snapshot.Agents) != 1 || observations[1].snapshot.Agents[0].AgentStatus != StatusBlocked {
		t.Fatalf("confirmed pre-resubscription observation = %+v", observations[1])
	}
	if !observations[2].baseline || len(observations[2].snapshot.Workspaces) != 2 || len(observations[2].snapshot.Panes) != 2 ||
		len(observations[2].snapshot.Agents) != 1 || observations[2].snapshot.Agents[0].AgentStatus != StatusBlocked {
		t.Fatalf("post-resubscription baseline = %+v", observations[2])
	}
	if got := subscriptions.Load(); got != 2 {
		t.Fatalf("subscriptions = %d, want initial plus replacement", got)
	}
	if got := snapshots.Load(); got < 4 {
		t.Fatalf("snapshots = %d, want setup and post-subscription reads across replacement", got)
	}
}

func TestWorktreeEventRefreshesOnceWithoutGapOrRetry(t *testing.T) {
	fixture := newLoopFixture(t, stableProjectorSnapshot())
	projector := NewProjector(NewClient(fixture.socketPath, discardLogger()))
	updates, unsubscribe := projector.Subscribe()
	defer unsubscribe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go projector.Run(ctx)
	<-updates

	first := waitForProjectorState(t, updates, func(state State) bool { return state.Connection == ConnectionLive })
	fixture.events <- fixtureEvent{kind: "worktree_created"}
	waitForSnapshotCompletion(t, fixture.snapshotCompleted, 3)

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
	projector := NewProjector(NewClient(fixture.socketPath, discardLogger()))
	updates, unsubscribe := projector.Subscribe()
	defer unsubscribe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go projector.Run(ctx)
	<-updates
	waitForProjectorState(t, updates, func(state State) bool { return state.Connection == ConnectionLive })

	projector.RequestRefresh()
	waitForSnapshotCompletion(t, fixture.snapshotCompleted, 3)
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
	socketPath := shortSocketPath(t)
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

	projector := NewProjector(NewClient(socketPath, discardLogger()))
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
	projector := NewProjector(NewClient(fixture.socketPath, discardLogger()))
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
	drop bool
	kind string
}

type loopFixture struct {
	socketPath         string
	snapshotForRead    func(int32) Snapshot
	events             chan fixtureEvent
	subscriptionReady  chan struct{}
	snapshotCompleted  chan int32
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
	socketPath := shortSocketPath(t)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	fixture := &loopFixture{
		socketPath:        socketPath,
		snapshotForRead:   func(int32) Snapshot { return snapshot },
		events:            make(chan fixtureEvent),
		subscriptionReady: make(chan struct{}, 8),
		snapshotCompleted: make(chan int32, 16),
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
		snapshot, ok := fixture.prepareSnapshot(ctx, number)
		if !ok {
			return
		}
		if encoder.Encode(map[string]any{
			"id": request.ID, "result": map[string]any{"type": "session_snapshot", "snapshot": snapshot},
		}) == nil {
			fixture.snapshotCompleted <- number
		}
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
			}
		}
	}
}

func (fixture *loopFixture) prepareSnapshot(ctx context.Context, number int32) (Snapshot, bool) {
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
			return Snapshot{}, false
		case <-fixture.releaseRead:
		}
	}
	return fixture.snapshotForRead(number), true
}

func stableProjectorSnapshot() Snapshot {
	return Snapshot{
		Version:    "0.9.0",
		Protocol:   protocol22,
		Workspaces: []WorkspaceInfo{{ActiveTabID: "w1:t1", WorkspaceID: "w1", Number: 1, Label: "One"}},
		Tabs:       []TabInfo{{TabID: "w1:t1", WorkspaceID: "w1", Number: 1, Label: "1"}},
		Panes: []PaneInfo{{
			PaneID: "w1:p1", TabID: "w1:t1", TerminalID: "term-1", TerminalTitleStripped: "One", WorkspaceID: "w1",
		}},
	}
}

func ordinaryTitleSnapshot() Snapshot {
	return Snapshot{
		Version:    "0.9.0",
		Protocol:   protocol22,
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

func waitForSnapshotCompletion(t *testing.T, completed <-chan int32, want int32) {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case number := <-completed:
			if number == want {
				return
			}
			if number > want {
				t.Fatalf("snapshot %d completed before snapshot %d", number, want)
			}
		case <-timeout.C:
			t.Fatalf("timed out waiting for snapshot %d", want)
		}
	}
}

func waitForSnapshotObservations(t *testing.T, observer *recordingSnapshotObserver, want int) []snapshotObservation {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		observations := observer.all()
		if len(observations) >= want {
			return observations
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("observations = %d, want at least %d", len(observer.all()), want)
	return nil
}

func serveStatusResubscriptionBoundaryFixture(
	ctx context.Context,
	listener net.Listener,
	triggerStatusAndNewPane <-chan struct{},
	snapshots, subscriptions *atomic.Int32,
) {
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
				snapshot := stableProjectorSnapshot()
				snapshot.Agents = []AgentInfo{{
					AgentStatus: StatusWorking, PaneID: "w1:p1", TabID: "w1:t1", TerminalID: "term-1", WorkspaceID: "w1",
				}}
				if number > 2 {
					snapshot.Agents[0].AgentStatus = StatusBlocked
					snapshot.Agents[0].StateChangeSeq = 1
					snapshot.Workspaces = append(snapshot.Workspaces, WorkspaceInfo{
						ActiveTabID: "w2:t1", WorkspaceID: "w2", Number: 2, Label: "Temporary",
					})
					snapshot.Tabs = append(snapshot.Tabs, TabInfo{
						TabID: "w2:t1", WorkspaceID: "w2", Number: 1, Label: "1",
					})
					snapshot.Panes = append(snapshot.Panes, PaneInfo{
						PaneID: "w2:p1", TabID: "w2:t1", TerminalID: "term-2", TerminalTitleStripped: "Two", WorkspaceID: "w2",
					})
				}
				_ = encoder.Encode(map[string]any{
					"id": request.ID, "result": map[string]any{"type": "session_snapshot", "snapshot": snapshot},
				})
			case "events.subscribe":
				number := subscriptions.Add(1)
				if encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"type": "subscription_started"}}) != nil {
					return
				}
				if number == 1 {
					select {
					case <-ctx.Done():
						return
					case <-triggerStatusAndNewPane:
					}
					if encoder.Encode(map[string]any{"event": "pane.agent_status_changed", "data": map[string]any{"type": "pane.agent_status_changed"}}) != nil {
						return
					}
				}
				<-ctx.Done()
			}
		}()
	}
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
				snapshot := Snapshot{Version: "0.9.0", Protocol: protocol22}
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

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

func TestRepeatedTransportFailureDoesNotAdvanceOrRepublishGeneration(t *testing.T) {
	projector := NewProjector(nil)
	projector.state = State{
		Connection: ConnectionLive,
		Gap:        7,
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
	if current := projector.Current(); current.Gap != 8 {
		t.Fatalf("repeated failure advanced gap to %d", current.Gap)
	}
}

func TestProjectorDoesNotPublishPaneMissingFromSubscriptionBasis(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "herdr.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	var snapshots atomic.Int32
	var subscriptions atomic.Int32
	var connectionsMu sync.Mutex
	var connections []net.Conn
	defer func() {
		connectionsMu.Lock()
		defer connectionsMu.Unlock()
		for _, connection := range connections {
			connection.Close()
		}
	}()

	serverCtx, stopServer := context.WithCancel(context.Background())
	defer stopServer()
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			connectionsMu.Lock()
			connections = append(connections, connection)
			connectionsMu.Unlock()
			go serveReconciliationRace(serverCtx, connection, &snapshots, &subscriptions)
		}
	}()

	projector := NewProjector(NewClient(socketPath))
	updates, unsubscribe := projector.Subscribe()
	defer unsubscribe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go projector.Run(ctx)

	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case state := <-updates:
			if state.Connection != ConnectionLive {
				continue
			}
			if got := subscriptions.Load(); got < 2 {
				t.Fatalf("published live after %d subscription; want a retry covering the newly appeared pane", got)
			}
			if len(projectedTerminals(state.Home)) != 1 || projectedTerminals(state.Home)[0].Agent == nil {
				t.Fatalf("unexpected live projection: %+v", state.Home)
			}
			return
		case <-timeout.C:
			t.Fatal("projector did not publish a covered live snapshot")
		}
	}
}

func TestSubscriptionCoverageRejectsPaneAddedBetweenSnapshots(t *testing.T) {
	first := Snapshot{Panes: []PaneInfo{{PaneID: "w1:p1"}}}
	second := Snapshot{Panes: []PaneInfo{{PaneID: "w1:p1"}, {PaneID: "w1:p2"}}}
	if subscriptionCoversSnapshot(first, second) {
		t.Fatal("subscription basis unexpectedly covered a pane that appeared later")
	}
	if !subscriptionCoversSnapshot(second, first) {
		t.Fatal("subscriptions for a removed pane should not make the current snapshot incoherent")
	}
}

func TestProjectorStableHerdrDoesNotRepublishOrResubscribe(t *testing.T) {
	fixture := newProjectorFixture(t, true, nil, "")

	projector := NewProjector(NewClient(fixture.socketPath))
	updates, unsubscribe := projector.Subscribe()
	defer unsubscribe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go projector.Run(ctx)

	waitForProjectorState(t, updates, func(state State) bool { return state.Connection == ConnectionLive })
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for fixture.snapshots.Load() < 12 {
		select {
		case state := <-updates:
			t.Fatalf("stable Herdr unexpectedly published %+v", state)
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("projector handled only %d snapshots", fixture.snapshots.Load())
		}
	}
	if got := fixture.subscriptions.Load(); got != 1 {
		t.Fatalf("stable Herdr established %d subscriptions, want 1", got)
	}

	quiet := time.NewTimer(100 * time.Millisecond)
	defer quiet.Stop()
	select {
	case state := <-updates:
		t.Fatalf("unchanged Home unexpectedly published %+v", state)
	case <-quiet.C:
	}
}

func TestProjectorPublishesTruthfulDisconnectAndRecovery(t *testing.T) {
	dropFirstSubscription := make(chan struct{})
	fixture := newProjectorFixture(t, false, dropFirstSubscription, "")

	projector := NewProjector(NewClient(fixture.socketPath))
	updates, unsubscribe := projector.Subscribe()
	defer unsubscribe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go projector.Run(ctx)

	live := waitForProjectorState(t, updates, func(state State) bool { return state.Connection == ConnectionLive })
	close(dropFirstSubscription)
	reconnecting := waitForProjectorState(t, updates, func(state State) bool { return state.Connection == ConnectionReconnecting && state.LastKnown })
	if reconnecting.Gap != live.Gap+1 {
		t.Fatalf("reconnecting gap = %d, want %d", reconnecting.Gap, live.Gap+1)
	}
	recovered := waitForProjectorState(t, updates, func(state State) bool { return state.Connection == ConnectionLive })
	if recovered.LastKnown || recovered.Gap != reconnecting.Gap || !reflect.DeepEqual(recovered.Home, live.Home) {
		t.Fatalf("unexpected recovered state: %+v", recovered)
	}
	if got := fixture.subscriptions.Load(); got < 2 {
		t.Fatalf("recovery established %d subscriptions, want at least 2", got)
	}
}

func TestUnexpectedEventCannotCreateAHotReconnectLoop(t *testing.T) {
	fixture := newProjectorFixture(t, false, nil, "future_event")
	projector := NewProjector(NewClient(fixture.socketPath))
	updates, unsubscribe := projector.Subscribe()
	defer unsubscribe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go projector.Run(ctx)

	waitForProjectorState(t, updates, func(state State) bool { return state.Connection == ConnectionLive })
	waitForProjectorState(t, updates, func(state State) bool { return state.Connection == ConnectionReconnecting })
	time.Sleep(200 * time.Millisecond)
	if got := fixture.subscriptions.Load(); got != 1 {
		t.Fatalf("unexpected event established %d subscriptions before retry delay, want 1", got)
	}
}

func TestSemanticEventCoverageIsExplicit(t *testing.T) {
	for _, event := range []string{
		"workspace_created", "workspace_updated", "workspace_metadata_updated", "workspace_renamed", "workspace_moved", "workspace_reordered", "workspace_closed", "workspace_focused",
		"tab_created", "tab_closed", "tab_renamed", "tab_moved", "tab_focused",
		"pane_created", "pane_closed", "pane_updated", "pane_moved", "pane_exited", "pane_agent_detected", "pane_focused", "pane_agent_status_changed",
		"layout_updated",
	} {
		if !semanticEvent(event) {
			t.Errorf("confirmed event %q was not recognized", event)
		}
	}
	if semanticEvent("pane_output_changed") || semanticEvent("pane.agent_status_changed") || semanticEvent("unexpected") {
		t.Fatal("non-semantic or unknown events were accepted")
	}
}

type projectorFixture struct {
	socketPath            string
	snapshot              Snapshot
	snapshots             atomic.Int32
	subscriptions         atomic.Int32
	replayTransientPane   bool
	dropFirstSubscription chan struct{}
	firstEvent            string
}

func newProjectorFixture(t *testing.T, replayTransientPane bool, dropFirstSubscription chan struct{}, firstEvent string) *projectorFixture {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "herdr.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	fixture := &projectorFixture{
		socketPath:            socketPath,
		snapshot:              stableProjectorSnapshot(),
		replayTransientPane:   replayTransientPane,
		dropFirstSubscription: dropFirstSubscription,
		firstEvent:            firstEvent,
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

func stableProjectorSnapshot() Snapshot {
	agent := "codex"
	return Snapshot{
		Version:    "0.8.0",
		Protocol:   Protocol,
		Workspaces: []WorkspaceInfo{{ActiveTabID: "w1:t1", WorkspaceID: "w1", Number: 1, Label: "One"}},
		Tabs:       []TabInfo{{TabID: "w1:t1", WorkspaceID: "w1", Number: 1, Label: "1"}},
		Panes:      []PaneInfo{{PaneID: "w1:p1", TabID: "w1:t1", TerminalID: "term-1", WorkspaceID: "w1"}},
		Agents: []AgentInfo{{
			Agent: &agent, AgentStatus: StatusIdle, PaneID: "w1:p1", TabID: "w1:t1", TerminalID: "term-1", WorkspaceID: "w1",
		}},
	}
}

func (fixture *projectorFixture) serve(ctx context.Context, connection net.Conn) {
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
		fixture.snapshots.Add(1)
		_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"type": "session_snapshot", "snapshot": fixture.snapshot}})
	case "events.subscribe":
		number := fixture.subscriptions.Add(1)
		if encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"type": "subscription_started"}}) != nil {
			return
		}
		if fixture.firstEvent != "" {
			if encoder.Encode(map[string]any{"event": fixture.firstEvent, "data": map[string]any{"type": fixture.firstEvent}}) != nil {
				return
			}
			<-ctx.Done()
			return
		}
		if fixture.replayTransientPane {
			if encoder.Encode(map[string]any{"event": "pane_created", "data": map[string]any{"pane": map[string]any{"pane_id": "w1:closed", "workspace_id": "w1"}}}) != nil {
				return
			}
			if encoder.Encode(map[string]any{"event": "pane_agent_status_changed", "data": map[string]any{"agent_status": "idle", "pane_id": "w1:p1", "workspace_id": "w1"}}) != nil {
				return
			}
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if encoder.Encode(map[string]any{"event": "pane_updated", "data": map[string]any{"pane": map[string]any{"pane_id": "w1:p1", "workspace_id": "w1"}}}) != nil {
						return
					}
				}
			}
		}
		if number == 1 && fixture.dropFirstSubscription != nil {
			select {
			case <-ctx.Done():
			case <-fixture.dropFirstSubscription:
			}
			return
		}
		<-ctx.Done()
	}
}

func waitForProjectorState(t *testing.T, updates <-chan State, matches func(State) bool) State {
	t.Helper()
	timeout := time.NewTimer(4 * time.Second)
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

func serveReconciliationRace(ctx context.Context, connection net.Conn, snapshots, subscriptions *atomic.Int32) {
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
		snapshot := map[string]any{
			"version": "0.8.0", "protocol": Protocol,
			"workspaces": []any{}, "tabs": []any{}, "panes": []any{}, "layouts": []any{}, "agents": []any{},
		}
		if number > 1 {
			snapshot["workspaces"] = []any{map[string]any{"active_tab_id": "w1:t1", "workspace_id": "w1", "number": 1, "label": "One"}}
			snapshot["tabs"] = []any{map[string]any{"tab_id": "w1:t1", "workspace_id": "w1", "number": 1, "label": "1"}}
			snapshot["panes"] = []any{map[string]any{"pane_id": "w1:p1", "tab_id": "w1:t1", "terminal_id": "term-1", "workspace_id": "w1"}}
			snapshot["agents"] = []any{map[string]any{
				"agent_status": "idle", "pane_id": "w1:p1", "tab_id": "w1:t1", "terminal_id": "term-1", "workspace_id": "w1",
			}}
		}
		_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"type": "session_snapshot", "snapshot": snapshot}})
	case "events.subscribe":
		subscriptions.Add(1)
		_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"type": "subscription_started"}})
		<-ctx.Done()
	}
}

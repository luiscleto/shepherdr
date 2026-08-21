package herdr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTransportClosureIsNotAnIncompatibleHerdr(t *testing.T) {
	for _, err := range []error{io.EOF, io.ErrUnexpectedEOF, net.ErrClosed, fmt.Errorf("wrapped: %w", io.EOF)} {
		if !IsNotRunning(err) {
			t.Errorf("IsNotRunning(%v) = false", err)
		}
	}
}

func TestClientUsesSnapshotAndConfirmedSubscriptions(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "herdr.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverDone := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		var request map[string]any
		if err := json.NewDecoder(connection).Decode(&request); err != nil {
			serverDone <- err
			return
		}
		if request["method"] != "session.snapshot" {
			serverDone <- &testError{"first method was not session.snapshot"}
			return
		}
		id := request["id"].(string)
		response := map[string]any{"id": id, "result": map[string]any{
			"type": "session_snapshot",
			"snapshot": map[string]any{
				"version": "0.8.0", "protocol": Protocol,
				"workspaces": []any{}, "tabs": []any{}, "panes": []any{}, "layouts": []any{}, "agents": []any{},
			},
		}}
		serverDone <- json.NewEncoder(connection).Encode(response)
		connection.Close()
	}()

	client := NewClient(socketPath)
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Protocol != Protocol {
		t.Fatalf("protocol = %d, want %d", snapshot.Protocol, Protocol)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}

	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer connection.Close()
		var request struct {
			ID     string `json:"id"`
			Method string `json:"method"`
			Params struct {
				Subscriptions []map[string]any `json:"subscriptions"`
			} `json:"params"`
		}
		if err := json.NewDecoder(connection).Decode(&request); err != nil {
			serverDone <- err
			return
		}
		if request.Method != "events.subscribe" || !hasSubscription(request.Params.Subscriptions, "pane.agent_status_changed", "w1:p1") {
			serverDone <- &testError{"confirmed pane status subscription was missing"}
			return
		}
		for _, kind := range []string{
			"workspace.focused",
			"worktree.created",
			"worktree.opened",
			"worktree.removed",
			"tab.focused",
			"pane.focused",
			"layout.updated",
		} {
			if !hasSubscription(request.Params.Subscriptions, kind, "") {
				serverDone <- &testError{"confirmed semantic subscription was missing: " + kind}
				return
			}
		}
		encoder := json.NewEncoder(connection)
		if err := encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"type": "subscription_started"}}); err != nil {
			serverDone <- err
			return
		}
		if err := encoder.Encode(map[string]any{"event": "pane_agent_status_changed", "data": map[string]any{"type": "pane_agent_status_changed"}}); err != nil {
			serverDone <- err
			return
		}
		serverDone <- nil
	}()

	snapshot.Panes = []PaneInfo{{PaneID: "w1:p1", WorkspaceID: "w1"}}
	subscription, err := client.Subscribe(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	event, err := subscription.Next()
	if err != nil {
		t.Fatal(err)
	}
	if event.Event != "pane_agent_status_changed" {
		t.Fatalf("event = %q", event.Event)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestRealHerdrSnapshot(t *testing.T) {
	socketPath := os.Getenv("SHEPHERDR_REAL_HERDR_SOCKET")
	if socketPath == "" {
		t.Skip("set SHEPHERDR_REAL_HERDR_SOCKET for the bounded production-path check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snapshot, err := NewClient(socketPath).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	home, err := Project(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	projected := projectedTerminals(home)
	if len(projected) != len(snapshot.Panes) {
		t.Fatalf("projected %d terminals from %d canonical panes", len(projected), len(snapshot.Panes))
	}
	canonical := make(map[string]PaneInfo, len(snapshot.Panes))
	for _, pane := range snapshot.Panes {
		canonical[pane.PaneID] = pane
	}
	seen := make(map[string]int, len(projected))
	decorated := 0
	for _, terminal := range projected {
		seen[terminal.PaneID]++
		pane, exists := canonical[terminal.PaneID]
		if !exists || pane.TerminalID != terminal.TerminalID {
			t.Fatalf("projected terminal does not exactly match a canonical pane: %+v", terminal)
		}
		if terminal.Agent != nil {
			decorated++
		}
	}
	for paneID := range canonical {
		if seen[paneID] != 1 {
			t.Fatalf("canonical pane %q appeared %d times", paneID, seen[paneID])
		}
	}
	if decorated != len(snapshot.Agents) {
		t.Fatalf("projected %d agent decorations from %d exact agent records", decorated, len(snapshot.Agents))
	}
	t.Logf("Herdr %s protocol %d: %d workspaces, %d tabs, %d panes exactly once, %d agents, %d working, %d blocked", snapshot.Version, snapshot.Protocol, len(snapshot.Workspaces), len(snapshot.Tabs), len(snapshot.Panes), len(snapshot.Agents), home.WorkingCount, home.BlockedCount)
	if helperAgent, helperOrdinary := findProjectedTerminal(home, "w2S:p1"), findProjectedTerminal(home, "w2S:p2"); helperAgent != nil || helperOrdinary != nil {
		if helperAgent == nil || helperAgent.Agent == nil || helperOrdinary == nil || helperOrdinary.Agent != nil {
			t.Fatalf("unexpected helper workspace projection: p1=%+v p2=%+v", helperAgent, helperOrdinary)
		}
		t.Logf("helper workspace includes agent pane %s and ordinary pane %s", helperAgent.PaneID, helperOrdinary.PaneID)
	}
}

func hasSubscription(subscriptions []map[string]any, kind, paneID string) bool {
	for _, subscription := range subscriptions {
		if subscription["type"] == kind && (paneID == "" || subscription["pane_id"] == paneID) {
			return true
		}
	}
	return false
}

type testError struct{ message string }

func (e *testError) Error() string { return e.message }

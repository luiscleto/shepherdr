package herdr

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// testCore answers one small, fixed set of requests from a captured read. It
// stands in for the core only to check Shepherdr's side of the protocol.
type testCore struct {
	hello    map[string]any
	read     CoreRead
	events   chan string
	mu       sync.Mutex
	requests []string
}

func newTestCore(t *testing.T) (*testCore, string) {
	t.Helper()
	read := loadCoreRead(t, "core-read-app.json")
	core := &testCore{
		hello: map[string]any{
			"core": map[string]any{"id": read.CoreID, "name": "host"}, "features": []string{"framed", "attach", "events", "status", "shell"},
			"framed": false, "generation": 1, "version": "0.1.15",
			"methods": []string{"hello", "server.status", "terminal.list", "terminal.attach", "terminal.keys", "doc.get", "doc.set", "events.subscribe"},
		},
		read:   read,
		events: make(chan string, 16),
	}
	path := shortSocketPath(t)
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go core.serve(conn)
		}
	}()
	return core, path
}

func (core *testCore) serve(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		var request struct {
			ID     uint64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			return
		}
		core.mu.Lock()
		core.requests = append(core.requests, request.Method+" "+string(request.Params))
		hello := core.hello
		read := core.read
		core.mu.Unlock()
		var result any
		switch request.Method {
		case "hello":
			result = hello
		case "terminal.list":
			result = map[string]any{"hidden_finished": 0, "terminals": read.Terminals}
		case "doc.get":
			result = map[string]any{"value": read.Document, "version": 7}
		case "events.subscribe":
			result = map[string]any{"channel": nil, "seq": 3, "terminals": []any{}}
		}
		if result == nil {
			_ = json.NewEncoder(conn).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32601, "message": "unknown method", "data": map[string]any{"kind": "unknown_method"}}})
			continue
		}
		_ = json.NewEncoder(conn).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		if request.Method == "events.subscribe" {
			for line := range core.events {
				if _, err := conn.Write([]byte(line + "\n")); err != nil {
					return
				}
			}
			return
		}
	}
}

func (core *testCore) set(change func(*testCore)) {
	core.mu.Lock()
	change(core)
	core.mu.Unlock()
}

func (core *testCore) methods() []string {
	core.mu.Lock()
	defer core.mu.Unlock()
	return append([]string(nil), core.requests...)
}

func TestCoreClientReadsOnceWithOnlyItsDocumentedMethods(t *testing.T) {
	core, path := newTestCore(t)
	read, err := NewCoreClient(path, discardLogger()).Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if read.CoreID != core.read.CoreID || read.Version != "0.1.15" || len(read.Terminals) != 7 || !json.Valid(read.Document) {
		t.Fatalf("read = %+v", read)
	}
	want := []string{`hello {"generation":1}`, `terminal.list {}`, `doc.get {"name":"herdr-app"}`}
	if got := core.methods(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests = %q", got)
	}
}

func TestCoreClientGate(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(map[string]any)
		detail string
	}{
		{name: "missing method", change: func(hello map[string]any) {
			hello["methods"] = []string{"hello", "terminal.list", "events.subscribe"}
		}, detail: "This Herdr Core can't serve Shepherdr: it doesn't offer doc.get."},
		{name: "missing feature", change: func(hello map[string]any) {
			hello["features"] = []string{"framed", "attach"}
		}, detail: "This Herdr Core can't serve Shepherdr: it doesn't offer events."},
		{name: "other generation", change: func(hello map[string]any) {
			hello["generation"] = 2
		}, detail: "This Herdr Core can't serve Shepherdr: it doesn't offer protocol generation 1."},
		{name: "older", change: func(hello map[string]any) {
			hello["version"] = "0.1.14"
		}, detail: "Herdr Core 0.1.14 is older than the supported range; Shepherdr supports Herdr Core 0.1.15 through 0.1.15."},
	} {
		t.Run(test.name, func(t *testing.T) {
			core, path := newTestCore(t)
			core.set(func(core *testCore) { test.change(core.hello) })
			_, err := NewCoreClient(path, discardLogger()).Read(context.Background())
			var compatibility *CoreCompatibilityError
			if !errors.As(err, &compatibility) || compatibility.Detail != test.detail {
				t.Fatalf("err = %v", err)
			}
			if methods := core.methods(); len(methods) != 1 {
				t.Fatalf("read continued past the gate: %q", methods)
			}
		})
	}
}

func TestCoreClientWarnsOnceForANewerCore(t *testing.T) {
	core, path := newTestCore(t)
	core.set(func(core *testCore) { core.hello["version"] = "0.1.16" })
	var logs bytes.Buffer
	client := NewCoreClient(path, slog.New(slog.NewTextHandler(&logs, nil)))
	for range 2 {
		if _, err := client.Read(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Count(logs.String(), "Herdr Core 0.1.16 is outside Shepherdr's supported Herdr Core range 0.1.15 through 0.1.15; continuing best-effort") != 1 {
		t.Fatalf("logs = %q", logs.String())
	}
}

func TestCoreSubscriptionMarksOnlyChangesThatCanMoveHome(t *testing.T) {
	core, path := newTestCore(t)
	subscription, err := NewCoreClient(path, discardLogger()).Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	data, err := os.ReadFile("testdata/core-events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	lines = append(lines, `{"jsonrpc":"2.0","method":"missed","params":{"seq":90}}`)
	for _, line := range lines {
		core.events <- line
	}
	for _, line := range lines {
		var message struct {
			Method string `json:"method"`
			Params struct {
				Type string `json:"type"`
			} `json:"params"`
		}
		_ = json.Unmarshal([]byte(line), &message)
		changes, err := subscription.Next()
		if err != nil {
			t.Fatal(err)
		}
		kind := message.Params.Type
		if kind == "" {
			kind = message.Method
		}
		want := map[string]bool{
			"heartbeat": false, "size": false, "foreground": false, "turn": false,
			"status": true, "labels": true, "title": true, "doc": true, "lost": true, "restarted": true, "missed": true,
		}[kind]
		if changes != want {
			t.Fatalf("%s: changes = %t", kind, changes)
		}
	}
	close(core.events)
	if _, err := subscription.Next(); err == nil {
		t.Fatal("a closed event connection was not reported")
	}
}

func TestCoreProjectorPublishesLiveHomeAndRereadsAfterAnEvent(t *testing.T) {
	core, path := newTestCore(t)
	projector := NewCoreProjector(NewCoreClient(path, discardLogger()), "/home/user")
	updates, unsubscribe := projector.Subscribe()
	defer unsubscribe()
	if initial := <-updates; initial.Connection != ConnectionReconnecting {
		t.Fatalf("initial = %+v", initial)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go projector.Run(ctx)

	live := waitForState(t, updates, func(state State) bool { return state.Connection == ConnectionLive })
	if live.HerdrVersion != "0.1.15" || len(live.Home.Workspaces) != 4 || live.Home.Workspaces[0].Label != "alpha" {
		t.Fatalf("live = %+v", live)
	}
	core.set(func(core *testCore) {
		document := strings.Replace(string(core.read.Document), `"name": "alpha"`, `"name": "beta"`, 1)
		document = strings.Replace(document, `"name":"alpha"`, `"name":"beta"`, 1)
		core.read.Document = json.RawMessage(document)
	})
	core.events <- `{"jsonrpc":"2.0","method":"event","params":{"data":{"name":"herdr-app","version":8},"seq":9,"type":"doc"}}`
	renamed := waitForState(t, updates, func(state State) bool {
		return len(state.Home.Workspaces) > 0 && state.Home.Workspaces[0].Label == "beta"
	})
	if renamed.Connection != ConnectionLive || renamed.Gap != live.Gap {
		t.Fatalf("renamed = %+v", renamed)
	}
	close(core.events)
	waitForState(t, updates, func(state State) bool { return state.Connection == ConnectionReconnecting && state.LastKnown })
}

func waitForState(t *testing.T, updates <-chan State, match func(State) bool) State {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case state := <-updates:
			if match(state) {
				return state
			}
		case <-timeout:
			t.Fatal("timed out waiting for Home")
		}
	}
}

func TestRealHerdrCoreRead(t *testing.T) {
	socketPath := os.Getenv("SHEPHERDR_REAL_HERDR_CORE_SOCKET")
	if socketPath == "" {
		t.Skip("set SHEPHERDR_REAL_HERDR_CORE_SOCKET for the bounded production-path check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	read, err := NewCoreClient(socketPath, discardLogger()).Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	_, projected, err := ProjectCore(read, home)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]int)
	for _, terminal := range projectedTerminals(projected) {
		seen[terminal.PaneID]++
	}
	local := 0
	for _, terminal := range read.Terminals {
		if strings.Contains(terminal.Address, "/") {
			continue
		}
		local++
		if seen[terminal.Address] != 1 {
			t.Fatalf("live terminal %s appeared %d times", terminal.Address, seen[terminal.Address])
		}
	}
	if len(seen) != local {
		t.Fatalf("Home shows %d terminals for %d live local terminals", len(seen), local)
	}
	t.Logf("Herdr Core %s: %d spaces and groups, %d live terminals exactly once, %d working, %d blocked, %d error", read.Version, len(projected.Workspaces), local, projected.WorkingCount, projected.BlockedCount, projected.ErrorCount)
}

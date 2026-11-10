package herdr

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
			"methods": []string{"hello", "server.status", "terminal.list", "terminal.attach", "terminal.keys", "terminal.kill", "terminal.run", "doc.get", "doc.set", "events.subscribe"},
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
			var params struct {
				Terminal string `json:"terminal"`
			}
			_ = json.Unmarshal(request.Params, &params)
			terminals := []CoreTerminal{}
			for _, terminal := range read.Terminals {
				if params.Terminal != "" && terminal.Address == params.Terminal {
					terminals = append(terminals, terminal)
				}
			}
			result = map[string]any{"channel": nil, "seq": 3, "terminals": terminals}
		case "terminal.run":
			// Core's own replies (herdr-core 0.1.15): a missing folder is
			// refused; otherwise the new terminal's record.
			var params struct {
				CWD string `json:"cwd"`
			}
			_ = json.Unmarshal(request.Params, &params)
			if strings.HasSuffix(params.CWD, "/gone") {
				_, _ = conn.Write([]byte(`{"error":{"code":-32000,"data":{"hint":"check the program name and cwd","kind":"spawn_failed"},"message":"cannot start \"/bin/bash\": No such file or directory (os error 2)"},"id":` + fmt.Sprint(request.ID) + `,"jsonrpc":"2.0"}` + "\n"))
				continue
			}
			result = json.RawMessage(`{"address":"t6","argv":["/bin/bash"],"cols":200,"created_ms":1793482795436,"cwd":"/tmp/w3/work/repo/sub","die_with_parent":false,"exit_code":null,"finished_ms":null,"identity":"ghostty","keep":true,"labels":{"herdr.space":"sp034c04caa6b61d2a58e5a07df15f9f97","herdr.tab":"tb17a00994450ff571460d347ac9acf657"},"name":null,"notify":true,"parent":null,"pid":786933,"reason":null,"reported":false,"rm":false,"rows":120,"screen":"exact","signal":null,"state":"live","status":null,"status_ms":null,"template":null,"title":"","turn":0}`)
		case "terminal.kill":
			var params struct {
				Terminal string `json:"terminal"`
			}
			_ = json.Unmarshal(request.Params, &params)
			for _, terminal := range read.Terminals {
				if terminal.Address == params.Terminal {
					result = map[string]any{"address": terminal.Address, "state": "finished", "reason": "killed", "signal": 1}
				}
			}
			if result == nil {
				_ = json.NewEncoder(conn).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32000, "message": "not_found", "data": map[string]any{"kind": "not_found"}}})
				continue
			}
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
		}, detail: "Herdr Core 0.1.14 is older than the supported range; Shepherdr supports Herdr Core 0.1.15 through 0.1.20."},
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
	core.set(func(core *testCore) { core.hello["version"] = "0.1.21" })
	var logs bytes.Buffer
	client := NewCoreClient(path, slog.New(slog.NewTextHandler(&logs, nil)))
	for range 2 {
		if _, err := client.Read(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Count(logs.String(), "Herdr Core 0.1.21 is outside Shepherdr's supported Herdr Core range 0.1.15 through 0.1.20; continuing best-effort") != 1 {
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
			"heartbeat": false, "size": false, "foreground": false,
			"turn": true, "status": true, "labels": true, "title": true, "doc": true, "lost": true, "restarted": true, "missed": true,
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

// heldWindow answers each window check only when the test says, so a check
// can hang for as long as the test needs.
type heldWindow struct {
	asked   chan struct{}
	answers chan bool
}

func (w *heldWindow) WindowOpen(ctx context.Context) bool {
	select {
	case w.asked <- struct{}{}:
	case <-ctx.Done():
		return false
	}
	select {
	case open := <-w.answers:
		return open
	case <-ctx.Done():
		return false
	}
}

func TestCoreProjectorKeepsHomeLiveWhileTheAppWindowHangs(t *testing.T) {
	core, path := newTestCore(t)
	window := &heldWindow{asked: make(chan struct{}), answers: make(chan bool)}
	projector := NewCoreProjector(NewCoreClient(path, discardLogger()), "/home/user")
	projector.SetAppWindow(window)
	updates, unsubscribe := projector.Subscribe()
	defer unsubscribe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go projector.Run(ctx)

	appActions := func(state State) bool { return len(state.Home.Actions) > 0 }
	<-window.asked
	window.answers <- true
	waitForState(t, updates, func(state State) bool { return state.Connection == ConnectionLive && appActions(state) })

	// The next check hangs. Core changes still show at once, with the last
	// answer kept.
	projector.RequestRefresh()
	<-window.asked
	core.set(func(core *testCore) {
		document := strings.Replace(string(core.read.Document), `"name": "alpha"`, `"name": "beta"`, 1)
		document = strings.Replace(document, `"name":"alpha"`, `"name":"beta"`, 1)
		core.read.Document = json.RawMessage(document)
	})
	started := time.Now()
	core.events <- `{"jsonrpc":"2.0","method":"event","params":{"data":{"name":"herdr-app","version":8},"seq":9,"type":"doc"}}`
	renamed := waitForState(t, updates, func(state State) bool {
		return len(state.Home.Workspaces) > 0 && state.Home.Workspaces[0].Label == "beta"
	})
	if elapsed := time.Since(started); elapsed > time.Second || !appActions(renamed) {
		t.Fatalf("a core change behind a hung window took %s, actions %v", elapsed, renamed.Home.Actions)
	}

	// The hung check ends as closed: Home says so, and Rename space goes. A
	// later answer brings it back.
	window.answers <- false
	closed := waitForState(t, updates, func(state State) bool { return state.Home.AppWindow == "closed" })
	if !appActions(closed) || len(closed.Home.Workspaces[0].Actions) != 2 {
		t.Fatalf("closed = %+v", closed.Home)
	}
	projector.RequestRefresh()
	<-window.asked
	window.answers <- true
	back := waitForState(t, updates, func(state State) bool { return state.Home.AppWindow == "" })
	if back.Connection != ConnectionLive || back.Home.Workspaces[0].Label != "beta" || len(back.Home.Workspaces[0].Actions) != 3 {
		t.Fatalf("back = %+v", back)
	}
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

func TestCoreClientKillsOneExactAddress(t *testing.T) {
	core, path := newTestCore(t)
	client := NewCoreClient(path, discardLogger())
	state, err := client.Kill(context.Background(), "t4")
	if err != nil || state != "finished" {
		t.Fatalf("kill = %q, %v", state, err)
	}
	want := []string{`hello {"generation":1}`, `terminal.kill {"terminal":"t4"}`}
	if got := core.methods(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests = %q", got)
	}

	_, err = client.Kill(context.Background(), "t99")
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.Code != "not_found" || MutationMayHaveRun(err) {
		t.Fatalf("unknown address: %v", err)
	}
	// Another machine's terminal, or nothing, is never sent.
	for _, address := range []string{"", "box/t4"} {
		if _, err := client.Kill(context.Background(), address); err == nil || MutationMayHaveRun(err) {
			t.Fatalf("kill %q = %v", address, err)
		}
	}
	if len(core.methods()) != 4 {
		t.Fatalf("invalid targets reached core: %q", core.methods())
	}

	// No core: nothing was sent.
	_, err = NewCoreClient(path+".missing", discardLogger()).Kill(context.Background(), "t4")
	if err == nil || MutationMayHaveRun(err) {
		t.Fatalf("missing core: %v", err)
	}
}

func TestCoreClientRunsOneLabelledShell(t *testing.T) {
	core, path := newTestCore(t)
	client := NewCoreClient(path, discardLogger())
	address, err := client.Run(context.Background(), "/tmp/w3/work/repo/sub", "sp034c04caa6b61d2a58e5a07df15f9f97", "tb17a00994450ff571460d347ac9acf657")
	if err != nil || address != "t6" {
		t.Fatalf("run = %q, %v", address, err)
	}
	// Core's own shell, kept like every app terminal: no argv, name, env or
	// parent, and nothing seen yet, as a Herdr client starts one.
	want := []string{`hello {"generation":1}`, `terminal.run {"cwd":"/tmp/w3/work/repo/sub","keep":true,"labels":{"herdr.seen":"0","herdr.space":"sp034c04caa6b61d2a58e5a07df15f9f97","herdr.tab":"tb17a00994450ff571460d347ac9acf657"}}`}
	if got := core.methods(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests = %q", got)
	}

	// Core's refusal is a refusal, with its message.
	_, err = client.Run(context.Background(), "/tmp/w3/gone", "sp1", "tb1")
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.Code != "spawn_failed" || !strings.Contains(apiError.Message, "No such file") || MutationMayHaveRun(err) {
		t.Fatalf("missing folder: %v", err)
	}

	// No core: nothing was sent.
	_, err = NewCoreClient(path+".missing", discardLogger()).Run(context.Background(), "/tmp", "sp1", "tb1")
	if err == nil || MutationMayHaveRun(err) {
		t.Fatalf("missing core: %v", err)
	}
}

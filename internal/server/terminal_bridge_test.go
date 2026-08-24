package server

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"
	"github.com/luiscleto/shepherdr/internal/herdr"
)

type fakeTerminalStateSource struct {
	listeners map[chan herdr.State]struct{}
	mutex     sync.Mutex
	state     herdr.State
}

func newFakeTerminalStateSource(state herdr.State) *fakeTerminalStateSource {
	return &fakeTerminalStateSource{listeners: make(map[chan herdr.State]struct{}), state: state}
}

func (source *fakeTerminalStateSource) Current() herdr.State {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	return source.state
}

func (source *fakeTerminalStateSource) Subscribe() (<-chan herdr.State, func()) {
	updates := make(chan herdr.State, 1)
	source.mutex.Lock()
	source.listeners[updates] = struct{}{}
	updates <- source.state
	source.mutex.Unlock()
	return updates, func() {
		source.mutex.Lock()
		if _, ok := source.listeners[updates]; ok {
			delete(source.listeners, updates)
			close(updates)
		}
		source.mutex.Unlock()
	}
}

func (source *fakeTerminalStateSource) publish(state herdr.State) {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	source.state = state
	for listener := range source.listeners {
		select {
		case listener <- state:
		default:
			<-listener
			listener <- state
		}
	}
}

func liveTerminalState(pane, terminal string, gap uint64) herdr.State {
	return herdr.State{
		Connection: herdr.ConnectionLive,
		Gap:        gap,
		Home:       herdr.Home{Workspaces: []herdr.Workspace{}},
		Snapshot: herdr.Snapshot{
			Layouts: []herdr.LayoutInfo{{Panes: []herdr.LayoutPane{{PaneID: pane, Rect: herdr.Rectangle{Width: 116, Height: 38}}}}},
			Panes:   []herdr.PaneInfo{{PaneID: pane, TerminalID: terminal}},
		},
	}
}

func testTerminalBridge(source TerminalStateSource) *TerminalBridge {
	bridge := NewTerminalBridge("helper", "/tmp/herdr-test.sock", slog.New(slog.NewTextHandler(io.Discard, nil)), source)
	bridge.command = terminalHelperCommand
	return bridge
}

func terminalHelperCommand(ctx context.Context, arguments ...string) *exec.Cmd {
	helperArguments := []string{"-test.run=TestTerminalBridgeHelperProcess", "--", "terminal-helper"}
	helperArguments = append(helperArguments, arguments...)
	return exec.CommandContext(ctx, os.Args[0], helperArguments...)
}

func TestTerminalBridgeHelperProcess(t *testing.T) {
	marker := slices.Index(os.Args, "terminal-helper")
	if marker < 0 {
		return
	}
	arguments := os.Args[marker+1:]
	if slices.Contains(arguments, "session") {
		target := arguments[3]
		if target == "term-bad" {
			fmt.Println(`{"bytes":"","encoding":"ansi","full":false,"height":24,"seq":1,"type":"terminal.frame","width":80}`)
		} else if target == "term-occupied" {
			fmt.Println(`{"reason":"another terminal already has an attached client","type":"terminal.closed"}`)
		} else {
			bytes := base64.StdEncoding.EncodeToString([]byte("full frame"))
			fmt.Printf(`{"bytes":%q,"encoding":"ansi","full":true,"height":24,"seq":1,"type":"terminal.frame","width":80}`+"\n", bytes)
		}
		if target == "term-send" {
			scanner := bufio.NewScanner(os.Stdin)
			var received []string
			for scanner.Scan() {
				received = append(received, scanner.Text())
				if strings.Contains(scanner.Text(), `"type":"terminal.release"`) {
					for _, argument := range arguments {
						if strings.HasPrefix(argument, "--record=") {
							_ = os.WriteFile(strings.TrimPrefix(argument, "--record="), []byte(strings.Join(received, "\n")), 0o600)
						}
					}
					return
				}
			}
			return
		}
		if target == "term-occupied" {
			return
		}
		time.Sleep(time.Hour)
		return
	}
	if slices.Contains(arguments, "read") && len(arguments) > 2 && arguments[2] == "pane-block" {
		time.Sleep(time.Hour)
		fmt.Print("replacement output must not escape")
		return
	}
	fmt.Print("history")
}

type failingBatchWriter struct {
	calls   int
	failAt  int
	partial bool
}

func (writer *failingBatchWriter) Write(data []byte) (int, error) {
	writer.calls++
	if writer.calls == writer.failAt {
		if writer.partial {
			return min(1, len(data)), io.ErrClosedPipe
		}
		return 0, io.ErrClosedPipe
	}
	return len(data), nil
}

func TestSharedTerminalBatchClassifiesPreForwardAndPostForwardFailure(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/terminal/files", nil)
	commands := []any{map[string]string{"type": "terminal.input", "text": "paste"}, map[string]string{"type": "terminal.input", "text": "\r"}}
	first := &failingBatchWriter{failAt: 1}
	outcome, err := forwardTerminalBatch(request, nil, first, commands, nil)
	if outcome != terminalBatchNotSent || err == nil {
		t.Fatalf("zero-byte first write failure = %d, %v", outcome, err)
	}
	partial := &failingBatchWriter{failAt: 1, partial: true}
	outcome, err = forwardTerminalBatch(request, nil, partial, commands, nil)
	if outcome != terminalBatchUnknown || err == nil {
		t.Fatalf("partial first write failure = %d, %v", outcome, err)
	}
	second := &failingBatchWriter{failAt: 2}
	outcome, err = forwardTerminalBatch(request, nil, second, commands, nil)
	if outcome != terminalBatchUnknown || err == nil {
		t.Fatalf("second write failure = %d, %v", outcome, err)
	}
	success := &failingBatchWriter{}
	outcome, err = forwardTerminalBatch(request, nil, success, commands, nil)
	if outcome != terminalBatchForwarded || err != nil || success.calls != 2 {
		t.Fatalf("successful batch = %d, %v, calls %d", outcome, err, success.calls)
	}
}

func TestShortTerminalBatchUsesExactTargetAndReportsOccupied(t *testing.T) {
	source := newFakeTerminalStateSource(liveTerminalState("pane-1", "term-send", 1))
	bridge := testTerminalBridge(source)
	defer bridge.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/terminal/files", nil)
	outcome, err := bridge.SendBatch(request, "pane-1", "term-send", false, func() error { return nil }, []string{"paste", "\r"})
	if outcome != terminalBatchForwarded || err != nil {
		t.Fatalf("send outcome = %d, %v", outcome, err)
	}

	source.publish(liveTerminalState("pane-1", "term-occupied", 1))
	outcome, err = bridge.SendBatch(request, "pane-1", "term-occupied", false, func() error { return nil }, []string{"paste", "\r"})
	if outcome != terminalBatchOccupied || err == nil {
		t.Fatalf("occupied outcome = %d, %v", outcome, err)
	}
}

func TestTerminalTargetLeaseFencesIdentityAndConnectionGeneration(t *testing.T) {
	source := newFakeTerminalStateSource(liveTerminalState("pane-1", "term-1", 4))
	bridge := testTerminalBridge(source)
	defer bridge.Close()

	lease, ok := bridge.acquireTarget("pane-1", "term-1")
	if !ok || !lease.Valid() {
		t.Fatal("current target did not acquire a valid lease")
	}
	source.publish(liveTerminalState("pane-1", "term-2", 4))
	select {
	case <-lease.done:
	case <-time.After(time.Second):
		t.Fatal("terminal replacement did not close its old lease")
	}
	if lease.Valid() {
		t.Fatal("replaced terminal lease remained valid")
	}
	lease.Close()

	source.publish(liveTerminalState("pane-1", "term-1", 5))
	lease, ok = bridge.acquireTarget("pane-1", "term-1")
	if !ok {
		t.Fatal("new connection generation did not acquire")
	}
	source.publish(liveTerminalState("pane-1", "term-1", 6))
	select {
	case <-lease.done:
	case <-time.After(time.Second):
		t.Fatal("connection generation change did not close its old lease")
	}
	lease.Close()
}

func TestTerminalCLIArgumentsKeepTargetsInert(t *testing.T) {
	settings := terminalBridgeSettings{cols: 80, mode: "takeover", pane: "--pane-evil", rows: 24, takeover: true}
	if validTerminalIdentity(settings.pane) || validTerminalIdentity("--help") || validTerminalIdentity("pane 1") {
		t.Fatal("option-like or whitespace targets passed inert target validation")
	}
	arguments := terminalSessionArguments(settings, "term_123abc")
	if arguments[3] != "term_123abc" {
		t.Fatalf("session target argument = %q", arguments)
	}
	if slices.Contains(arguments, settings.pane) {
		t.Fatalf("session command used mutable pane ID: %q", arguments)
	}
	read := terminalReadArguments(terminalReadSettings{lines: 20, pane: "w4B:p1", source: "recent-unwrapped"})
	if read[2] != "w4B:p1" {
		t.Fatalf("read target argument = %q", read)
	}
}

func terminalFrame(full bool, sequence uint64) []byte {
	value, _ := json.Marshal(map[string]any{
		"bytes": base64.StdEncoding.EncodeToString([]byte("output")), "encoding": "ansi", "full": full,
		"height": 24, "seq": sequence, "type": "terminal.frame", "width": 80,
	})
	return value
}

func TestTerminalFrameValidatorRequiresFullFirstAndIncreasingSequence(t *testing.T) {
	validator := terminalFrameValidator{}
	if _, err := validator.Accept(terminalFrame(false, 1)); err == nil {
		t.Fatal("partial first frame was accepted")
	}
	validator = terminalFrameValidator{}
	if _, err := validator.Accept(terminalFrame(true, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := validator.Accept(terminalFrame(false, 2)); err != nil {
		t.Fatal(err)
	}
	for name, frame := range map[string][]byte{
		"duplicate": terminalFrame(false, 2),
		"older":     terminalFrame(false, 1),
		"malformed": []byte(`{"type":"terminal.frame","seq":3,"full":false,"encoding":"ansi","bytes":"!","width":80,"height":24}`),
		"unknown":   []byte(`{"type":"terminal.frame","seq":3,"full":false,"encoding":"ansi","bytes":"","width":80,"height":24,"extra":true}`),
	} {
		if _, err := validator.Accept(frame); err == nil {
			t.Errorf("%s frame was accepted", name)
		}
	}
	reason, err := validator.Accept([]byte(`{"reason":"already attached","type":"terminal.closed"}`))
	if err != nil || reason != "already attached" {
		t.Fatalf("terminal closure = %q, %v", reason, err)
	}

	gap := terminalFrameValidator{}
	if _, err := gap.Accept(terminalFrame(true, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := gap.Accept(terminalFrame(false, 3)); err == nil {
		t.Fatal("delta frame after a missing sequence was accepted")
	}
	if _, err := gap.Accept(terminalFrame(true, 3)); err != nil {
		t.Fatalf("full frame did not reset after a sequence gap: %v", err)
	}
	if _, err := gap.Accept(terminalFrame(false, 4)); err != nil {
		t.Fatalf("ordered delta after full reset failed: %v", err)
	}
}

func TestTerminalInputBatchPreservesAcknowledgementIDAndRejectsExtraFields(t *testing.T) {
	command, err := validatedTerminalCommand([]byte(`{"type":"terminal.input-batch","chunks":["paste","\\r"],"request_id":17}`))
	if err != nil {
		t.Fatal(err)
	}
	if command.requestID != 17 || len(command.childCommands) != 2 {
		t.Fatalf("validated batch = %#v", command)
	}
	if _, err := validatedTerminalCommand([]byte(`{"type":"terminal.release","text":"surprise"}`)); err == nil {
		t.Fatal("release with hostile extra field was accepted")
	}
}

func TestProductionObserverClosesBeforeReplacementOutput(t *testing.T) {
	source := newFakeTerminalStateSource(liveTerminalState("pane-1", "term-1", 1))
	bridge := testTerminalBridge(source)
	defer bridge.Close()
	server := httptest.NewServer(New(fstest.MapFS{}, nil, bridge, false).Handler())
	defer server.Close()

	endpoint, _ := url.Parse(server.URL)
	endpoint.Scheme = "ws"
	endpoint.Path = "/api/terminal"
	endpoint.RawQuery = url.Values{
		"pane": {"pane-1"}, "terminal": {"term-1"}, "mode": {"observe"}, "cols": {"80"}, "rows": {"24"},
	}.Encode()
	connection, _, err := websocket.DefaultDialer.Dial(endpoint.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	var first map[string]any
	if err := connection.ReadJSON(&first); err != nil || first["type"] != "terminal.frame" {
		t.Fatalf("first observer message = %#v, %v", first, err)
	}

	source.publish(liveTerminalState("pane-1", "term-2", 1))
	for {
		var message map[string]any
		err := connection.ReadJSON(&message)
		if err != nil {
			break
		}
		if message["type"] == "terminal.frame" {
			t.Fatalf("old observer emitted a frame after replacement: %#v", message)
		}
	}
}

func TestProductionHistoryIsDiscardedWhenTargetChanges(t *testing.T) {
	source := newFakeTerminalStateSource(liveTerminalState("pane-block", "term-1", 1))
	bridge := testTerminalBridge(source)
	defer bridge.Close()
	request := httptest.NewRequest(http.MethodGet, "/api/terminal/read?pane=pane-block&terminal=term-1&lines=20&source=recent-unwrapped", nil)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		bridge.productionRead(response, request)
		close(done)
	}()
	waitForTerminalChildren(t, bridge, 1)
	source.publish(liveTerminalState("pane-block", "term-2", 1))
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("fenced history command did not stop")
	}
	if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "replacement output") {
		t.Fatalf("replacement history response = %d %q", response.Code, response.Body.String())
	}
}

func TestTerminalBridgeCloseCollectsChildren(t *testing.T) {
	bridge := testTerminalBridge(nil)
	result := make(chan error, 1)
	go func() {
		_, err := bridge.output(context.Background(), nil, "pane", "read", "pane-block")
		result <- err
	}()
	waitForTerminalChildren(t, bridge, 1)
	closed := make(chan struct{})
	go func() {
		bridge.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge close did not collect its child")
	}
	if err := <-result; err == nil {
		t.Fatal("cancelled child command unexpectedly succeeded")
	}
	waitForTerminalChildren(t, bridge, 0)
}

func waitForTerminalChildren(t *testing.T, bridge *TerminalBridge, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		bridge.mutex.Lock()
		count := len(bridge.children)
		bridge.mutex.Unlock()
		if count == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("terminal child count did not become %d", want)
}

func TestTerminalLabRoutesStayOffWithoutFlag(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":        {Data: []byte("home")},
		"terminal-lab.html": {Data: []byte("lab")},
	}
	application := New(assets, nil, &TerminalBridge{}, false).Handler()
	for _, path := range []string{"/terminal-lab", "/terminal-lab.js", "/api/terminal-lab", "/api/terminal-lab/read"} {
		response := httptest.NewRecorder()
		application.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Errorf("%s returned %d, want 404", path, response.Code)
		}
	}
}

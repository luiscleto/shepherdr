package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luisc/shepherdr/internal/herdr"
)

func TestValidateListenAddressAllowsOnlyLocalhost(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8787", "[::1]:0", "localhost:9000"} {
		if err := ValidateListenAddress(address); err != nil {
			t.Errorf("ValidateListenAddress(%q) = %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:8787", "192.168.1.5:8787", ":8787"} {
		if err := ValidateListenAddress(address); err == nil {
			t.Errorf("ValidateListenAddress(%q) unexpectedly succeeded", address)
		}
	}
}

func TestServerEpochIsPresentAndProcessLocal(t *testing.T) {
	first := New(nil, nil, "", "", nil)
	second := New(nil, nil, "", "", nil)
	if first.epoch == "" || second.epoch == "" {
		t.Fatal("server epoch must be present")
	}
	if first.epoch == second.epoch {
		t.Fatalf("independent server instances shared epoch %q", first.epoch)
	}
}

func TestServerCloseCancelsTerminalAttachments(t *testing.T) {
	server := New(nil, nil, "", "", nil)
	attachment := server.beginTerminalAttachment(context.Background(), "w1:p1")
	go func() {
		<-attachment.ctx.Done()
		attachment.close()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-attachment.ctx.Done():
	default:
		t.Fatal("Server.Close did not cancel the attachment")
	}
}

func TestServerCloseDoesNotSucceedWhileExactChildIsUncollected(t *testing.T) {
	server := New(nil, nil, "", "", nil)
	attachment := server.beginTerminalAttachment(context.Background(), "w1:p1")
	_, process := stubbornTestChild(t, attachment, false)
	attachment.close()

	short, stop := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer stop()
	if err := server.Close(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close with uncollected child = %v, want deadline exceeded", err)
	}
	close(process.done)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Close(ctx); err != nil {
		t.Fatalf("Close after exact child collection: %v", err)
	}
}

func TestResolveTargetUsesOnlyExactCurrentPaneAndFreshness(t *testing.T) {
	state := herdr.State{
		Connection: herdr.ConnectionLive,
		Gap:        4,
		Home: herdr.Home{
			Workspaces: []herdr.Workspace{
				{
					ID: "w1",
					Tabs: []herdr.Tab{{
						ID: "w1:t1",
						Terminals: []herdr.Terminal{{
							PaneID: "w1:p1", TerminalID: "term-1", Title: "One",
							Agent: &herdr.Agent{Name: "old", Status: herdr.StatusIdle},
						}},
					}},
				},
			},
		},
	}
	expected := targetExpectation{PaneID: "w1:p1", TerminalID: "term-1", Gap: 4}
	if resolved, reason := resolveTarget(state, expected); reason != "" || resolved.PaneID != "w1:p1" {
		t.Fatalf("initial resolution = %+v, %q", resolved, reason)
	}

	state.Home.Workspaces[0].Tabs[0].Terminals[0].Agent = &herdr.Agent{Name: "replacement", Status: herdr.StatusWorking}
	if _, reason := resolveTarget(state, expected); reason != "" {
		t.Fatalf("agent replacement invalidated a surviving pane: %q", reason)
	}
	state.Home.Workspaces[0].Tabs[0].Terminals[0].Agent = nil
	if _, reason := resolveTarget(state, expected); reason != "" {
		t.Fatalf("agent exit invalidated a surviving pane: %q", reason)
	}

	state.Home.Workspaces[0].Tabs[0].Terminals[0].TerminalID = "replacement"
	if _, reason := resolveTarget(state, expected); reason != "terminal_unavailable" {
		t.Fatalf("same-generation terminal replacement reason = %q, want terminal_unavailable", reason)
	}

	state.Gap = 5
	if resolved, reason := resolveTarget(state, expected); reason != "" || resolved.TerminalID != "replacement" {
		t.Fatalf("cold restoration = %+v, %q; want surviving pane with fresh terminal", resolved, reason)
	}

	state.Home.Workspaces[0].Tabs[0].Terminals = []herdr.Terminal{
		{PaneID: "w1:p2", TerminalID: "term-2", Title: "Similar label"},
	}
	if _, reason := resolveTarget(state, expected); reason != "terminal_unavailable" {
		t.Fatalf("new pane with a similar label reason = %q, want terminal_unavailable", reason)
	}
}

func TestResolveTargetKeepsConnectionFailuresDistinctFromMissingPane(t *testing.T) {
	expected := targetExpectation{PaneID: "w1:p1", TerminalID: "term-1", Gap: 4}
	state := herdr.State{Connection: herdr.ConnectionReconnecting, Gap: 4}
	if _, reason := resolveTarget(state, expected); reason != "reconnecting" {
		t.Fatalf("reconnecting reason = %q", reason)
	}
	state.Connection = herdr.ConnectionNotRunning
	if _, reason := resolveTarget(state, expected); reason != "herdr_not_running" {
		t.Fatalf("not-running reason = %q", reason)
	}
	state.Connection = herdr.ConnectionIncompatible
	if _, reason := resolveTarget(state, expected); reason != "incompatible" {
		t.Fatalf("incompatible reason = %q", reason)
	}
}

func TestParseTerminalRequestTreatsHostilePaneIDAsOpaqueData(t *testing.T) {
	request := httptest.NewRequest("GET", "http://localhost/api/terminal?pane=pane%2F%3F%23%3Cscript%3E&terminal=freshness-only&gap=7&cols=80&rows=24", nil)
	expected, cols, rows, err := parseTerminalRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if expected.PaneID != "pane/?#<script>" || expected.TerminalID != "freshness-only" || expected.Gap != 7 || cols != 80 || rows != 24 {
		t.Fatalf("parsed request = %+v, %d x %d", expected, cols, rows)
	}
}

func TestTerminalArgumentsUseTakeoverOnlyWhenExplicit(t *testing.T) {
	ordinary := terminalArguments("control", false, "w1:p1", 80, 24)
	if strings.Contains(strings.Join(ordinary, " "), "--takeover") {
		t.Fatalf("ordinary control contains --takeover: %v", ordinary)
	}
	takeover := terminalArguments("control", true, "w1:p1", 80, 24)
	want := []string{"terminal", "session", "control", "w1:p1", "--takeover", "--cols", "80", "--rows", "24"}
	if !reflect.DeepEqual(takeover, want) {
		t.Fatalf("takeover arguments = %v, want %v", takeover, want)
	}
	hostile := terminalArguments("control", false, "--takeover;$(touch /tmp/not-run)", 80, 24)
	if hostile[3] != "--takeover;$(touch /tmp/not-run)" || strings.Count(strings.Join(hostile, "\x00"), "--takeover") != 1 {
		t.Fatalf("opaque pane id was not confined to one positional argument: %v", hostile)
	}
}

func TestControllerRegistryCollectsExactChildBeforeReplacement(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "fake-herdr")
	script := "#!/bin/sh\nif [ \"$3\" = observe ]; then exec tail -f /dev/null; fi\nwhile IFS= read -r line; do\n  case \"$line\" in *terminal.release*) exit 0;; esac\ndone\n"
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	server := New(nil, nil, filepath.Join(directory, "herdr.sock"), binary, nil)
	attachment := server.beginTerminalAttachment(context.Background(), "w1:p1")
	first, err := server.startController(attachment, false, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.startController(attachment, false, 80, 24); !errors.Is(err, errTerminalControlledElsewhere) {
		t.Fatalf("overlapping controller error = %v", err)
	}
	if err := server.collectChild(first, true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-first.process.done:
	default:
		t.Fatal("released controller exit was not collected")
	}
	second, err := server.startController(attachment, false, 80, 24)
	if err != nil {
		t.Fatalf("replacement after collected exit: %v", err)
	}
	if second == first {
		t.Fatal("replacement reused the prior exact child")
	}
	if err := server.collectChild(second, true); err != nil {
		t.Fatal(err)
	}
	if len(server.controllers) != 0 {
		t.Fatalf("controller inventory after cleanup = %d, want 0", len(server.controllers))
	}
	observer, err := attachment.startChild("observe", false, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.collectChild(observer, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-observer.process.done:
	default:
		t.Fatal("observer exit was not collected")
	}
	attachment.close()
}

func TestExactChildCleanupFailureIsBoundedAndCollectedAsFailure(t *testing.T) {
	server := New(nil, nil, "", "", nil)
	attachment := server.beginTerminalAttachment(context.Background(), "w1:p1")
	child, process := stubbornTestChild(t, attachment, true)
	started := time.Now()
	err := server.collectChild(child, false)
	if err == nil || !strings.Contains(err.Error(), "did not exit") {
		t.Fatalf("cleanup error = %v", err)
	}
	if process.kill == nil {
		t.Fatal("bounded cleanup did not use the exact-child kill fallback")
	}
	if time.Since(started) > 100*time.Millisecond {
		t.Fatalf("cleanup failure was not bounded: %s", time.Since(started))
	}
	replacement := server.beginTerminalAttachment(context.Background(), "w1:p1")
	if _, err := server.startController(replacement, true, 80, 24); err == nil {
		t.Fatal("replacement controller started while the prior exact child was uncollected")
	}
	waitCtx, stop := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer stop()
	if err := server.waitForPaneCleanup(waitCtx, "w1:p1"); err == nil {
		t.Fatal("a replacement was allowed before the failed exact child exited")
	}
	close(process.done)
	if err := server.waitForPaneCleanup(context.Background(), "w1:p1"); err != nil {
		t.Fatalf("replacement stayed blocked after exact exit was collected: %v", err)
	}
	replacement.close()
	attachment.close()
}

func stubbornTestChild(t *testing.T, attachment *terminalAttachment, controller bool) (*terminalChild, *terminalProcess) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	killed := false
	process := &terminalProcess{
		cancel:      cancel,
		cleanupDone: make(chan struct{}),
		ctx:         ctx,
		done:        make(chan struct{}),
		events:      make(chan processEvent),
		exitGrace:   time.Millisecond,
		killWait:    time.Millisecond,
		interrupt:   func() error { return nil },
		kill: func() error {
			killed = true
			return nil
		},
	}
	child := &terminalChild{attachment: attachment, controller: controller, paneID: attachment.paneID, process: process}
	attachment.server.lifecycleMu.Lock()
	attachment.children[child] = struct{}{}
	if controller {
		attachment.server.controllers[attachment.paneID] = child
	}
	attachment.server.signalLifecycleChangeLocked()
	attachment.server.lifecycleMu.Unlock()
	go func() {
		<-process.done
		if !killed {
			t.Error("test child exited without the exact kill fallback")
		}
		attachment.server.finishTerminalChild(child)
	}()
	return child, process
}

func TestSecurityHeadersConfineBrowserContent(t *testing.T) {
	request := httptest.NewRequest("GET", "http://localhost/", nil)
	response := httptest.NewRecorder()
	securityHeaders(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(204)
	})).ServeHTTP(response, request)
	policy := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "object-src 'none'") || !strings.Contains(policy, "connect-src 'self'") {
		t.Fatalf("unexpected content security policy %q", policy)
	}
}

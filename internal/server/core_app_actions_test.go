package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/luiscleto/shepherdr/internal/herdr"
)

// fakeApp records the commands sent to the app window and answers each with
// reply, or with err.
type fakeApp struct {
	mu       sync.Mutex
	open     bool
	commands []string
	reply    string
	err      error
	after    func()
}

func (f *fakeApp) WindowOpen(context.Context) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.open
}

func (f *fakeApp) Do(_ context.Context, command string, args map[string]any, result any) error {
	f.mu.Lock()
	encoded, _ := json.Marshal(args)
	f.commands = append(f.commands, command+" "+string(encoded))
	reply, err, after := f.reply, f.err, f.after
	f.mu.Unlock()
	if err != nil {
		return err
	}
	if after != nil {
		after()
	}
	return json.Unmarshal([]byte(reply), result)
}

func (f *fakeApp) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.commands...)
}

// fakeHome publishes core Homes to whoever subscribes.
type fakeHome struct {
	mu        sync.Mutex
	listeners []chan herdr.State
}

func (f *fakeHome) Subscribe() (<-chan herdr.State, func()) {
	updates := make(chan herdr.State, 4)
	f.mu.Lock()
	f.listeners = append(f.listeners, updates)
	f.mu.Unlock()
	updates <- herdr.State{Connection: herdr.ConnectionLive, HasHome: true}
	return updates, func() {}
}

func (f *fakeHome) publish(t *testing.T, core *fakeCore) {
	t.Helper()
	read, _ := core.Read(context.Background())
	_, home, err := herdr.ProjectCore(read, "/home/user")
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, listener := range f.listeners {
		listener <- herdr.State{Connection: herdr.ConnectionLive, HasHome: true, Home: home}
	}
}

func coreAppHandler(core *fakeCore, app *fakeApp, published homeWatcher, refresh homeRefresher) http.Handler {
	coordinator := newCoreActionCoordinator(core, "/home/user", refresh)
	coordinator.app = app
	coordinator.published = published
	coordinator.homeDir = func() (string, error) { return "/home/user", nil }
	return (&Server{coreActions: coordinator}).Handler()
}

func TestCoreNewSpaceSendsTheExpandedFolderAndOptionalName(t *testing.T) {
	core := newFakeCore(t)
	app := &fakeApp{open: true, reply: `{"space":"sp1","tab":"tb1","terminal":"t20"}`}
	refresh := &countingHomeRefresher{}
	home, repo := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "work"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "notes"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	coordinator := newCoreActionCoordinator(core, home, refresh)
	coordinator.app = app
	coordinator.homeDir = func() (string, error) { return home, nil }
	handler := (&Server{coreActions: coordinator}).Handler()

	assertOutcome(t, performSideActionRequest(t, handler, "/api/workspace-actions",
		`{"herdr":"core","action":"create_space","working_directory":"~/work","label":"api work"}`), http.StatusOK, "succeeded", "")
	assertOutcome(t, performSideActionRequest(t, handler, "/api/workspace-actions",
		`{"herdr":"core","action":"create_space","working_directory":"`+repo+`","label":"  "}`), http.StatusOK, "succeeded", "")
	// A relative folder would be the app's own; nothing is sent.
	assertOutcome(t, performSideActionRequest(t, handler, "/api/workspace-actions",
		`{"herdr":"core","action":"create_space","working_directory":"repo"}`), http.StatusBadRequest, "refused", "invalid_request")
	// A folder that isn't here is refused before the app is asked.
	for _, missing := range []string{"~/gone", repo + "/gone", repo + "/notes", repo + "/notes/deeper"} {
		assertOutcome(t, performSideActionRequest(t, handler, "/api/workspace-actions",
			`{"herdr":"core","action":"create_space","working_directory":"`+missing+`"}`), http.StatusUnprocessableEntity, "refused", "folder_missing")
	}
	want := []string{
		`space.new {"folder":"` + home + `/work","name":"api work"}`,
		`space.new {"folder":"` + repo + `"}`,
	}
	if got := app.sent(); !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %q, want %q", got, want)
	}
	if refresh.count.Load() != 2 {
		t.Fatalf("refreshes = %d", refresh.count.Load())
	}

	// A reply without the new space can't be confirmed.
	app.reply = `{"space":"sp1"}`
	assertOutcome(t, performSideActionRequest(t, handler, "/api/workspace-actions",
		`{"herdr":"core","action":"create_space","working_directory":"`+repo+`"}`), http.StatusBadGateway, "unknown", "")
}

func TestCoreRenameSpaceSendsTheNameOrEmptyForAutomatic(t *testing.T) {
	core := newFakeCore(t)
	app := &fakeApp{open: true, reply: `{"ok":true}`}
	handler := coreAppHandler(core, app, nil, &countingHomeRefresher{})
	for _, name := range []string{"123", "   "} {
		assertOutcome(t, performSideActionRequest(t, handler, "/api/workspace-actions",
			`{"herdr":"core","action":"rename_space","workspace_id":"`+coreAlpha+`","name":"`+name+`"}`), http.StatusOK, "succeeded", "")
	}
	assertOutcome(t, performSideActionRequest(t, handler, "/api/workspace-actions",
		`{"herdr":"core","action":"rename_space","workspace_id":"other","name":"x"}`), http.StatusConflict, "refused", "not_applicable")
	want := []string{
		`space.rename {"id":"` + coreAlpha + `","name":"123"}`,
		`space.rename {"id":"` + coreAlpha + `","name":""}`,
	}
	if got := app.sent(); !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %q, want %q", got, want)
	}
}

func TestCoreAppActionOutcomes(t *testing.T) {
	core := newFakeCore(t)
	rename := `{"herdr":"core","action":"rename_space","workspace_id":"` + coreAlpha + `","name":"x"}`
	cases := []struct {
		name    string
		open    bool
		err     error
		status  int
		outcome string
		reason  string
		detail  string
		sent    int
	}{
		// The window was closed when Shepherdr checked: nothing is sent.
		{name: "closed", open: false, status: http.StatusServiceUnavailable, outcome: "refused", reason: "app_closed"},
		// It closed between the check and the command.
		{name: "closed after check", open: true, err: herdr.ErrAppWindowClosed, status: http.StatusServiceUnavailable, outcome: "refused", reason: "app_closed", sent: 1},
		{name: "refused", open: true, err: &herdr.APIError{Code: "app_refused", Message: `no space "sp4"`}, status: http.StatusUnprocessableEntity, outcome: "refused", reason: "herdr_refused", detail: `no space "sp4"`, sent: 1},
		{name: "no answer", open: true, err: &herdr.MutationError{Err: context.DeadlineExceeded, Submitted: true}, status: http.StatusGatewayTimeout, outcome: "unknown", sent: 1},
		{name: "hung up", open: true, err: &herdr.MutationError{Err: errors.New("the window hung up"), Submitted: true}, status: http.StatusBadGateway, outcome: "unknown", sent: 1},
	}
	for _, test := range cases {
		app := &fakeApp{open: test.open, err: test.err}
		refresh := &countingHomeRefresher{}
		response := performSideActionRequest(t, coreAppHandler(core, app, nil, refresh), "/api/workspace-actions", rename)
		assertOutcome(t, response, test.status, test.outcome, test.reason)
		var body struct{ Detail string }
		_ = json.Unmarshal(response.Body.Bytes(), &body)
		// Every closed window, unknown result or success asks Home again, and nothing retries.
		wantRefresh := int32(1)
		if test.reason == "herdr_refused" {
			wantRefresh = 0
		}
		if body.Detail != test.detail || len(app.sent()) != test.sent || refresh.count.Load() != wantRefresh {
			t.Fatalf("%s: detail %q, sent %q, refreshes %d", test.name, body.Detail, app.sent(), refresh.count.Load())
		}
	}
}

func TestCoreSplitNamesTheExactPaneAndWaitsForHome(t *testing.T) {
	core := newFakeCore(t)
	published := &fakeHome{}
	app := &fakeApp{open: true, reply: `{"pane":"pNEW","terminal":"t20"}`}
	app.after = func() {
		// The app adds t20 right of t4, then Home publishes it.
		core.change(func(read *herdr.CoreRead) {
			read.Terminals = append(read.Terminals, herdr.CoreTerminal{Address: "t20", CWD: "/tmp/w1/work/repo", CreatedMS: 99})
			read.Document = json.RawMessage(strings.Replace(compactJSON(t, read.Document),
				`{"content":{"terminal":"t4"},`, `{"content":{"terminal":"t20"},"id":"pNEW","type":"block"},{"content":{"terminal":"t4"},`, 1))
		})
		// Listeners are buffered, so it publishes before the handler waits.
		published.publish(t, core)
	}
	handler := coreAppHandler(core, app, published, &countingHomeRefresher{})
	response := performSideActionRequest(t, handler, "/api/terminal-actions",
		`{"herdr":"core","action":"split_terminal",`+coreTarget(coreAlpha, coreAlphaTab, "t4")+`,"direction":"down"}`)
	assertOutcome(t, response, http.StatusOK, "succeeded", "")
	var body struct {
		Terminal terminalActionTarget `json:"terminal"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := terminalActionTarget{WorkspaceID: coreAlpha, TabID: coreAlphaTab, PaneID: "t20", TerminalID: coreID + ":t20"}
	if body.Terminal != want {
		t.Fatalf("terminal = %+v, want %+v", body.Terminal, want)
	}
	if got := app.sent(); !reflect.DeepEqual(got, []string{`pane.split {"direction":"down","pane":"p20daff1ca339f27e18f44423f49f4290"}`}) {
		t.Fatalf("commands = %q", got)
	}

	// A worker has no pane, Other terminals has no layout, and a stale target is gone.
	app.after = nil
	for _, test := range []struct {
		target, reason string
		status         int
	}{
		{coreTarget(coreAlpha, coreAlphaTab, "t9"), "not_applicable", http.StatusConflict},
		{coreTarget(herdr.OtherTerminalsID, herdr.OtherTerminalsID, "t8"), "not_applicable", http.StatusConflict},
		{coreTarget(coreAlpha, coreSecond, "t4"), "not_found", http.StatusNotFound},
	} {
		assertOutcome(t, performSideActionRequest(t, handler, "/api/terminal-actions",
			`{"herdr":"core","action":"split_terminal",`+test.target+`,"direction":"right"}`), test.status, "refused", test.reason)
	}
	if len(app.sent()) != 1 {
		t.Fatalf("commands = %q", app.sent())
	}
}

func compactJSON(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return string(mustMarshal(t, value))
}

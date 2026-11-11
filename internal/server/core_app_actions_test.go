package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/luiscleto/shepherdr/internal/herdr"
)

// fakeApp records the commands sent to the app window and answers each with
// its replies entry, else reply, or with its errs entry, else err.
type fakeApp struct {
	mu       sync.Mutex
	open     bool
	commands []string
	reply    string
	replies  map[string]string
	err      error
	errs     map[string]error
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
	if commandReply, ok := f.replies[command]; ok {
		reply = commandReply
	}
	if commandErr, ok := f.errs[command]; ok {
		err = commandErr
	}
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
	return signInOffHandler(&Server{coreActions: coordinator})
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
	handler := signInOffHandler(&Server{coreActions: coordinator})

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

// Rename tab names the exact tab of the exact space, from a fresh read; the
// app refuses one it no longer has.
func TestCoreRenameTabSendsTheExactTabAndNameOrEmptyForItsNumber(t *testing.T) {
	core := newFakeCore(t)
	app := &fakeApp{open: true, reply: `{"ok":true}`}
	refresh := &countingHomeRefresher{}
	handler := coreAppHandler(core, app, nil, refresh)
	renameIn := func(space, tab, name string) *httptest.ResponseRecorder {
		return performSideActionRequest(t, handler, "/api/workspace-actions",
			`{"herdr":"core","action":"rename_tab","workspace_id":"`+space+`","tab_id":"`+tab+`","name":"`+name+`"}`)
	}
	rename := func(tab, name string) *httptest.ResponseRecorder { return renameIn(coreAlpha, tab, name) }
	assertOutcome(t, rename(coreSecond, "fix <b>login</b> 🐑"), http.StatusOK, "succeeded", "")
	assertOutcome(t, rename(coreAlphaTab, "  "), http.StatusOK, "succeeded", "")
	assertOutcome(t, rename(herdr.OtherTerminalsID, "x"), http.StatusConflict, "refused", "not_applicable")
	assertOutcome(t, rename("", "x"), http.StatusBadRequest, "refused", "invalid_request")
	assertOutcome(t, performSideActionRequest(t, handler, "/api/workspace-actions",
		`{"herdr":"core","action":"rename_tab","workspace_id":"`+coreAlpha+`","name":"x"}`), http.StatusBadRequest, "refused", "invalid_request")
	// A tab that isn't in that space now, or a space that doesn't list its
	// tabs, is refused before the app is asked.
	assertOutcome(t, rename("tbgone", "x"), http.StatusNotFound, "refused", "not_found")
	assertOutcome(t, rename(coreRepoTab, "x"), http.StatusNotFound, "refused", "not_found")
	assertOutcome(t, renameIn("spgone", coreSecond, "x"), http.StatusNotFound, "refused", "not_found")
	assertOutcome(t, renameIn(coreRepo, coreRepoTab, "x"), http.StatusConflict, "refused", "not_applicable")

	// The tab closed after the read: the app's refusal is passed on.
	app.err = &herdr.APIError{Code: "app_refused", Message: `no tab "` + coreSecond + `"`}
	response := rename(coreSecond, "x")
	assertOutcome(t, response, http.StatusUnprocessableEntity, "refused", "herdr_refused")
	if !strings.Contains(response.Body.String(), `no tab \"`+coreSecond+`\"`) {
		t.Fatalf("refusal = %s", response.Body.String())
	}
	want := []string{
		`tab.rename {"id":"` + coreSecond + `","name":"fix \u003cb\u003elogin\u003c/b\u003e 🐑"}`,
		`tab.rename {"id":"` + coreAlphaTab + `","name":""}`,
		`tab.rename {"id":"` + coreSecond + `","name":"x"}`,
	}
	if got := app.sent(); !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %q, want %q", got, want)
	}
	if refresh.count.Load() != 2 {
		t.Fatalf("refreshes = %d", refresh.count.Load())
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

func TestCoreNewSpaceWhileClosedStartsALabelledShellInCore(t *testing.T) {
	core := newFakeCore(t)
	app := &fakeApp{open: false}
	refresh := &countingHomeRefresher{}
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "work"), 0o700); err != nil {
		t.Fatal(err)
	}
	coordinator := newCoreActionCoordinator(core, home, refresh)
	coordinator.app = app
	coordinator.homeDir = func() (string, error) { return home, nil }
	handler := signInOffHandler(&Server{coreActions: coordinator})
	run := func(body string) *httptest.ResponseRecorder {
		return performSideActionRequest(t, handler, "/api/workspace-actions", `{"herdr":"core","action":"create_space",`+body+`}`)
	}

	assertOutcome(t, run(`"working_directory":"~/work","best_effort":true`), http.StatusOK, "succeeded", "")
	assertOutcome(t, run(`"working_directory":"`+home+`","best_effort":true`), http.StatusOK, "succeeded", "")
	runs := core.ran()
	idShape := regexp.MustCompile(`^(\S+) sp[0-9a-f]{32} tb[0-9a-f]{32}$`)
	if len(runs) != 2 || idShape.FindStringSubmatch(runs[0]) == nil || idShape.FindStringSubmatch(runs[0])[1] != home+"/work" ||
		idShape.FindStringSubmatch(runs[1]) == nil || idShape.FindStringSubmatch(runs[1])[1] != home || runs[0][len(home)+6:] == runs[1][len(home)+1:] {
		t.Fatalf("runs = %q", runs)
	}
	if len(app.sent()) != 0 || refresh.count.Load() != 2 {
		t.Fatalf("app commands %q, refreshes %d", app.sent(), refresh.count.Load())
	}

	// A missing folder is refused before core uses up an address. There is
	// no name while the window is closed, and only true confirms the sheet.
	assertOutcome(t, run(`"working_directory":"~/gone","best_effort":true`), http.StatusUnprocessableEntity, "refused", "folder_missing")
	for _, body := range []string{
		`"working_directory":"~/work","best_effort":true,"label":"api"`,
		`"working_directory":"~/work","best_effort":false`,
		`"working_directory":"work","best_effort":true`,
	} {
		assertOutcome(t, run(body), http.StatusBadRequest, "refused", "invalid_request")
	}
	// Confirming the open window's sheet after it closed sends nothing.
	assertOutcome(t, run(`"working_directory":"~/work"`), http.StatusServiceUnavailable, "refused", "app_closed")
	if len(core.ran()) != 2 {
		t.Fatalf("runs = %q", core.ran())
	}

	// Core's refusal, no answer, and no core.
	for _, test := range []struct {
		err             error
		status          int
		outcome, reason string
	}{
		{&herdr.APIError{Code: "spawn_failed", Message: "cannot start"}, http.StatusUnprocessableEntity, "refused", "herdr_refused"},
		{&herdr.MutationError{Err: errors.New("read Herdr Core terminal.run response: EOF"), Submitted: true}, http.StatusBadGateway, "unknown", ""},
		{&herdr.MutationError{Err: errors.New("connect to Herdr Core: no such file")}, http.StatusServiceUnavailable, "refused", "herdr_unavailable"},
	} {
		core.runErr = test.err
		assertOutcome(t, run(`"working_directory":"~/work","best_effort":true`), test.status, test.outcome, test.reason)
	}

	// Without the app, nothing would add the space.
	core.runErr = nil
	coordinator.app = nil
	assertOutcome(t, run(`"working_directory":"~/work","best_effort":true`), http.StatusBadRequest, "refused", "invalid_request")
	if len(core.ran()) != 5 {
		t.Fatalf("runs = %q", core.ran())
	}
}

func TestCoreNewTerminalWhileClosedStartsInTheTerminalsFolderAndOpens(t *testing.T) {
	core := newFakeCore(t)
	published := &fakeHome{}
	app := &fakeApp{open: false}
	moved := filepath.Join(t.TempDir(), "sub")
	if err := os.Mkdir(moved, 0o700); err != nil {
		t.Fatal(err)
	}
	// t4's shell moved into a subfolder with cd.
	core.change(func(read *herdr.CoreRead) {
		for index := range read.Terminals {
			if read.Terminals[index].Address == "t4" {
				read.Terminals[index].Foreground = &herdr.CoreForeground{CWD: moved}
			}
		}
	})
	core.afterRun = func() { published.publish(t, core) }
	handler := coreAppHandler(core, app, published, &countingHomeRefresher{})
	run := func(target string, extra string) *httptest.ResponseRecorder {
		return performSideActionRequest(t, handler, "/api/terminal-actions", `{"herdr":"core","action":"split_terminal",`+target+extra+`}`)
	}

	response := run(coreTarget(coreAlpha, coreAlphaTab, "t4"), `,"best_effort":true`)
	assertOutcome(t, response, http.StatusOK, "succeeded", "")
	var body struct {
		Terminal terminalActionTarget `json:"terminal"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	// It opens in t4's tab, where Home shows it until the app adds it.
	want := terminalActionTarget{WorkspaceID: coreAlpha, TabID: coreAlphaTab, PaneID: "t31", TerminalID: coreID + ":t31"}
	if body.Terminal != want {
		t.Fatalf("terminal = %+v, want %+v", body.Terminal, want)
	}
	if got := core.ran(); !reflect.DeepEqual(got, []string{moved + " " + coreAlpha + " " + coreAlphaTab}) || len(app.sent()) != 0 {
		t.Fatalf("runs %q, app commands %q", got, app.sent())
	}

	// The new terminal has no pane yet; a worker has none; Other terminals has
	// no layout; a stale target is gone; and a gone folder is refused before
	// core uses up an address.
	core.afterRun = nil
	for _, test := range []struct {
		target, reason string
		status         int
	}{
		{coreTarget(coreAlpha, coreAlphaTab, "t31"), "not_applicable", http.StatusConflict},
		{coreTarget(coreAlpha, coreAlphaTab, "t9"), "not_applicable", http.StatusConflict},
		{coreTarget(herdr.OtherTerminalsID, herdr.OtherTerminalsID, "t8"), "not_applicable", http.StatusConflict},
		{coreTarget(coreAlpha, coreSecond, "t4"), "not_found", http.StatusNotFound},
	} {
		assertOutcome(t, run(test.target, `,"best_effort":true`), test.status, "refused", test.reason)
	}
	if err := os.Remove(moved); err != nil {
		t.Fatal(err)
	}
	assertOutcome(t, run(coreTarget(coreAlpha, coreAlphaTab, "t4"), `,"best_effort":true`), http.StatusUnprocessableEntity, "refused", "folder_missing")
	// There is no Beside or Below while closed; the open sheet confirmed after
	// the window closed sends nothing.
	assertOutcome(t, run(coreTarget(coreAlpha, coreAlphaTab, "t3"), `,"best_effort":true,"direction":"right"`), http.StatusBadRequest, "refused", "invalid_request")
	assertOutcome(t, run(coreTarget(coreAlpha, coreAlphaTab, "t3"), `,"direction":"right"`), http.StatusServiceUnavailable, "refused", "app_closed")
	if len(core.ran()) != 1 || len(app.sent()) != 0 {
		t.Fatalf("runs %q, app commands %q", core.ran(), app.sent())
	}

	// Without a foreground folder, t3's starting folder is used. Home that
	// never shows the new terminal makes the result unknown.
	start := t.TempDir()
	core.change(func(read *herdr.CoreRead) {
		for index := range read.Terminals {
			if read.Terminals[index].Address == "t3" {
				read.Terminals[index].CWD, read.Terminals[index].Foreground = start, nil
			}
		}
	})
	timeoutHandler := coreAppHandler(core, app, nil, &countingHomeRefresher{})
	assertOutcome(t, performSideActionRequest(t, timeoutHandler, "/api/terminal-actions",
		`{"herdr":"core","action":"split_terminal",`+coreTarget(coreAlpha, coreAlphaTab, "t3")+`,"best_effort":true}`), http.StatusBadGateway, "unknown", "")
	if got := core.ran(); len(got) != 2 || got[1] != start+" "+coreAlpha+" "+coreAlphaTab {
		t.Fatalf("runs %q", got)
	}
}

func TestCoreNewTabAddsATabAtTheEndOfTheExactSpaceAndOpensIt(t *testing.T) {
	core := newFakeCore(t)
	published := &fakeHome{}
	app := &fakeApp{open: true, replies: map[string]string{
		"tab.new":  `{"space":"` + coreAlpha + `","tab":"tbNEW","terminal":"t20"}`,
		"tab.move": `{"ok":true}`,
	}}
	app.after = func() {
		// The app adds tbNEW to alpha, then Home publishes it.
		core.change(func(read *herdr.CoreRead) {
			if strings.Contains(string(read.Document), "tbNEW") {
				return
			}
			read.Terminals = append(read.Terminals, herdr.CoreTerminal{Address: "t20", CWD: "/tmp/w1/work/repo", CreatedMS: 99})
			read.Document = json.RawMessage(strings.Replace(compactJSON(t, read.Document),
				`"id":"`+coreAlpha+`","name":"alpha","tabs":[`,
				`"id":"`+coreAlpha+`","name":"alpha","tabs":[{"id":"tbNEW","name":"","root":{"content":{"terminal":"t20"},"id":"pNEW","type":"block"}},`, 1))
		})
		published.publish(t, core)
	}
	handler := coreAppHandler(core, app, published, &countingHomeRefresher{})
	run := func(workspaceID string) *httptest.ResponseRecorder {
		return performSideActionRequest(t, handler, "/api/workspace-actions", `{"herdr":"core","action":"new_tab","workspace_id":"`+workspaceID+`"}`)
	}
	opened := func(response *httptest.ResponseRecorder) terminalActionTarget {
		t.Helper()
		assertOutcome(t, response, http.StatusOK, "succeeded", "")
		var body struct {
			Terminal terminalActionTarget `json:"terminal"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Terminal
	}

	// The app chooses the folder and name, then the tab moves after alpha's
	// two tabs and opens.
	want := terminalActionTarget{WorkspaceID: coreAlpha, TabID: "tbNEW", PaneID: "t20", TerminalID: coreID + ":t20"}
	if got := opened(run(coreAlpha)); got != want {
		t.Fatalf("terminal = %+v, want %+v", got, want)
	}
	if got := app.sent(); !reflect.DeepEqual(got, []string{`tab.new {"space":"` + coreAlpha + `"}`, `tab.move {"id":"tbNEW","index":3}`}) {
		t.Fatalf("commands = %q", got)
	}

	// A failed move leaves the tab where the app put it: it still opens, with
	// no retry and no undo.
	app.errs = map[string]error{"tab.move": &herdr.APIError{Code: "app", Message: "no tab"}}
	if got := opened(run(coreAlpha)); got != want {
		t.Fatalf("terminal after a failed move = %+v", got)
	}
	if got := app.sent(); len(got) != 4 || got[3] != `tab.move {"id":"tbNEW","index":4}` {
		t.Fatalf("commands = %q", got)
	}

	// Other terminals isn't a space, a closed space is gone, the app's own
	// refusal shows, and an answer for another space is unknown.
	app.after, app.errs = nil, nil
	assertOutcome(t, run(herdr.OtherTerminalsID), http.StatusConflict, "refused", "not_applicable")
	assertOutcome(t, run("spGONE"), http.StatusNotFound, "refused", "not_found")
	app.errs = map[string]error{"tab.new": &herdr.APIError{Code: "app", Message: "no space"}}
	assertOutcome(t, run(coreRepo), http.StatusUnprocessableEntity, "refused", "herdr_refused")
	app.errs = nil
	app.replies["tab.new"] = `{"space":"` + coreRepo + `x","tab":"tbX","terminal":"t21"}`
	assertOutcome(t, run(coreRepo), http.StatusBadGateway, "unknown", "")
	if got := app.sent(); len(got) != 6 {
		t.Fatalf("commands = %q", got)
	}

	// The open window's sheet confirmed after the window closed sends nothing.
	app.open = false
	assertOutcome(t, run(coreRepo), http.StatusServiceUnavailable, "refused", "app_closed")
	if len(app.sent()) != 6 || len(core.ran()) != 0 {
		t.Fatalf("commands %q, runs %q", app.sent(), core.ran())
	}
}

func TestCoreNewTabWhileClosedStartsInTheSpaceFolderAndOpens(t *testing.T) {
	core := newFakeCore(t)
	published := &fakeHome{}
	app := &fakeApp{open: false}
	folder := t.TempDir()
	core.change(func(read *herdr.CoreRead) {
		read.Document = json.RawMessage(strings.Replace(compactJSON(t, read.Document), `"folder":"/tmp/w1/work/repo",`, `"folder":"`+folder+`",`, 1))
	})
	core.afterRun = func() { published.publish(t, core) }
	handler := coreAppHandler(core, app, published, &countingHomeRefresher{})
	run := func(body string) *httptest.ResponseRecorder {
		return performSideActionRequest(t, handler, "/api/workspace-actions", `{"herdr":"core","action":"new_tab",`+body+`}`)
	}

	response := run(`"workspace_id":"` + coreAlpha + `","best_effort":true`)
	assertOutcome(t, response, http.StatusOK, "succeeded", "")
	var body struct {
		Terminal terminalActionTarget `json:"terminal"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	// It starts in alpha's folder with a new tab id, and opens in that tab,
	// which Home shows at the end of alpha until the app adds it.
	runs := core.ran()
	shape := regexp.MustCompile(`^` + regexp.QuoteMeta(folder+" "+coreAlpha) + ` (tb[0-9a-f]{32})$`)
	match := shape.FindStringSubmatch(strings.Join(runs, "\n"))
	if len(runs) != 1 || match == nil || len(app.sent()) != 0 {
		t.Fatalf("runs %q, app commands %q", runs, app.sent())
	}
	want := terminalActionTarget{WorkspaceID: coreAlpha, TabID: match[1], PaneID: "t31", TerminalID: coreID + ":t31"}
	if body.Terminal != want {
		t.Fatalf("terminal = %+v, want %+v", body.Terminal, want)
	}
	read, _ := core.Read(context.Background())
	_, home, _ := herdr.ProjectCore(read, "/home/user")
	if tabs := home.Workspaces[0].Tabs; tabs[len(tabs)-1].ID != match[1] || tabs[len(tabs)-1].Label != "3" {
		t.Fatalf("alpha's tabs = %+v", tabs)
	}

	// Other terminals isn't a space, a closed space is gone, a space the app
	// hasn't added yet has no tabs to add to, and only true confirms the sheet.
	core.afterRun = nil
	waiting, _ := core.Run(context.Background(), folder, "sp"+strings.Repeat("a", 32), "tb"+strings.Repeat("b", 32))
	if waiting == "" {
		t.Fatal("no waiting space")
	}
	for _, test := range []struct {
		body, reason string
		status       int
	}{
		{`"workspace_id":"` + herdr.OtherTerminalsID + `","best_effort":true`, "not_applicable", http.StatusConflict},
		{`"workspace_id":"spGONE","best_effort":true`, "not_found", http.StatusNotFound},
		{`"workspace_id":"sp` + strings.Repeat("a", 32) + `","best_effort":true`, "not_applicable", http.StatusConflict},
		{`"workspace_id":"` + coreAlpha + `","best_effort":false`, "invalid_request", http.StatusBadRequest},
		{`"workspace_id":"` + coreAlpha + `","best_effort":true,"folder":"/tmp"`, "invalid_request", http.StatusBadRequest},
	} {
		assertOutcome(t, run(test.body), test.status, "refused", test.reason)
	}
	// A space whose folder is gone is refused before core uses up an address.
	if err := os.Remove(folder); err != nil {
		t.Fatal(err)
	}
	assertOutcome(t, run(`"workspace_id":"`+coreAlpha+`","best_effort":true`), http.StatusUnprocessableEntity, "refused", "folder_missing")
	if len(core.ran()) != 2 || len(app.sent()) != 0 {
		t.Fatalf("runs %q, app commands %q", core.ran(), app.sent())
	}
}

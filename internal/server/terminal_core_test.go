package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/luiscleto/shepherdr/internal/herdr"
)

func TestCoreKeyRefusalIsOnlyCoreTurningTheKeyDown(t *testing.T) {
	for _, test := range []struct {
		err  error
		want bool
	}{
		{&herdr.APIError{Code: "invalid_params", Message: `unknown key "ctrl+super+c"`}, true},
		{fmt.Errorf("send: %w", &herdr.APIError{Code: "invalid_params"}), true},
		{&herdr.APIError{Code: "not_found"}, false},
		{herdr.ErrCoreKeysUnconfirmed, false},
		{errors.New("terminal unavailable"), false},
		{nil, false},
	} {
		if got := coreKeyRefused(test.err); got != test.want {
			t.Errorf("coreKeyRefused(%v) = %v; want %v", test.err, got, test.want)
		}
	}
}

func TestCoreKeyNamesFollowTheCoreTable(t *testing.T) {
	for _, test := range []struct {
		key  terminalKey
		want string
	}{
		{terminalKey{Base: "esc"}, "esc"},
		{terminalKey{Base: "escape"}, "esc"},
		{terminalKey{Base: "return"}, "enter"},
		{terminalKey{Base: "bs"}, "backspace"},
		{terminalKey{Base: "c", Ctrl: true}, "ctrl+c"},
		{terminalKey{Base: "F5"}, "f5"},
		{terminalKey{Base: "tab", Shift: true}, "shift+tab"},
		{terminalKey{Base: "A"}, "shift+a"},
		{terminalKey{Base: "A", Ctrl: true, Shift: true}, "ctrl+shift+a"},
		{terminalKey{Base: "k", Super: true, Alt: true, Ctrl: true}, "ctrl+alt+super+k"},
		{terminalKey{Base: " "}, "space"},
		{terminalKey{Base: "minus"}, "-"},
		{terminalKey{Base: "quote"}, "'"},
		{terminalKey{Base: "backslash"}, "backslash"},
		{terminalKey{Base: "\\", Ctrl: true}, "ctrl+\\"},
		{terminalKey{Base: "/"}, "/"},
	} {
		got, err := coreKeyName(test.key)
		if err != nil || got != test.want {
			t.Errorf("coreKeyName(%+v) = %q, %v; want %q", test.key, got, err, test.want)
		}
	}
	for _, base := range []string{"plus", "+", "percent", "%", "double_quote", "\"", "colon", ":", "ampersand", "&", "!", "é", "f13", "home"} {
		if got, err := coreKeyName(terminalKey{Base: base, Ctrl: true}); err == nil {
			t.Errorf("coreKeyName(%q) = %q, want no key", base, got)
		}
	}
}

func TestTerminalEndpointsNeedTheirHerdr(t *testing.T) {
	application := New(fstest.MapFS{"index.html": {Data: []byte("home")}}, nil, herdr.NewNotFollowedProjector(), nil, false)
	application.SetCoreTerminal(NewCoreTerminalBridge(herdr.NewCoreClient("/nonexistent/core.sock", nil), nil, herdr.NewNotFollowedProjector()))
	application.ConfigureSignInOff()
	handler := application.Handler()
	results := []string{}
	for _, path := range []string{
		"/api/terminal?pane=t1&terminal=c:t1&mode=observe&cols=80&rows=24",
		"/api/terminal?herdr=other&pane=t1&terminal=c:t1&mode=observe&cols=80&rows=24",
		"/api/terminal?herdr=tui&pane=t1&terminal=c:t1&mode=observe&cols=80&rows=24",
		"/api/terminal?herdr=core&pane=t1&terminal=c:t1&mode=takeover&cols=80&rows=24",
		"/api/terminal?herdr=core&pane=t1&terminal=c:t1&mode=observe&cols=80&rows=24",
		"/api/terminal/read?pane=t1&terminal=c:t1&lines=20&source=recent",
		"/api/terminal/read?herdr=core&pane=t1&terminal=c:t1&lines=20&source=recent",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://localhost"+path, nil))
		results = append(results, http.StatusText(response.Code))
	}
	// No herdr or an unknown one is refused; a Herdr without a bridge, Take
	// over on core, and a target core's Home doesn't hold are not served.
	want := "Bad Request,Bad Request,Not Found,Bad Request,Not Found,Bad Request,Not Found"
	if got := strings.Join(results, ","); got != want {
		t.Fatalf("statuses = %s, want %s", got, want)
	}
}

func TestCoreFileTargetsNeedAStatusAndAListedGroup(t *testing.T) {
	state := herdr.State{Connection: herdr.ConnectionLive, Snapshot: herdr.Snapshot{
		Workspaces: []herdr.WorkspaceInfo{{WorkspaceID: "sp1", Label: "alpha"}},
		Panes: []herdr.PaneInfo{
			{WorkspaceID: "sp1", TabID: "tb1", PaneID: "t1", TerminalID: "c:t1"},
			{WorkspaceID: "sp1", TabID: "tb1", PaneID: "t2", TerminalID: "c:t2"},
			{WorkspaceID: herdr.OtherTerminalsID, TabID: herdr.OtherTerminalsID, PaneID: "t3", TerminalID: "c:t3"},
		},
		Agents: []herdr.AgentInfo{
			{WorkspaceID: "sp1", TabID: "tb1", PaneID: "t1", TerminalID: "c:t1", AgentStatus: herdr.StatusError},
			{WorkspaceID: herdr.OtherTerminalsID, TabID: herdr.OtherTerminalsID, PaneID: "t3", TerminalID: "c:t3", AgentStatus: herdr.StatusIdle},
		},
	}}
	results := []string{}
	for _, target := range [][3]string{
		{"sp1", "t1", "c:t1"},                  // an agent reporting error
		{"sp1", "t2", "c:t2"},                  // a shell: no status
		{herdr.OtherTerminalsID, "t3", "c:t3"}, // an agent under Other terminals
		{"sp1", "t3", "c:t3"},                  // the wrong group
		{"sp2", "t1", "c:t1"},                  // a space Home doesn't list
		{"sp1", "t1", "other:t1"},              // another terminal at that address
	} {
		resolved, ok := resolveCoreUploadTarget(state, target[0], target[1], target[2])
		results = append(results, map[bool]string{true: resolved.label, false: "-"}[ok])
	}
	if got, want := strings.Join(results, ","), "alpha,-,Other terminals,-,-,-"; got != want {
		t.Fatalf("targets = %s, want %s", got, want)
	}
	state.LastKnown = true
	if _, ok := resolveCoreUploadTarget(state, "sp1", "t1", "c:t1"); ok {
		t.Fatal("a last-known Home must not accept files")
	}
}

func TestCoreTargetStaysCurrentWhenTheAppMovesTheTerminal(t *testing.T) {
	home := func(lastKnown bool, workspace, tab, terminal string) herdr.State {
		return herdr.State{Connection: herdr.ConnectionLive, LastKnown: lastKnown, Snapshot: herdr.Snapshot{
			Panes: []herdr.PaneInfo{{WorkspaceID: workspace, TabID: tab, PaneID: "t1", TerminalID: terminal}},
		}}
	}
	source := newFakeTerminalStateSource(home(false, "sp1", "tb1", "c:t1"))
	lease, ok := acquireTerminalTarget(t.Context(), source, coreTerminalTargetFromState, "t1", "c:t1")
	if !ok {
		t.Fatal("a listed core terminal must be a target")
	}
	defer lease.Close()
	results := []string{}
	for _, state := range []herdr.State{
		home(false, "sp1", "tb2", "c:t1"),                                                                // another tab
		home(false, "sp2", "tb3", "c:t1"),                                                                // another space
		home(false, herdr.OtherTerminalsID, herdr.OtherTerminalsID, "c:t1"),                              // out of every space
		home(false, "sp1", "tb1", "c:t1"),                                                                // placed in a space again
		home(true, "sp1", "tb1", "c:t1"),                                                                 // last-known Home
		home(false, "sp1", "tb1", "c:other"),                                                             // another terminal at the address
		{Connection: herdr.ConnectionReconnecting, Snapshot: home(false, "sp1", "tb1", "c:t1").Snapshot}, // Home not live
	} {
		results = append(results, map[bool]string{true: "current", false: "-"}[lease.target.matches(state, coreTerminalTargetFromState)])
	}
	if got, want := strings.Join(results, ","), "current,current,current,current,-,-,-"; got != want {
		t.Fatalf("after each Home: %s, want %s", got, want)
	}
	source.publish(home(false, "sp2", "tb3", "c:t1"))
	if !lease.Valid() {
		t.Fatal("moving the terminal to another space must not end its stream")
	}
	source.publish(home(false, "sp2", "tb3", "c:other"))
	select {
	case <-lease.done:
	case <-time.After(5 * time.Second):
		t.Fatal("a replaced terminal must end the stream")
	}
}

func TestCoreResizeIsSentOnlyWhenItChangesTheSize(t *testing.T) {
	session := &coreLiveSession{controlled: true}
	session.size.Store(coreSize(46, 41))
	command, err := validatedTerminalCommand([]byte(`{"type":"terminal.resize","cols":46,"rows":41}`))
	if err != nil {
		t.Fatal(err)
	}
	// The session has no attachment, so sending a resize would panic.
	outcome, err := (&CoreTerminalBridge{}).applyCommand(httptest.NewRequest(http.MethodGet, "/", nil), session, nil, "t1", command)
	if outcome != terminalBatchNotSent || err != nil {
		t.Fatalf("outcome %v, error %v", outcome, err)
	}
	session.size.Store(coreSize(7, 14))
	defer func() {
		if recover() == nil {
			t.Fatal("a resize to a different size was not sent")
		}
	}()
	_, _ = (&CoreTerminalBridge{}).applyCommand(httptest.NewRequest(http.MethodGet, "/", nil), session, nil, "t1", command)
}

func TestCoreSizeIsTakenBackOnceSoonAfterThisBrowsersInput(t *testing.T) {
	phone, composer, app := coreSize(46, 41), coreSize(46, 35), coreSize(7, 14)
	start := time.Unix(1_000, 0)
	at := func(ms int) time.Time { return start.Add(time.Duration(ms) * time.Millisecond) }
	var keep coreSizeKeep
	keep.asked(composer)
	keep.asked(phone)
	if keep.changed(phone, app, at(0)) {
		t.Fatal("without input, another viewer taking the size is left alone")
	}
	keep.input(at(1_000))
	if keep.changed(app, app, at(1_010)) || keep.changed(app, composer, at(1_050)) || keep.changed(composer, phone, at(1_100)) {
		t.Fatal("an unchanged size and this browser's own sizes are not taken back")
	}
	if !keep.changed(phone, app, at(1_170)) {
		t.Fatal("a size taken soon after the input is taken back")
	}
	if keep.changed(app, phone, at(1_180)) || keep.changed(phone, app, at(1_300)) {
		t.Fatal("at most once per input, so a resend never leads to another")
	}
	keep.reached(at(1_400))
	if keep.changed(phone, app, at(1_500)) {
		t.Fatal("a spent input stays spent")
	}
	if size, _ := keep.wanted(); size != phone {
		t.Fatalf("wanted %x, not the latest size asked for", size)
	}

	keep.input(at(5_000))
	if keep.changed(phone, app, at(5_000).Add(keepSizeAfterInput+time.Millisecond)) {
		t.Fatal("a size change after the window is left alone")
	}
	keep.reached(at(9_000))
	if !keep.changed(app, coreSize(120, 40), at(9_000).Add(keepSizeAfterInput)) {
		t.Fatal("a key core held back restarts the window when it is pressed")
	}
}

func TestOnlyACoreResizeCarriesKeep(t *testing.T) {
	command, err := validatedTerminalCommand([]byte(`{"type":"terminal.resize","cols":46,"rows":41,"keep":true}`))
	if err != nil || !command.keepSize {
		t.Fatalf("keep resize: %+v %v", command, err)
	}
	for _, message := range []string{
		`{"type":"terminal.resize","cols":46,"rows":41,"keep":false}`,
		`{"type":"terminal.input","text":"x","request_id":1,"keep":true}`,
		`{"type":"terminal.release","keep":true}`,
	} {
		if _, err := validatedTerminalCommand([]byte(message)); err == nil {
			t.Fatalf("accepted %s", message)
		}
	}
}

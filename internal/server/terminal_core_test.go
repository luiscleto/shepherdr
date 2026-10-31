package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/luiscleto/shepherdr/internal/herdr"
)

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

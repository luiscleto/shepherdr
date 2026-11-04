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
	application := New(fstest.MapFS{"index.html": {Data: []byte("home")}}, nil, herdr.NewNotFollowedProjector(), nil)
	application.SetCoreTerminal(NewCoreTerminalBridge(herdr.NewCoreClient("/nonexistent/core.sock", nil), nil, herdr.NewNotFollowedProjector()))
	configureTestSignInOff(application)
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

func TestCoreSizeIsHeldOnlyWhileThePageIsInView(t *testing.T) {
	session := &coreLiveSession{controlled: true}
	session.size.Store(coreSize(46, 41))
	bridge := &CoreTerminalBridge{}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	apply := func(message string) {
		t.Helper()
		command, err := validatedTerminalCommand([]byte(message))
		if err != nil {
			t.Fatal(err)
		}
		// The session has no attachment, so sending a resize would panic.
		if _, err := bridge.applyCommand(request, session, nil, "t1", command); err != nil {
			t.Fatal(err)
		}
	}
	apply(`{"type":"terminal.resize","cols":46,"rows":41}`)
	if _, holding := session.hold.wanted(); holding {
		t.Fatal("a size is held only once the page says it is in view")
	}
	apply(`{"type":"terminal.resize","cols":46,"rows":41,"keep":true}`)
	if size, holding := session.hold.wanted(); !holding || size != coreSize(46, 41) {
		t.Fatalf("in view: wanted %x, holding %v", size, holding)
	}
	if !session.hold.spend() || session.hold.spend() {
		t.Fatal("each keep:true allows one take-back")
	}
	apply(`{"type":"terminal.resize","cols":46,"rows":41,"keep":true}`)
	session.size.Store(coreSize(120, 40))
	apply(`{"type":"terminal.resize","cols":46,"rows":41,"keep":false}`)
	if _, holding := session.hold.wanted(); holding || session.hold.spend() {
		t.Fatal("out of view, the size is no longer held")
	}
}

// Two Shepherdr browsers holding one terminal, and the Herdr app window.
// Core applies each resize in turn, and every holder sees each size change
// as the stream's size callback does. A program may answer each size change
// with something the app window replies to, which gives the app the size.
func TestCoreTakeBacksAreBoundedAndNeverAnswerEachOther(t *testing.T) {
	bridge := &CoreTerminalBridge{holders: make(map[*coreLiveSession]struct{})}
	phone, desktop, other := &coreLiveSession{terminal: "t1"}, &coreLiveSession{terminal: "t1"}, &coreLiveSession{terminal: "t2"}
	for _, holder := range []*coreLiveSession{phone, desktop, other} {
		bridge.holders[holder] = struct{}{}
	}
	phone.hold.asked(coreSize(46, 35))
	phone.hold.asked(coreSize(46, 41))
	desktop.hold.asked(coreSize(160, 48))
	other.hold.asked(coreSize(120, 40))
	other.hold.keep(bridge.keeps.Add(1))
	phoneSize, desktopSize, app := coreSize(46, 41), coreSize(160, 48), coreSize(120, 40)
	current := phoneSize
	for _, holder := range []*coreLiveSession{phone, desktop} {
		holder.size.Store(current)
	}
	// run gives the terminal a size and plays out what follows.
	run := func(to uint32, programAnswers bool) int {
		t.Helper()
		pending, takeBacks := []uint32{to}, 0
		for len(pending) > 0 && takeBacks < 100 {
			next := pending[0]
			pending = pending[1:]
			if next == current {
				continue
			}
			current = next
			for _, holder := range []*coreLiveSession{phone, desktop} {
				if previous := holder.size.Swap(next); previous != next && bridge.takesSizeBack(holder, next) {
					size, _ := holder.hold.wanted()
					pending = append(pending, size)
					takeBacks++
				}
			}
			if programAnswers && next != app {
				pending = append(pending, app)
			}
		}
		return takeBacks
	}
	keep := func(holder *coreLiveSession) { holder.hold.keep(bridge.keeps.Add(1)) }
	check := func(name string, takeBacks, wantTakeBacks int, want uint32) {
		t.Helper()
		if current != want || takeBacks != wantTakeBacks {
			t.Fatalf("%s: ended at %x after %d take-backs, want %x after %d", name, current, takeBacks, want, wantTakeBacks)
		}
	}

	if run(app, false) != 0 || current != app {
		t.Fatal("a page that has not said it is in view does not hold")
	}
	keep(phone)
	check("phone comes into view", run(phoneSize, false), 0, phoneSize)
	keep(desktop)
	check("desktop's input", run(desktopSize, false), 0, desktopSize)
	check("app after desktop's input", run(app, false), 1, desktopSize)
	check("app again before desktop's next keep", run(app, false), 0, app)
	keep(phone)
	check("phone's input", run(phoneSize, false), 0, phoneSize)
	check("app after phone's input", run(app, false), 1, phoneSize)
	keep(phone)
	check("a program answering every size change", run(app, true), 1, app)
	keep(desktop)
	check("desktop's input after the loop", run(desktopSize, true), 1, app)
	keep(desktop)
	check("desktop switches view", run(desktopSize, false), 0, desktopSize)
	keep(phone)
	check("phone switches view", run(phoneSize, false), 0, phoneSize)
	phone.hold.stop()
	check("phone out of view: the desktop in view holds", run(app, false), 1, desktopSize)
	desktop.hold.stop()
	check("both out of view", run(app, false), 0, app)
	keep(desktop)
	keep(phone)
	run(phoneSize, false)
	check("Shepherdr sizes between pages", run(desktopSize, false), 0, desktopSize)
	check("and back", run(phoneSize, false), 0, phoneSize)
}

func TestOnlyACoreResizeCarriesKeep(t *testing.T) {
	for _, message := range []string{
		`{"type":"terminal.resize","cols":46,"rows":41,"keep":true}`,
		`{"type":"terminal.resize","cols":46,"rows":41,"keep":false}`,
	} {
		if command, err := validatedTerminalCommand([]byte(message)); err != nil || command.holdSize == nil {
			t.Fatalf("keep resize %s: %+v %v", message, command, err)
		}
	}
	for _, message := range []string{
		`{"type":"terminal.input","text":"x","request_id":1,"keep":true}`,
		`{"type":"terminal.release","keep":true}`,
	} {
		if _, err := validatedTerminalCommand([]byte(message)); err == nil {
			t.Fatalf("accepted %s", message)
		}
	}
}

// The page says only whether it is in view; it never names a turn or label.
func TestCoreViewMessageCarriesOnlyVisibility(t *testing.T) {
	for _, test := range []struct {
		message       string
		visible, isOK bool
	}{
		{`{"type":"terminal.view","visible":true}`, true, true},
		{`{"type":"terminal.view","visible":false}`, false, true},
		{`{"type":"terminal.view"}`, false, false},
		{`{"type":"terminal.view","visible":"yes"}`, false, false},
		{`{"type":"terminal.view","visible":true,"turn":9}`, false, false},
		{`{"type":"terminal.view","visible":true} {}`, false, false},
		{`{"type":"terminal.resize","visible":true}`, false, false},
	} {
		visible, ok := coreViewMessage([]byte(test.message))
		if visible != test.visible || ok != test.isOK {
			t.Errorf("%s = %t, %t", test.message, visible, ok)
		}
	}
}

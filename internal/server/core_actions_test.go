package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/luiscleto/shepherdr/internal/herdr"
)

const (
	coreAlpha    = "sp4d15c560d9928dee4d7842adfe949cb6"
	coreAlphaTab = "tba5ed0454645e0dac10d29a4b83da7df7"
	coreSecond   = "tb7c3a8ed2988a3811e31c1b41c81d4ba0"
	coreRepo     = "spc240a9a46604c00dbc3b616dd243bddc"
	coreRepoTab  = "tbdc8eb027b46091d1a984e46cd625290a"
	coreID       = "cac4bb11530d086b9153628d8382a040"
)

// fakeCore answers reads from the recorded app read and records kills. A kill
// ends the terminal and the workers that die with it, as core does.
type fakeCore struct {
	mu       sync.Mutex
	read     herdr.CoreRead
	kills    []string
	readErr  error
	kill     func(address string) (string, error)
	runs     []string
	runErr   error
	afterRun func()
	labels   []string
}

func newFakeCore(t *testing.T) *fakeCore {
	t.Helper()
	data, err := os.ReadFile("../herdr/testdata/core-read-app.json")
	if err != nil {
		t.Fatal(err)
	}
	var captured struct {
		Hello struct {
			Core    struct{ ID string } `json:"core"`
			Version string              `json:"version"`
		} `json:"hello"`
		List struct {
			Terminals []herdr.CoreTerminal `json:"terminals"`
		} `json:"terminal.list"`
		Document struct {
			Value json.RawMessage `json:"value"`
		} `json:"doc.get"`
	}
	if err := json.Unmarshal(data, &captured); err != nil {
		t.Fatal(err)
	}
	return &fakeCore{read: herdr.CoreRead{
		CoreID: captured.Hello.Core.ID, Version: captured.Hello.Version,
		Terminals: captured.List.Terminals, Document: captured.Document.Value,
	}}
}

func (f *fakeCore) Read(context.Context) (herdr.CoreRead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	read := f.read
	read.Terminals = slices.Clone(f.read.Terminals)
	return read, f.readErr
}

func (f *fakeCore) Kill(_ context.Context, address string) (string, error) {
	f.mu.Lock()
	f.kills = append(f.kills, address)
	custom := f.kill
	f.mu.Unlock()
	if custom != nil {
		return custom(address)
	}
	f.end(address)
	return "finished", nil
}

// Run records the run and starts a labelled terminal, as core does, then
// calls afterRun.
func (f *fakeCore) Run(_ context.Context, folder, space, tab string) (string, error) {
	f.mu.Lock()
	f.runs = append(f.runs, folder+" "+space+" "+tab)
	if f.runErr != nil {
		defer f.mu.Unlock()
		return "", f.runErr
	}
	address := fmt.Sprintf("t%d", 30+len(f.runs))
	f.read.Terminals = append(f.read.Terminals, herdr.CoreTerminal{
		Address: address, CWD: folder, CreatedMS: uint64(2_000_000_000_000 + len(f.runs)),
		Labels: map[string]string{"herdr.space": space, "herdr.tab": tab},
	})
	after := f.afterRun
	f.mu.Unlock()
	if after != nil {
		after()
	}
	return address, nil
}

// Label sets labels as core does: an empty value removes one.
func (f *fakeCore) Label(_ context.Context, address string, labels map[string]string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.labels = append(f.labels, address)
	for index := range f.read.Terminals {
		terminal := &f.read.Terminals[index]
		if terminal.Address != address {
			continue
		}
		merged := map[string]string{}
		for key, value := range terminal.Labels {
			merged[key] = value
		}
		for key, value := range labels {
			if value == "" {
				delete(merged, key)
			} else {
				merged[key] = value
			}
		}
		terminal.Labels = merged
		return merged, nil
	}
	return nil, &herdr.APIError{Code: "not_found", Message: "not_found"}
}

func (f *fakeCore) ran() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.runs)
}

func (f *fakeCore) end(address string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ending := herdr.CoreEndsWith(f.read.Terminals, address)
	f.read.Terminals = slices.DeleteFunc(f.read.Terminals, func(terminal herdr.CoreTerminal) bool {
		_, ends := ending[terminal.Address]
		return ends
	})
}

func (f *fakeCore) change(change func(*herdr.CoreRead)) {
	f.mu.Lock()
	change(&f.read)
	f.mu.Unlock()
}

func (f *fakeCore) killed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.kills)
}

func coreActionHandler(core *fakeCore, refresh homeRefresher) http.Handler {
	return (&Server{coreActions: newCoreActionCoordinator(core, "/home/user", refresh)}).Handler()
}

func coreTarget(workspaceID, tabID, address string) string {
	return `"workspace_id":"` + workspaceID + `","tab_id":"` + tabID + `","pane_id":"` + address + `","terminal_id":"` + coreID + ":" + address + `"`
}

// prepareCore prepares an action and returns the body that confirms it.
func prepareCore(t *testing.T, handler http.Handler, endpoint, body string) (map[string]json.RawMessage, string) {
	t.Helper()
	response := performSideActionRequest(t, handler, endpoint+"/prepare", body)
	assertOutcome(t, response, http.StatusOK, "prepared", "")
	var prepared map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &prepared); err != nil {
		t.Fatal(err)
	}
	confirm := `{"herdr":"core","action":` + string(prepared["action"]) + `,"expected":` + string(prepared["expected"]) + `}`
	if workspaceID, ok := prepared["workspace_id"]; ok {
		confirm = `{"herdr":"core","action":` + string(prepared["action"]) + `,"workspace_id":` + string(workspaceID) + `,"expected":` + string(prepared["expected"]) + `}`
	}
	var expected map[string]json.RawMessage
	if err := json.Unmarshal(prepared["expected"], &expected); err != nil {
		t.Fatal(err)
	}
	return expected, confirm
}

func TestCoreCloseTerminalKillsTheExactAddressAndCountsItsWorkers(t *testing.T) {
	core := newFakeCore(t)
	refresh := &countingHomeRefresher{}
	handler := coreActionHandler(core, refresh)

	// t3 started worker t9, which dies with it; t4 stays in the tab.
	expected, confirm := prepareCore(t, handler, "/api/terminal-actions",
		`{"herdr":"core","action":"close_terminal",`+coreTarget(coreAlpha, coreAlphaTab, "t3")+`}`)
	if string(expected["closes_workers"]) != "1" || string(expected["closes_tab"]) != "false" ||
		string(expected["workspace_label"]) != `"alpha"` || string(expected["tab_label"]) != `"main"` || expected["workspace_expected"] != nil {
		t.Fatalf("facts = %v", expected)
	}
	assertOutcome(t, performSideActionRequest(t, handler, "/api/terminal-actions", confirm), http.StatusOK, "succeeded", "")
	if got := core.killed(); !reflect.DeepEqual(got, []string{"t3"}) {
		t.Fatalf("kills = %q", got)
	}
	if refresh.count.Load() != 1 {
		t.Fatalf("refreshes = %d", refresh.count.Load())
	}
}

func TestCoreCloseTerminalSaysWhenItsTabCloses(t *testing.T) {
	core := newFakeCore(t)
	expected, _ := prepareCore(t, coreActionHandler(core, nil), "/api/terminal-actions",
		`{"herdr":"core","action":"close_terminal",`+coreTarget(coreAlpha, coreSecond, "t5")+`}`)
	if string(expected["closes_tab"]) != "true" || string(expected["tab_label"]) != `"2"` || expected["closes_workers"] != nil {
		t.Fatalf("facts = %v", expected)
	}
}

func TestCoreCloseOfASpacesLastTerminalConfirmsAndClosesTheSpace(t *testing.T) {
	core := newFakeCore(t)
	refresh := &countingHomeRefresher{}
	handler := coreActionHandler(core, refresh)
	expected, confirm := prepareCore(t, handler, "/api/terminal-actions",
		`{"herdr":"core","action":"close_terminal",`+coreTarget(coreRepo, coreRepoTab, "t6")+`}`)
	if string(expected["workspace_action"]) != `"close_workspace"` || string(expected["closes_tab"]) != "false" {
		t.Fatalf("facts = %v", expected)
	}
	var space confirmationFacts
	if err := json.Unmarshal(expected["workspace_expected"], &space); err != nil || space.WorkspaceLabel != "repo" ||
		!reflect.DeepEqual(space.ScopeWorkspaceIDs, []string{coreRepo}) {
		t.Fatalf("space facts = %s", expected["workspace_expected"])
	}
	assertOutcome(t, performSideActionRequest(t, handler, "/api/terminal-actions", confirm), http.StatusOK, "succeeded", "")
	if got := core.killed(); !reflect.DeepEqual(got, []string{"t6"}) {
		t.Fatalf("kills = %q", got)
	}
}

func TestCoreCloseSpaceKillsWorkersFirstAndSkipsTerminalsAlreadyEnded(t *testing.T) {
	core := newFakeCore(t)
	working, blocked, failed := "working", "blocked", "error"
	core.change(func(read *herdr.CoreRead) {
		read.Terminals[0].Status = &herdr.CoreStatus{App: "claude", State: working}
		read.Terminals[1].Status = &herdr.CoreStatus{App: "claude", State: blocked}
		read.Terminals[2].Status = &herdr.CoreStatus{App: "codex", State: failed}
	})
	refresh := &countingHomeRefresher{}
	handler := coreActionHandler(core, refresh)
	expected, confirm := prepareCore(t, handler, "/api/workspace-actions", `{"herdr":"core","action":"close_workspace","workspace_id":"`+coreAlpha+`"}`)
	var facts confirmationFacts
	if err := json.Unmarshal(mustMarshal(t, expected), &facts); err != nil {
		t.Fatal(err)
	}
	want := confirmationFacts{
		WorkspaceLabel: "alpha", ScopeWorkspaceIDs: []string{coreAlpha}, AgentTotal: 3,
		InterruptionCounts: interruptionCounts{Working: 1, Blocked: 1, Error: 1},
	}
	if !reflect.DeepEqual(facts, want) {
		t.Fatalf("facts = %+v", facts)
	}
	// t5 exits on its own while t9 closes, so it is not killed.
	core.kill = func(address string) (string, error) {
		core.end(address)
		if address == "t9" {
			core.end("t5")
		}
		return "finished", nil
	}
	assertOutcome(t, performSideActionRequest(t, handler, "/api/workspace-actions", confirm), http.StatusOK, "succeeded", "")
	// t9 is t3's worker, so it ends before t3.
	if got := core.killed(); !reflect.DeepEqual(got, []string{"t9", "t3", "t4"}) {
		t.Fatalf("kills = %q", got)
	}
	if refresh.count.Load() != 1 {
		t.Fatalf("refreshes = %d", refresh.count.Load())
	}
}

func TestCoreCloseSpaceEndsOnlyItsOwnDescendantsDeepestFirst(t *testing.T) {
	core := newFakeCore(t)
	t4, t9 := "t4", "t9"
	core.change(func(read *herdr.CoreRead) {
		read.Terminals = append(read.Terminals,
			// t11 is t9's worker, two levels below t3.
			herdr.CoreTerminal{Address: "t11", Parent: &t9, DieWithParent: true, CreatedMS: 1793406425400},
			// t10 outlives t4 but is shown with it.
			herdr.CoreTerminal{Address: "t10", Parent: &t4, CreatedMS: 1793406425500},
		)
		for index := range read.Terminals {
			if read.Terminals[index].Address == "t7" {
				// The app placed t7, a sub-agent of t4, in another space.
				read.Terminals[index].Parent = &t4
			}
		}
	})
	handler := coreActionHandler(core, nil)
	_, confirm := prepareCore(t, handler, "/api/workspace-actions", `{"herdr":"core","action":"close_workspace","workspace_id":"`+coreAlpha+`"}`)
	assertOutcome(t, performSideActionRequest(t, handler, "/api/workspace-actions", confirm), http.StatusOK, "succeeded", "")
	if got := core.killed(); !reflect.DeepEqual(got, []string{"t11", "t9", "t10", "t3", "t4", "t5"}) {
		t.Fatalf("kills = %q", got)
	}
	// t7 and t8, under Other terminals, are left running.
	read, _ := core.Read(context.Background())
	if !coreTerminalLive(read.Terminals, "t7") || !coreTerminalLive(read.Terminals, "t8") {
		t.Fatalf("live after = %+v", read.Terminals)
	}
}

func TestCoreCloseTerminalCountsAWorkerThatMovesToOtherTerminalsAsGone(t *testing.T) {
	core := newFakeCore(t)
	t5, t6 := "t5", "t6"
	core.change(func(read *herdr.CoreRead) {
		read.Terminals = append(read.Terminals,
			// Both outlive their parent; Home shows them with it until it ends.
			herdr.CoreTerminal{Address: "t16", Parent: &t6, CreatedMS: 1793406425400},
			herdr.CoreTerminal{Address: "t17", Parent: &t5, CreatedMS: 1793406425500},
		)
	})
	refresh := &countingHomeRefresher{}
	handler := coreActionHandler(core, refresh)

	// t5 is the only terminal its tab places, so the tab closes.
	expected, _ := prepareCore(t, handler, "/api/terminal-actions",
		`{"herdr":"core","action":"close_terminal",`+coreTarget(coreAlpha, coreSecond, "t5")+`}`)
	if string(expected["closes_tab"]) != "true" || expected["closes_workers"] != nil || expected["workspace_expected"] != nil {
		t.Fatalf("tab facts = %v", expected)
	}

	// t6 is the only terminal the space places, so the space closes.
	expected, confirm := prepareCore(t, handler, "/api/terminal-actions",
		`{"herdr":"core","action":"close_terminal",`+coreTarget(coreRepo, coreRepoTab, "t6")+`}`)
	if string(expected["workspace_action"]) != `"close_workspace"` || string(expected["closes_tab"]) != "false" {
		t.Fatalf("space facts = %v", expected)
	}
	assertOutcome(t, performSideActionRequest(t, handler, "/api/terminal-actions", confirm), http.StatusOK, "succeeded", "")
	// The person confirmed Close space, which ends every terminal shown in it.
	if got := core.killed(); !reflect.DeepEqual(got, []string{"t16", "t6"}) {
		t.Fatalf("kills = %q", got)
	}
}

func TestCoreCloseSpaceIsUnknownWhenATerminalRemains(t *testing.T) {
	core := newFakeCore(t)
	refresh := &countingHomeRefresher{}
	handler := coreActionHandler(core, refresh)
	_, confirm := prepareCore(t, handler, "/api/workspace-actions", `{"herdr":"core","action":"close_workspace","workspace_id":"`+coreRepo+`"}`)
	// The person adds a tab to the space in the app while it closes.
	core.kill = func(address string) (string, error) {
		core.end(address)
		core.change(func(read *herdr.CoreRead) {
			read.Document = json.RawMessage(strings.Replace(string(read.Document), `"t6"`, `"t8"`, 1))
		})
		return "finished", nil
	}
	response := performSideActionRequest(t, handler, "/api/workspace-actions", confirm)
	assertOutcome(t, response, http.StatusBadGateway, "unknown", "")
	if refresh.count.Load() != 1 {
		t.Fatalf("refreshes = %d", refresh.count.Load())
	}
}

func TestCoreActionsRefuseStaleAndMissingTargets(t *testing.T) {
	core := newFakeCore(t)
	handler := coreActionHandler(core, nil)
	_, closeTerminal := prepareCore(t, handler, "/api/terminal-actions",
		`{"herdr":"core","action":"close_terminal",`+coreTarget(coreAlpha, coreAlphaTab, "t4")+`}`)
	_, closeSpace := prepareCore(t, handler, "/api/workspace-actions", `{"herdr":"core","action":"close_workspace","workspace_id":"`+coreAlpha+`"}`)

	// An agent starts in the space: both confirmations are out of date.
	core.change(func(read *herdr.CoreRead) {
		read.Terminals[0].Status = &herdr.CoreStatus{App: "claude", State: "working"}
	})
	assertOutcome(t, performSideActionRequest(t, handler, "/api/workspace-actions", closeSpace), http.StatusConflict, "refused", "stale")
	core.change(func(read *herdr.CoreRead) { read.Terminals[0].Status = nil })
	// The person closes t3 and t9 in the app: t4 is now the tab's last.
	core.end("t3")
	assertOutcome(t, performSideActionRequest(t, handler, "/api/terminal-actions", closeTerminal), http.StatusConflict, "refused", "stale")
	// The person closes the space in the app.
	for _, address := range []string{"t4", "t5"} {
		core.end(address)
	}
	assertOutcome(t, performSideActionRequest(t, handler, "/api/workspace-actions", closeSpace), http.StatusNotFound, "refused", "not_found")
	assertOutcome(t, performSideActionRequest(t, handler, "/api/terminal-actions", closeTerminal), http.StatusNotFound, "refused", "not_found")
	if got := core.killed(); len(got) != 0 {
		t.Fatalf("kills = %q", got)
	}
}

func TestCoreOtherTerminalsHasNoSpaceActionButItsTerminalsClose(t *testing.T) {
	core := newFakeCore(t)
	handler := coreActionHandler(core, nil)
	assertOutcome(t, performSideActionRequest(t, handler, "/api/workspace-actions/prepare",
		`{"herdr":"core","action":"close_workspace","workspace_id":"other"}`), http.StatusConflict, "refused", "not_applicable")
	expected, confirm := prepareCore(t, handler, "/api/terminal-actions",
		`{"herdr":"core","action":"close_terminal",`+coreTarget("other", "other", "t8")+`}`)
	if string(expected["closes_tab"]) != "false" || expected["workspace_expected"] != nil || string(expected["workspace_label"]) != `"Other terminals"` {
		t.Fatalf("facts = %v", expected)
	}
	assertOutcome(t, performSideActionRequest(t, handler, "/api/terminal-actions", confirm), http.StatusOK, "succeeded", "")
}

func TestCoreKillErrorsMapToNotSentRefusedOrUnknown(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		status  int
		outcome string
		reason  string
	}{
		{"no core", &herdr.MutationError{Err: errors.New("connect")}, http.StatusServiceUnavailable, "refused", "herdr_unavailable"},
		{"gone", &herdr.APIError{Code: "not_found", Message: "not_found"}, http.StatusNotFound, "refused", "not_found"},
		{"refused", &herdr.APIError{Code: "machine_unknown", Message: "machine_unknown"}, http.StatusUnprocessableEntity, "refused", "herdr_refused"},
		{"no reply", &herdr.MutationError{Err: errors.New("read"), Submitted: true}, http.StatusBadGateway, "unknown", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			core := newFakeCore(t)
			handler := coreActionHandler(core, nil)
			_, confirm := prepareCore(t, handler, "/api/terminal-actions",
				`{"herdr":"core","action":"close_terminal",`+coreTarget(coreAlpha, coreAlphaTab, "t4")+`}`)
			core.kill = func(string) (string, error) { return "", test.err }
			assertOutcome(t, performSideActionRequest(t, handler, "/api/terminal-actions", confirm), test.status, test.outcome, test.reason)
			if got := core.killed(); len(got) != 1 {
				t.Fatalf("kills = %q, want one with no retry", got)
			}
		})
	}

	// After one kill took effect, any error is unknown.
	core := newFakeCore(t)
	handler := coreActionHandler(core, nil)
	_, confirm := prepareCore(t, handler, "/api/workspace-actions", `{"herdr":"core","action":"close_workspace","workspace_id":"`+coreAlpha+`"}`)
	core.kill = func(address string) (string, error) {
		if address == "t9" {
			core.end(address)
			return "finished", nil
		}
		return "", &herdr.APIError{Code: "not_found", Message: "not_found"}
	}
	assertOutcome(t, performSideActionRequest(t, handler, "/api/workspace-actions", confirm), http.StatusBadGateway, "unknown", "")
	if got := core.killed(); !reflect.DeepEqual(got, []string{"t9", "t3"}) {
		t.Fatalf("kills = %q", got)
	}
}

func TestActionsGoOnlyToTheNamedHerdr(t *testing.T) {
	core := newFakeCore(t)
	coreOnly := coreActionHandler(core, nil)
	for _, body := range []string{
		`{"action":"close_workspace","workspace_id":"` + coreAlpha + `"}`,
		`{"herdr":"tui","action":"close_workspace","workspace_id":"` + coreAlpha + `"}`,
		`{"herdr":"app","action":"close_workspace","workspace_id":"` + coreAlpha + `"}`,
		`{"herdr":"core","action":"delete_checkout","workspace_id":"` + coreAlpha + `"}`,
	} {
		assertOutcome(t, performSideActionRequest(t, coreOnly, "/api/workspace-actions/prepare", body), http.StatusBadRequest, "refused", "invalid_request")
	}
	for _, body := range []string{
		`{"herdr":"core","action":"create_space","working_directory":"/work"}`,
		`{"herdr":"core","action":"split_terminal",` + coreTarget(coreAlpha, coreAlphaTab, "t4") + `,"direction":"right"}`,
		`{"herdr":"core","action":"rename_terminal",` + coreTarget(coreAlpha, coreAlphaTab, "t4") + `,"name":"x"}`,
	} {
		endpoint := "/api/terminal-actions"
		if strings.Contains(body, "create_space") {
			endpoint = "/api/workspace-actions"
		}
		assertOutcome(t, performSideActionRequest(t, coreOnly, endpoint, body), http.StatusBadRequest, "refused", "invalid_request")
	}
	if got := core.killed(); len(got) != 0 {
		t.Fatalf("kills = %q", got)
	}

	// The TUI's actions never reach core.
	tuiOnly := actionHandler(&fakeWorkspaceActionClient{}, nil)
	assertOutcome(t, performSideActionRequest(t, tuiOnly, "/api/workspace-actions/prepare",
		`{"herdr":"core","action":"close_workspace","workspace_id":"`+coreAlpha+`"}`), http.StatusBadRequest, "refused", "invalid_request")
}

func mustMarshal(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Pin and Unpin write only herdr.pinned on the exact current agent, which
// must still offer the action, with the app window open or closed.
func TestCorePinAndUnpinWriteTheExactAgentsPinLabel(t *testing.T) {
	core := newFakeCore(t)
	core.change(func(read *herdr.CoreRead) {
		read.Terminals[0].Status = &herdr.CoreStatus{App: "claude", State: "done"}
	})
	refresh := &countingHomeRefresher{}
	handler := coreActionHandler(core, refresh)
	pin := `{"herdr":"core","action":"pin_agent",` + coreTarget(coreAlpha, coreAlphaTab, "t3") + `}`
	unpin := `{"herdr":"core","action":"unpin_agent",` + coreTarget(coreAlpha, coreAlphaTab, "t3") + `}`
	pinned := func() string {
		read, _ := core.Read(context.Background())
		return read.Terminals[0].Labels[herdr.CorePinnedLabel]
	}

	assertOutcome(t, performSideActionRequest(t, handler, "/api/terminal-actions", pin), http.StatusOK, "succeeded", "")
	if pinned() != "1" || refresh.count.Load() != 1 {
		t.Fatalf("pin label = %q, refreshes = %d", pinned(), refresh.count.Load())
	}
	// Closing a pinned agent confirms the same facts as before it was pinned.
	expected, confirm := prepareCore(t, handler, "/api/terminal-actions",
		`{"herdr":"core","action":"close_terminal",`+coreTarget(coreAlpha, coreAlphaTab, "t3")+`}`)
	if string(expected["agent"]) != `{"kind":"claude","name":"claude","status":"done"}` {
		t.Fatalf("close facts agent = %s", expected["agent"])
	}
	if !strings.Contains(confirm, `"status":"done"}`) {
		t.Fatalf("confirm = %s", confirm)
	}
	// Already pinned: only Unpin applies now.
	assertOutcome(t, performSideActionRequest(t, handler, "/api/terminal-actions", pin), http.StatusConflict, "refused", "not_applicable")
	assertOutcome(t, performSideActionRequest(t, handler, "/api/terminal-actions", unpin), http.StatusOK, "succeeded", "")
	if _, present := core.read.Terminals[0].Labels[herdr.CorePinnedLabel]; present {
		t.Fatalf("unpin left labels %v", core.read.Terminals[0].Labels)
	}

	// A plain terminal is not an agent; another core's terminal id, a stale
	// tab, an extra field and an unknown address are refused, and nothing is
	// written.
	before := slices.Clone(core.labels)
	for _, test := range []struct {
		body, reason string
		status       int
	}{
		{`{"herdr":"core","action":"pin_agent",` + coreTarget(coreAlpha, coreAlphaTab, "t4") + `}`, "not_applicable", http.StatusConflict},
		{`{"herdr":"core","action":"pin_agent","workspace_id":"` + coreAlpha + `","tab_id":"` + coreAlphaTab + `","pane_id":"t3","terminal_id":"other:t3"}`, "not_found", http.StatusNotFound},
		{`{"herdr":"core","action":"pin_agent",` + coreTarget(coreAlpha, coreSecond, "t3") + `}`, "not_found", http.StatusNotFound},
		{`{"herdr":"core","action":"pin_agent",` + coreTarget(coreAlpha, coreAlphaTab, "t77") + `}`, "not_found", http.StatusNotFound},
		{`{"herdr":"core","action":"pin_agent","labels":{"herdr.seen":"9"},` + coreTarget(coreAlpha, coreAlphaTab, "t3") + `}`, "invalid_request", http.StatusBadRequest},
	} {
		assertOutcome(t, performSideActionRequest(t, handler, "/api/terminal-actions", test.body), test.status, "refused", test.reason)
	}
	if !slices.Equal(core.labels, before) {
		t.Fatalf("labels written = %q", core.labels)
	}
}

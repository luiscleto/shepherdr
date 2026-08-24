package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luiscleto/shepherdr/internal/herdr"
)

func TestDestructiveActionsUseFreshExactFactsAndOpaqueTarget(t *testing.T) {
	base := actionGroupSnapshot()
	var mu sync.Mutex
	current := base
	var closed string
	client := &fakeWorkspaceActionClient{
		snapshot: func(context.Context) (herdr.Snapshot, error) {
			mu.Lock()
			defer mu.Unlock()
			return current, nil
		},
		closeWorkspace: func(_ context.Context, workspaceID string) error {
			closed = workspaceID
			return nil
		},
	}
	refresh := &countingHomeRefresher{}
	handler := actionHandler(client, refresh)

	preparedResponse := performActionRequest(t, handler, "/api/workspace-actions/prepare", `{"action":"close_group","workspace_id":"opaque-parent"}`)
	if preparedResponse.Code != http.StatusOK {
		t.Fatalf("prepare status = %d body=%s", preparedResponse.Code, preparedResponse.Body.String())
	}
	var prepared struct {
		Outcome     string            `json:"outcome"`
		Action      string            `json:"action"`
		WorkspaceID string            `json:"workspace_id"`
		Expected    confirmationFacts `json:"expected"`
	}
	if err := json.Unmarshal(preparedResponse.Body.Bytes(), &prepared); err != nil {
		t.Fatal(err)
	}
	if prepared.Outcome != "prepared" || prepared.Action != "close_group" || prepared.WorkspaceID != "opaque-parent" {
		t.Fatalf("prepared response = %+v", prepared)
	}
	if got, want := strings.Join(prepared.Expected.ScopeWorkspaceIDs, ","), "opaque-child,opaque-parent"; got != want {
		t.Fatalf("scope ids = %q, want %q", got, want)
	}
	if prepared.Expected.WorkspaceLabel != "Repository" || prepared.Expected.AgentTotal != 5 ||
		prepared.Expected.InterruptionCounts != (interruptionCounts{Working: 1, Blocked: 1, Unknown: 1}) {
		t.Fatalf("prepared facts = %+v", prepared.Expected)
	}

	runBody, err := json.Marshal(map[string]any{
		"action": "close_group", "workspace_id": "opaque-parent", "expected": prepared.Expected,
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	current.Workspaces = append(current.Workspaces, herdr.WorkspaceInfo{
		WorkspaceID: "new-child", Label: "New", Worktree: &herdr.WorktreeInfo{
			Valid: true, RepoKey: "repo", CheckoutPath: "/repo/new", IsLinkedWorktree: true,
		},
	})
	mu.Unlock()
	stale := performActionRequest(t, handler, "/api/workspace-actions", string(runBody))
	assertOutcome(t, stale, http.StatusConflict, "refused", "stale")
	if closed != "" || refresh.count.Load() != 0 {
		t.Fatalf("stale request mutated %q or requested %d refreshes", closed, refresh.count.Load())
	}

	mu.Lock()
	current = base
	mu.Unlock()
	succeeded := performActionRequest(t, handler, "/api/workspace-actions", string(runBody))
	assertOutcome(t, succeeded, http.StatusOK, "succeeded", "")
	if closed != "opaque-parent" {
		t.Fatalf("workspace.close targeted %q, want opaque-parent", closed)
	}
	if refresh.count.Load() != 1 {
		t.Fatalf("successful action requested %d Home refreshes, want 1", refresh.count.Load())
	}
}

func TestCreationExpandsOnlyTildeAndOmitsBlankOptionalFields(t *testing.T) {
	snapshot := herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{{
		WorkspaceID: "opaque-parent", Worktree: &herdr.WorktreeInfo{Valid: true, RepoKey: "repo", CheckoutPath: "/repo"},
	}}}
	type createCall struct {
		cwd   string
		label *string
	}
	var spaces []createCall
	var worktreeSource herdr.CreateWorktreeSource
	var worktreeBranch *string
	client := &fakeWorkspaceActionClient{
		snapshot: func(context.Context) (herdr.Snapshot, error) { return snapshot, nil },
		createWorkspace: func(_ context.Context, cwd string, label *string) error {
			spaces = append(spaces, createCall{cwd: cwd, label: label})
			return nil
		},
		createWorktree: func(_ context.Context, source herdr.CreateWorktreeSource, branch *string) error {
			worktreeSource, worktreeBranch = source, branch
			return nil
		},
	}
	coordinator := newWorkspaceActionCoordinator(client, &countingHomeRefresher{})
	coordinator.homeDir = func() (string, error) { return "/home/operator", nil }
	handler := (&Server{workspaceActions: coordinator}).Handler()

	for _, body := range []string{
		`{"action":"create_space","working_directory":"~","label":""}`,
		`{"action":"create_space","working_directory":"~/src/$PROJECT"}`,
		`{"action":"create_space","working_directory":"~other/repo"}`,
		`{"action":"create_space","working_directory":"$HOME/literal"}`,
		`{"action":"create_space","working_directory":"/labeled","label":"Useful"}`,
	} {
		assertOutcome(t, performActionRequest(t, handler, "/api/workspace-actions", body), http.StatusOK, "succeeded", "")
	}
	if got, want := []string{spaces[0].cwd, spaces[1].cwd, spaces[2].cwd, spaces[3].cwd, spaces[4].cwd}, []string{"/home/operator", "/home/operator/src/$PROJECT", "~other/repo", "$HOME/literal", "/labeled"}; !equalStrings(got, want) {
		t.Fatalf("created paths = %v, want %v", got, want)
	}
	for _, call := range spaces[:4] {
		if call.label != nil {
			t.Fatalf("blank or omitted label reached Herdr as %q", *call.label)
		}
	}
	if spaces[4].label == nil || *spaces[4].label != "Useful" {
		t.Fatalf("optional label = %v, want Useful", spaces[4].label)
	}
	assertOutcome(t, performActionRequest(t, handler, "/api/workspace-actions", `{"action":"create_worktree","workspace_id":"opaque-parent","branch":""}`), http.StatusOK, "succeeded", "")
	if worktreeSource != (herdr.CreateWorktreeSource{WorkspaceID: "opaque-parent"}) || worktreeBranch != nil {
		t.Fatalf("create worktree source=%+v branch=%v", worktreeSource, worktreeBranch)
	}
	assertOutcome(t, performActionRequest(t, handler, "/api/workspace-actions", `{"action":"create_worktree","workspace_id":"opaque-parent","branch":"feature/exact"}`), http.StatusOK, "succeeded", "")
	if worktreeBranch == nil || *worktreeBranch != "feature/exact" {
		t.Fatalf("optional branch = %v, want feature/exact", worktreeBranch)
	}

	coordinator.homeDir = func() (string, error) { return "", errors.New("unavailable") }
	response := performActionRequest(t, handler, "/api/workspace-actions", `{"action":"create_space","working_directory":"~"}`)
	assertOutcome(t, response, http.StatusBadRequest, "refused", "invalid_request")
}

func TestCreateWorktreeUsesFreshOrdinaryPaneState(t *testing.T) {
	current := ordinaryOnePaneActionSnapshot("/shown/one-pane")
	home, err := herdr.Project(current)
	if err != nil {
		t.Fatal(err)
	}
	if got := home.Workspaces[0].Actions; !containsWorkspaceAction(got, herdr.WorkspaceActionCreateWorktree) {
		t.Fatalf("one-pane Home actions = %v, want create_worktree", got)
	}

	var sources []herdr.CreateWorktreeSource
	client := &fakeWorkspaceActionClient{
		snapshot: func(context.Context) (herdr.Snapshot, error) { return current, nil },
		createWorktree: func(_ context.Context, source herdr.CreateWorktreeSource, _ *string) error {
			sources = append(sources, source)
			return nil
		},
	}
	handler := actionHandler(client, &countingHomeRefresher{})
	current.Panes[0].CWD = "/fresh/one-pane"
	assertOutcome(t, performActionRequest(t, handler, "/api/workspace-actions", `{"action":"create_worktree","workspace_id":"ordinary"}`), http.StatusOK, "succeeded", "")

	current = ordinaryMultiPaneActionSnapshot()
	home, err = herdr.Project(current)
	if err != nil {
		t.Fatal(err)
	}
	if got := home.Workspaces[0].Actions; !containsWorkspaceAction(got, herdr.WorkspaceActionCreateWorktree) {
		t.Fatalf("multi-pane Home actions = %v, want create_worktree", got)
	}
	current.Workspaces[0].ActiveTabID = "tab-two"
	current.Layouts[1].FocusedPaneID = "pane-three"
	current.Panes[2].CWD = "/fresh/active-focused"
	assertOutcome(t, performActionRequest(t, handler, "/api/workspace-actions", `{"action":"create_worktree","workspace_id":"ordinary","branch":""}`), http.StatusOK, "succeeded", "")

	want := []herdr.CreateWorktreeSource{{CWD: "/fresh/one-pane"}, {CWD: "/fresh/active-focused"}}
	if len(sources) != len(want) {
		t.Fatalf("create worktree sources = %+v, want %+v", sources, want)
	}
	for index := range want {
		if sources[index] != want[index] {
			t.Fatalf("create worktree source %d = %+v, want %+v", index, sources[index], want[index])
		}
	}
}

func TestCreateWorktreeRefusesUnresolvedOrdinaryPaneWithoutCallingHerdr(t *testing.T) {
	tests := map[string]func(*herdr.Snapshot){
		"missing cwd": func(snapshot *herdr.Snapshot) {
			snapshot.Panes[0].CWD = ""
		},
		"ambiguous layout": func(snapshot *herdr.Snapshot) {
			snapshot.Layouts = append(snapshot.Layouts, snapshot.Layouts[0])
		},
		"mismatched focused pane": func(snapshot *herdr.Snapshot) {
			snapshot.Layouts[0].FocusedPaneID = "pane-three"
		},
		"non-absolute cwd": func(snapshot *herdr.Snapshot) {
			snapshot.Panes[0].CWD = "relative/path"
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			snapshot := ordinaryMultiPaneActionSnapshot()
			snapshot.Workspaces[0].ActiveTabID = "tab-one"
			change(&snapshot)
			var createCalls atomic.Int32
			refresh := &countingHomeRefresher{}
			client := &fakeWorkspaceActionClient{
				snapshot: func(context.Context) (herdr.Snapshot, error) { return snapshot, nil },
				createWorktree: func(context.Context, herdr.CreateWorktreeSource, *string) error {
					createCalls.Add(1)
					return nil
				},
			}
			response := performActionRequest(t, actionHandler(client, refresh), "/api/workspace-actions", `{"action":"create_worktree","workspace_id":"ordinary"}`)
			assertOutcome(t, response, http.StatusConflict, "refused", "not_applicable")
			if createCalls.Load() != 0 || refresh.count.Load() != 0 {
				t.Fatalf("unresolved source reached Herdr %d times or refreshed Home %d times", createCalls.Load(), refresh.count.Load())
			}
		})
	}
}

func TestOrdinaryWorktreeHerdrRefusalLeavesHomeUnchanged(t *testing.T) {
	var createCalls atomic.Int32
	refresh := &countingHomeRefresher{}
	client := &fakeWorkspaceActionClient{
		snapshot: func(context.Context) (herdr.Snapshot, error) {
			return ordinaryOnePaneActionSnapshot("/not/a/repository"), nil
		},
		createWorktree: func(_ context.Context, source herdr.CreateWorktreeSource, _ *string) error {
			createCalls.Add(1)
			if source != (herdr.CreateWorktreeSource{CWD: "/not/a/repository"}) {
				t.Fatalf("Herdr source = %+v", source)
			}
			return &herdr.APIError{Code: "worktree_create_failed", Message: "not a Git repository"}
		},
	}
	response := performActionRequest(t, actionHandler(client, refresh), "/api/workspace-actions", `{"action":"create_worktree","workspace_id":"ordinary"}`)
	assertOutcome(t, response, http.StatusUnprocessableEntity, "refused", "herdr_refused")
	if createCalls.Load() != 1 || refresh.count.Load() != 0 {
		t.Fatalf("Herdr calls=%d Home refreshes=%d, want 1 and 0", createCalls.Load(), refresh.count.Load())
	}
}

func TestOneMutationAtATimeAndClientDisconnectDoesNotCancelIt(t *testing.T) {
	started := make(chan context.Context, 2)
	release := make(chan struct{})
	client := &fakeWorkspaceActionClient{
		createWorkspace: func(ctx context.Context, _ string, _ *string) error {
			started <- ctx
			<-release
			return nil
		},
	}
	refresh := &countingHomeRefresher{}
	handler := actionHandler(client, refresh)
	requestContext, cancelRequest := context.WithCancel(context.Background())
	request := httptest.NewRequest("POST", "http://localhost/api/workspace-actions", strings.NewReader(`{"action":"create_space","working_directory":"/one"}`)).WithContext(requestContext)
	request.Header.Set("Content-Type", "application/json")
	first := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() {
		handler.ServeHTTP(first, request)
		close(finished)
	}()
	mutationContext := <-started
	cancelRequest()
	select {
	case <-mutationContext.Done():
		t.Fatal("browser disconnect canceled the admitted mutation")
	default:
	}

	busy := performActionRequest(t, handler, "/api/workspace-actions", `{"action":"create_space","working_directory":"/two"}`)
	assertOutcome(t, busy, http.StatusConflict, "refused", "busy")
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("admitted mutation did not finish after browser disconnect")
	}
	assertOutcome(t, first, http.StatusOK, "succeeded", "")

	third := performActionRequest(t, handler, "/api/workspace-actions", `{"action":"create_space","working_directory":"/three"}`)
	assertOutcome(t, third, http.StatusOK, "succeeded", "")
	if refresh.count.Load() != 2 {
		t.Fatalf("completed mutations requested %d refreshes, want 2", refresh.count.Load())
	}
}

func TestHerdrRefusalAndUnknownMutationOutcomes(t *testing.T) {
	linked := herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{{
		WorkspaceID: "opaque-child", Label: "Feature", Worktree: &herdr.WorktreeInfo{
			Valid: true, RepoKey: "repo", CheckoutPath: "/repo/feature", IsLinkedWorktree: true,
		},
	}}}
	current := linked
	var removeCalls atomic.Int32
	client := &fakeWorkspaceActionClient{
		snapshot: func(context.Context) (herdr.Snapshot, error) { return current, nil },
		removeWorktree: func(context.Context, string) error {
			removeCalls.Add(1)
			return &herdr.APIError{Code: "dirty_worktree_requires_force", Message: "contains modified or untracked files"}
		},
	}
	refresh := &countingHomeRefresher{}
	handler := actionHandler(client, refresh)
	prepared := performActionRequest(t, handler, "/api/workspace-actions/prepare", `{"action":"delete_checkout","workspace_id":"opaque-child"}`)
	if prepared.Code != http.StatusOK {
		t.Fatalf("prepare delete status=%d body=%s", prepared.Code, prepared.Body.String())
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(prepared.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	runBody := `{"action":"delete_checkout","workspace_id":"opaque-child","expected":` + string(value["expected"]) + `}`
	current.Workspaces = append([]herdr.WorkspaceInfo(nil), linked.Workspaces...)
	current.Workspaces[0].Worktree = &herdr.WorktreeInfo{
		Valid: true, RepoKey: "repo", CheckoutPath: "/repo/moved-feature", IsLinkedWorktree: true,
	}
	stale := performActionRequest(t, handler, "/api/workspace-actions", runBody)
	assertOutcome(t, stale, http.StatusConflict, "refused", "stale")
	if removeCalls.Load() != 0 {
		t.Fatal("changed checkout path reached Herdr")
	}
	current = linked
	dirty := performActionRequest(t, handler, "/api/workspace-actions", runBody)
	assertOutcome(t, dirty, http.StatusUnprocessableEntity, "refused", "checkout_has_changes")
	if removeCalls.Load() != 1 || refresh.count.Load() != 0 {
		t.Fatalf("dirty refusal calls=%d refreshes=%d", removeCalls.Load(), refresh.count.Load())
	}

	for _, test := range []struct {
		name    string
		err     error
		status  int
		outcome string
		reason  string
	}{
		{"other Herdr refusal", &herdr.APIError{Code: "worktree_remove_failed", Message: "no"}, http.StatusUnprocessableEntity, "refused", "herdr_refused"},
		{"socket loss after send", &herdr.MutationError{Err: io.EOF, Submitted: true}, http.StatusBadGateway, "unknown", ""},
		{"timeout after send", &herdr.MutationError{Err: actionTimeoutError{}, Submitted: true}, http.StatusGatewayTimeout, "unknown", ""},
		{"unavailable before send", &herdr.MutationError{Err: io.EOF}, http.StatusServiceUnavailable, "refused", "herdr_unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			coordinator := newWorkspaceActionCoordinator(&fakeWorkspaceActionClient{}, nil)
			response := httptest.NewRecorder()
			coordinator.finishMutation(response, test.err)
			assertOutcome(t, response, test.status, test.outcome, test.reason)
		})
	}
}

func TestActionRoutesRequireStrictSameOriginJSON(t *testing.T) {
	handler := actionHandler(&fakeWorkspaceActionClient{}, nil)
	unknown := performActionRequest(t, handler, "/api/workspace-actions", `{"action":"create_space","working_directory":"/work","force":true}`)
	assertOutcome(t, unknown, http.StatusBadRequest, "refused", "invalid_request")
	browserCWD := performActionRequest(t, handler, "/api/workspace-actions", `{"action":"create_worktree","workspace_id":"ordinary","cwd":"/browser/chosen"}`)
	assertOutcome(t, browserCWD, http.StatusBadRequest, "refused", "invalid_request")

	wrongType := httptest.NewRequest("POST", "http://localhost/api/workspace-actions", strings.NewReader(`{"action":"create_space","working_directory":"/work"}`))
	wrongType.Header.Set("Content-Type", "text/plain")
	wrongTypeResponse := httptest.NewRecorder()
	handler.ServeHTTP(wrongTypeResponse, wrongType)
	assertOutcome(t, wrongTypeResponse, http.StatusBadRequest, "refused", "invalid_request")

	crossOrigin := httptest.NewRequest("POST", "http://localhost/api/workspace-actions", strings.NewReader(`{"action":"create_space","working_directory":"/work"}`))
	crossOrigin.Header.Set("Content-Type", "application/json")
	crossOrigin.Header.Set("Origin", "https://other.example")
	crossOriginResponse := httptest.NewRecorder()
	handler.ServeHTTP(crossOriginResponse, crossOrigin)
	assertOutcome(t, crossOriginResponse, http.StatusBadRequest, "refused", "invalid_request")
}

func TestInvalidNonNullWorktreeProvenanceCannotReachClose(t *testing.T) {
	snapshot := herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{{
		WorkspaceID: "ambiguous", Label: "Ambiguous", Worktree: &herdr.WorktreeInfo{},
	}}}
	var closeCalls atomic.Int32
	client := &fakeWorkspaceActionClient{
		snapshot: func(context.Context) (herdr.Snapshot, error) { return snapshot, nil },
		closeWorkspace: func(context.Context, string) error {
			closeCalls.Add(1)
			return nil
		},
	}
	handler := actionHandler(client, nil)

	prepared := performActionRequest(t, handler, "/api/workspace-actions/prepare", `{"action":"close_workspace","workspace_id":"ambiguous"}`)
	assertOutcome(t, prepared, http.StatusConflict, "refused", "not_applicable")
	run := performActionRequest(t, handler, "/api/workspace-actions", `{
		"action":"close_workspace",
		"workspace_id":"ambiguous",
		"expected":{
			"workspace_label":"Ambiguous",
			"scope_workspace_ids":["ambiguous"],
			"agent_total":0,
			"interruption_counts":{"working":0,"blocked":0,"unknown":0}
		}
	}`)
	assertOutcome(t, run, http.StatusConflict, "refused", "not_applicable")
	if closeCalls.Load() != 0 {
		t.Fatalf("invalid worktree provenance reached workspace.close %d times", closeCalls.Load())
	}
}

type fakeWorkspaceActionClient struct {
	snapshot        func(context.Context) (herdr.Snapshot, error)
	createWorkspace func(context.Context, string, *string) error
	createWorktree  func(context.Context, herdr.CreateWorktreeSource, *string) error
	closeWorkspace  func(context.Context, string) error
	removeWorktree  func(context.Context, string) error
}

func (c *fakeWorkspaceActionClient) Snapshot(ctx context.Context) (herdr.Snapshot, error) {
	if c.snapshot == nil {
		return herdr.Snapshot{}, nil
	}
	return c.snapshot(ctx)
}

func (c *fakeWorkspaceActionClient) CreateWorkspace(ctx context.Context, cwd string, label *string) error {
	if c.createWorkspace == nil {
		return nil
	}
	return c.createWorkspace(ctx, cwd, label)
}

func (c *fakeWorkspaceActionClient) CreateWorktree(ctx context.Context, source herdr.CreateWorktreeSource, branch *string) error {
	if c.createWorktree == nil {
		return nil
	}
	return c.createWorktree(ctx, source, branch)
}

func (c *fakeWorkspaceActionClient) CloseWorkspace(ctx context.Context, workspaceID string) error {
	if c.closeWorkspace == nil {
		return nil
	}
	return c.closeWorkspace(ctx, workspaceID)
}

func (c *fakeWorkspaceActionClient) RemoveWorktree(ctx context.Context, workspaceID string) error {
	if c.removeWorktree == nil {
		return nil
	}
	return c.removeWorktree(ctx, workspaceID)
}

type countingHomeRefresher struct{ count atomic.Int32 }

func (r *countingHomeRefresher) RequestRefresh() { r.count.Add(1) }

type actionTimeoutError struct{}

func (actionTimeoutError) Error() string   { return "timed out" }
func (actionTimeoutError) Timeout() bool   { return true }
func (actionTimeoutError) Temporary() bool { return true }

func actionHandler(client workspaceActionClient, refresh homeRefresher) http.Handler {
	return (&Server{workspaceActions: newWorkspaceActionCoordinator(client, refresh)}).Handler()
}

func performActionRequest(t *testing.T, handler http.Handler, endpoint, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest("POST", "http://localhost"+endpoint, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertOutcome(t *testing.T, response *httptest.ResponseRecorder, status int, outcome, reason string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d body=%s", response.Code, status, response.Body.String())
	}
	var body struct {
		Outcome string `json:"outcome"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
	if body.Outcome != outcome || body.Reason != reason {
		t.Fatalf("response outcome=%q reason=%q, want %q %q body=%s", body.Outcome, body.Reason, outcome, reason, response.Body.String())
	}
}

func actionGroupSnapshot() herdr.Snapshot {
	return herdr.Snapshot{
		Workspaces: []herdr.WorkspaceInfo{
			{WorkspaceID: "opaque-parent", Label: "Repository", Worktree: &herdr.WorktreeInfo{Valid: true, RepoKey: "repo", CheckoutPath: "/repo"}},
			{WorkspaceID: "opaque-child", Label: "Feature", Worktree: &herdr.WorktreeInfo{Valid: true, RepoKey: "repo", CheckoutPath: "/repo/feature", IsLinkedWorktree: true}},
		},
		Agents: []herdr.AgentInfo{
			{WorkspaceID: "opaque-parent", AgentStatus: herdr.StatusWorking},
			{WorkspaceID: "opaque-child", AgentStatus: herdr.StatusBlocked},
			{WorkspaceID: "opaque-child", AgentStatus: herdr.StatusUnknown},
			{WorkspaceID: "opaque-child", AgentStatus: herdr.StatusIdle},
			{WorkspaceID: "opaque-child", AgentStatus: herdr.StatusDone},
		},
	}
}

func ordinaryOnePaneActionSnapshot(cwd string) herdr.Snapshot {
	return herdr.Snapshot{
		Workspaces: []herdr.WorkspaceInfo{{ActiveTabID: "tab-one", WorkspaceID: "ordinary", Label: "Ordinary"}},
		Tabs:       []herdr.TabInfo{{TabID: "tab-one", WorkspaceID: "ordinary"}},
		Panes: []herdr.PaneInfo{{
			CWD: cwd, PaneID: "pane-one", TabID: "tab-one", TerminalID: "terminal-one", WorkspaceID: "ordinary",
		}},
	}
}

func ordinaryMultiPaneActionSnapshot() herdr.Snapshot {
	return herdr.Snapshot{
		Workspaces: []herdr.WorkspaceInfo{{ActiveTabID: "tab-one", WorkspaceID: "ordinary", Label: "Ordinary"}},
		Tabs: []herdr.TabInfo{
			{TabID: "tab-one", WorkspaceID: "ordinary"},
			{TabID: "tab-two", WorkspaceID: "ordinary"},
		},
		Panes: []herdr.PaneInfo{
			{CWD: "/tab-one/current", PaneID: "pane-one", TabID: "tab-one", TerminalID: "terminal-one", WorkspaceID: "ordinary"},
			{CWD: "/tab-two/other", Focused: true, PaneID: "pane-two", TabID: "tab-two", TerminalID: "terminal-two", WorkspaceID: "ordinary"},
			{CWD: "/tab-two/shown-focused", PaneID: "pane-three", TabID: "tab-two", TerminalID: "terminal-three", WorkspaceID: "ordinary"},
		},
		Layouts: []herdr.LayoutInfo{
			{FocusedPaneID: "pane-one", TabID: "tab-one", WorkspaceID: "ordinary"},
			{FocusedPaneID: "pane-two", TabID: "tab-two", WorkspaceID: "ordinary"},
		},
		FocusedPaneID: "pane-one",
	}
}

func containsWorkspaceAction(actions []herdr.WorkspaceAction, want herdr.WorkspaceAction) bool {
	for _, action := range actions {
		if action == want {
			return true
		}
	}
	return false
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

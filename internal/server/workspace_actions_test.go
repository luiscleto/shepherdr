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

	"github.com/luisc/shepherdr/internal/herdr"
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
	var worktreeID string
	var worktreeBranch *string
	client := &fakeWorkspaceActionClient{
		snapshot: func(context.Context) (herdr.Snapshot, error) { return snapshot, nil },
		createWorkspace: func(_ context.Context, cwd string, label *string) error {
			spaces = append(spaces, createCall{cwd: cwd, label: label})
			return nil
		},
		createWorktree: func(_ context.Context, workspaceID string, branch *string) error {
			worktreeID, worktreeBranch = workspaceID, branch
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
	if worktreeID != "opaque-parent" || worktreeBranch != nil {
		t.Fatalf("create worktree target=%q branch=%v", worktreeID, worktreeBranch)
	}
	assertOutcome(t, performActionRequest(t, handler, "/api/workspace-actions", `{"action":"create_worktree","workspace_id":"opaque-parent","branch":"feature/exact"}`), http.StatusOK, "succeeded", "")
	if worktreeBranch == nil || *worktreeBranch != "feature/exact" {
		t.Fatalf("optional branch = %v, want feature/exact", worktreeBranch)
	}

	coordinator.homeDir = func() (string, error) { return "", errors.New("unavailable") }
	response := performActionRequest(t, handler, "/api/workspace-actions", `{"action":"create_space","working_directory":"~"}`)
	assertOutcome(t, response, http.StatusBadRequest, "refused", "invalid_request")
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

type fakeWorkspaceActionClient struct {
	snapshot        func(context.Context) (herdr.Snapshot, error)
	createWorkspace func(context.Context, string, *string) error
	createWorktree  func(context.Context, string, *string) error
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

func (c *fakeWorkspaceActionClient) CreateWorktree(ctx context.Context, workspaceID string, branch *string) error {
	if c.createWorktree == nil {
		return nil
	}
	return c.createWorktree(ctx, workspaceID, branch)
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

package herdr

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"reflect"
	"testing"
)

func TestProjectDerivesOnlyConfirmedWorkspaceActions(t *testing.T) {
	snapshot := Snapshot{Workspaces: []WorkspaceInfo{
		{WorkspaceID: "parent", Label: "Repository", Worktree: &WorktreeInfo{Valid: true, RepoKey: "repo", CheckoutPath: "/repo"}},
		{WorkspaceID: "child", Label: "Feature", Worktree: &WorktreeInfo{Valid: true, RepoKey: "repo", CheckoutPath: "/repo-feature", IsLinkedWorktree: true}},
		{WorkspaceID: "ordinary", Label: "Scratch"},
		{WorkspaceID: "linked-without-path", Label: "Incomplete", Worktree: &WorktreeInfo{Valid: true, RepoKey: "other", IsLinkedWorktree: true}},
	}}
	home, err := Project(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := home.Workspaces[0].Actions, []WorkspaceAction{WorkspaceActionCreateWorktree, WorkspaceActionCloseGroup}; !reflect.DeepEqual(got, want) {
		t.Fatalf("top-level actions = %v, want %v", got, want)
	}
	if home.Workspaces[0].CheckoutPath != "/repo" {
		t.Fatalf("top-level checkout_path = %q", home.Workspaces[0].CheckoutPath)
	}
	child := home.Workspaces[0].Worktrees[0]
	if got, want := child.Actions, []WorkspaceAction{WorkspaceActionCloseWorkspace, WorkspaceActionDeleteCheckout}; !reflect.DeepEqual(got, want) {
		t.Fatalf("linked actions = %v, want %v", got, want)
	}
	if child.CheckoutPath != "" {
		t.Fatalf("linked Home checkout_path = %q, want omitted", child.CheckoutPath)
	}
	if got, want := home.Workspaces[1].Actions, []WorkspaceAction{WorkspaceActionCloseWorkspace}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ordinary actions = %v, want %v", got, want)
	}
	if got, want := home.Workspaces[2].Actions, []WorkspaceAction{WorkspaceActionCloseWorkspace}; !reflect.DeepEqual(got, want) {
		t.Fatalf("linked workspace without a confirmed path actions = %v, want %v", got, want)
	}
}

func TestSnapshotCheckoutPathIsPublishedOnlyForTopLevelWorkspace(t *testing.T) {
	var snapshot Snapshot
	if err := json.Unmarshal([]byte(`{
		"workspaces":[
			{"workspace_id":"parent","label":"Repo","worktree":{"repo_key":"repo","checkout_path":"/repo","is_linked_worktree":false}},
			{"workspace_id":"child","label":"Feature","worktree":{"repo_key":"repo","checkout_path":"/repo/feature","is_linked_worktree":true}}
		]
	}`), &snapshot); err != nil {
		t.Fatal(err)
	}
	home, err := Project(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if home.Workspaces[0].CheckoutPath != "/repo" || home.Workspaces[0].Worktrees[0].CheckoutPath != "" {
		t.Fatalf("published checkout paths: parent=%q child=%q", home.Workspaces[0].CheckoutPath, home.Workspaces[0].Worktrees[0].CheckoutPath)
	}
}

func TestClientSendsExactConfirmedWorkspaceMutations(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "herdr.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	want := []struct {
		method string
		params map[string]any
		result map[string]any
	}{
		{"workspace.create", map[string]any{"cwd": "/work", "label": "Useful", "focus": false}, map[string]any{"type": "workspace_created", "workspace": map[string]any{}, "tab": map[string]any{}, "root_pane": map[string]any{}}},
		{"workspace.create", map[string]any{"cwd": "$HOME/literal", "focus": false}, map[string]any{"type": "workspace_created", "workspace": map[string]any{}, "tab": map[string]any{}, "root_pane": map[string]any{}}},
		{"worktree.create", map[string]any{"workspace_id": "opaque-parent", "branch": "feature/exact", "focus": false}, map[string]any{"type": "worktree_created", "workspace": map[string]any{}, "tab": map[string]any{}, "root_pane": map[string]any{}, "worktree": map[string]any{}}},
		{"worktree.create", map[string]any{"workspace_id": "opaque-parent", "focus": false}, map[string]any{"type": "worktree_created", "workspace": map[string]any{}, "tab": map[string]any{}, "root_pane": map[string]any{}, "worktree": map[string]any{}}},
		{"workspace.close", map[string]any{"workspace_id": "opaque-child"}, map[string]any{"type": "ok"}},
		{"worktree.remove", map[string]any{"workspace_id": "opaque-child", "force": false}, map[string]any{"type": "worktree_removed", "workspace_id": "opaque-child", "path": "/work/child", "forced": false}},
	}
	serverDone := make(chan error, 1)
	go func() {
		for _, expectation := range want {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				serverDone <- acceptErr
				return
			}
			var request struct {
				ID     string         `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			decodeErr := json.NewDecoder(connection).Decode(&request)
			if decodeErr == nil && (request.ID == "" || request.Method != expectation.method || !reflect.DeepEqual(request.Params, expectation.params)) {
				decodeErr = &testError{"unexpected mutation request"}
			}
			if decodeErr == nil {
				decodeErr = json.NewEncoder(connection).Encode(map[string]any{"id": request.ID, "result": expectation.result})
			}
			connection.Close()
			if decodeErr != nil {
				serverDone <- decodeErr
				return
			}
		}
		serverDone <- nil
	}()

	client := NewClient(socketPath)
	label, branch := "Useful", "feature/exact"
	for index, call := range []func() error{
		func() error { return client.CreateWorkspace(context.Background(), "/work", &label) },
		func() error { return client.CreateWorkspace(context.Background(), "$HOME/literal", nil) },
		func() error { return client.CreateWorktree(context.Background(), "opaque-parent", &branch) },
		func() error { return client.CreateWorktree(context.Background(), "opaque-parent", nil) },
		func() error { return client.CloseWorkspace(context.Background(), "opaque-child") },
		func() error { return client.RemoveWorktree(context.Background(), "opaque-child") },
	} {
		if err := call(); err != nil {
			t.Fatalf("mutation %d: %v", index, err)
		}
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestMutationWithoutMatchingResultIsUnknown(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "herdr.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		var request struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(connection).Decode(&request)
		_ = json.NewEncoder(connection).Encode(map[string]any{"id": request.ID, "result": map[string]any{"type": "ok"}})
	}()
	err = NewClient(socketPath).RemoveWorktree(context.Background(), "opaque-child")
	if err == nil || !MutationMayHaveRun(err) {
		t.Fatalf("mismatched mutation result = %v, want uncertain error", err)
	}
}

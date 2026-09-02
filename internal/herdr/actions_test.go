package herdr

import (
	"context"
	"encoding/json"
	"net"
	"reflect"
	"testing"
)

func TestProjectDerivesOnlyConfirmedWorkspaceActions(t *testing.T) {
	snapshot := Snapshot{Workspaces: []WorkspaceInfo{
		{WorkspaceID: "parent", Label: "Repository", Worktree: &WorktreeInfo{Valid: true, RepoKey: "repo", CheckoutPath: "/repo"}},
		{WorkspaceID: "child", Label: "Feature", Worktree: &WorktreeInfo{Valid: true, RepoKey: "repo", CheckoutPath: "/repo-feature", IsLinkedWorktree: true}},
		{ActiveTabID: "ordinary:tab", WorkspaceID: "ordinary", Label: "Scratch"},
		{WorkspaceID: "linked-without-path", Label: "Incomplete", Worktree: &WorktreeInfo{Valid: true, RepoKey: "other", IsLinkedWorktree: true}},
		{WorkspaceID: "malformed", Label: "Untrusted provenance", Worktree: &WorktreeInfo{}},
	}, Tabs: []TabInfo{
		{TabID: "ordinary:tab", WorkspaceID: "ordinary"},
	}, Panes: []PaneInfo{
		{CWD: "/work/current", PaneID: "ordinary:pane", TabID: "ordinary:tab", TerminalID: "ordinary:terminal", WorkspaceID: "ordinary"},
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
	if got, want := home.Workspaces[1].Actions, []WorkspaceAction{WorkspaceActionCreateWorktree, WorkspaceActionCloseWorkspace}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ordinary actions = %v, want %v", got, want)
	}
	if got, want := home.Workspaces[2].Actions, []WorkspaceAction{WorkspaceActionCloseWorkspace}; !reflect.DeepEqual(got, want) {
		t.Fatalf("linked workspace without a confirmed path actions = %v, want %v", got, want)
	}
	if got := home.Workspaces[3].Actions; len(got) != 0 {
		t.Fatalf("workspace with invalid non-null provenance actions = %v, want none", got)
	}
}

func TestResolveOrdinaryWorkspaceWorktreeSourceFromCurrentPane(t *testing.T) {
	var onePane Snapshot
	if err := json.Unmarshal([]byte(`{
		"workspaces":[{"workspace_id":"ordinary"}],
		"tabs":[{"tab_id":"tab","workspace_id":"ordinary"}],
		"panes":[{"cwd":"/one/current","pane_id":"one","tab_id":"tab","workspace_id":"ordinary"}]
	}`), &onePane); err != nil {
		t.Fatal(err)
	}
	target, found, applicable := ResolveWorkspaceAction(onePane, WorkspaceActionCreateWorktree, "ordinary")
	if !found || !applicable || target.WorktreeSource != (CreateWorktreeSource{CWD: "/one/current"}) {
		t.Fatalf("one-pane resolution = target=%+v found=%t applicable=%t", target, found, applicable)
	}

	multiPane := ordinaryMultiPaneSnapshot()
	target, found, applicable = ResolveWorkspaceAction(multiPane, WorkspaceActionCreateWorktree, "ordinary")
	if !found || !applicable || target.WorktreeSource != (CreateWorktreeSource{CWD: "/active/focused"}) {
		t.Fatalf("multi-pane resolution = target=%+v found=%t applicable=%t", target, found, applicable)
	}
	multiPane.Workspaces[0].ActiveTabID = "tab-one"
	target, found, applicable = ResolveWorkspaceAction(multiPane, WorkspaceActionCreateWorktree, "ordinary")
	if !found || !applicable || target.WorktreeSource != (CreateWorktreeSource{CWD: "/inactive/only"}) {
		t.Fatalf("changed active tab resolution = target=%+v found=%t applicable=%t", target, found, applicable)
	}
}

func TestOrdinaryWorkspaceOmitsWorktreeActionWithoutExactAbsolutePaneCWD(t *testing.T) {
	tests := map[string]func(*Snapshot){
		"missing active tab": func(snapshot *Snapshot) {
			snapshot.Workspaces[0].ActiveTabID = ""
		},
		"ambiguous active layout": func(snapshot *Snapshot) {
			snapshot.Layouts = append(snapshot.Layouts, snapshot.Layouts[1])
		},
		"focused pane from another tab": func(snapshot *Snapshot) {
			snapshot.Layouts[1].FocusedPaneID = "pane-one"
		},
		"relative focused cwd": func(snapshot *Snapshot) {
			snapshot.Panes[2].CWD = "relative/path"
		},
		"global and pane focus only": func(snapshot *Snapshot) {
			snapshot.Layouts[1].FocusedPaneID = ""
			snapshot.FocusedPaneID = "pane-active-other"
			snapshot.Panes[1].Focused = true
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			snapshot := ordinaryMultiPaneSnapshot()
			change(&snapshot)
			_, found, applicable := ResolveWorkspaceAction(snapshot, WorkspaceActionCreateWorktree, "ordinary")
			if !found || applicable {
				t.Fatalf("resolution found=%t applicable=%t, want found and inapplicable", found, applicable)
			}
			if got := AvailableWorkspaceActions(snapshot, "ordinary"); !reflect.DeepEqual(got, []WorkspaceAction{WorkspaceActionCloseWorkspace}) {
				t.Fatalf("actions = %v, want only close_workspace", got)
			}
		})
	}

	var foregroundOnly Snapshot
	if err := json.Unmarshal([]byte(`{
		"workspaces":[{"workspace_id":"ordinary","active_tab_id":"tab"}],
		"tabs":[{"workspace_id":"ordinary","tab_id":"tab"}],
		"panes":[{"workspace_id":"ordinary","tab_id":"tab","pane_id":"only","foreground_cwd":"/must/not/use"}]
	}`), &foregroundOnly); err != nil {
		t.Fatal(err)
	}
	if got := AvailableWorkspaceActions(foregroundOnly, "ordinary"); !reflect.DeepEqual(got, []WorkspaceAction{WorkspaceActionCloseWorkspace}) {
		t.Fatalf("foreground-only actions = %v, want only close_workspace", got)
	}
}

func ordinaryMultiPaneSnapshot() Snapshot {
	return Snapshot{
		Workspaces: []WorkspaceInfo{{ActiveTabID: "tab-active", WorkspaceID: "ordinary"}},
		Tabs: []TabInfo{
			{TabID: "tab-one", WorkspaceID: "ordinary"},
			{TabID: "tab-active", WorkspaceID: "ordinary"},
		},
		Panes: []PaneInfo{
			{CWD: "/inactive/only", PaneID: "pane-one", TabID: "tab-one", WorkspaceID: "ordinary"},
			{CWD: "/active/other", Focused: true, PaneID: "pane-active-other", TabID: "tab-active", WorkspaceID: "ordinary"},
			{CWD: "/active/focused", PaneID: "pane-active-focused", TabID: "tab-active", WorkspaceID: "ordinary"},
		},
		Layouts: []LayoutInfo{
			{FocusedPaneID: "pane-one", TabID: "tab-one", WorkspaceID: "ordinary"},
			{FocusedPaneID: "pane-active-focused", TabID: "tab-active", WorkspaceID: "ordinary"},
		},
		FocusedPaneID: "pane-one",
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
	socketPath := shortSocketPath(t)
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
		{"worktree.create", map[string]any{"cwd": "/ordinary/current", "focus": false}, map[string]any{"type": "worktree_created", "workspace": map[string]any{}, "tab": map[string]any{}, "root_pane": map[string]any{}, "worktree": map[string]any{}}},
		{"workspace.close", map[string]any{"workspace_id": "opaque-child"}, map[string]any{"type": "ok"}},
		{"worktree.remove", map[string]any{"workspace_id": "opaque-child", "force": false}, map[string]any{"type": "worktree_removed", "workspace_id": "opaque-child", "path": "/work/child", "forced": false}},
		{"pane.split", map[string]any{"target_pane_id": "opaque-pane", "direction": "right", "cwd": "/work/current", "focus": false}, map[string]any{
			"type": "pane_info", "pane": map[string]any{"workspace_id": "opaque-workspace", "tab_id": "opaque-tab", "pane_id": "created-pane", "terminal_id": "created-terminal", "cwd": "/work/current"},
		}},
		{"pane.rename", map[string]any{"pane_id": "opaque-pane", "label": "Useful terminal"}, map[string]any{
			"type": "pane_info", "pane": map[string]any{"workspace_id": "opaque-workspace", "tab_id": "opaque-tab", "pane_id": "opaque-pane", "terminal_id": "opaque-terminal", "label": "Useful terminal"},
		}},
		{"pane.rename", map[string]any{"pane_id": "opaque-pane"}, map[string]any{
			"type": "pane_info", "pane": map[string]any{"workspace_id": "opaque-workspace", "tab_id": "opaque-tab", "pane_id": "opaque-pane", "terminal_id": "opaque-terminal"},
		}},
		{"pane.close", map[string]any{"pane_id": "opaque-pane"}, map[string]any{"type": "ok"}},
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

	client := NewClient(socketPath, discardLogger())
	label, branch, terminalLabel := "Useful", "feature/exact", "Useful terminal"
	for index, call := range []func() error{
		func() error { return client.CreateWorkspace(context.Background(), "/work", &label) },
		func() error { return client.CreateWorkspace(context.Background(), "$HOME/literal", nil) },
		func() error {
			return client.CreateWorktree(context.Background(), CreateWorktreeSource{WorkspaceID: "opaque-parent"}, &branch)
		},
		func() error {
			return client.CreateWorktree(context.Background(), CreateWorktreeSource{CWD: "/ordinary/current"}, nil)
		},
		func() error { return client.CloseWorkspace(context.Background(), "opaque-child") },
		func() error { return client.RemoveWorktree(context.Background(), "opaque-child") },
		func() error {
			pane, splitErr := client.SplitTerminal(context.Background(), "opaque-pane", "/work/current", SplitRight)
			if splitErr == nil && (pane.PaneID != "created-pane" || pane.TerminalID != "created-terminal") {
				return &testError{"pane.split did not return the exact created pane"}
			}
			return splitErr
		},
		func() error {
			pane, renameErr := client.RenameTerminal(context.Background(), "opaque-pane", &terminalLabel)
			if renameErr == nil && (pane.Label == nil || *pane.Label != terminalLabel) {
				return &testError{"pane.rename did not return the exact manual label"}
			}
			return renameErr
		},
		func() error {
			pane, renameErr := client.RenameTerminal(context.Background(), "opaque-pane", nil)
			if renameErr == nil && pane.Label != nil {
				return &testError{"pane.rename did not clear the manual label"}
			}
			return renameErr
		},
		func() error { return client.CloseTerminal(context.Background(), "opaque-pane") },
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
	socketPath := shortSocketPath(t)
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
	err = NewClient(socketPath, discardLogger()).RemoveWorktree(context.Background(), "opaque-child")
	if err == nil || !MutationMayHaveRun(err) {
		t.Fatalf("mismatched mutation result = %v, want uncertain error", err)
	}
}

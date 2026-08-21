package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"time"
)

type WorkspaceAction string

const (
	WorkspaceActionCreateWorktree WorkspaceAction = "create_worktree"
	WorkspaceActionCloseWorkspace WorkspaceAction = "close_workspace"
	WorkspaceActionCloseGroup     WorkspaceAction = "close_group"
	WorkspaceActionDeleteCheckout WorkspaceAction = "delete_checkout"
)

type WorkspaceActionTarget struct {
	Workspace         WorkspaceInfo
	ScopeWorkspaceIDs []string
	CheckoutPath      string
}

func AvailableWorkspaceActions(snapshot Snapshot, workspaceID string) []WorkspaceAction {
	actions := make([]WorkspaceAction, 0, 2)
	for _, action := range []WorkspaceAction{
		WorkspaceActionCreateWorktree,
		WorkspaceActionCloseWorkspace,
		WorkspaceActionCloseGroup,
		WorkspaceActionDeleteCheckout,
	} {
		if _, found, applicable := ResolveWorkspaceAction(snapshot, action, workspaceID); found && applicable {
			actions = append(actions, action)
		}
	}
	return actions
}

func ResolveWorkspaceAction(snapshot Snapshot, action WorkspaceAction, workspaceID string) (WorkspaceActionTarget, bool, bool) {
	workspace, found := findWorkspace(snapshot, workspaceID)
	if !found {
		return WorkspaceActionTarget{}, false, false
	}
	target := WorkspaceActionTarget{
		Workspace:         workspace,
		ScopeWorkspaceIDs: []string{workspaceID},
	}
	if workspace.Worktree != nil && !workspace.Worktree.Valid {
		return target, true, false
	}
	topLevel := isTopLevelRepository(snapshot, workspace)
	repositoryRoot := workspace.Worktree != nil && workspace.Worktree.Valid && !workspace.Worktree.IsLinkedWorktree
	switch action {
	case WorkspaceActionCreateWorktree:
		if !topLevel {
			return target, true, false
		}
		target.CheckoutPath = workspace.Worktree.CheckoutPath
	case WorkspaceActionCloseWorkspace:
		if repositoryRoot {
			return target, true, false
		}
	case WorkspaceActionCloseGroup:
		if !topLevel {
			return target, true, false
		}
		target.ScopeWorkspaceIDs = target.ScopeWorkspaceIDs[:0]
		for _, candidate := range snapshot.Workspaces {
			if candidate.Worktree != nil && candidate.Worktree.Valid && candidate.Worktree.RepoKey == workspace.Worktree.RepoKey {
				target.ScopeWorkspaceIDs = append(target.ScopeWorkspaceIDs, candidate.WorkspaceID)
			}
		}
		sort.Strings(target.ScopeWorkspaceIDs)
	case WorkspaceActionDeleteCheckout:
		if workspace.Worktree == nil || !workspace.Worktree.Valid || !workspace.Worktree.IsLinkedWorktree || workspace.Worktree.CheckoutPath == "" {
			return target, true, false
		}
		target.CheckoutPath = workspace.Worktree.CheckoutPath
	default:
		return target, true, false
	}
	return target, true, true
}

func findWorkspace(snapshot Snapshot, workspaceID string) (WorkspaceInfo, bool) {
	for _, workspace := range snapshot.Workspaces {
		if workspace.WorkspaceID == workspaceID {
			return workspace, true
		}
	}
	return WorkspaceInfo{}, false
}

func isTopLevelRepository(snapshot Snapshot, workspace WorkspaceInfo) bool {
	if workspace.Worktree == nil || !workspace.Worktree.Valid || workspace.Worktree.IsLinkedWorktree {
		return false
	}
	ordinary := 0
	for _, candidate := range snapshot.Workspaces {
		if candidate.Worktree != nil && candidate.Worktree.Valid && !candidate.Worktree.IsLinkedWorktree &&
			candidate.Worktree.RepoKey == workspace.Worktree.RepoKey {
			ordinary++
		}
	}
	return ordinary == 1
}

type MutationError struct {
	Err       error
	Submitted bool
}

func (e *MutationError) Error() string { return e.Err.Error() }
func (e *MutationError) Unwrap() error { return e.Err }

func MutationMayHaveRun(err error) bool {
	var mutation *MutationError
	return errors.As(err, &mutation) && mutation.Submitted
}

func MutationTimedOut(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netError net.Error
	return errors.As(err, &netError) && netError.Timeout()
}

func (c *Client) CreateWorkspace(ctx context.Context, workingDirectory string, label *string) error {
	type params struct {
		CWD   string  `json:"cwd"`
		Label *string `json:"label,omitempty"`
		Focus bool    `json:"focus"`
	}
	return c.mutate(ctx, "workspace-create", "workspace.create", params{
		CWD: workingDirectory, Label: label, Focus: false,
	}, expectMutationResult("workspace_created", "workspace", "tab", "root_pane"))
}

func (c *Client) CreateWorktree(ctx context.Context, workspaceID string, branch *string) error {
	type params struct {
		WorkspaceID string  `json:"workspace_id"`
		Branch      *string `json:"branch,omitempty"`
		Focus       bool    `json:"focus"`
	}
	return c.mutate(ctx, "worktree-create", "worktree.create", params{
		WorkspaceID: workspaceID, Branch: branch, Focus: false,
	}, expectMutationResult("worktree_created", "workspace", "tab", "root_pane", "worktree"))
}

func (c *Client) CloseWorkspace(ctx context.Context, workspaceID string) error {
	return c.mutate(ctx, "workspace-close", "workspace.close", struct {
		WorkspaceID string `json:"workspace_id"`
	}{WorkspaceID: workspaceID}, expectMutationResult("ok"))
}

func (c *Client) RemoveWorktree(ctx context.Context, workspaceID string) error {
	type params struct {
		WorkspaceID string `json:"workspace_id"`
		Force       bool   `json:"force"`
	}
	return c.mutate(ctx, "worktree-remove", "worktree.remove", params{
		WorkspaceID: workspaceID, Force: false,
	}, func(raw json.RawMessage) error {
		if err := expectMutationResult("worktree_removed", "workspace_id", "path", "forced")(raw); err != nil {
			return err
		}
		var result struct {
			WorkspaceID string `json:"workspace_id"`
			Forced      bool   `json:"forced"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			return err
		}
		if result.WorkspaceID != workspaceID || result.Forced {
			return fmt.Errorf("Herdr returned removal for workspace %q with force %t", result.WorkspaceID, result.Forced)
		}
		return nil
	})
}

func (c *Client) mutate(ctx context.Context, prefix, method string, params any, validate func(json.RawMessage) error) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return &MutationError{Err: err}
	}
	defer conn.Close()
	_ = conn.SetDeadline(requestDeadline())
	id := c.id(prefix)
	request := struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params"`
	}{ID: id, Method: method, Params: params}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return &MutationError{Err: fmt.Errorf("write Herdr %s request: %w", method, err), Submitted: true}
	}

	var envelope responseEnvelope
	if err := json.NewDecoder(conn).Decode(&envelope); err != nil {
		return &MutationError{Err: fmt.Errorf("read Herdr %s response: %w", method, err), Submitted: true}
	}
	if err := validateResponse(envelope, id); err != nil {
		var apiError *APIError
		if errors.As(err, &apiError) {
			return apiError
		}
		return &MutationError{Err: err, Submitted: true}
	}
	if err := validate(envelope.Result); err != nil {
		return &MutationError{Err: fmt.Errorf("validate Herdr %s response: %w", method, err), Submitted: true}
	}
	return nil
}

func expectMutationResult(resultType string, required ...string) func(json.RawMessage) error {
	return func(raw json.RawMessage) error {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		var actualType string
		if err := json.Unmarshal(fields["type"], &actualType); err != nil || actualType != resultType {
			return fmt.Errorf("Herdr returned unexpected result type %q", actualType)
		}
		for _, field := range required {
			value, present := fields[field]
			if !present || string(value) == "null" {
				return fmt.Errorf("Herdr %s result omitted %s", resultType, field)
			}
		}
		return nil
	}
}

func requestDeadline() (deadline time.Time) {
	return time.Now().Add(requestTimeout)
}

package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
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
	WorktreeSource    CreateWorktreeSource
}

type CreateWorktreeSource struct {
	WorkspaceID string
	CWD         string
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
		if !uniqueWorkspace(snapshot, workspaceID) {
			return target, true, false
		}
		if topLevel {
			target.CheckoutPath = workspace.Worktree.CheckoutPath
			target.WorktreeSource.WorkspaceID = workspaceID
		} else if workspace.Worktree == nil {
			cwd, resolved := resolveOrdinaryWorkspaceCWD(snapshot, workspace)
			if !resolved {
				return target, true, false
			}
			target.WorktreeSource.CWD = cwd
		} else {
			return target, true, false
		}
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

func ResolveWorkspaceCloseAction(snapshot Snapshot, workspaceID string) (WorkspaceAction, bool) {
	for _, action := range []WorkspaceAction{WorkspaceActionCloseWorkspace, WorkspaceActionCloseGroup} {
		if _, found, applicable := ResolveWorkspaceAction(snapshot, action, workspaceID); found && applicable {
			return action, true
		}
	}
	return "", false
}

func findWorkspace(snapshot Snapshot, workspaceID string) (WorkspaceInfo, bool) {
	for _, workspace := range snapshot.Workspaces {
		if workspace.WorkspaceID == workspaceID {
			return workspace, true
		}
	}
	return WorkspaceInfo{}, false
}

func uniqueWorkspace(snapshot Snapshot, workspaceID string) bool {
	matches := 0
	for _, workspace := range snapshot.Workspaces {
		if workspace.WorkspaceID == workspaceID {
			matches++
		}
	}
	return matches == 1
}

func resolveOrdinaryWorkspaceCWD(snapshot Snapshot, workspace WorkspaceInfo) (string, bool) {
	panes := make([]PaneInfo, 0, 1)
	for _, pane := range snapshot.Panes {
		if pane.WorkspaceID == workspace.WorkspaceID {
			panes = append(panes, pane)
		}
	}
	if len(panes) == 1 {
		pane := panes[0]
		_, paneUnique := findPane(snapshot, pane.PaneID)
		tab, unique := findTab(snapshot, pane.TabID)
		if pane.PaneID == "" || !paneUnique || !unique || tab.WorkspaceID != workspace.WorkspaceID || !filepath.IsAbs(pane.CWD) {
			return "", false
		}
		return pane.CWD, true
	}
	if len(panes) < 2 || workspace.ActiveTabID == "" {
		return "", false
	}

	tab, unique := findTab(snapshot, workspace.ActiveTabID)
	if !unique || tab.WorkspaceID != workspace.WorkspaceID {
		return "", false
	}
	layout, unique := findLayout(snapshot, tab.TabID)
	if !unique || layout.WorkspaceID != workspace.WorkspaceID || layout.FocusedPaneID == "" {
		return "", false
	}
	pane, unique := findPane(snapshot, layout.FocusedPaneID)
	if !unique || pane.WorkspaceID != workspace.WorkspaceID || pane.TabID != tab.TabID || !filepath.IsAbs(pane.CWD) {
		return "", false
	}
	return pane.CWD, true
}
func findPane(snapshot Snapshot, paneID string) (PaneInfo, bool) {
	var match PaneInfo
	matches := 0
	for _, pane := range snapshot.Panes {
		if pane.PaneID == paneID {
			match = pane
			matches++
		}
	}
	return match, matches == 1
}

func findTab(snapshot Snapshot, tabID string) (TabInfo, bool) {
	var match TabInfo
	matches := 0
	for _, tab := range snapshot.Tabs {
		if tab.TabID == tabID {
			match = tab
			matches++
		}
	}
	return match, matches == 1
}

func findLayout(snapshot Snapshot, tabID string) (LayoutInfo, bool) {
	var match LayoutInfo
	matches := 0
	for _, layout := range snapshot.Layouts {
		if layout.TabID == tabID {
			match = layout
			matches++
		}
	}
	return match, matches == 1
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

func (c *Client) CreateWorktree(ctx context.Context, source CreateWorktreeSource, branch *string) error {
	type params struct {
		WorkspaceID string  `json:"workspace_id,omitempty"`
		CWD         string  `json:"cwd,omitempty"`
		Branch      *string `json:"branch,omitempty"`
		Focus       bool    `json:"focus"`
	}
	if (source.WorkspaceID == "") == (source.CWD == "") || source.CWD != "" && !filepath.IsAbs(source.CWD) {
		return &MutationError{Err: fmt.Errorf("worktree source must contain exactly one confirmed workspace id or absolute cwd")}
	}
	return c.mutate(ctx, "worktree-create", "worktree.create", params{
		WorkspaceID: source.WorkspaceID, CWD: source.CWD, Branch: branch, Focus: false,
	}, expectMutationResult("worktree_created", "workspace", "tab", "root_pane", "worktree"))
}

func (c *Client) CloseWorkspace(ctx context.Context, workspaceID string, closeGroup bool) error {
	return c.mutate(ctx, "workspace-close", "workspace.close", struct {
		WorkspaceID string `json:"workspace_id"`
		CloseGroup  bool   `json:"close_group,omitempty"`
	}{WorkspaceID: workspaceID, CloseGroup: closeGroup}, expectMutationResult("ok"))
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

type SplitDirection string

const (
	SplitRight SplitDirection = "right"
	SplitDown  SplitDirection = "down"
)

func (c *Client) SplitTerminal(ctx context.Context, paneID, cwd string, direction SplitDirection) (PaneInfo, error) {
	type params struct {
		TargetPaneID string         `json:"target_pane_id"`
		Direction    SplitDirection `json:"direction"`
		CWD          string         `json:"cwd,omitempty"`
		Focus        bool           `json:"focus"`
	}
	if paneID == "" || (direction != SplitRight && direction != SplitDown) {
		return PaneInfo{}, &MutationError{Err: errors.New("split target or direction is invalid")}
	}
	raw, err := c.mutateResult(ctx, "pane-split", "pane.split", params{
		TargetPaneID: paneID, Direction: direction, CWD: cwd, Focus: false,
	}, validatePaneInfoMutation)
	if err != nil {
		return PaneInfo{}, err
	}
	pane, err := decodePaneInfoMutation(raw)
	if err != nil {
		return PaneInfo{}, &MutationError{Err: fmt.Errorf("decode Herdr pane.split response: %w", err), Submitted: true}
	}
	return pane, nil
}

func (c *Client) RenameTerminal(ctx context.Context, paneID string, label *string) (PaneInfo, error) {
	type params struct {
		PaneID string  `json:"pane_id"`
		Label  *string `json:"label,omitempty"`
	}
	if paneID == "" {
		return PaneInfo{}, &MutationError{Err: errors.New("rename target is invalid")}
	}
	raw, err := c.mutateResult(ctx, "pane-rename", "pane.rename", params{PaneID: paneID, Label: label}, validatePaneInfoMutation)
	if err != nil {
		return PaneInfo{}, err
	}
	pane, err := decodePaneInfoMutation(raw)
	if err != nil {
		return PaneInfo{}, &MutationError{Err: fmt.Errorf("decode Herdr pane.rename response: %w", err), Submitted: true}
	}
	return pane, nil
}

func (c *Client) CloseTerminal(ctx context.Context, paneID string) error {
	if paneID == "" {
		return &MutationError{Err: errors.New("close target is invalid")}
	}
	return c.mutate(ctx, "pane-close", "pane.close", struct {
		PaneID string `json:"pane_id"`
	}{PaneID: paneID}, expectMutationResult("ok"))
}

func validatePaneInfoMutation(raw json.RawMessage) error {
	_, err := decodePaneInfoMutation(raw)
	return err
}

func decodePaneInfoMutation(raw json.RawMessage) (PaneInfo, error) {
	var result struct {
		Pane PaneInfo `json:"pane"`
		Type string   `json:"type"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return PaneInfo{}, err
	}
	if result.Type != "pane_info" || result.Pane.PaneID == "" || result.Pane.TerminalID == "" ||
		result.Pane.WorkspaceID == "" || result.Pane.TabID == "" {
		return PaneInfo{}, errors.New("Herdr returned incomplete pane_info")
	}
	return result.Pane, nil
}

func (c *Client) mutate(ctx context.Context, prefix, method string, params any, validate func(json.RawMessage) error) error {
	_, err := c.mutateResult(ctx, prefix, method, params, validate)
	return err
}

func (c *Client) mutateResult(ctx context.Context, prefix, method string, params any, validate func(json.RawMessage) error) (json.RawMessage, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, &MutationError{Err: err}
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
		return nil, &MutationError{Err: fmt.Errorf("write Herdr %s request: %w", method, err), Submitted: true}
	}

	var envelope responseEnvelope
	if err := json.NewDecoder(conn).Decode(&envelope); err != nil {
		return nil, &MutationError{Err: fmt.Errorf("read Herdr %s response: %w", method, err), Submitted: true}
	}
	if err := validateResponse(envelope, id); err != nil {
		var apiError *APIError
		if errors.As(err, &apiError) {
			return nil, apiError
		}
		return nil, &MutationError{Err: err, Submitted: true}
	}
	if err := validate(envelope.Result); err != nil {
		return nil, &MutationError{Err: fmt.Errorf("validate Herdr %s response: %w", method, err), Submitted: true}
	}
	return envelope.Result, nil
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

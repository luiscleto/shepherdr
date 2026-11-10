package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/luiscleto/shepherdr/internal/access"
	"github.com/luiscleto/shepherdr/internal/herdr"
)

// Herdr Core offers Close terminal and Close space, both through terminal.kill.
// Each is prepared from a fresh complete read, confirmed, then checked against
// another fresh complete read before anything is killed. With the Herdr app,
// it also offers New space, New terminal and New tab: the window does them
// while it is open, and while it is closed they start in core, labelled so the
// app adds them when it next opens. Rename space needs the window open. There is no
// retry.

type coreActionClient interface {
	Read(context.Context) (herdr.CoreRead, error)
	Kill(context.Context, string) (string, error)
	Run(ctx context.Context, folder, space, tab string) (string, error)
	Label(ctx context.Context, address string, labels map[string]string) (map[string]string, error)
}

// appActionClient runs commands in the Herdr app window.
type appActionClient interface {
	WindowOpen(context.Context) bool
	Do(context.Context, string, map[string]any, any) error
}

// homeWatcher is the published core Home.
type homeWatcher interface {
	Subscribe() (<-chan herdr.State, func())
}

// splitWait bounds how long a split waits for Home to show the new terminal.
const splitWait = 5 * time.Second

type coreActionCoordinator struct {
	client     coreActionClient
	app        appActionClient
	published  homeWatcher
	homeFolder string
	homeDir    func() (string, error)
	refresh    homeRefresher
	mutation   chan struct{}
}

func newCoreActionCoordinator(client coreActionClient, homeFolder string, refresh homeRefresher) *coreActionCoordinator {
	return &coreActionCoordinator{
		client: client, homeFolder: homeFolder, homeDir: os.UserHomeDir, refresh: refresh, mutation: make(chan struct{}, 1),
	}
}

// coreView is one complete read and the Home it projects to.
type coreView struct {
	read herdr.CoreRead
	home herdr.Home
}

// view reads and projects Home with the app actions for window.
func (c *coreActionCoordinator) view(ctx context.Context, window herdr.AppWindowState) (coreView, error) {
	read, err := c.client.Read(ctx)
	if err != nil {
		return coreView{}, err
	}
	read.AppWindow = window
	_, home, err := herdr.ProjectCore(read, c.homeFolder)
	if err != nil {
		return coreView{}, err
	}
	return coreView{read: read, home: home}, nil
}

func (c *coreActionCoordinator) prepare(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	if !requireObjectKeys(writer, fields, []string{"action", "workspace_id"}, []string{"action", "workspace_id"}) {
		return
	}
	action, actionOK := decodeString(fields["action"])
	workspaceID, workspaceOK := decodeString(fields["workspace_id"])
	if !actionOK || !workspaceOK || workspaceID == "" || action != string(herdr.WorkspaceActionCloseWorkspace) {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	current, ok := c.preparedView(writer, request)
	if !ok {
		return
	}
	facts, status, reason := coreSpaceFacts(current.home, workspaceID)
	if reason != "" {
		writeRefusal(writer, status, reason, "")
		return
	}
	writeActionJSON(writer, http.StatusOK, struct {
		Outcome     string            `json:"outcome"`
		Action      string            `json:"action"`
		WorkspaceID string            `json:"workspace_id"`
		Expected    confirmationFacts `json:"expected"`
	}{Outcome: "prepared", Action: action, WorkspaceID: workspaceID, Expected: facts})
}

func (c *coreActionCoordinator) run(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	action, actionOK := decodeString(fields["action"])
	switch {
	case actionOK && action == "create_space":
		c.runCreateSpace(writer, request, fields)
		return
	case actionOK && action == string(herdr.WorkspaceActionRenameSpace):
		c.runRenameSpace(writer, request, fields)
		return
	case actionOK && action == string(herdr.WorkspaceActionNewTab):
		c.runNewTab(writer, request, fields)
		return
	case !actionOK || action != string(herdr.WorkspaceActionCloseWorkspace):
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if !requireObjectKeys(writer, fields, []string{"action", "workspace_id", "expected"}, []string{"action", "workspace_id", "expected"}) {
		return
	}
	workspaceID, ok := decodeString(fields["workspace_id"])
	expected, factsOK := decodeConfirmationFacts(fields["expected"], false)
	if !ok || workspaceID == "" || !factsOK {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if !c.acquire() {
		writeRefusal(writer, http.StatusConflict, "busy", "")
		return
	}
	defer c.release()

	current, err := c.view(context.Background(), herdr.AppWindowUnpaired)
	if err != nil {
		writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
		return
	}
	facts, status, reason := coreSpaceFacts(current.home, workspaceID)
	if reason != "" {
		writeRefusal(writer, status, reason, "")
		return
	}
	if !reflect.DeepEqual(facts, expected) {
		writeRefusal(writer, http.StatusConflict, "stale", "")
		return
	}
	c.closeSpace(writer, request, current, workspaceID)
}

func (c *coreActionCoordinator) prepareTerminal(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	keys := []string{"action", "workspace_id", "tab_id", "pane_id", "terminal_id"}
	if !requireObjectKeys(writer, fields, keys, keys) {
		return
	}
	action, actionOK := decodeString(fields["action"])
	target, targetOK := decodeTerminalTarget(fields)
	if !actionOK || action != string(herdr.TerminalActionClose) || !targetOK {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	current, ok := c.preparedView(writer, request)
	if !ok {
		return
	}
	facts, status, reason := coreTerminalFacts(current, target)
	if reason != "" {
		writeRefusal(writer, status, reason, "")
		return
	}
	writeActionJSON(writer, http.StatusOK, struct {
		Outcome  string             `json:"outcome"`
		Action   string             `json:"action"`
		Expected terminalCloseFacts `json:"expected"`
	}{Outcome: "prepared", Action: action, Expected: facts})
}

func (c *coreActionCoordinator) runTerminal(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	action, actionOK := decodeString(fields["action"])
	if actionOK && action == string(herdr.TerminalActionSplit) {
		c.runSplitTerminal(writer, request, fields)
		return
	}
	if actionOK && (action == string(herdr.TerminalActionPin) || action == string(herdr.TerminalActionUnpin)) {
		c.runPinAgent(writer, request, fields, herdr.TerminalAction(action))
		return
	}
	if !actionOK || action != string(herdr.TerminalActionClose) {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if !requireObjectKeys(writer, fields, []string{"action", "expected"}, []string{"action", "expected"}) {
		return
	}
	expected, ok := decodeTerminalCloseFacts(fields["expected"])
	if !ok {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if !c.acquire() {
		writeRefusal(writer, http.StatusConflict, "busy", "")
		return
	}
	defer c.release()

	current, err := c.view(context.Background(), herdr.AppWindowUnpaired)
	if err != nil {
		writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
		return
	}
	facts, status, reason := coreTerminalFacts(current, expected.Target)
	if reason != "" {
		writeRefusal(writer, status, reason, "")
		return
	}
	if !reflect.DeepEqual(facts, expected) {
		writeRefusal(writer, http.StatusConflict, "stale", "")
		return
	}
	// The space's last terminal: the person confirmed Close space.
	if facts.WorkspaceExpected != nil {
		c.closeSpace(writer, request, current, facts.Target.WorkspaceID)
		return
	}
	var state string
	err = withCommitAuthority(request, func() error {
		var killErr error
		state, killErr = c.client.Kill(context.Background(), facts.Target.PaneID)
		return killErr
	})
	if err != nil {
		c.finishError(writer, err)
		return
	}
	if state != "finished" {
		c.unknown(writer, http.StatusBadGateway)
		return
	}
	c.succeeded(writer)
}

// closeSpace kills every live terminal Home shows in the space, in
// coreSpaceCloseOrder. A terminal no longer live is skipped. It succeeds only
// when one complete read afterwards shows nothing live left in the space.
func (c *coreActionCoordinator) closeSpace(writer http.ResponseWriter, request *http.Request, current coreView, workspaceID string) {
	workspace, _ := findCoreWorkspace(current.home, workspaceID)
	killed, finished := false, true
	for _, address := range coreSpaceCloseOrder(current.read.Terminals, workspace) {
		if killed {
			fresh, err := c.client.Read(context.Background())
			if err != nil {
				c.unknown(writer, http.StatusBadGateway)
				return
			}
			if !coreTerminalLive(fresh.Terminals, address) {
				continue
			}
		}
		var state string
		err := withCommitAuthority(request, func() error {
			var killErr error
			state, killErr = c.client.Kill(context.Background(), address)
			return killErr
		})
		if err != nil {
			if killed && !errors.Is(err, access.ErrUnauthorized) {
				c.unknown(writer, unknownStatus(err))
				return
			}
			if killed {
				c.refreshHome()
			}
			c.finishError(writer, err)
			return
		}
		killed = true
		finished = finished && state == "finished"
	}
	after, err := c.view(context.Background(), herdr.AppWindowUnpaired)
	if _, present := findCoreWorkspace(after.home, workspaceID); err != nil || !finished || present {
		c.unknown(writer, http.StatusBadGateway)
		return
	}
	c.succeeded(writer)
}

// runCreateSpace makes a space in the app window for an existing folder, with
// the label as its name. The window shows the new space. A request confirmed
// on the closed window's sheet starts the space in core instead.
func (c *coreActionCoordinator) runCreateSpace(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	if _, bestEffort := fields["best_effort"]; bestEffort {
		c.runCreateSpaceInCore(writer, request, fields)
		return
	}
	if !requireObjectKeys(writer, fields, []string{"action", "working_directory", "label"}, []string{"action", "working_directory"}) {
		return
	}
	workingDirectory, ok := decodeString(fields["working_directory"])
	if !ok || workingDirectory == "" {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	args := map[string]any{}
	if raw, present := fields["label"]; present {
		label, valid := decodeString(raw)
		if !valid {
			writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
			return
		}
		if strings.TrimSpace(label) != "" {
			args["name"] = label
		}
	}
	folder, ok := c.spaceFolder(writer, workingDirectory)
	if !ok {
		return
	}
	args["folder"] = folder
	if !c.startAppAction(writer) {
		return
	}
	defer c.release()
	// The folder is on this machine. If it goes before the app starts the
	// space, the app's own refusal still shows.
	if folderMissing(folder) {
		writeRefusal(writer, http.StatusUnprocessableEntity, "folder_missing", "")
		return
	}
	var created struct {
		Space    string `json:"space"`
		Tab      string `json:"tab"`
		Terminal string `json:"terminal"`
	}
	if err := c.doApp(request, "space.new", args, &created); err != nil {
		c.finishAppError(writer, err)
		return
	}
	if created.Space == "" || created.Tab == "" || created.Terminal == "" {
		c.unknown(writer, http.StatusBadGateway)
		return
	}
	c.succeeded(writer)
}

// runCreateSpaceInCore starts core's shell in the folder, labelled with a new
// space and tab id in the app's shape. The app adds it as an unnamed space at
// the end of its list when its window next opens, and until then Home shows
// it there. It runs whether or not the window has opened since.
func (c *coreActionCoordinator) runCreateSpaceInCore(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	keys := []string{"action", "working_directory", "best_effort"}
	if !requireObjectKeys(writer, fields, keys, keys) {
		return
	}
	workingDirectory, ok := decodeString(fields["working_directory"])
	if !ok || workingDirectory == "" || !decodeTrue(fields["best_effort"]) {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	folder, ok := c.spaceFolder(writer, workingDirectory)
	if !ok || !c.startCoreAction(writer) {
		return
	}
	defer c.release()
	// Core refuses a missing folder only after using up an address.
	if folderMissing(folder) {
		writeRefusal(writer, http.StatusUnprocessableEntity, "folder_missing", "")
		return
	}
	current, err := c.client.Read(context.Background())
	if err != nil {
		writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
		return
	}
	space, tab := unusedAppID("sp", current), unusedAppID("tb", current)
	if _, err := c.runInCore(request, folder, space, tab); err != nil {
		c.finishAppError(writer, err)
		return
	}
	c.succeeded(writer)
}

// spaceFolder expands "~" and refuses a relative folder, which would be the
// app's or core's own.
func (c *coreActionCoordinator) spaceFolder(writer http.ResponseWriter, workingDirectory string) (string, bool) {
	folder, err := expandTilde(workingDirectory, c.homeDir)
	if err != nil {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "Could not find the current user's home directory.")
		return "", false
	}
	if !filepath.IsAbs(folder) {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return "", false
	}
	return folder, true
}

// folderMissing reports a folder that isn't a folder on this machine.
func folderMissing(folder string) bool {
	info, err := os.Stat(folder)
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || err == nil && !info.IsDir()
}

// unusedAppID makes an id in the app's shape, prefix and 32 hex digits, that
// the read doesn't hold anywhere.
func unusedAppID(prefix string, read herdr.CoreRead) string {
	for {
		var random [16]byte
		_, _ = rand.Read(random[:])
		id := prefix + hex.EncodeToString(random[:])
		if !bytes.Contains(read.Document, []byte(id)) && !slices.ContainsFunc(read.Terminals, func(terminal herdr.CoreTerminal) bool {
			return terminal.Labels["herdr.space"] == id || terminal.Labels["herdr.tab"] == id
		}) {
			return id
		}
	}
}

// runInCore runs the labelled shell under the session's authority.
func (c *coreActionCoordinator) runInCore(request *http.Request, folder, space, tab string) (string, error) {
	var address string
	err := withCommitAuthority(request, func() error {
		var runErr error
		address, runErr = c.client.Run(context.Background(), folder, space, tab)
		return runErr
	})
	return address, err
}

// runNewTab adds a tab at the end of the exact space in the app window, with
// the app's own folder and name, then waits for Home to show its terminal so
// the browser can open it. The app puts a new tab after the space's selected
// tab, so it is then moved to the end. A request confirmed on the closed
// window's sheet starts the tab in core instead.
func (c *coreActionCoordinator) runNewTab(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	if _, bestEffort := fields["best_effort"]; bestEffort {
		c.runNewTabInCore(writer, request, fields)
		return
	}
	keys := []string{"action", "workspace_id"}
	if !requireObjectKeys(writer, fields, keys, keys) {
		return
	}
	workspaceID, ok := decodeString(fields["workspace_id"])
	if !ok || workspaceID == "" {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if !c.startAppAction(writer) {
		return
	}
	defer c.release()
	_, tabs, ok := c.tabSpace(writer, herdr.AppWindowOpen, workspaceID)
	if !ok {
		return
	}
	updates, unsubscribe := c.subscribeHome()
	defer unsubscribe()
	var created struct {
		Space    string `json:"space"`
		Tab      string `json:"tab"`
		Terminal string `json:"terminal"`
	}
	if err := c.doApp(request, "tab.new", map[string]any{"space": workspaceID}, &created); err != nil {
		c.finishAppError(writer, err)
		return
	}
	if created.Space != workspaceID || created.Tab == "" || created.Terminal == "" {
		c.unknown(writer, http.StatusBadGateway)
		return
	}
	// The tab exists either way: if the move fails, it stays where the app
	// put it and still opens. There is no retry or undo.
	var moved struct {
		OK bool `json:"ok"`
	}
	_ = c.doApp(request, "tab.move", map[string]any{"id": created.Tab, "index": tabs + 1}, &moved)
	c.openWhenShown(writer, updates, terminalActionTarget{WorkspaceID: workspaceID, TabID: created.Tab}, created.Terminal)
}

// runNewTabInCore starts core's shell in the space's folder, labelled with
// the space and a new tab id. The app adds the tab when its window next
// opens, after the space's selected tab, and until then Home shows it at the
// end of the space. The space comes from a fresh read just before the run:
// while the window is closed only the app writes the document.
func (c *coreActionCoordinator) runNewTabInCore(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	keys := []string{"action", "workspace_id", "best_effort"}
	if !requireObjectKeys(writer, fields, keys, keys) {
		return
	}
	workspaceID, ok := decodeString(fields["workspace_id"])
	if !ok || workspaceID == "" || !decodeTrue(fields["best_effort"]) {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if !c.startCoreAction(writer) {
		return
	}
	defer c.release()
	current, _, ok := c.tabSpace(writer, herdr.AppWindowClosed, workspaceID)
	if !ok {
		return
	}
	folder, _, _ := herdr.CoreSpace(current.read, workspaceID)
	// Core refuses a missing folder only after using up an address.
	if !filepath.IsAbs(folder) || folderMissing(folder) {
		writeRefusal(writer, http.StatusUnprocessableEntity, "folder_missing", "")
		return
	}
	tab := unusedAppID("tb", current.read)
	updates, unsubscribe := c.subscribeHome()
	defer unsubscribe()
	address, err := c.runInCore(request, folder, workspaceID, tab)
	if err != nil {
		c.finishAppError(writer, err)
		return
	}
	c.openWhenShown(writer, updates, terminalActionTarget{WorkspaceID: workspaceID, TabID: tab}, address)
}

// tabSpace reads Home with window's actions and finds the exact space, which
// must still offer New tab. It returns how many tabs the app holds for it.
func (c *coreActionCoordinator) tabSpace(writer http.ResponseWriter, window herdr.AppWindowState, workspaceID string) (coreView, int, bool) {
	current, err := c.view(context.Background(), window)
	if err != nil {
		writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
		return coreView{}, 0, false
	}
	workspace, found := findCoreWorkspace(current.home, workspaceID)
	if !found {
		writeRefusal(writer, http.StatusNotFound, "not_found", "")
		return coreView{}, 0, false
	}
	_, tabs, inDocument := herdr.CoreSpace(current.read, workspaceID)
	if !slices.Contains(workspace.Actions, herdr.WorkspaceActionNewTab) || !inDocument {
		writeRefusal(writer, http.StatusConflict, "not_applicable", "")
		return coreView{}, 0, false
	}
	return current, tabs, true
}

// runRenameSpace names a space in the app window; an empty name returns it to
// the automatic name. The app refuses an id it no longer has.
func (c *coreActionCoordinator) runRenameSpace(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	keys := []string{"action", "workspace_id", "name"}
	if !requireObjectKeys(writer, fields, keys, keys) {
		return
	}
	workspaceID, workspaceOK := decodeString(fields["workspace_id"])
	name, nameOK := decodeString(fields["name"])
	if !workspaceOK || !nameOK || workspaceID == "" {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if workspaceID == herdr.OtherTerminalsID {
		writeRefusal(writer, http.StatusConflict, "not_applicable", "")
		return
	}
	if strings.TrimSpace(name) == "" {
		name = ""
	}
	if !c.startAppAction(writer) {
		return
	}
	defer c.release()
	var renamed struct {
		OK bool `json:"ok"`
	}
	if err := c.doApp(request, "space.rename", map[string]any{"id": workspaceID, "name": name}, &renamed); err != nil {
		c.finishAppError(writer, err)
		return
	}
	if !renamed.OK {
		c.unknown(writer, http.StatusBadGateway)
		return
	}
	c.succeeded(writer)
}

// runPinAgent pins or unpins the exact current agent through its
// herdr.pinned label in core, which every Herdr client reads, so it works
// with the app window open or closed. The agent must still offer the action.
func (c *coreActionCoordinator) runPinAgent(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage, action herdr.TerminalAction) {
	keys := []string{"action", "workspace_id", "tab_id", "pane_id", "terminal_id"}
	if !requireObjectKeys(writer, fields, keys, keys) {
		return
	}
	target, targetOK := decodeTerminalTarget(fields)
	if !targetOK {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if !c.acquire() {
		writeRefusal(writer, http.StatusConflict, "busy", "")
		return
	}
	defer c.release()
	current, err := c.view(context.Background(), herdr.AppWindowUnpaired)
	if err != nil {
		writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
		return
	}
	terminal, found := findCoreTerminal(current.home, target)
	if !found {
		writeRefusal(writer, http.StatusNotFound, "not_found", "")
		return
	}
	if !containsTerminalAction(terminal.Actions, action) {
		writeRefusal(writer, http.StatusConflict, "not_applicable", "")
		return
	}
	value := ""
	if action == herdr.TerminalActionPin {
		value = "1"
	}
	var labels map[string]string
	err = withCommitAuthority(request, func() error {
		var labelErr error
		labels, labelErr = c.client.Label(context.Background(), target.PaneID, map[string]string{herdr.CorePinnedLabel: value})
		return labelErr
	})
	if err != nil {
		c.finishError(writer, err)
		return
	}
	if (labels[herdr.CorePinnedLabel] != "") != (action == herdr.TerminalActionPin) {
		c.unknown(writer, http.StatusBadGateway)
		return
	}
	c.succeeded(writer)
}

// runSplitTerminal splits the exact terminal's pane in the app window, then
// waits for the published Home to show the new terminal in the same tab so
// the browser can open it. A request confirmed on the closed window's sheet
// starts the terminal in core instead.
func (c *coreActionCoordinator) runSplitTerminal(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	if _, bestEffort := fields["best_effort"]; bestEffort {
		c.runNewTerminalInCore(writer, request, fields)
		return
	}
	keys := []string{"action", "workspace_id", "tab_id", "pane_id", "terminal_id", "direction"}
	if !requireObjectKeys(writer, fields, keys, keys) {
		return
	}
	target, targetOK := decodeTerminalTarget(fields)
	direction, directionOK := decodeString(fields["direction"])
	if !targetOK || !directionOK || (direction != string(herdr.SplitRight) && direction != string(herdr.SplitDown)) {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if !c.startAppAction(writer) {
		return
	}
	defer c.release()
	_, block, ok := c.paneTarget(writer, herdr.AppWindowOpen, target)
	if !ok {
		return
	}
	// Subscribe first, so no Home published after the split is missed.
	updates, unsubscribe := c.subscribeHome()
	defer unsubscribe()
	var created struct {
		Pane     string `json:"pane"`
		Terminal string `json:"terminal"`
	}
	if err := c.doApp(request, "pane.split", map[string]any{"direction": direction, "pane": block}, &created); err != nil {
		c.finishAppError(writer, err)
		return
	}
	if created.Pane == "" || created.Terminal == "" {
		c.unknown(writer, http.StatusBadGateway)
		return
	}
	c.openWhenShown(writer, updates, target, created.Terminal)
}

// runNewTerminalInCore starts core's shell in the exact terminal's current
// folder, labelled with its space and tab. The app adds it as the tab's last
// pane when its window next opens, and until then Home shows it there. The
// ids come from a fresh read just before the run: while the window is closed
// only the app writes the document, so they are the app's current ones.
func (c *coreActionCoordinator) runNewTerminalInCore(writer http.ResponseWriter, request *http.Request, fields map[string]json.RawMessage) {
	keys := []string{"action", "workspace_id", "tab_id", "pane_id", "terminal_id", "best_effort"}
	if !requireObjectKeys(writer, fields, keys, keys) {
		return
	}
	target, targetOK := decodeTerminalTarget(fields)
	if !targetOK || !decodeTrue(fields["best_effort"]) {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if !c.startCoreAction(writer) {
		return
	}
	defer c.release()
	current, _, ok := c.paneTarget(writer, herdr.AppWindowClosed, target)
	if !ok {
		return
	}
	index := slices.IndexFunc(current.read.Terminals, func(terminal herdr.CoreTerminal) bool { return terminal.Address == target.PaneID })
	folder := current.read.Terminals[index].Folder()
	// Core refuses a missing folder only after using up an address.
	if !filepath.IsAbs(folder) || folderMissing(folder) {
		writeRefusal(writer, http.StatusUnprocessableEntity, "folder_missing", "")
		return
	}
	updates, unsubscribe := c.subscribeHome()
	defer unsubscribe()
	address, err := c.runInCore(request, folder, target.WorkspaceID, target.TabID)
	if err != nil {
		c.finishAppError(writer, err)
		return
	}
	c.openWhenShown(writer, updates, target, address)
}

// paneTarget reads Home with window's actions and finds the exact terminal,
// which must be one New terminal starts next to: live, and placed in a pane by
// the document in that space and tab. It returns the app's id for the pane.
func (c *coreActionCoordinator) paneTarget(writer http.ResponseWriter, window herdr.AppWindowState, target terminalActionTarget) (coreView, string, bool) {
	current, err := c.view(context.Background(), window)
	if err != nil {
		writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
		return coreView{}, "", false
	}
	terminal, found := findCoreTerminal(current.home, target)
	if !found {
		writeRefusal(writer, http.StatusNotFound, "not_found", "")
		return coreView{}, "", false
	}
	block, placed := herdr.CorePaneBlock(current.read, target.WorkspaceID, target.TabID, target.PaneID)
	if !containsTerminalAction(terminal.Actions, herdr.TerminalActionSplit) || !placed {
		writeRefusal(writer, http.StatusConflict, "not_applicable", "")
		return coreView{}, "", false
	}
	return current, block, true
}

// subscribeHome follows the published Home; without one, updates is nil and
// nothing new can be shown.
func (c *coreActionCoordinator) subscribeHome() (<-chan herdr.State, func()) {
	if c.published == nil {
		return nil, func() {}
	}
	return c.published.Subscribe()
}

// openWhenShown waits for the published Home to show address in target's
// space and tab, then answers with it so the browser opens it.
func (c *coreActionCoordinator) openWhenShown(writer http.ResponseWriter, updates <-chan herdr.State, target terminalActionTarget, address string) {
	if updates == nil {
		c.unknown(writer, http.StatusBadGateway)
		return
	}
	c.refreshHome()
	timeout := time.NewTimer(splitWait)
	defer timeout.Stop()
	for {
		select {
		case state := <-updates:
			shown, ok := findCoreTerminal(state.Home, terminalActionTarget{WorkspaceID: target.WorkspaceID, TabID: target.TabID, PaneID: address})
			if !ok || state.Connection != herdr.ConnectionLive {
				continue
			}
			writeActionJSON(writer, http.StatusOK, struct {
				Outcome  string               `json:"outcome"`
				Terminal terminalActionTarget `json:"terminal"`
			}{Outcome: "succeeded", Terminal: terminalActionTarget{
				WorkspaceID: target.WorkspaceID, TabID: target.TabID, PaneID: shown.PaneID, TerminalID: shown.TerminalID,
			}})
			return
		case <-timeout.C:
			c.unknown(writer, http.StatusGatewayTimeout)
			return
		}
	}
}

// startAppAction takes the one-action latch and checks the app window is
// open. When it is closed nothing is sent, and Home asks the window again.
// Without the app, core has no such action.
func (c *coreActionCoordinator) startAppAction(writer http.ResponseWriter) bool {
	if c.app == nil {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return false
	}
	if !c.acquire() {
		writeRefusal(writer, http.StatusConflict, "busy", "")
		return false
	}
	if !c.app.WindowOpen(context.Background()) {
		c.release()
		c.refreshHome()
		writeRefusal(writer, http.StatusServiceUnavailable, "app_closed", "")
		return false
	}
	return true
}

// startCoreAction takes the one-action latch for an action confirmed on the
// closed window's sheet. It needs the app, as the app adds what core starts.
func (c *coreActionCoordinator) startCoreAction(writer http.ResponseWriter) bool {
	if c.app == nil {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return false
	}
	if !c.acquire() {
		writeRefusal(writer, http.StatusConflict, "busy", "")
		return false
	}
	return true
}

func decodeTrue(raw json.RawMessage) bool {
	var value bool
	return json.Unmarshal(raw, &value) == nil && value
}

func (c *coreActionCoordinator) doApp(request *http.Request, command string, args map[string]any, result any) error {
	return withCommitAuthority(request, func() error {
		return c.app.Do(context.Background(), command, args, result)
	})
}

// finishAppError answers an app command or core run that did not succeed.
func (c *coreActionCoordinator) finishAppError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, access.ErrUnauthorized):
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
	case errors.Is(err, herdr.ErrAppWindowClosed):
		c.refreshHome()
		writeRefusal(writer, http.StatusServiceUnavailable, "app_closed", "")
	default:
		var apiError *herdr.APIError
		if errors.As(err, &apiError) {
			writeRefusal(writer, http.StatusUnprocessableEntity, "herdr_refused", apiError.Message)
			return
		}
		if herdr.MutationMayHaveRun(err) {
			c.unknown(writer, unknownStatus(err))
			return
		}
		writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
	}
}

// findCoreTerminal finds the terminal at target's space, tab and address, and
// matches its terminal id when target names one.
func findCoreTerminal(home herdr.Home, target terminalActionTarget) (herdr.Terminal, bool) {
	workspace, _ := findCoreWorkspace(home, target.WorkspaceID)
	for _, tab := range workspace.Tabs {
		if tab.ID != target.TabID {
			continue
		}
		for _, terminal := range tab.Terminals {
			if terminal.PaneID == target.PaneID && (target.TerminalID == "" || terminal.TerminalID == target.TerminalID) {
				return terminal, true
			}
		}
	}
	return herdr.Terminal{}, false
}

// preparedView reads under the session's authority, as preparing writes facts
// the browser shows.
func (c *coreActionCoordinator) preparedView(writer http.ResponseWriter, request *http.Request) (coreView, bool) {
	var current coreView
	err := withCommitAuthority(request, func() error {
		var viewErr error
		current, viewErr = c.view(request.Context(), herdr.AppWindowUnpaired)
		return viewErr
	})
	if err != nil {
		if errors.Is(err, access.ErrUnauthorized) {
			writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
			return coreView{}, false
		}
		writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
		return coreView{}, false
	}
	return current, true
}

// finishError answers an error from the first kill, before any kill took effect.
func (c *coreActionCoordinator) finishError(writer http.ResponseWriter, err error) {
	if errors.Is(err, access.ErrUnauthorized) {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	var apiError *herdr.APIError
	if errors.As(err, &apiError) {
		if apiError.Code == "not_found" {
			writeRefusal(writer, http.StatusNotFound, "not_found", "")
			return
		}
		writeRefusal(writer, http.StatusUnprocessableEntity, "herdr_refused", apiError.Message)
		return
	}
	if herdr.MutationMayHaveRun(err) {
		c.unknown(writer, unknownStatus(err))
		return
	}
	writeRefusal(writer, http.StatusServiceUnavailable, "herdr_unavailable", err.Error())
}

func unknownStatus(err error) int {
	if herdr.MutationTimedOut(err) {
		return http.StatusGatewayTimeout
	}
	return http.StatusBadGateway
}

func (c *coreActionCoordinator) succeeded(writer http.ResponseWriter) {
	c.refreshHome()
	writeActionJSON(writer, http.StatusOK, struct {
		Outcome string `json:"outcome"`
	}{Outcome: "succeeded"})
}

func (c *coreActionCoordinator) unknown(writer http.ResponseWriter, status int) {
	c.refreshHome()
	writeActionJSON(writer, status, struct {
		Outcome string `json:"outcome"`
	}{Outcome: "unknown"})
}

func (c *coreActionCoordinator) refreshHome() {
	if c.refresh != nil {
		c.refresh.RequestRefresh()
	}
}

func (c *coreActionCoordinator) acquire() bool {
	select {
	case c.mutation <- struct{}{}:
		return true
	default:
		return false
	}
}

func (c *coreActionCoordinator) release() { <-c.mutation }

// coreSpaceFacts describes closing a space Home shows. Other terminals is not
// a space and has no actions.
func coreSpaceFacts(home herdr.Home, workspaceID string) (confirmationFacts, int, string) {
	workspace, found := findCoreWorkspace(home, workspaceID)
	if !found {
		return confirmationFacts{}, http.StatusNotFound, "not_found"
	}
	if !slices.Contains(workspace.Actions, herdr.WorkspaceActionCloseWorkspace) {
		return confirmationFacts{}, http.StatusConflict, "not_applicable"
	}
	facts := confirmationFacts{WorkspaceLabel: displayWorkspaceLabel(workspace.Label), ScopeWorkspaceIDs: []string{workspaceID}}
	for _, tab := range workspace.Tabs {
		for _, terminal := range tab.Terminals {
			if terminal.Agent == nil {
				continue
			}
			facts.AgentTotal++
			switch terminal.Agent.Status {
			case herdr.StatusWorking:
				facts.InterruptionCounts.Working++
			case herdr.StatusBlocked:
				facts.InterruptionCounts.Blocked++
			case herdr.StatusError:
				facts.InterruptionCounts.Error++
			}
		}
	}
	return facts, http.StatusOK, ""
}

// coreTerminalFacts describes closing one exact terminal. Workers that die
// with it are counted. When nothing would remain in its space, the facts are
// the space's, as for the TUI's last pane.
func coreTerminalFacts(current coreView, target terminalActionTarget) (terminalCloseFacts, int, string) {
	workspace, found := findCoreWorkspace(current.home, target.WorkspaceID)
	var tab herdr.Tab
	var terminal herdr.Terminal
	terminalFound := false
	for _, candidateTab := range workspace.Tabs {
		if candidateTab.ID != target.TabID {
			continue
		}
		for _, candidate := range candidateTab.Terminals {
			if candidate.PaneID == target.PaneID && candidate.TerminalID == target.TerminalID {
				tab, terminal, terminalFound = candidateTab, candidate, true
			}
		}
	}
	if !found || !terminalFound {
		return terminalCloseFacts{}, http.StatusNotFound, "not_found"
	}
	if !containsTerminalAction(terminal.Actions, herdr.TerminalActionClose) {
		return terminalCloseFacts{}, http.StatusConflict, "not_applicable"
	}
	facts := terminalCloseFacts{
		Target: target, WorkspaceLabel: displayWorkspaceLabel(workspace.Label),
		TabLabel: displayTabLabel(tab.Label), TerminalTitle: terminal.Title,
	}
	// A pin is no fact of closing, so pinning meanwhile never makes it stale.
	if terminal.Agent != nil {
		agent := *terminal.Agent
		agent.Pinned = false
		facts.Agent = &agent
	}
	ending := herdr.CoreEndsWith(current.read.Terminals, target.PaneID)
	for _, shown := range current.home.Workspaces {
		for _, shownTab := range shown.Tabs {
			for _, candidate := range shownTab.Terminals {
				if _, ends := ending[candidate.PaneID]; ends && candidate.PaneID != target.PaneID {
					facts.ClosesWorkers++
				}
			}
		}
	}
	if workspace.ID == herdr.OtherTerminalsID {
		return facts, http.StatusOK, ""
	}
	// What remains is what Home shows once the kill lands: a worker that
	// outlives its placed parent moves to Other terminals and keeps nothing here.
	after := current.read
	after.Terminals = slices.DeleteFunc(slices.Clone(current.read.Terminals), func(terminal herdr.CoreTerminal) bool {
		_, ends := ending[terminal.Address]
		return ends
	})
	_, afterHome, err := herdr.ProjectCore(after, "")
	if err != nil {
		return terminalCloseFacts{}, http.StatusServiceUnavailable, "herdr_unavailable"
	}
	remaining, spaceRemains := findCoreWorkspace(afterHome, workspace.ID)
	if !spaceRemains {
		spaceFacts, status, reason := coreSpaceFacts(current.home, workspace.ID)
		if reason != "" {
			return terminalCloseFacts{}, status, reason
		}
		facts.ClosesWorkers = 0
		facts.WorkspaceAction = herdr.WorkspaceActionCloseWorkspace
		facts.WorkspaceExpected = &spaceFacts
		return facts, http.StatusOK, ""
	}
	facts.ClosesTab = !slices.ContainsFunc(remaining.Tabs, func(tab herdr.Tab) bool { return tab.ID == target.TabID })
	return facts, http.StatusOK, ""
}

func findCoreWorkspace(home herdr.Home, workspaceID string) (herdr.Workspace, bool) {
	for _, workspace := range home.Workspaces {
		if workspace.ID == workspaceID {
			return workspace, true
		}
	}
	return herdr.Workspace{}, false
}

// coreSpaceCloseOrder lists the terminals Home shows in the space so that each
// ends after the workers it started, deepest first, and otherwise in Home
// order. Only terminals shown in this space count: a sub-agent the app placed
// elsewhere, or one under Other terminals, is never listed.
func coreSpaceCloseOrder(terminals []herdr.CoreTerminal, workspace herdr.Workspace) []string {
	parents := make(map[string]string, len(terminals))
	for _, terminal := range terminals {
		if terminal.Parent != nil {
			parents[terminal.Address] = *terminal.Parent
		}
	}
	var order []string
	shown := map[string]struct{}{}
	for _, tab := range workspace.Tabs {
		for _, terminal := range tab.Terminals {
			order = append(order, terminal.PaneID)
			shown[terminal.PaneID] = struct{}{}
		}
	}
	// A terminal's depth counts its ancestors shown in this space, so a worker
	// is always deeper than the terminal that started it.
	depth := make(map[string]int, len(order))
	for _, address := range order {
		seen := map[string]struct{}{address: {}}
		for parent, ok := parents[address]; ok; parent, ok = parents[parent] {
			if _, loop := seen[parent]; loop {
				break
			}
			seen[parent] = struct{}{}
			if _, inSpace := shown[parent]; inSpace {
				depth[address]++
			}
		}
	}
	slices.SortStableFunc(order, func(a, b string) int { return depth[b] - depth[a] })
	return order
}

func coreTerminalLive(terminals []herdr.CoreTerminal, address string) bool {
	for _, terminal := range terminals {
		if terminal.Address == address {
			return true
		}
	}
	return false
}

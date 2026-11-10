import assert from "node:assert/strict";
import test from "node:test";

import { parseCompleteHomeState } from "./home-parser.ts";

function completeFrame(): Record<string, unknown> {
  return {
    core: {
      can_open_terminals: false,
      connection: "not_running",
      gap: 0,
      has_home: false,
      herdr_version: "",
      home: { blocked_count: 0, error_count: 0, working_count: 0, workspaces: [] },
      last_known: false,
    },
    epoch: "0123456789abcdef0123456789abcdef",
    tui: completeSide(),
  };
}

function completeSide(): Record<string, unknown> {
  return {
    can_open_terminals: true,
    connection: "live",
    gap: 0,
    has_home: true,
    herdr_version: "0.8.0<script>",
    home: {
      blocked_count: 0,
      error_count: 0,
      working_count: 1,
      workspaces: [{
        actions: ["create_worktree", "close_group"],
        agent_counts: { working: 1 },
        group_agent_counts: { blocked: 1, working: 1 },
        checkout_path: "/work/<b>one</b>",
        id: "opaque-parent",
        label: "Parent <script>",
        number: 1,
        tabs: [{
          current: true,
          id: "opaque-tab",
          label: "Main",
          number: 1,
          terminals: [{
            actions: ["split_terminal", "rename_terminal", "close_terminal"],
            agent: { kind: "codex", name: "Builder", status: "working" },
            manual_name: "Builder",
            pane_id: "opaque-pane",
            terminal_id: "opaque-terminal",
            title: "Builder",
          }],
        }],
        worktrees: [{
          actions: ["close_workspace", "delete_checkout"],
          id: "opaque-child",
          label: "Child",
          number: 2,
          tabs: [],
        }],
      }],
    },
    last_known: false,
  };
}

test("strictly parses documented Home actions and top-level checkout paths", () => {
  const parsed = parseCompleteHomeState(completeFrame())?.tui;

  assert.equal(parsed?.home.workspaces[0].actions.join(","), "create_worktree,close_group");
  assert.equal(parsed?.home.workspaces[0].checkout_path, "/work/<b>one</b>");
  assert.equal(parsed?.home.workspaces[0].group_agent_counts?.blocked, 1);
  assert.equal(parsed?.home.workspaces[0].tabs[0].terminals[0].actions.join(","), "split_terminal,rename_terminal,close_terminal");
  assert.equal(parsed?.home.workspaces[0].tabs[0].terminals[0].manual_name, "Builder");
  assert.equal(parsed?.home.workspaces[0].worktrees?.[0].actions.join(","), "close_workspace,delete_checkout");
  assert.equal(parsed?.home.workspaces[0].label, "Parent <script>");
  assert.equal(parsed?.herdr_version, "0.8.0<script>");
});

test("rejects missing, duplicate, unknown, misplaced, or extra additive Home fields", () => {
  const missing = completeFrame();
  delete ((tuiOf(missing).home as { workspaces: Array<Record<string, unknown>> }).workspaces[0]).actions;
  assert.equal(parseCompleteHomeState(missing), undefined);

  const duplicate = completeFrame();
  (tuiOf(duplicate).home as { workspaces: Array<Record<string, unknown>> }).workspaces[0].actions = ["close_group", "close_group"];
  assert.equal(parseCompleteHomeState(duplicate), undefined);

  const unknown = completeFrame();
  (tuiOf(unknown).home as { workspaces: Array<Record<string, unknown>> }).workspaces[0].actions = ["remove_forcefully"];
  assert.equal(parseCompleteHomeState(unknown), undefined);

  const unknownTerminal = completeFrame();
  (tuiOf(unknownTerminal).home as { workspaces: Array<{ tabs: Array<{ terminals: Array<Record<string, unknown>> }> }> })
    .workspaces[0].tabs[0].terminals[0].actions = ["split_terminal", "invent_terminal"];
  assert.equal(parseCompleteHomeState(unknownTerminal), undefined);

  const childGroupCounts = completeFrame();
  const childWithCounts = (tuiOf(childGroupCounts).home as { workspaces: Array<{ worktrees: Array<Record<string, unknown>> }> })
    .workspaces[0].worktrees[0];
  childWithCounts.group_agent_counts = { working: 1 };
  assert.equal(parseCompleteHomeState(childGroupCounts), undefined);

  const childPath = completeFrame();
  const child = (tuiOf(childPath).home as { workspaces: Array<{ worktrees: Array<Record<string, unknown>> }> }).workspaces[0]
    .worktrees[0];
  child.checkout_path = "/must/not/be/a/suggestion";
  assert.equal(parseCompleteHomeState(childPath), undefined);

  const extra = completeFrame();
  (tuiOf(extra).home as Record<string, unknown>).patch = [];
  assert.equal(parseCompleteHomeState(extra), undefined);
});

function tuiOf(frame: Record<string, unknown>): Record<string, unknown> {
  return frame.tui as Record<string, unknown>;
}

test("parses both Herdrs, with Herdr Core's error status and error counts", () => {
  const frame = completeFrame();
  const core = completeSide();
  frame.core = core;
  const home = core.home as Record<string, unknown>;
  home.error_count = 1;
  const workspace = (home.workspaces as Array<Record<string, unknown>>)[0];
  workspace.agent_counts = { error: 1 };
  const tab = (workspace.tabs as Array<Record<string, unknown>>)[0];
  (tab.terminals as Array<Record<string, unknown>>)[0].agent = { kind: "claude-code", name: "Claude Code", status: "error" };

  const parsed = parseCompleteHomeState(frame);
  assert.equal(parsed?.epoch, "0123456789abcdef0123456789abcdef");
  assert.equal(parsed?.tui.home.error_count, 0);
  assert.equal(parsed?.core.home.error_count, 1);
  assert.equal(parsed?.core.home.workspaces[0].agent_counts?.error, 1);
  assert.equal(parsed?.core.home.workspaces[0].tabs[0].terminals[0].agent?.status, "error");

  const missingErrorCount = completeFrame();
  delete (tuiOf(missingErrorCount).home as Record<string, unknown>).error_count;
  assert.equal(parseCompleteHomeState(missingErrorCount), undefined);
});

test("requires exactly epoch, tui and core, each a complete state", () => {
  for (const key of ["epoch", "tui", "core"]) {
    const missing = completeFrame();
    delete missing[key];
    assert.equal(parseCompleteHomeState(missing), undefined, `without ${key}`);
  }
  const extra = completeFrame();
  extra.herdr = "tui";
  assert.equal(parseCompleteHomeState(extra), undefined);
  const named = completeFrame();
  tuiOf(named).herdr = "tui";
  assert.equal(parseCompleteHomeState(named), undefined);
  const single = { ...completeSide(), epoch: "0123456789abcdef0123456789abcdef" };
  assert.equal(parseCompleteHomeState(single), undefined);
  const brokenCore = completeFrame();
  (brokenCore.core as Record<string, unknown>).connection = "sleeping";
  assert.equal(parseCompleteHomeState(brokenCore), undefined);
});

test("reads whether each Herdr's terminals can open, and requires it", () => {
  const parsed = parseCompleteHomeState(completeFrame());
  assert.equal(parsed?.tui.can_open_terminals, true);
  assert.equal(parsed?.core.can_open_terminals, false);
  const missing = completeFrame();
  delete tuiOf(missing).can_open_terminals;
  assert.equal(parseCompleteHomeState(missing), undefined);
  const wrongType = completeFrame();
  (wrongType.core as Record<string, unknown>).can_open_terminals = "false";
  assert.equal(parseCompleteHomeState(wrongType), undefined);
});

test("parses the Herdr app window's New space and a space's own name, and nothing else", () => {
  const frame = completeFrame();
  const core = completeSide();
  const home = core.home as { actions?: unknown; workspaces: Array<Record<string, unknown>> };
  home.actions = ["create_space"];
  home.workspaces[0].actions = ["rename_space", "close_workspace"];
  home.workspaces[0].manual_name = "Space <b>";
  frame.core = core;
  const parsed = parseCompleteHomeState(frame)?.core;
  assert.equal(parsed?.home.actions?.join(","), "create_space");
  assert.equal(parsed?.home.workspaces[0].actions.join(","), "rename_space,close_workspace");
  assert.equal(parsed?.home.workspaces[0].manual_name, "Space <b>");
  assert.equal(parseCompleteHomeState(completeFrame())?.tui.home.actions, undefined);

  for (const actions of [[], ["create_space", "create_space"], ["create_worktree"], "create_space"]) {
    home.actions = actions;
    assert.equal(parseCompleteHomeState(frame), undefined);
  }
  home.actions = ["create_space"];
  home.workspaces[0].manual_name = 7;
  assert.equal(parseCompleteHomeState(frame), undefined);
});

test("parses Rename tab and a tab's own name", () => {
  const frame = completeFrame();
  const core = completeSide();
  const home = core.home as { workspaces: Array<{ actions: unknown; tabs: Array<Record<string, unknown>> }> };
  home.workspaces[0].actions = ["rename_space", "rename_tab", "close_workspace"];
  home.workspaces[0].tabs[0].manual_name = "Tab <b>";
  frame.core = core;
  const parsed = parseCompleteHomeState(frame)?.core;
  assert.equal(parsed?.home.workspaces[0].actions.join(","), "rename_space,rename_tab,close_workspace");
  assert.equal(parsed?.home.workspaces[0].tabs[0].manual_name, "Tab <b>");
  assert.equal(Object.hasOwn(parseCompleteHomeState(completeFrame())!.tui.home.workspaces[0].tabs[0], "manual_name"), false);
  home.workspaces[0].tabs[0].manual_name = 7;
  assert.equal(parseCompleteHomeState(frame), undefined);
});

test("parses a closed Herdr app window, and nothing else", () => {
  const frame = completeFrame();
  const core = completeSide();
  const home = core.home as Record<string, unknown>;
  home.actions = ["create_space"];
  home.app_window = "closed";
  frame.core = core;
  assert.equal(parseCompleteHomeState(frame)?.core.home.app_window, "closed");
  assert.equal(parseCompleteHomeState(completeFrame())?.core.home.app_window, undefined);
  for (const value of ["open", "", null, true]) {
    home.app_window = value;
    assert.equal(parseCompleteHomeState(frame), undefined);
  }
});

test("parses a pinned agent with Pin and Unpin, and nothing else as a pin", () => {
  const withAgent = (agent: Record<string, unknown>, actions = ["unpin_agent", "close_terminal"]) => {
    const frame = completeFrame();
    const home = tuiOf(frame).home as Record<string, unknown>;
    const workspace = (home.workspaces as Array<Record<string, unknown>>)[0];
    const terminal = ((workspace.tabs as Array<Record<string, unknown>>)[0].terminals as Array<Record<string, unknown>>)[0];
    terminal.agent = agent;
    terminal.actions = actions;
    return parseCompleteHomeState(frame)?.tui.home.workspaces[0].tabs[0].terminals[0];
  };
  const pinned = withAgent({ kind: "claude-code", name: "Claude Code", status: "idle", pinned: true });
  assert.deepEqual(pinned?.agent, { kind: "claude-code", name: "Claude Code", status: "idle", pinned: true });
  assert.deepEqual(pinned?.actions, ["unpin_agent", "close_terminal"]);
  assert.deepEqual(withAgent({ kind: "pi", name: "pi", status: "done" }, ["pin_agent"])?.actions, ["pin_agent"]);
  assert.equal(withAgent({ kind: "pi", name: "pi", status: "done", pinned: false }), undefined);
  assert.equal(withAgent({ kind: "pi", name: "pi", status: "done", pinned: "1" }), undefined);
  assert.equal(withAgent({ kind: "pi", name: "pi", status: "done" }, ["pin_terminal"]), undefined);
});

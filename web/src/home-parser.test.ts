import assert from "node:assert/strict";
import test from "node:test";

import { parseCompleteHomeState } from "./home-parser.ts";

function completeFrame(): Record<string, unknown> {
  return {
    connection: "live",
    epoch: "0123456789abcdef0123456789abcdef",
    gap: 0,
    has_home: true,
    home: {
      blocked_count: 0,
      working_count: 1,
      workspaces: [{
        actions: ["create_worktree", "close_group"],
        agent_counts: { working: 1 },
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
            agent: { kind: "codex", name: "Builder", status: "working" },
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
  const parsed = parseCompleteHomeState(completeFrame());

  assert.equal(parsed?.home.workspaces[0].actions.join(","), "create_worktree,close_group");
  assert.equal(parsed?.home.workspaces[0].checkout_path, "/work/<b>one</b>");
  assert.equal(parsed?.home.workspaces[0].worktrees?.[0].actions.join(","), "close_workspace,delete_checkout");
  assert.equal(parsed?.home.workspaces[0].label, "Parent <script>");
});

test("rejects missing, duplicate, unknown, misplaced, or extra additive Home fields", () => {
  const missing = completeFrame();
  delete ((missing.home as { workspaces: Array<Record<string, unknown>> }).workspaces[0]).actions;
  assert.equal(parseCompleteHomeState(missing), undefined);

  const duplicate = completeFrame();
  (duplicate.home as { workspaces: Array<Record<string, unknown>> }).workspaces[0].actions = ["close_group", "close_group"];
  assert.equal(parseCompleteHomeState(duplicate), undefined);

  const unknown = completeFrame();
  (unknown.home as { workspaces: Array<Record<string, unknown>> }).workspaces[0].actions = ["remove_forcefully"];
  assert.equal(parseCompleteHomeState(unknown), undefined);

  const childPath = completeFrame();
  const child = (childPath.home as { workspaces: Array<{ worktrees: Array<Record<string, unknown>> }> }).workspaces[0]
    .worktrees[0];
  child.checkout_path = "/must/not/be/a/suggestion";
  assert.equal(parseCompleteHomeState(childPath), undefined);

  const extra = completeFrame();
  (extra.home as Record<string, unknown>).patch = [];
  assert.equal(parseCompleteHomeState(extra), undefined);
});

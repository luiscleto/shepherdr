import assert from "node:assert/strict";
import test from "node:test";

import {
  parseWorkspaceActionResponse,
  WorkspaceActionsClient,
  type ConfirmationFacts,
} from "./workspace-actions.ts";

const expected: ConfirmationFacts = {
  workspace_label: "Review <script>",
  scope_workspace_ids: ["opaque-a", "opaque-b"],
  agent_total: 4,
  interruption_counts: { working: 2, blocked: 1, unknown: 0 },
};

function jsonResponse(value: unknown, status = 200): Response {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json; charset=utf-8" },
  });
}

test("parses only exact documented action outcomes and prepared facts", () => {
  const prepared = parseWorkspaceActionResponse({
    outcome: "prepared",
    action: "close_group",
    workspace_id: "opaque-parent",
    expected,
  });
  assert.equal(prepared?.outcome, "prepared");
  assert.equal(prepared?.outcome === "prepared" ? prepared.expected.agent_total : -1, 4);

  assert.equal(parseWorkspaceActionResponse({ outcome: "succeeded", resource: {} }), undefined);
  assert.equal(parseWorkspaceActionResponse({ outcome: "unknown", detail: "maybe" }), undefined);
  assert.equal(parseWorkspaceActionResponse({ outcome: "refused", reason: "dirty" }), undefined);
  assert.equal(parseWorkspaceActionResponse({
    outcome: "prepared",
    action: "close_group",
    workspace_id: "opaque-parent",
    expected: { ...expected, scope_workspace_ids: ["opaque-b", "opaque-a"] },
  }), undefined);
  assert.equal(parseWorkspaceActionResponse({
    outcome: "prepared",
    action: "close_group",
    workspace_id: "opaque-parent",
    expected: { ...expected, agent_total: 2 },
  }), undefined);
  assert.equal(parseWorkspaceActionResponse({
    outcome: "prepared",
    action: "close_group",
    workspace_id: "opaque-parent",
    expected: { ...expected, checkout_path: "/unexpected" },
  }), undefined);
  assert.equal(parseWorkspaceActionResponse({
    outcome: "prepared",
    action: "delete_checkout",
    workspace_id: "opaque-child",
    expected,
  }), undefined);
});

test("uses exact same-origin JSON requests and returns prepared facts unchanged", async () => {
  const calls: Array<{ endpoint: string; body: string; method?: string; mode?: RequestMode }> = [];
  const client = new WorkspaceActionsClient(async (input, init) => {
    calls.push({ endpoint: String(input), body: String(init?.body), method: init?.method, mode: init?.mode });
    return jsonResponse({
      outcome: "prepared",
      action: "close_group",
      workspace_id: "opaque-parent",
      expected,
    });
  });

  const response = await client.prepare({ action: "close_group", workspace_id: "opaque-parent" });

  assert.equal(calls.length, 1);
  assert.equal(calls[0].endpoint, "/api/workspace-actions/prepare");
  assert.equal(calls[0].method, "POST");
  assert.equal(calls[0].mode, "same-origin");
  assert.equal(calls[0].body, '{"action":"close_group","workspace_id":"opaque-parent"}');
  assert.equal(response.outcome, "prepared");
  assert.equal(response.outcome === "prepared" ? JSON.stringify(response.expected) : "", JSON.stringify(expected));
});

test("omits blank optional creation fields and never retries an unconfirmed run", async () => {
  const calls: string[] = [];
  const client = new WorkspaceActionsClient(async (_input, init) => {
    calls.push(String(init?.body));
    throw new TypeError("connection lost");
  });

  const response = await client.run({ action: "create_space", working_directory: "~/exact $VALUE *" });

  assert.equal(calls.length, 1);
  assert.equal(calls[0], '{"action":"create_space","working_directory":"~/exact $VALUE *"}');
  assert.equal(response.outcome, "unknown");
});

test("posts complete prepared facts as stale-state checks without adding authority fields", async () => {
  const calls: string[] = [];
  const client = new WorkspaceActionsClient(async (_input, init) => {
    calls.push(String(init?.body));
    return jsonResponse({ outcome: "succeeded" });
  });
  const request = { action: "close_group" as const, workspace_id: "opaque-parent", expected };

  const response = await client.run(request);

  assert.equal(response.outcome, "succeeded");
  assert.equal(calls.length, 1);
  assert.equal(calls[0], JSON.stringify(request));
  assert.doesNotMatch(calls[0], /force|target_path|branch/);
});

test("rejects mismatched identities and status/outcome combinations", async () => {
  const mismatched = new WorkspaceActionsClient(async () => jsonResponse({
    outcome: "prepared",
    action: "close_workspace",
    workspace_id: "another-workspace",
    expected,
  }));
  await assert.rejects(
    mismatched.prepare({ action: "close_workspace", workspace_id: "opaque-workspace" }),
    { name: "WorkspaceActionRequestError" },
  );

  const wrongStatus = new WorkspaceActionsClient(async () => jsonResponse({ outcome: "succeeded" }, 409));
  const result = await wrongStatus.run({ action: "create_worktree", workspace_id: "opaque-parent" });
  assert.equal(result.outcome, "unknown");
});

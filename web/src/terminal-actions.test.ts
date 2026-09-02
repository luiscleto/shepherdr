import assert from "node:assert/strict";
import test from "node:test";

import {
  parseTerminalActionResponse,
  TerminalActionRequestError,
  TerminalActionsClient,
  type TerminalCloseFacts,
} from "./terminal-actions.ts";

const expected: TerminalCloseFacts = {
  target: {
    workspace_id: "opaque-workspace",
    tab_id: "opaque-tab",
    pane_id: "opaque-pane",
    terminal_id: "opaque-terminal",
  },
  workspace_label: "Repository <script>",
  tab_label: "Main",
  terminal_title: "Builder",
  agent: { kind: "codex", name: "Builder", status: "working" },
  closes_tab: false,
};

test("parses exact terminal action outcomes and rejects ambiguous facts", () => {
  assert.deepEqual(parseTerminalActionResponse({
    outcome: "prepared",
    action: "close_terminal",
    expected,
  }), { outcome: "prepared", action: "close_terminal", expected });
  assert.deepEqual(parseTerminalActionResponse({
    outcome: "succeeded",
    terminal: expected.target,
  }), { outcome: "succeeded", terminal: expected.target });
  assert.deepEqual(parseTerminalActionResponse({ outcome: "unknown" }), { outcome: "unknown" });

  assert.equal(parseTerminalActionResponse({ outcome: "unknown", retry: true }), undefined);
  assert.equal(parseTerminalActionResponse({
    outcome: "prepared",
    action: "close_terminal",
    expected: { ...expected, closes_tab: true, workspace_action: "close_workspace", workspace_expected: {} },
  }), undefined);
  assert.equal(parseTerminalActionResponse({
    outcome: "succeeded",
    terminal: { ...expected.target, pane_id: "" },
  }), undefined);
});

test("posts exact same-origin terminal requests once and preserves returned identity", async () => {
  const calls: Array<{ input: RequestInfo | URL; init?: RequestInit }> = [];
  const client = new TerminalActionsClient(async (input, init) => {
    calls.push({ input, init });
    return new Response(JSON.stringify({ outcome: "succeeded", terminal: expected.target }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  });
  const response = await client.run({
    action: "split_terminal",
    ...expected.target,
    direction: "down",
  });

  assert.deepEqual(response, { outcome: "succeeded", terminal: expected.target });
  assert.equal(calls.length, 1);
  assert.equal(calls[0].input, "/api/terminal-actions");
  assert.equal(calls[0].init?.method, "POST");
  assert.equal(calls[0].init?.mode, "same-origin");
  assert.equal(calls[0].init?.credentials, "same-origin");
  assert.equal(
    calls[0].init?.body,
    '{"action":"split_terminal","workspace_id":"opaque-workspace","tab_id":"opaque-tab","pane_id":"opaque-pane","terminal_id":"opaque-terminal","direction":"down"}',
  );
});

test("rejects mismatched HTTP outcomes without retrying", async () => {
  let calls = 0;
  const client = new TerminalActionsClient(async () => {
    calls++;
    return new Response(JSON.stringify({ outcome: "unknown" }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  });

  await assert.rejects(
    client.run({ action: "rename_terminal", ...expected.target, name: "New name" }),
    TerminalActionRequestError,
  );
  assert.equal(calls, 1);
});

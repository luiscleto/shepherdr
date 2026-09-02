import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { Window } from "happy-dom";

import { HomeView } from "./home-view.ts";
import type { Home, HomeState } from "./home-model.ts";
import type {
  PreparedWorkspaceActionResponse,
  RunWorkspaceActionRequest,
  RunWorkspaceActionResponse,
} from "./workspace-actions.ts";
import type {
  PreparedTerminalActionResponse,
  RunTerminalActionRequest,
  RunTerminalActionResponse,
  TerminalActionTarget,
} from "./terminal-actions.ts";

function actionHome(): Home {
  return {
    blocked_count: 1,
    working_count: 2,
    workspaces: [{
      actions: ["create_worktree", "close_group"],
      agent_counts: { working: 1 },
      group_agent_counts: { blocked: 1, working: 2 },
      checkout_path: "/repo/<main>",
      id: "opaque-parent",
      label: "Parent <script>",
      number: 1,
      tabs: [{
        current: true,
        id: "parent-tab",
        label: "Main",
        number: 1,
        terminals: [{
          actions: [],
          agent: { kind: "codex", name: "Builder", status: "working" },
          pane_id: "parent-pane",
          terminal_id: "parent-terminal",
          title: "Parent terminal",
        }],
      }],
      worktrees: [{
        actions: ["close_workspace", "delete_checkout"],
        id: "opaque-child",
        label: "Child <img>",
        number: 2,
        tabs: [{
          current: true,
          id: "child-tab",
          label: "Child",
          number: 1,
          terminals: [{
            actions: [],
            agent: { kind: "codex", name: "Reviewer", status: "blocked" },
            pane_id: "child-pane",
            terminal_id: "child-terminal",
            title: "Child terminal",
          }],
        }],
      }],
    }, {
      actions: ["close_workspace"],
      checkout_path: "/repo/<main>",
      id: "other-parent",
      label: "Other",
      number: 2,
      tabs: [],
    }, {
      actions: ["close_workspace"],
      checkout_path: "/repo/other",
      id: "third-parent",
      label: "Third",
      number: 3,
      tabs: [],
    }],
  };
}

function terminalHome(): Home {
  return {
    blocked_count: 0,
    working_count: 1,
    workspaces: [{
      actions: ["close_workspace"],
      agent_counts: { idle: 1, working: 1 },
      id: "terminal-workspace",
      label: "Terminal workspace",
      number: 1,
      tabs: [{
        current: true,
        id: "terminal-tab-main",
        label: "Main",
        number: 1,
        terminals: [{
          actions: ["split_terminal", "rename_terminal", "close_terminal"],
          agent: { kind: "codex", name: "Builder", status: "working" },
          manual_name: "Build <script>",
          pane_id: "pane:anchor",
          terminal_id: "terminal:anchor",
          title: "Build <script>",
        }],
      }, {
        current: false,
        id: "terminal-tab-other",
        label: "Logs",
        number: 2,
        terminals: [{
          actions: ["split_terminal", "rename_terminal", "close_terminal"],
          agent: { kind: "codex", name: "Watcher", status: "idle" },
          pane_id: "pane:other",
          terminal_id: "terminal:other",
          title: "Logs",
        }],
      }],
    }],
  };
}

function state(home = actionHome()): HomeState {
  return { connection: "live", gap: 0, has_home: true, herdr_version: "0.8.0", home, last_known: false };
}

interface ViewOverrides {
  isHomeActive?: () => boolean;
  onOpenCreated?: (target: TerminalActionTarget) => void;
  prepare?: (request: { action: "close_workspace" | "close_group" | "delete_checkout"; workspace_id: string }) => Promise<PreparedWorkspaceActionResponse>;
  prepareTerminal?: (request: { action: "close_terminal" } & TerminalActionTarget) => Promise<PreparedTerminalActionResponse>;
  run?: (request: RunWorkspaceActionRequest) => Promise<RunWorkspaceActionResponse>;
  runTerminal?: (request: RunTerminalActionRequest) => Promise<RunTerminalActionResponse>;
}

function makeView(window: Window, overrides: ViewOverrides = {}, home = actionHome()) {
  const app = window.document.createElement("main");
  window.document.body.append(app);
  const view = new HomeView(app, {
    isHomeActive: overrides.isHomeActive ?? (() => true),
    onFocusPane: () => undefined,
    onOpen: () => undefined,
    onOpenCreated: overrides.onOpenCreated ?? (() => undefined),
    onShowAll: () => undefined,
    onShowBlocked: () => undefined,
    prepareWorkspaceAction: overrides.prepare ?? (async () => ({ outcome: "refused", reason: "not_applicable" })),
    prepareTerminalAction: overrides.prepareTerminal ?? (async () => ({ outcome: "refused", reason: "not_applicable" })),
    runWorkspaceAction: overrides.run ?? (async () => ({ outcome: "succeeded" })),
    runTerminalAction: overrides.runTerminal ?? (async () => ({ outcome: "succeeded" })),
  });
  view.render({ actionsAvailable: true, mode: "all", reachability: "current", state: state(home) });
  return { app, view };
}

function requiredElement<T extends HTMLElement = HTMLElement>(root: ParentNode, selector: string): T {
  const node = root.querySelector<T>(selector);
  if (!node) throw new Error(`missing ${selector}`);
  return node;
}

function requiredMatchingElement<T extends HTMLElement>(
  root: ParentNode,
  selector: string,
  matches: (node: T) => boolean,
): T {
  const node = Array.from(root.querySelectorAll<T>(selector)).find(matches);
  if (!node) throw new Error(`missing matching ${selector}`);
  return node;
}

async function settle(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

test("New space filters deduplicated top-level paths and submits exact free-form input", async () => {
  const window = new Window({ url: "http://localhost/" });
  let submitted: RunWorkspaceActionRequest | undefined;
  let finish: ((response: RunWorkspaceActionResponse) => void) | undefined;
  const pending = new Promise<RunWorkspaceActionResponse>((resolve) => {
    finish = resolve;
  });
  const { app, view } = makeView(window, {
    run: async (request) => {
      submitted = request;
      return pending;
    },
  });
  const row = requiredElement(app, '[data-pane-key="parent-pane"]');
  row.dataset.testIdentity = "original-parent-row";
  const filter = requiredElement<HTMLInputElement>(app, ".home-filter input");
  filter.value = "Parent";
  filter.dispatchEvent(new window.Event("input", { bubbles: true }));
  window.scrollTo(0, 217);
  const newSpace = requiredElement<HTMLButtonElement>(app, ".new-space-action");

  newSpace.click();
  const directory = requiredElement<HTMLInputElement>(app, 'input[name="working_directory"]');
  assert.equal(directory.value, "~");
  assert.deepEqual(
    Array.from(app.querySelectorAll(".checkout-suggestion"), (node) => node.textContent),
    ["/repo/<main>", "/repo/other"],
  );
  directory.value = "other";
  directory.dispatchEvent(new window.Event("input", { bubbles: true }));
  assert.deepEqual(Array.from(app.querySelectorAll(".checkout-suggestion"), (node) => node.textContent), ["/repo/other"]);

  directory.value = "~/free $HOME * <b>";
  requiredElement<HTMLInputElement>(app, 'input[name="label"]').value = "   ";
  requiredElement<HTMLFormElement>(app, ".home-action-form")
    .dispatchEvent(new window.Event("submit", { bubbles: true, cancelable: true }));
  await settle();

  assert.equal(JSON.stringify(submitted), '{"action":"create_space","working_directory":"~/free $HOME * <b>"}');
  assert.equal(app.querySelectorAll(".new-space-action").length, 0);
  assert.equal(app.querySelectorAll(".workspace-menu-trigger").length, 0);
  assert.equal(view.row("parent-pane")?.dataset.testIdentity, "original-parent-row");
  assert.equal(requiredElement(app, ".terminal-name").textContent, "Parent terminal");
  assert.equal(filter.value, "Parent");
  assert.equal(window.scrollY, 217);

  finish?.({ outcome: "succeeded" });
  await settle();
  assert.match(requiredElement(app, ".home-action-panel").textContent ?? "", /Space created/);
  assert.equal(view.row("parent-pane")?.dataset.testIdentity, "original-parent-row");
  requiredElement<HTMLButtonElement>(app, ".home-action-panel button").click();
  assert.equal((window.document.activeElement as HTMLElement | null)?.className, "new-space-action");
  assert.equal(filter.value, "Parent");
  assert.equal(window.scrollY, 217);
  window.close();
});

test("a pending Home action cannot repaint the shared root after Terminal navigation", async () => {
  const window = new Window({ url: "http://localhost/" });
  let finish: ((response: RunWorkspaceActionResponse) => void) | undefined;
  const pending = new Promise<RunWorkspaceActionResponse>((resolve) => {
    finish = resolve;
  });
  const { app } = makeView(window, {
    isHomeActive: () => !window.location.hash.startsWith("#terminal="),
    run: async () => pending,
  });
  requiredElement<HTMLButtonElement>(app, ".new-space-action").click();
  requiredElement<HTMLFormElement>(app, ".home-action-form")
    .dispatchEvent(new window.Event("submit", { bubbles: true, cancelable: true }));
  await settle();

  window.location.hash = "#terminal=parent-pane";
  const terminalRoute = window.document.createElement("section");
  terminalRoute.className = "terminal-route-sentinel";
  terminalRoute.textContent = "Terminal route remains active";
  app.className = "terminal-screen";
  app.replaceChildren(terminalRoute);
  finish?.({ outcome: "succeeded" });
  await settle();

  assert.equal(window.location.hash, "#terminal=parent-pane");
  assert.equal(app.className, "terminal-screen");
  assert.equal(app.childElementCount, 1);
  assert.equal(app.firstElementChild?.className, "terminal-route-sentinel");
  assert.equal(app.textContent, "Terminal route remains active");
  assert.equal(app.querySelectorAll(".home-action-layer, .masthead, .home-tools").length, 0);
  window.close();
});

test("workspace menu is immediately after Open and keyboard reaches fresh close confirmation", async () => {
  const window = new Window({ url: "http://localhost/" });
  let finishRun: ((response: RunWorkspaceActionResponse) => void) | undefined;
  let prepareRequest: { action: "close_workspace" | "close_group" | "delete_checkout"; workspace_id: string } | undefined;
  let runRequest: RunWorkspaceActionRequest | undefined;
  const pendingRun = new Promise<RunWorkspaceActionResponse>((resolve) => {
    finishRun = resolve;
  });
  const prepared = {
    outcome: "prepared" as const,
    action: "close_group" as const,
    workspace_id: "opaque-parent",
    expected: {
      workspace_label: "Parent <script>",
      scope_workspace_ids: ["opaque-child", "opaque-parent"],
      agent_total: 7,
      interruption_counts: { working: 2, blocked: 1, unknown: 3 },
    },
  };
  const { app, view } = makeView(window, {
    prepare: async (request) => {
      prepareRequest = request;
      return prepared;
    },
    run: async (request) => {
      runRequest = request;
      return pendingRun;
    },
  });
  requiredElement<HTMLInputElement>(app, ".home-filter input").value = "";
  const disclosure = requiredElement<HTMLButtonElement>(app, ".workspace-set-disclosure");
  disclosure.click();
  const row = requiredElement(app, '[data-pane-key="parent-pane"]');
  row.dataset.testIdentity = "original-group-row";
  const actionRow = row.parentElement;
  if (!actionRow) throw new Error("missing parent workspace action row");
  assert.equal(actionRow?.className, "workspace-action-row");
  assert.deepEqual(Array.from(actionRow.children, (node) => (node as HTMLElement).className), [
    "terminal-row",
    "workspace-menu terminal-menu",
  ]);

  const trigger = requiredElement<HTMLButtonElement>(actionRow, ".workspace-menu-trigger");
  trigger.click();
  const items = Array.from(actionRow.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'));
  assert.deepEqual(items.map((item) => item.textContent), ["New worktree", "Close workspace"]);
  assert.equal(window.document.activeElement?.textContent, "New worktree");
  items[0].dispatchEvent(new window.KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
  assert.equal(window.document.activeElement?.textContent, "Close workspace");
  items[1].dispatchEvent(new window.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
  assert.equal((window.document.activeElement as HTMLElement | null)?.getAttribute("aria-label"), "Actions for Parent <script>");
  assert.equal(requiredElement(actionRow, '[role="menu"]').hidden, true);

  trigger.click();
  items[1].click();
  await settle();
  const panel = requiredElement(app, ".home-action-panel");
  const text = panel.textContent ?? "";
  assert.equal(requiredElement(panel, ".home-action-title").textContent, "Close Parent <script>?");
  assert.match(text, /1 additional linked workspace will also close\./);
  assert.match(text, /7 agents will be affected\./);
  assert.equal(
    requiredElement(panel, ".home-action-warning").textContent,
    "Agents may be interrupted: 2 working, 1 blocked, 3 unknown.",
  );
  assert.doesNotMatch(text, /opaque-child|opaque-parent/);
  assert.equal(panel.querySelectorAll("script").length, 0);
  assert.equal(disclosure.getAttribute("aria-expanded"), "false");
  assert.equal(JSON.stringify(prepareRequest), '{"action":"close_group","workspace_id":"opaque-parent"}');
  assert.equal(requiredElement<HTMLButtonElement>(panel, ".home-action-primary").textContent, "Close workspace");

  requiredElement<HTMLButtonElement>(panel, ".home-action-primary").click();
  await settle();
  assert.equal(requiredElement(panel, ".home-action-title").textContent, "Closing workspace");
  assert.equal(requiredElement(panel, ".home-action-copy").textContent, "Wait for Herdr to finish.");
  assert.equal(runRequest && "action" in runRequest ? runRequest.action : "", "close_group");
  assert.equal(
    runRequest && "expected" in runRequest ? JSON.stringify(runRequest.expected) : "",
    JSON.stringify(prepared.expected),
  );
  assert.equal(view.row("parent-pane")?.dataset.testIdentity, "original-group-row");
  assert.equal(disclosure.getAttribute("aria-expanded"), "false");

  finishRun?.({ outcome: "succeeded" });
  await settle();
  assert.equal(requiredElement(panel, ".home-action-title").textContent, "Workspace closed");
  assert.equal(
    requiredElement(panel, ".home-action-copy").textContent,
    "Linked checkout folders and branches remain. Home will update when it is ready.",
  );
  requiredElement<HTMLButtonElement>(app, ".home-action-panel button").click();
  assert.equal((window.document.activeElement as HTMLElement | null)?.getAttribute("aria-label"), "Actions for Parent <script>");
  window.close();
});

test("close confirmation describes only current nonzero scope and agent facts", async () => {
  const cases = [{
    name: "top-level repository without linked workspaces or agents",
    scope_workspace_ids: ["opaque-parent"],
    agent_total: 0,
    expectedCopy: "This closes this workspace. Its terminals will end, and unsaved work can be lost. The folder and branch remain.",
  }, {
    name: "repository with linked workspaces and no agents",
    scope_workspace_ids: ["opaque-child-a", "opaque-child-b", "opaque-parent"],
    agent_total: 0,
    expectedCopy: "This closes this workspace. 2 additional linked workspaces will also close. Their terminals will end, and unsaved work can be lost. Linked checkout folders and branches remain.",
  }, {
    name: "repository with only idle or done agents",
    scope_workspace_ids: ["opaque-parent"],
    agent_total: 4,
    expectedCopy: "This closes this workspace. 4 agents will be affected. Its terminals will end, and unsaved work can be lost. The folder and branch remain.",
  }];

  for (const closeCase of cases) {
    const window = new Window({ url: "http://localhost/" });
    const { app } = makeView(window, {
      prepare: async () => ({
        outcome: "prepared",
        action: "close_group",
        workspace_id: "opaque-parent",
        expected: {
          workspace_label: "Parent <script>",
          scope_workspace_ids: closeCase.scope_workspace_ids,
          agent_total: closeCase.agent_total,
          interruption_counts: { working: 0, blocked: 0, unknown: 0 },
        },
      }),
    });
    const trigger = requiredMatchingElement<HTMLButtonElement>(
      app,
      ".workspace-menu-trigger",
      (button) => button.getAttribute("aria-label") === "Actions for Parent <script>",
    );
    trigger.click();
    const closeAction = requiredMatchingElement<HTMLButtonElement>(
      app,
      '[role="menuitem"]',
      (button) => button.textContent === "Close workspace",
    );
    closeAction.click();
    await settle();

    const panel = requiredElement(app, ".home-action-panel");
    assert.equal(requiredElement(panel, ".home-action-title").textContent, "Close Parent <script>?", closeCase.name);
    assert.equal(requiredElement(panel, ".home-action-copy").textContent, closeCase.expectedCopy, closeCase.name);
    assert.equal(panel.querySelectorAll(".home-action-warning").length, 0, closeCase.name);
    assert.doesNotMatch(panel.textContent ?? "", /whole group|Close group|0 agents/, closeCase.name);
    window.close();
  }
});

test("delete confirmation and dirty refusal keep hostile path and detail inert", async () => {
  const window = new Window({ url: "http://localhost/" });
  const path = "/repo/<img src=x onerror=run>";
  const { app } = makeView(window, {
    prepare: async (request) => ({
      outcome: "prepared",
      action: "delete_checkout",
      workspace_id: request.workspace_id,
      expected: {
        workspace_label: "Child <img>",
        scope_workspace_ids: [request.workspace_id],
        agent_total: 1,
        interruption_counts: { working: 0, blocked: 1, unknown: 0 },
        checkout_path: path,
      },
    }),
    run: async () => ({
      outcome: "refused",
      reason: "checkout_has_changes",
      detail: "<svg onload=run>",
    }),
  });
  const childTrigger = requiredMatchingElement<HTMLButtonElement>(
    app,
    ".workspace-menu-trigger",
    (button) => button.getAttribute("aria-label") === "Actions for Child <img>",
  );
  childTrigger.click();
  const deleteAction = requiredMatchingElement<HTMLButtonElement>(
    app,
    '[role="menuitem"]',
    (button) => button.textContent === "Delete checkout",
  );
  deleteAction.click();
  await settle();

  const panel = requiredElement(app, ".home-action-panel");
  assert.equal(requiredElement(panel, ".checkout-path").textContent, path);
  assert.match(panel.textContent ?? "", /Git branch remains/);
  assert.equal(panel.querySelectorAll("img").length, 0);
  requiredElement<HTMLButtonElement>(panel, ".home-action-primary").click();
  await settle();
  assert.match(
    panel.textContent ?? "",
    /This folder has changes\. It was not removed\. Resolve the changes in the terminal, then try again\./,
  );
  assert.doesNotMatch(panel.textContent ?? "", /svg onload/);
  assert.equal(panel.querySelectorAll("svg").length, 0);
  window.close();
});

test("New worktree omits a blank branch and preserves an entered branch exactly", async () => {
  const window = new Window({ url: "http://localhost/" });
  const requests: RunWorkspaceActionRequest[] = [];
  const { app } = makeView(window, {
    run: async (request) => {
      requests.push(request);
      return { outcome: "succeeded" };
    },
  });
  const trigger = requiredMatchingElement<HTMLButtonElement>(
    app,
    ".workspace-menu-trigger",
    (button) => button.getAttribute("aria-label") === "Actions for Parent <script>",
  );

  const submitWorktree = async (branchValue: string): Promise<void> => {
    trigger.click();
    const action = requiredMatchingElement<HTMLButtonElement>(
      app,
      '[role="menuitem"]',
      (button) => !button.hidden && button.textContent === "New worktree",
    );
    action.click();
    const branch = requiredElement<HTMLInputElement>(app, 'input[name="branch"]');
    branch.value = branchValue;
    requiredElement<HTMLFormElement>(app, ".home-action-form")
      .dispatchEvent(new window.Event("submit", { bubbles: true, cancelable: true }));
    await settle();
    requiredElement<HTMLButtonElement>(app, ".home-action-panel button").click();
  };

  await submitWorktree("   ");
  await submitWorktree(" feature/<b> ");
  assert.equal(JSON.stringify(requests[0]), '{"action":"create_worktree","workspace_id":"opaque-parent"}');
  assert.equal(
    JSON.stringify(requests[1]),
    '{"action":"create_worktree","workspace_id":"opaque-parent","branch":" feature/<b> "}',
  );
  assert.equal(app.querySelectorAll("b").length, 0);
  window.close();
});

test("server refusal detail is displayed as inert text", async () => {
  const window = new Window({ url: "http://localhost/" });
  const detail = "Herdr said <img src=x onerror=run>";
  const { app } = makeView(window, {
    prepare: async () => ({ outcome: "refused", reason: "herdr_refused", detail }),
  });
  const trigger = requiredMatchingElement<HTMLButtonElement>(
    app,
    ".workspace-menu-trigger",
    (button) => button.getAttribute("aria-label") === "Actions for Parent <script>",
  );
  trigger.click();
  const closeWorkspace = requiredMatchingElement<HTMLButtonElement>(
    app,
    '[role="menuitem"]',
    (button) => button.textContent === "Close workspace",
  );
  closeWorkspace.click();
  await settle();

  assert.equal(requiredElement(app, ".home-action-detail").textContent, detail);
  assert.equal(app.querySelectorAll("img").length, 0);
  window.close();
});

test("multi-terminal Home keeps one row and opens the complete terminal picker", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app } = makeView(window, {}, terminalHome());
  const trigger = requiredElement<HTMLButtonElement>(app, ".workspace-terminal-trigger");

  assert.equal(trigger.localName, "button");
  assert.equal(trigger.getAttribute("aria-label"), "Choose a terminal in Terminal workspace, 2 terminals, 1 working, 1 idle");
  assert.equal(requiredElement(trigger, ".terminal-count-badge").textContent, "2");
  assert.deepEqual(
    Array.from(trigger.querySelectorAll(".workspace-summary-status"), (node) => node.textContent),
    ["1 working", "1 idle"],
  );
  assert.equal(app.querySelectorAll(".terminal-section, .terminal-section-disclosure").length, 0);
  assert.equal(app.querySelectorAll(".terminal-picker-body .terminal-row").length, 0);

  trigger.focus();
  window.scrollTo(0, 218);
  trigger.click();
  const layer = requiredElement(app, ".terminal-picker-layer");
  assert.equal(layer.hidden, false);
  assert.equal(requiredElement(app, ".terminal-picker-panel").getAttribute("role"), "dialog");
  assert.equal(requiredElement(app, ".terminal-picker-panel").getAttribute("aria-modal"), "true");
  assert.equal(requiredElement(app, ".terminal-picker-title").textContent, "Terminal workspace");
  assert.equal(requiredElement(app, ".terminal-picker-count").textContent, "2 terminals");
  assert.equal(window.document.activeElement === requiredElement(app, ".terminal-picker-close"), true);
  assert.deepEqual(
    Array.from(app.querySelectorAll(".terminal-picker-body .tab-heading h3"), (node) => node.textContent),
    ["Main", "Logs"],
  );
  assert.equal(app.querySelectorAll(".terminal-picker-body .terminal-row").length, 2);
  assert.deepEqual(
    Array.from(app.querySelectorAll(".terminal-picker-body .terminal-agent"), (node) => node.textContent),
    ["Terminal workspace · Builder", "Terminal workspace · Watcher"],
  );
  layer.dispatchEvent(new window.KeyboardEvent("keydown", { bubbles: true, key: "Tab", shiftKey: true }));
  assert.equal(
    (window.document.activeElement as HTMLElement | null)?.getAttribute("aria-label"),
    "Actions for Logs",
  );
  layer.dispatchEvent(new window.KeyboardEvent("keydown", { bubbles: true, key: "Tab" }));
  assert.equal(window.document.activeElement === requiredElement(app, ".terminal-picker-close"), true);

  requiredElement<HTMLButtonElement>(app, ".terminal-picker-close").click();
  assert.equal(layer.hidden, true);
  assert.equal(app.querySelectorAll(".terminal-picker-body .terminal-row").length, 0);
  assert.equal(window.document.activeElement === trigger, true);
  assert.equal(window.scrollY, 218);

  const filter = requiredElement<HTMLInputElement>(app, ".home-filter input");
  filter.value = "Logs";
  filter.dispatchEvent(new window.Event("input", { bubbles: true }));
  assert.equal(app.querySelectorAll(".workspace-terminal-trigger").length, 1);
  assert.equal(requiredElement(app, ".terminal-count-badge").textContent, "2");
  requiredElement<HTMLButtonElement>(app, ".workspace-terminal-trigger").click();
  assert.equal(app.querySelectorAll(".terminal-picker-body .terminal-row").length, 2);
  assert.deepEqual(
    Array.from(app.querySelectorAll(".terminal-picker-body .tab-heading h3"), (node) => node.textContent),
    ["Main", "Logs"],
  );
  assert.deepEqual(
    Array.from(app.querySelectorAll(".workspace-terminal-trigger .workspace-summary-status"), (node) => node.textContent),
    ["1 working", "1 idle"],
  );
  requiredElement<HTMLButtonElement>(app, ".terminal-picker-close").click();
  filter.value = "";
  filter.dispatchEvent(new window.Event("input", { bubbles: true }));
  assert.equal(app.querySelectorAll(".workspace-terminal-trigger").length, 1);
  window.close();
});

test("phone picker terminal actions use a contained sheet and preserve picker place", () => {
  const window = new Window({ url: "http://localhost/", innerWidth: 360, innerHeight: 740 });
  Object.defineProperty(window, "matchMedia", { value: () => ({ matches: true }) });
  const { app } = makeView(window, {}, terminalHome());
  requiredElement<HTMLButtonElement>(app, ".workspace-terminal-trigger").click();
  const pickerBody = requiredElement(app, ".terminal-picker-body");
  pickerBody.scrollTop = 73;
  const trigger = requiredMatchingElement<HTMLButtonElement>(
    app,
    ".terminal-menu .workspace-menu-trigger",
    (button) => button.getAttribute("aria-label") === "Actions for Logs",
  );

  trigger.click();
  const layer = requiredElement(app, ".terminal-action-sheet-layer");
  assert.equal(layer.hidden, false);
  assert.equal(requiredElement(app, ".terminal-action-sheet-panel").getAttribute("role"), "dialog");
  assert.equal(requiredElement(app, ".terminal-action-sheet-title").textContent, "Actions for Logs");
  assert.equal(trigger.getAttribute("aria-haspopup"), "dialog");
  assert.deepEqual(
    Array.from(app.querySelectorAll(".terminal-action-sheet-body [role='menuitem']"), (node) => node.textContent),
    ["New terminal", "Rename terminal", "Close terminal"],
  );
  assert.equal(requiredElement(app, ".terminal-picker-layer").hidden, false);
  assert.equal(pickerBody.scrollTop, 73);
  assert.equal((window.document.activeElement as HTMLElement | null)?.getAttribute("aria-label"), "Close terminal actions");
  layer.dispatchEvent(new window.KeyboardEvent("keydown", { bubbles: true, key: "Tab", shiftKey: true }));
  assert.equal(window.document.activeElement?.textContent, "Close terminal");
  layer.dispatchEvent(new window.KeyboardEvent("keydown", { bubbles: true, key: "Tab" }));
  assert.equal((window.document.activeElement as HTMLElement | null)?.getAttribute("aria-label"), "Close terminal actions");

  requiredElement<HTMLButtonElement>(app, ".terminal-action-sheet-close").click();
  assert.equal(layer.hidden, true);
  assert.equal((window.document.activeElement as HTMLElement | null)?.getAttribute("aria-label"), "Actions for Logs");
  assert.equal(pickerBody.scrollTop, 73);

  trigger.click();
  layer.click();
  assert.equal(layer.hidden, true);
  trigger.click();
  layer.dispatchEvent(new window.KeyboardEvent("keydown", { bubbles: true, key: "Escape" }));
  assert.equal(layer.hidden, true);
  assert.equal(requiredElement(app, ".terminal-picker-layer").hidden, false);
  assert.equal(pickerBody.scrollTop, 73);
  trigger.click();
  requiredMatchingElement<HTMLButtonElement>(
    app,
    ".terminal-action-sheet-body [role='menuitem']",
    (button) => button.textContent === "Rename terminal",
  ).click();
  assert.equal(layer.hidden, true);
  assert.equal(requiredElement(app, ".home-action-title").textContent, "Rename Logs");
  assert.equal(pickerBody.scrollTop, 73);
  window.close();
});

test("Home filters by a tab name only when its heading is displayed", () => {
  const window = new Window({ url: "http://localhost/" });
  const home = terminalHome();
  const workspace = home.workspaces[0];
  workspace.tabs[0].label = "Hidden tab name";
  workspace.tabs[0].terminals.push(workspace.tabs[1].terminals[0]);
  workspace.tabs[1].terminals = [];
  const { app } = makeView(window, {}, home);
  const filter = requiredElement<HTMLInputElement>(app, ".home-filter input");

  requiredElement<HTMLButtonElement>(app, ".workspace-terminal-trigger").click();
  assert.equal(app.querySelectorAll(".terminal-picker-body .tab-heading").length, 0);
  assert.equal(app.querySelectorAll(".terminal-picker-body .terminal-row").length, 2);
  requiredElement<HTMLButtonElement>(app, ".terminal-picker-close").click();
  filter.value = "Hidden tab name";
  filter.dispatchEvent(new window.Event("input", { bubbles: true }));
  assert.equal(app.querySelectorAll(".workspace-terminal-trigger").length, 0);
  assert.equal(requiredElement(app, ".home-no-matches strong").textContent, "No matches");

  filter.value = "Builder";
  filter.dispatchEvent(new window.Event("input", { bubbles: true }));
  assert.equal(app.querySelectorAll(".workspace-terminal-trigger").length, 1);
  assert.equal(requiredElement(app, ".workspace-terminal-trigger .terminal-name").textContent, "Terminal workspace");
  assert.equal(requiredElement(app, ".terminal-count-badge").textContent, "2");
  requiredElement<HTMLButtonElement>(app, ".workspace-terminal-trigger").click();
  assert.deepEqual(
    Array.from(app.querySelectorAll(".terminal-picker-body .terminal-name"), (node) => node.textContent),
    ["Build <script>", "Logs"],
  );
  assert.equal(app.querySelectorAll(".terminal-picker-body .tab-heading").length, 0);
  window.close();
});

test("terminal menu uses exact action order and split opens only the returned terminal", async () => {
  const window = new Window({ url: "http://localhost/" });
  let request: RunTerminalActionRequest | undefined;
  let opened: TerminalActionTarget | undefined;
  const returned = {
    workspace_id: "terminal-workspace",
    tab_id: "terminal-tab-main",
    pane_id: "pane:created",
    terminal_id: "terminal:created",
  };
  const { app } = makeView(window, {
    onOpenCreated: (target) => { opened = target; },
    runTerminal: async (value) => {
      request = value;
      return { outcome: "succeeded", terminal: returned };
    },
  }, terminalHome());
  requiredElement<HTMLButtonElement>(app, ".workspace-terminal-trigger").click();
  const trigger = requiredMatchingElement<HTMLButtonElement>(
    app,
    ".terminal-menu .workspace-menu-trigger",
    (button) => button.getAttribute("aria-label") === "Actions for Build <script>",
  );
  trigger.click();
  assert.equal(trigger.getAttribute("aria-haspopup"), "menu");
  assert.equal(requiredElement(app, ".terminal-action-sheet-layer").hidden, true);
  const items = Array.from(trigger.parentElement!.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'));
  assert.deepEqual(items.map((item) => item.textContent), ["New terminal", "Rename terminal", "Close terminal"]);
  items[0].click();
  assert.equal(requiredElement(app, ".home-action-title").textContent, "Split terminal");
  assert.equal(app.querySelectorAll("script").length, 0);
  const below = requiredMatchingElement<HTMLButtonElement>(app, ".home-action-buttons button", (button) => button.textContent === "Below");
  below.click();
  await settle();

  assert.equal(JSON.stringify(request), JSON.stringify({
    action: "split_terminal",
    workspace_id: "terminal-workspace",
    tab_id: "terminal-tab-main",
    pane_id: "pane:anchor",
    terminal_id: "terminal:anchor",
    direction: "down",
  }));
  assert.deepEqual(opened, returned);
  assert.equal((app.querySelector<HTMLElement>(".home-action-layer")?.hidden), true);
  window.close();
});

test("rename sends the manual pane name exactly and blank remains available to clear it", async () => {
  const window = new Window({ url: "http://localhost/" });
  let request: RunTerminalActionRequest | undefined;
  const { app } = makeView(window, {
    runTerminal: async (value) => {
      request = value;
      return { outcome: "succeeded" };
    },
  }, terminalHome());
  requiredElement<HTMLButtonElement>(app, ".workspace-terminal-trigger").click();
  const trigger = requiredMatchingElement<HTMLButtonElement>(
    app,
    ".terminal-menu .workspace-menu-trigger",
    (button) => button.getAttribute("aria-label") === "Actions for Build <script>",
  );
  trigger.click();
  requiredMatchingElement<HTMLButtonElement>(app, '[role="menuitem"]', (button) => button.textContent === "Rename terminal").click();
  const name = requiredElement<HTMLInputElement>(app, 'input[name="terminal_name"]');
  assert.equal(name.value, "Build <script>");
  assert.match(requiredElement(app, ".home-action-help").textContent ?? "", /blank to use the automatic name/);
  name.value = "  Exact <name>  ";
  requiredElement<HTMLFormElement>(app, ".home-action-form")
    .dispatchEvent(new window.Event("submit", { bubbles: true, cancelable: true }));
  await settle();
  assert.equal(JSON.stringify(request), JSON.stringify({
    action: "rename_terminal",
    workspace_id: "terminal-workspace",
    tab_id: "terminal-tab-main",
    pane_id: "pane:anchor",
    terminal_id: "terminal:anchor",
    name: "  Exact <name>  ",
  }));
  assert.equal(app.querySelectorAll("script").length, 0);
  window.close();
});

test("close terminal prepares fresh exact facts and names interruption and tab impact", async () => {
  const window = new Window({ url: "http://localhost/" });
  let prepareRequest: ({ action: "close_terminal" } & TerminalActionTarget) | undefined;
  let runRequest: RunTerminalActionRequest | undefined;
  const closeFacts = {
    target: {
      workspace_id: "terminal-workspace",
      tab_id: "terminal-tab-main",
      pane_id: "pane:anchor",
      terminal_id: "terminal:anchor",
    },
    workspace_label: "Terminal workspace",
    tab_label: "Main",
    terminal_title: "Build <script>",
    agent: { kind: "codex", name: "Builder", status: "working" } as const,
    closes_tab: true,
  };
  const { app } = makeView(window, {
    prepareTerminal: async (value) => {
      prepareRequest = value;
      return { outcome: "prepared", action: "close_terminal", expected: closeFacts };
    },
    runTerminal: async (value) => {
      runRequest = value;
      return { outcome: "unknown" };
    },
  }, terminalHome());
  requiredElement<HTMLButtonElement>(app, ".workspace-terminal-trigger").click();
  const trigger = requiredMatchingElement<HTMLButtonElement>(
    app,
    ".terminal-menu .workspace-menu-trigger",
    (button) => button.getAttribute("aria-label") === "Actions for Build <script>",
  );
  trigger.click();
  requiredMatchingElement<HTMLButtonElement>(app, '[role="menuitem"]', (button) => button.textContent === "Close terminal").click();
  await settle();
  const panel = requiredElement(app, ".home-action-panel");
  assert.equal(requiredElement(panel, ".home-action-title").textContent, "Close Build <script>?");
  assert.equal(
    requiredElement(panel, ".home-action-copy").textContent,
    "Closing this terminal stops what is running there. Builder is working. The Main tab will also close.",
  );
  assert.equal(JSON.stringify(prepareRequest), JSON.stringify({ action: "close_terminal", ...closeFacts.target }));
  requiredElement<HTMLButtonElement>(panel, ".home-action-primary").click();
  await settle();
  assert.equal(JSON.stringify(runRequest), JSON.stringify({ action: "close_terminal", expected: closeFacts }));
  assert.equal(requiredElement(panel, ".home-action-title").textContent, "Result unknown");
  assert.match(requiredElement(panel, ".home-action-copy").textContent ?? "", /Check Home before trying to close this terminal again/);
  window.close();
});

test("successful terminal closes keep picker focus contained and restore the remaining row", async () => {
  const window = new Window({ url: "http://localhost/" });
  const thirdTerminal = {
    actions: ["split_terminal", "rename_terminal", "close_terminal"] as const,
    pane_id: "pane:third",
    terminal_id: "terminal:third",
    title: "Third",
  };
  const initial = terminalHome();
  initial.workspaces[0].agent_counts = { idle: 1, unknown: 1, working: 1 };
  initial.workspaces[0].tabs[1].terminals.push({ ...thirdTerminal, actions: [...thirdTerminal.actions] });
  const { app, view } = makeView(window, {
    prepareTerminal: async ({ action: _, ...target }) => ({
      action: "close_terminal",
      expected: {
        closes_tab: target.pane_id === "pane:anchor",
        tab_label: target.pane_id === "pane:anchor" ? "Main" : "Logs",
        target,
        terminal_title: target.pane_id === "pane:anchor" ? "Build <script>" : "Logs",
        workspace_label: "Terminal workspace",
      },
      outcome: "prepared",
    }),
    runTerminal: async () => ({ outcome: "succeeded" }),
  }, initial);

  async function closeTerminal(title: string): Promise<void> {
    const menu = requiredMatchingElement<HTMLButtonElement>(
      app,
      ".terminal-menu .workspace-menu-trigger",
      (button) => button.getAttribute("aria-label") === `Actions for ${title}`,
    );
    menu.click();
    requiredMatchingElement<HTMLButtonElement>(
      app,
      '[role="menuitem"]',
      (button) => button.textContent === "Close terminal",
    ).click();
    await settle();
    requiredElement<HTMLButtonElement>(app, ".home-action-panel .home-action-primary").click();
    await settle();
    assert.equal(requiredElement(app, ".home-action-title").textContent, "Terminal closed");
  }

  requiredElement<HTMLButtonElement>(app, ".workspace-terminal-trigger").click();
  await closeTerminal("Build <script>");

  const afterFirst = terminalHome();
  afterFirst.workspaces[0].agent_counts = { idle: 1, unknown: 1 };
  afterFirst.workspaces[0].tabs[0].terminals = [];
  afterFirst.workspaces[0].tabs[1].terminals.push({ ...thirdTerminal, actions: [...thirdTerminal.actions] });
  view.render({ actionsAvailable: true, mode: "all", reachability: "current", state: state(afterFirst) });
  requiredElement<HTMLButtonElement>(app, ".home-action-panel button").click();
  assert.equal((window.document.activeElement as HTMLElement | null)?.className, "terminal-picker-close");
  assert.equal(requiredElement(app, ".terminal-picker-layer").hidden, false);
  assert.deepEqual(
    Array.from(app.querySelectorAll(".terminal-picker-body .terminal-name"), (node) => node.textContent),
    ["Logs", "Third"],
  );

  await closeTerminal("Logs");
  window.scrollTo(0, 371);
  const afterSecond = terminalHome();
  afterSecond.workspaces[0].agent_counts = { unknown: 1 };
  afterSecond.workspaces[0].tabs = [{
    ...afterSecond.workspaces[0].tabs[1],
    terminals: [{ ...thirdTerminal, actions: [...thirdTerminal.actions] }],
  }];
  view.render({ actionsAvailable: true, mode: "all", reachability: "current", state: state(afterSecond) });
  assert.equal(requiredElement(app, ".terminal-picker-layer").hidden, true);
  assert.equal(window.scrollY, 0);
  requiredElement<HTMLButtonElement>(app, ".home-action-panel button").click();
  assert.equal((window.document.activeElement as HTMLElement | null)?.dataset.paneKey, "pane:third");
  window.close();
});

test("one-terminal rows keep terminal and workspace actions in one compact menu", () => {
  const window = new Window({ url: "http://localhost/" });
  const home = terminalHome();
  home.workspaces[0].tabs = [home.workspaces[0].tabs[0]];
  const { app } = makeView(window, {}, home);
  assert.equal(app.querySelectorAll(".workspace-terminal-trigger").length, 0);
  assert.equal(app.querySelectorAll(".terminal-row").length, 1);
  assert.equal(requiredElement(app, ".terminal-picker-layer").hidden, true);
  const trigger = requiredElement<HTMLButtonElement>(app, ".terminal-menu .workspace-menu-trigger");
  assert.equal(trigger.getAttribute("aria-label"), "Actions for Terminal workspace");
  trigger.click();
  assert.deepEqual(
    Array.from(app.querySelectorAll('[role="menuitem"]'), (node) => node.textContent),
    ["New terminal", "Rename terminal", "Close terminal", "Close workspace"],
  );
  window.close();
});

test("literal Herdr 0.8.1 remains openable while Home infers no management actions", () => {
  const window = new Window({ url: "http://localhost/" });
  let opened = 0;
  const home = actionHome();
  home.workspaces[0].actions = [];
  home.workspaces[1].actions = [];
  home.workspaces[2].actions = [];
  home.workspaces[0].worktrees![0].actions = [];
  const app = window.document.createElement("main");
  window.document.body.append(app);
  const view = new HomeView(app, {
    isHomeActive: () => true,
    onFocusPane: () => undefined,
    onOpen: () => { opened++; },
    onOpenCreated: () => undefined,
    onShowAll: () => undefined,
    onShowBlocked: () => undefined,
    prepareWorkspaceAction: async () => ({ outcome: "refused", reason: "not_applicable" }),
    prepareTerminalAction: async () => ({ outcome: "refused", reason: "not_applicable" }),
    runWorkspaceAction: async () => ({ outcome: "succeeded" }),
    runTerminalAction: async () => ({ outcome: "succeeded" }),
  });
  const unverified = state(home);
  unverified.herdr_version = "0.8.1";
  view.render({ actionsAvailable: true, mode: "all", reachability: "current", state: unverified });

  assert.equal(app.querySelectorAll(".workspace-menu-trigger").length, 0);
  assert.equal(app.querySelectorAll(".new-space-action").length, 1);
  requiredElement<HTMLButtonElement>(app, '[data-pane-key="parent-pane"]').click();
  assert.equal(opened, 1);
  assert.equal(app.querySelector(".herdr-version")?.textContent, "Herdr 0.8.1");
  window.close();
});

test("collapsed groups with zero or multiple parent terminals keep their group actions available", () => {
  for (const terminalCount of [0, 2]) {
    const window = new Window({ url: "http://localhost/" });
    const home = actionHome();
    const terminals = home.workspaces[0].tabs[0].terminals;
    home.workspaces[0].tabs[0].terminals = terminalCount === 0
      ? []
      : [
          terminals[0],
          { actions: [], pane_id: "second-parent-pane", terminal_id: "second-parent-terminal", title: "Second terminal" },
        ];
    const { app } = makeView(window, {}, home);
    const disclosure = requiredElement<HTMLButtonElement>(app, ".workspace-set-disclosure");
    disclosure.click();
    assert.equal(disclosure.getAttribute("aria-expanded"), "false");
    assert.equal(requiredElement(app, ".workspace-set-body").hidden, true);
    const header = requiredElement(app, ".workspace-set-header");
    if (terminalCount === 2) {
      const pickerTrigger = requiredElement<HTMLButtonElement>(header, ".workspace-terminal-trigger");
      assert.equal(requiredElement(pickerTrigger, ".terminal-count-badge").textContent, "2");
      pickerTrigger.click();
      assert.equal(requiredElement(app, ".terminal-picker-title").textContent, "Parent <script>");
      assert.equal(app.querySelectorAll(".terminal-picker-body .terminal-row").length, 2);
      requiredElement<HTMLButtonElement>(app, ".terminal-picker-close").click();
    }
    const trigger = requiredElement<HTMLButtonElement>(header, ".workspace-menu-trigger");
    assert.equal(trigger.getAttribute("aria-label"), "Actions for Parent <script>");
    trigger.click();
    assert.deepEqual(
      Array.from(header.querySelectorAll('[role="menuitem"]'), (node) => node.textContent),
      ["New worktree", "Close workspace"],
    );
    window.close();
  }
});

test("Home action controls retain phone-sized touch targets in the Home-owned stylesheet", () => {
  const styles = readFileSync(new URL("./style.css", import.meta.url), "utf8");
  assert.match(styles, /\.workspace-menu-trigger\s*\{[^}]*width:\s*44px;[^}]*min-width:\s*44px;[^}]*min-height:\s*44px;/s);
  assert.match(styles, /\.home-action-buttons button\s*\{[^}]*min-width:\s*44px;[^}]*min-height:\s*44px;/s);
  assert.match(styles, /\.workspace-set\s*\{[^}]*overflow:\s*visible;/s);
  assert.match(styles, /\.terminal-picker-close\s*\{[^}]*width:\s*44px;[^}]*min-width:\s*44px;[^}]*min-height:\s*44px;/s);
  assert.match(styles, /\.terminal-picker-body\s*\{[^}]*overflow-y:\s*auto;[^}]*overscroll-behavior:\s*contain;/s);
  assert.match(styles, /@media \(max-width: 520px\)[\s\S]*\.terminal-picker-layer\s*\{[^}]*place-items:\s*end center;/);
  assert.match(styles, /@media \(max-width: 520px\)[\s\S]*\.terminal-picker-header\s*\{[^}]*safe-area-inset-right[^}]*safe-area-inset-left/);
  assert.match(styles, /@media \(max-width: 520px\)[\s\S]*\.home-filter\s*\{[^}]*grid-column:\s*1 \/ -1;/);
});

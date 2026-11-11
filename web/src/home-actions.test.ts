import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { Window } from "happy-dom";

import { HomeView } from "./home-view.ts";
import { emptyHomeState, suggestedFolders, type Herdr, type Home, type HomeState } from "./home-model.ts";
import type {
  PrepareWorkspaceActionRequest,
  PreparedWorkspaceActionResponse,
  RunWorkspaceActionRequest,
  RunWorkspaceActionResponse,
} from "./workspace-actions.ts";
import type {
  PrepareTerminalCloseRequest,
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
  return { can_open_terminals: true, connection: "live", gap: 0, has_home: true, herdr_version: "0.8.0", home, last_known: false };
}

interface ViewOverrides {
  herdr?: Herdr;
  isHomeActive?: () => boolean;
  onOpenCreated?: (target: TerminalActionTarget) => void;
  prepare?: (request: PrepareWorkspaceActionRequest) => Promise<PreparedWorkspaceActionResponse>;
  prepareTerminal?: (request: PrepareTerminalCloseRequest) => Promise<PreparedTerminalActionResponse>;
  run?: (request: RunWorkspaceActionRequest) => Promise<RunWorkspaceActionResponse>;
  runTerminal?: (request: RunTerminalActionRequest) => Promise<RunTerminalActionResponse>;
  suggestedFolders?: string[];
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
  const herdr = overrides.herdr ?? "tui";
  const sides = { core: emptyHomeState(), tui: emptyHomeState(), [herdr]: state(home) };
  view.render({
    actionsAvailable: true, herdr, mode: "all", reachability: "current", state: state(home),
    suggestedFolders: overrides.suggestedFolders ?? suggestedFolders(sides),
  });
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

  assert.equal(JSON.stringify(submitted), '{"herdr":"tui","action":"create_space","working_directory":"~/free $HOME * <b>"}');
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
      interruption_counts: { working: 2, blocked: 1, error: 0, unknown: 3 },
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
  assert.equal(JSON.stringify(prepareRequest), '{"herdr":"tui","action":"close_group","workspace_id":"opaque-parent"}');
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
          interruption_counts: { working: 0, blocked: 0, error: 0, unknown: 0 },
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

test("delete confirmation states the agent total only when nonzero", async () => {
  const cases = [{
    agent_total: 0,
    expectedCopy: "This deletes the checkout and closes this workspace. Its terminals will end, and unsaved work can be lost. The Git branch remains.",
  }, {
    agent_total: 1,
    expectedCopy: "This deletes the checkout and closes this workspace and 1 agent. Its terminals will end, and unsaved work can be lost. The Git branch remains.",
  }, {
    agent_total: 3,
    expectedCopy: "This deletes the checkout and closes this workspace and 3 agents. Its terminals will end, and unsaved work can be lost. The Git branch remains.",
  }];

  for (const deleteCase of cases) {
    const window = new Window({ url: "http://localhost/" });
    const { app } = makeView(window, {
      prepare: async (request) => ({
        outcome: "prepared",
        action: "delete_checkout",
        workspace_id: request.workspace_id,
        expected: {
          workspace_label: "Child <img>",
          scope_workspace_ids: [request.workspace_id],
          agent_total: deleteCase.agent_total,
          interruption_counts: { working: 0, blocked: 0, error: 0, unknown: 0 },
          checkout_path: "/repo/child",
        },
      }),
    });
    const trigger = requiredMatchingElement<HTMLButtonElement>(
      app,
      ".workspace-menu-trigger",
      (button) => button.getAttribute("aria-label") === "Actions for Child <img>",
    );
    trigger.click();
    const deleteAction = requiredMatchingElement<HTMLButtonElement>(
      app,
      '[role="menuitem"]',
      (button) => button.textContent === "Delete checkout",
    );
    deleteAction.click();
    await settle();

    const panel = requiredElement(app, ".home-action-panel");
    const name = `${deleteCase.agent_total} agents`;
    assert.equal(requiredElement(panel, ".home-action-copy").textContent, deleteCase.expectedCopy, name);
    assert.equal(panel.querySelectorAll(".home-action-warning").length, 0, name);
    assert.doesNotMatch(panel.textContent ?? "", /\b0 agents?\b/, name);
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
        interruption_counts: { working: 0, blocked: 1, error: 0, unknown: 0 },
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
  assert.equal(JSON.stringify(requests[0]), '{"herdr":"tui","action":"create_worktree","workspace_id":"opaque-parent"}');
  assert.equal(
    JSON.stringify(requests[1]),
    '{"herdr":"tui","action":"create_worktree","workspace_id":"opaque-parent","branch":" feature/<b> "}',
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
    herdr: "tui",
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
    herdr: "tui",
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
  assert.equal(JSON.stringify(prepareRequest), JSON.stringify({ herdr: "tui", action: "close_terminal", ...closeFacts.target }));
  requiredElement<HTMLButtonElement>(panel, ".home-action-primary").click();
  await settle();
  assert.equal(JSON.stringify(runRequest), JSON.stringify({ herdr: "tui", action: "close_terminal", expected: closeFacts }));
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
  view.render({ actionsAvailable: true, herdr: "tui", mode: "all", reachability: "current", state: state(afterFirst), suggestedFolders: [] });
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
  view.render({ actionsAvailable: true, herdr: "tui", mode: "all", reachability: "current", state: state(afterSecond), suggestedFolders: [] });
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
  view.render({ actionsAvailable: true, herdr: "tui", mode: "all", reachability: "current", state: unverified, suggestedFolders: [] });

  assert.equal(app.querySelectorAll(".workspace-menu-trigger").length, 0);
  assert.equal(app.querySelectorAll(".new-space-action").length, 1);
  requiredElement<HTMLButtonElement>(app, '[data-pane-key="parent-pane"]').click();
  assert.equal(opened, 1);
  assert.equal(app.querySelector(".herdr-version")?.textContent, "Herdr TUI 0.8.1");
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

function coreHome(): Home {
  return {
    blocked_count: 1,
    error_count: 1,
    working_count: 1,
    workspaces: [{
      actions: ["close_workspace"],
      agent_counts: { blocked: 1, error: 1, working: 1 },
      id: "sp-core",
      label: "Core <img src=x>",
      number: 1,
      tabs: [{
        current: false,
        id: "tb-main",
        label: "main",
        number: 1,
        terminals: [{
          actions: ["close_terminal"],
          agent: { kind: "claude", name: "Claude", status: "working" },
          pane_id: "t3",
          terminal_id: "core:t3",
          title: "Claude",
        }, {
          actions: ["close_terminal"],
          agent: { kind: "claude", name: "Worker <b>", status: "blocked" },
          pane_id: "t9",
          terminal_id: "core:t9",
          title: "Worker <b>",
        }],
      }, {
        current: false,
        id: "tb-two",
        label: "2",
        number: 2,
        terminals: [{
          actions: ["close_terminal"],
          agent: { kind: "codex", name: "Codex", status: "error" },
          pane_id: "t5",
          terminal_id: "core:t5",
          title: "Codex",
        }],
      }],
    }, {
      actions: [],
      id: "other",
      label: "Other terminals",
      number: 2,
      tabs: [{
        current: false,
        id: "other",
        label: "Other terminals",
        number: 1,
        terminals: [{
          actions: ["close_terminal"],
          agent: { kind: "claude", name: "Outside <i>", status: "working" },
          pane_id: "t8",
          terminal_id: "core:t8",
          title: "Outside <i>",
        }],
      }],
    }],
  } as Home;
}

test("Herdr Core closes a space in space words and sends the request to core", async () => {
  const window = new Window({ url: "http://localhost/" });
  let prepareRequest: PrepareWorkspaceActionRequest | undefined;
  let runRequest: RunWorkspaceActionRequest | undefined;
  const expected = {
    workspace_label: "Core <img src=x>",
    scope_workspace_ids: ["sp-core"],
    agent_total: 3,
    interruption_counts: { working: 1, blocked: 1, error: 1, unknown: 0 },
  };
  const { app } = makeView(window, {
    herdr: "core",
    prepare: async (request) => {
      prepareRequest = request;
      return { outcome: "prepared", action: "close_workspace", workspace_id: "sp-core", expected };
    },
    run: async (request) => {
      runRequest = request;
      return { outcome: "succeeded" };
    },
  }, coreHome());
  assert.equal(app.querySelectorAll(".new-space-action").length, 0);
  const spaceTrigger = requiredMatchingElement<HTMLButtonElement>(
    app, ".workspace-menu-trigger", (node) => node.getAttribute("aria-label") === "Actions for Core <img src=x>",
  );
  spaceTrigger.click();
  const items = Array.from(spaceTrigger.parentElement!.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'));
  assert.deepEqual(items.map((item) => item.textContent), ["Close space"]);
  items[0].click();
  await settle();

  const panel = requiredElement(app, ".home-action-panel");
  assert.equal(JSON.stringify(prepareRequest), '{"herdr":"core","action":"close_workspace","workspace_id":"sp-core"}');
  assert.equal(requiredElement(panel, ".home-action-title").textContent, "Close Core <img src=x>?");
  assert.equal(
    requiredElement(panel, ".home-action-copy").textContent,
    "This closes this space. 3 agents will be affected. Its terminals will end, and unsaved work can be lost. The folder and branch remain.",
  );
  assert.equal(requiredElement(panel, ".home-action-warning").textContent, "Agents may be interrupted: 1 working, 1 blocked, 1 error.");
  assert.equal(panel.querySelectorAll("img").length, 0);
  assert.equal(requiredElement<HTMLButtonElement>(panel, ".home-action-primary").textContent, "Close space");

  requiredElement<HTMLButtonElement>(panel, ".home-action-primary").click();
  await settle();
  assert.equal(JSON.stringify(runRequest), JSON.stringify({ herdr: "core", action: "close_workspace", workspace_id: "sp-core", expected }));
  assert.equal(requiredElement(panel, ".home-action-title").textContent, "Space closed");
  assert.equal(requiredElement(panel, ".home-action-copy").textContent, "Its folder and branch remain. Home will update when it is ready.");
  window.close();
});

test("Herdr Core refusals and checks say space", async () => {
  const window = new Window({ url: "http://localhost/" });
  const refusals: Array<"not_found" | "stale" | "busy"> = ["not_found", "stale", "busy"];
  const { app } = makeView(window, {
    herdr: "core",
    prepare: async () => {
      const reason = refusals.shift()!;
      return { outcome: "refused", reason };
    },
  }, coreHome());
  const messages: string[] = [];
  for (let index = 0; index < 3; index++) {
    const trigger = requiredMatchingElement<HTMLButtonElement>(
      app, ".workspace-menu-trigger", (node) => node.getAttribute("aria-label") === "Actions for Core <img src=x>",
    );
    trigger.click();
    requiredElement<HTMLButtonElement>(trigger.parentElement!, '[role="menuitem"]').click();
    await settle();
    const panel = requiredElement(app, ".home-action-panel");
    messages.push(requiredElement(panel, ".home-action-copy").textContent ?? "");
    requiredElement<HTMLButtonElement>(panel, "button").click();
  }
  assert.deepEqual(messages, [
    "This space is no longer here.",
    "This space changed. Open the action again to check the latest details.",
    "Another space action is running. Try again when it finishes.",
  ]);
  window.close();
});

test("Herdr Core Close terminal counts the terminals it started and Other terminals has no group menu", async () => {
  const window = new Window({ url: "http://localhost/" });
  let prepareRequest: PrepareTerminalCloseRequest | undefined;
  let runRequest: RunTerminalActionRequest | undefined;
  const facts = {
    target: { workspace_id: "other", tab_id: "other", pane_id: "t8", terminal_id: "core:t8" },
    workspace_label: "Other terminals",
    tab_label: "Other terminals",
    terminal_title: "Outside <i>",
    agent: { kind: "claude", name: "Outside <i>", status: "working" as const },
    closes_tab: false,
    closes_workers: 2,
  };
  const { app } = makeView(window, {
    herdr: "core",
    prepareTerminal: async (request) => {
      prepareRequest = request;
      return { outcome: "prepared", action: "close_terminal", expected: facts };
    },
    runTerminal: async (request) => {
      runRequest = request;
      return { outcome: "succeeded" };
    },
  }, coreHome());
  assert.equal(
    Array.from(app.querySelectorAll(".workspace-menu-trigger"), (node) => node.getAttribute("aria-label"))
      .includes("Actions for Other terminals"),
    false,
  );
  const trigger = requiredMatchingElement<HTMLButtonElement>(
    app, ".workspace-menu-trigger", (node) => node.getAttribute("aria-label") === "Actions for Outside <i>",
  );
  trigger.click();
  const items = Array.from(trigger.parentElement!.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'));
  assert.deepEqual(items.map((item) => item.textContent), ["Close terminal"]);
  items[0].click();
  await settle();

  const panel = requiredElement(app, ".home-action-panel");
  assert.equal(JSON.stringify(prepareRequest), JSON.stringify({ herdr: "core", action: "close_terminal", ...facts.target }));
  assert.equal(requiredElement(panel, ".home-action-title").textContent, "Close Outside <i>?");
  assert.equal(
    requiredElement(panel, ".home-action-copy").textContent,
    "Closing this terminal stops what is running there. Outside <i> is working. 2 terminals it started will also close.",
  );
  assert.equal(panel.querySelectorAll("i").length, 0);
  requiredElement<HTMLButtonElement>(panel, ".home-action-primary").click();
  await settle();
  assert.equal(JSON.stringify(runRequest), JSON.stringify({ herdr: "core", action: "close_terminal", expected: facts }));
  assert.equal(requiredElement(panel, ".home-action-title").textContent, "Terminal closed");
  window.close();
});

test("Herdr Core Close terminal on a space's last terminal confirms Close space", async () => {
  const window = new Window({ url: "http://localhost/" });
  const space = {
    workspace_label: "Core <img src=x>",
    scope_workspace_ids: ["sp-core"],
    agent_total: 1,
    interruption_counts: { working: 0, blocked: 0, error: 1, unknown: 0 },
  };
  const { app } = makeView(window, {
    herdr: "core",
    prepareTerminal: async (request) => ({
      outcome: "prepared",
      action: "close_terminal",
      expected: {
        target: { workspace_id: request.workspace_id, tab_id: request.tab_id, pane_id: request.pane_id, terminal_id: request.terminal_id },
        workspace_label: "Core <img src=x>",
        tab_label: "2",
        terminal_title: "Codex",
        closes_tab: false,
        workspace_action: "close_workspace",
        workspace_expected: space,
      },
    }),
    runTerminal: async () => ({ outcome: "succeeded" }),
  }, coreHome());
  requiredMatchingElement<HTMLButtonElement>(
    app, ".workspace-menu-trigger", (node) => node.getAttribute("aria-label") === "Actions for Core <img src=x>",
  ).click();
  // Codex is alone in tab 2, so that tab's row has its menu, with Close terminal.
  const codexTrigger = requiredMatchingElement<HTMLButtonElement>(
    app, ".workspace-menu-trigger", (node) => node.getAttribute("aria-label") === "Actions for 2",
  );
  codexTrigger.click();
  requiredMatchingElement<HTMLButtonElement>(app, '[role="menuitem"]', (node) => node.textContent === "Close terminal").click();
  await settle();
  const panel = requiredElement(app, ".home-action-panel");
  assert.equal(requiredElement(panel, ".home-action-title").textContent, "Close Core <img src=x>?");
  assert.equal(
    requiredElement(panel, ".home-action-copy").textContent,
    "This closes this space. 1 agent will be affected. Its terminals will end, and unsaved work can be lost. The folder and branch remain.",
  );
  assert.equal(requiredElement(panel, ".home-action-warning").textContent, "Agents may be interrupted: 1 error.");
  assert.equal(requiredElement<HTMLButtonElement>(panel, ".home-action-primary").textContent, "Close space");
  requiredElement<HTMLButtonElement>(panel, ".home-action-primary").click();
  await settle();
  assert.equal(requiredElement(panel, ".home-action-title").textContent, "Space closed");
  window.close();
});

test("a confirmed Close terminal whose target is gone names what the person confirmed", async () => {
  const terminalFacts = {
    target: { workspace_id: "other", tab_id: "other", pane_id: "t8", terminal_id: "core:t8" },
    workspace_label: "Other terminals",
    tab_label: "Other terminals",
    terminal_title: "Outside <i>",
    closes_tab: false,
  };
  const spaceFacts = {
    ...terminalFacts,
    workspace_action: "close_workspace" as const,
    workspace_expected: {
      workspace_label: "Other terminals",
      scope_workspace_ids: ["other"],
      agent_total: 0,
      interruption_counts: { working: 0, blocked: 0, error: 0, unknown: 0 },
    },
  };
  const tuiFacts = {
    target: { workspace_id: "terminal-workspace", tab_id: "terminal-tab-main", pane_id: "pane:anchor", terminal_id: "terminal:anchor" },
    workspace_label: "Terminal workspace",
    tab_label: "Main",
    terminal_title: "Build <script>",
    closes_tab: false,
  };
  const cases = [
    { herdr: "core" as const, facts: terminalFacts, home: coreHome(), menu: "Actions for Outside <i>" },
    { herdr: "core" as const, facts: spaceFacts, home: coreHome(), menu: "Actions for Outside <i>" },
    { herdr: "tui" as const, facts: tuiFacts, home: terminalHome(), menu: "Actions for Build <script>" },
  ];
  const messages: string[] = [];
  for (const { herdr, facts, home, menu } of cases) {
    const window = new Window({ url: "http://localhost/" });
    const { app } = makeView(window, {
      herdr,
      prepareTerminal: async () => ({ outcome: "prepared", action: "close_terminal", expected: facts }),
      runTerminal: async () => ({ outcome: "refused", reason: "not_found" }),
    }, home);
    if (herdr === "tui") requiredElement<HTMLButtonElement>(app, ".workspace-terminal-trigger").click();
    requiredMatchingElement<HTMLButtonElement>(app, ".workspace-menu-trigger", (node) => node.getAttribute("aria-label") === menu).click();
    requiredMatchingElement<HTMLButtonElement>(app, '[role="menuitem"]', (node) => node.textContent === "Close terminal").click();
    await settle();
    const panel = requiredElement(app, ".home-action-panel");
    requiredElement<HTMLButtonElement>(panel, ".home-action-primary").click();
    await settle();
    messages.push(requiredElement(panel, ".home-action-copy").textContent ?? "");
    window.close();
  }
  assert.deepEqual(messages, [
    "This terminal is no longer here.",
    "This space is no longer here.",
    "This workspace is no longer here.",
  ]);
});

test("opening Close terminal on a terminal that is gone names the terminal on Herdr Core only", async () => {
  const cases = [
    { herdr: "core" as const, home: coreHome(), menu: "Actions for Outside <i>" },
    { herdr: "tui" as const, home: terminalHome(), menu: "Actions for Build <script>" },
  ];
  const messages: string[] = [];
  for (const { herdr, home, menu } of cases) {
    const window = new Window({ url: "http://localhost/" });
    const { app } = makeView(window, {
      herdr,
      prepareTerminal: async () => ({ outcome: "refused", reason: "not_found" }),
    }, home);
    if (herdr === "tui") requiredElement<HTMLButtonElement>(app, ".workspace-terminal-trigger").click();
    requiredMatchingElement<HTMLButtonElement>(app, ".workspace-menu-trigger", (node) => node.getAttribute("aria-label") === menu).click();
    requiredMatchingElement<HTMLButtonElement>(app, '[role="menuitem"]', (node) => node.textContent === "Close terminal").click();
    await settle();
    messages.push(requiredElement(app, ".home-action-copy").textContent ?? "");
    window.close();
  }
  assert.deepEqual(messages, ["This terminal is no longer here.", "This workspace is no longer here."]);
});

// Home on Herdr Core while the Herdr app window is open.
function coreAppHome(): Home {
  const home = coreHome();
  home.actions = ["create_space"];
  const space = home.workspaces[0];
  space.actions = ["rename_space", "close_workspace"];
  space.manual_name = "Core <img src=x>";
  space.tabs[0].terminals[0].actions = ["split_terminal", "close_terminal"];
  space.tabs[1].terminals[0].actions = ["split_terminal", "close_terminal"];
  return home;
}

test("Herdr Core shows New space only while the app window offers it, with both Herdrs' folders", async () => {
  const window = new Window({ url: "http://localhost/" });
  let request: RunWorkspaceActionRequest | undefined;
  const { app } = makeView(window, {
    herdr: "core",
    run: async (value) => {
      request = value;
      return { outcome: "succeeded" };
    },
    suggestedFolders: ["/repo/<tui>", "/repo/core"],
  }, coreAppHome());
  requiredElement<HTMLButtonElement>(app, ".new-space-action").click();
  const panel = requiredElement(app, ".home-action-panel");
  assert.equal(requiredElement(panel, ".home-action-copy").textContent, "Add a space for an existing folder.");
  assert.deepEqual(
    Array.from(panel.querySelectorAll(".checkout-suggestion"), (node) => node.textContent),
    ["/repo/<tui>", "/repo/core"],
  );
  requiredElement<HTMLInputElement>(panel, 'input[name="working_directory"]').value = "~/repo";
  requiredElement<HTMLInputElement>(panel, 'input[name="label"]').value = "api <b>";
  requiredElement<HTMLFormElement>(panel, ".home-action-form")
    .dispatchEvent(new window.Event("submit", { bubbles: true, cancelable: true }));
  await settle();
  assert.equal(JSON.stringify(request), '{"herdr":"core","action":"create_space","working_directory":"~/repo","label":"api <b>"}');
  assert.equal(requiredElement(panel, ".home-action-title").textContent, "Space created");
  window.close();
});

test("Herdr Core renames a space from its menu, starting from its own name", async () => {
  const window = new Window({ url: "http://localhost/" });
  let request: RunWorkspaceActionRequest | undefined;
  const { app } = makeView(window, {
    herdr: "core",
    run: async (value) => {
      request = value;
      return { outcome: "succeeded" };
    },
  }, coreAppHome());
  const trigger = requiredMatchingElement<HTMLButtonElement>(
    app, ".workspace-menu-trigger", (node) => node.getAttribute("aria-label") === "Actions for Core <img src=x>",
  );
  trigger.click();
  const items = Array.from(trigger.parentElement!.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'));
  assert.deepEqual(items.map((item) => item.textContent), ["Rename space", "Close space"]);
  items[0].click();

  const panel = requiredElement(app, ".home-action-panel");
  assert.equal(requiredElement(panel, ".home-action-title").textContent, "Rename Core <img src=x>");
  assert.equal(panel.querySelectorAll("img").length, 0);
  assert.equal(requiredElement(panel, ".home-action-help").textContent, "Leave the name blank to use the automatic name.");
  assert.equal(requiredElement(panel, ".home-action-primary").textContent, "Rename space");
  const name = requiredElement<HTMLInputElement>(panel, 'input[name="space_name"]');
  assert.equal(name.value, "Core <img src=x>");
  name.value = "";
  requiredElement<HTMLFormElement>(panel, ".home-action-form")
    .dispatchEvent(new window.Event("submit", { bubbles: true, cancelable: true }));
  await settle();
  assert.equal(JSON.stringify(request), '{"herdr":"core","action":"rename_space","workspace_id":"sp-core","name":""}');
  assert.equal(requiredElement(panel, ".home-action-title").textContent, "Space renamed");
  assert.equal(requiredElement(panel, ".home-action-copy").textContent, "The new name is ready on Home.");
  window.close();
});

test("Herdr Core splits only a terminal with a pane and opens the new terminal on core", async () => {
  const window = new Window({ url: "http://localhost/" });
  let request: RunTerminalActionRequest | undefined;
  let opened: TerminalActionTarget | undefined;
  const created = { workspace_id: "sp-core", tab_id: "tb-main", pane_id: "t20", terminal_id: "core:t20" };
  const { app } = makeView(window, {
    herdr: "core",
    onOpenCreated: (target) => { opened = target; },
    runTerminal: async (value) => {
      request = value;
      return { outcome: "succeeded", terminal: created };
    },
  }, coreAppHome());
  requiredElement<HTMLButtonElement>(app, ".workspace-terminal-trigger").click();
  const menuItems = (title: string): string[] => {
    const trigger = requiredMatchingElement<HTMLButtonElement>(
      app, ".terminal-menu .workspace-menu-trigger", (button) => button.getAttribute("aria-label") === `Actions for ${title}`,
    );
    return Array.from(trigger.parentElement!.querySelectorAll('[role="menuitem"]'), (item) => item.textContent ?? "");
  };
  // A worker has no pane in the app window.
  assert.deepEqual(menuItems("Worker <b>"), ["Close terminal"]);
  assert.deepEqual(menuItems("Claude"), ["New terminal", "Close terminal"]);
  const trigger = requiredMatchingElement<HTMLButtonElement>(
    app, ".terminal-menu .workspace-menu-trigger", (button) => button.getAttribute("aria-label") === "Actions for Claude",
  );
  trigger.click();
  requiredElement<HTMLButtonElement>(trigger.parentElement!, '[role="menuitem"]').click();
  assert.equal(requiredElement(app, ".home-action-title").textContent, "Split terminal");
  requiredMatchingElement<HTMLButtonElement>(app, ".home-action-buttons button", (button) => button.textContent === "Beside").click();
  await settle();
  assert.equal(JSON.stringify(request), JSON.stringify({
    herdr: "core", action: "split_terminal", workspace_id: "sp-core", tab_id: "tb-main", pane_id: "t3", terminal_id: "core:t3", direction: "right",
  }));
  assert.deepEqual(opened, created);
  window.close();
});

test("Herdr Core says when the app window is closed, a folder is missing, and a split's terminal is gone", async () => {
  const window = new Window({ url: "http://localhost/" });
  const terminalRefusals: Array<"app_closed" | "not_found"> = ["app_closed", "not_found"];
  const spaceRefusals: Array<"app_closed" | "folder_missing"> = ["app_closed", "folder_missing"];
  const { app } = makeView(window, {
    herdr: "core",
    run: async () => ({ outcome: "refused", reason: spaceRefusals.shift()! }),
    runTerminal: async () => ({ outcome: "refused", reason: terminalRefusals.shift()! }),
  }, coreAppHome());
  const messages: string[] = [];
  const readPanel = (): void => {
    const panel = requiredElement(app, ".home-action-panel");
    messages.push(requiredElement(panel, ".home-action-copy").textContent ?? "");
    requiredMatchingElement<HTMLButtonElement>(panel, "button", (button) => button.textContent === "Done").click();
  };
  for (let index = 0; index < 2; index++) {
    requiredElement<HTMLButtonElement>(app, ".new-space-action").click();
    requiredElement<HTMLFormElement>(app, ".home-action-form")
      .dispatchEvent(new window.Event("submit", { bubbles: true, cancelable: true }));
    await settle();
    readPanel();
  }
  for (let index = 0; index < 2; index++) {
    requiredElement<HTMLButtonElement>(app, ".workspace-terminal-trigger").click();
    const trigger = requiredMatchingElement<HTMLButtonElement>(
      app, ".terminal-menu .workspace-menu-trigger", (button) => button.getAttribute("aria-label") === "Actions for Claude",
    );
    trigger.click();
    requiredElement<HTMLButtonElement>(trigger.parentElement!, '[role="menuitem"]').click();
    requiredMatchingElement<HTMLButtonElement>(app, ".home-action-buttons button", (button) => button.textContent === "Below").click();
    await settle();
    readPanel();
  }
  // Rename space has no Herdr Core route, so its copy says to open the window.
  spaceRefusals.push("app_closed");
  requiredMatchingElement<HTMLButtonElement>(
    app, ".workspace-menu-trigger", (node) => node.getAttribute("aria-label") === "Actions for Core <img src=x>",
  ).click();
  requiredMatchingElement<HTMLButtonElement>(app, '[role="menuitem"]', (node) => node.textContent === "Rename space").click();
  requiredElement<HTMLFormElement>(app, ".home-action-form")
    .dispatchEvent(new window.Event("submit", { bubbles: true, cancelable: true }));
  await settle();
  readPanel();
  assert.deepEqual(messages, [
    "The Herdr app window is closed. Open the action again to start it in Herdr Core.",
    "This folder doesn't exist.",
    "The Herdr app window is closed. Open the action again to start it in Herdr Core.",
    "This terminal is no longer here.",
    "The Herdr app window is closed. Open it on the computer, then try again.",
  ]);
  window.close();
});

// Home on Herdr Core while the Herdr app window is closed.
function coreClosedHome(): Home {
  const home = coreAppHome();
  home.app_window = "closed";
  home.workspaces[0].actions = ["close_workspace"];
  return home;
}

test("Herdr Core starts New space in Herdr Core while the app window is closed, with a warning and no label", async () => {
  const window = new Window({ url: "http://localhost/" });
  let request: RunWorkspaceActionRequest | undefined;
  const { app } = makeView(window, {
    herdr: "core",
    run: async (value) => {
      request = value;
      return { outcome: "succeeded" };
    },
  }, coreClosedHome());
  const trigger = requiredMatchingElement<HTMLButtonElement>(
    app, ".workspace-menu-trigger", (node) => node.getAttribute("aria-label") === "Actions for Core <img src=x>",
  );
  trigger.click();
  assert.deepEqual(Array.from(trigger.parentElement!.querySelectorAll('[role="menuitem"]'), (item) => item.textContent), ["Close space"]);
  trigger.click();
  requiredElement<HTMLButtonElement>(app, ".new-space-action").click();
  const panel = requiredElement(app, ".home-action-panel");
  assert.equal(requiredElement(panel, ".home-action-title").textContent, "New space");
  assert.equal(requiredElement(panel, ".home-action-copy").textContent, "Add a space for an existing folder.");
  assert.equal(
    requiredElement(panel, ".home-action-warning").textContent,
    "The Herdr app is closed. Shepherdr will start this space in Herdr Core, and the app will add it when it next opens. Its name and place in the list may change then.",
  );
  assert.equal(panel.querySelectorAll('input[name="label"]').length, 0);
  assert.equal(requiredElement(panel, ".home-action-primary").textContent, "Create space");
  requiredElement<HTMLInputElement>(panel, 'input[name="working_directory"]').value = "~/repo";
  requiredElement<HTMLFormElement>(panel, ".home-action-form")
    .dispatchEvent(new window.Event("submit", { bubbles: true, cancelable: true }));
  await settle();
  assert.equal(JSON.stringify(request), '{"herdr":"core","action":"create_space","working_directory":"~/repo","best_effort":true}');
  assert.equal(requiredElement(panel, ".home-action-title").textContent, "Space created");
  assert.equal(requiredElement(panel, ".home-action-copy").textContent, "It's on Home now. The Herdr app will add it when it next opens.");
  window.close();
});

test("Herdr Core starts New terminal in Herdr Core while the app window is closed, and opens it", async () => {
  const window = new Window({ url: "http://localhost/" });
  const requests: RunTerminalActionRequest[] = [];
  let opened: TerminalActionTarget | undefined;
  const created = { workspace_id: "sp-core", tab_id: "tb-main", pane_id: "t20", terminal_id: "core:t20" };
  const outcomes: RunTerminalActionResponse[] = [
    { outcome: "succeeded", terminal: created },
    { outcome: "unknown" },
    { outcome: "refused", reason: "folder_missing" },
  ];
  const { app } = makeView(window, {
    herdr: "core",
    onOpenCreated: (target) => { opened = target; },
    runTerminal: async (value) => {
      requests.push(value);
      return outcomes.shift()!;
    },
  }, coreClosedHome());
  const results: string[] = [];
  for (let index = 0; index < 3; index++) {
    requiredElement<HTMLButtonElement>(app, ".workspace-terminal-trigger").click();
    const trigger = requiredMatchingElement<HTMLButtonElement>(
      app, ".terminal-menu .workspace-menu-trigger", (button) => button.getAttribute("aria-label") === "Actions for Claude",
    );
    trigger.click();
    requiredMatchingElement<HTMLButtonElement>(trigger.parentElement!, '[role="menuitem"]', (item) => item.textContent === "New terminal").click();
    const panel = requiredElement(app, ".home-action-panel");
    if (index === 0) {
      assert.equal(requiredElement(panel, ".home-action-title").textContent, "New terminal");
      assert.equal(requiredElement(panel, ".home-action-copy").textContent, "This starts a terminal next to Claude and opens it.");
      assert.equal(
        requiredElement(panel, ".home-action-warning").textContent,
        "The Herdr app is closed. Shepherdr will start this terminal in Herdr Core, and the app will add it at the end of this tab when it next opens.",
      );
      assert.deepEqual(Array.from(panel.querySelectorAll(".home-action-buttons button"), (button) => button.textContent), ["New terminal", "Cancel"]);
    }
    requiredMatchingElement<HTMLButtonElement>(panel, ".home-action-buttons button", (button) => button.textContent === "New terminal").click();
    await settle();
    if (index > 0) {
      results.push(requiredElement(panel, ".home-action-copy").textContent ?? "");
      requiredMatchingElement<HTMLButtonElement>(panel, "button", (button) => button.textContent === "Done").click();
    }
  }
  assert.equal(JSON.stringify(requests[0]), JSON.stringify({
    herdr: "core", action: "split_terminal", workspace_id: "sp-core", tab_id: "tb-main", pane_id: "t3", terminal_id: "core:t3", best_effort: true,
  }));
  assert.deepEqual(opened, created);
  assert.deepEqual(results, ["Check Home before starting another terminal.", "This folder doesn't exist."]);
  window.close();
});

// Home on Herdr Core with New tab: a space with two tabs, and a space with one
// tab and one terminal.
function coreNewTabHome(closed = false): Home {
  const home = closed ? coreClosedHome() : coreAppHome();
  home.workspaces[0].actions = closed ? ["new_tab", "close_workspace"] : ["new_tab", "rename_space", "close_workspace"];
  home.workspaces.splice(1, 0, {
    actions: closed ? ["new_tab", "close_workspace"] : ["new_tab", "rename_space", "close_workspace"],
    id: "sp-one",
    label: "One",
    number: 2,
    tabs: [{
      current: false,
      id: "tb-one",
      label: "1",
      number: 1,
      terminals: [{ actions: ["split_terminal", "close_terminal"], pane_id: "t7", terminal_id: "core:t7", title: "shell" }],
    }],
  });
  return home;
}

function menuTrigger(app: HTMLElement, label: string): HTMLButtonElement {
  return requiredMatchingElement<HTMLButtonElement>(
    app, ".workspace-menu-trigger", (node) => node.getAttribute("aria-label") === label,
  );
}

function menuItemTexts(trigger: HTMLButtonElement): string[] {
  return Array.from(trigger.parentElement!.querySelectorAll('[role="menuitem"]'), (item) => item.textContent ?? "");
}

test("Herdr Core offers New tab in each space's top-level menu and opens the new tab's terminal", async () => {
  const window = new Window({ url: "http://localhost/" });
  const requests: RunWorkspaceActionRequest[] = [];
  const opened: TerminalActionTarget[] = [];
  const created = { workspace_id: "sp-core", tab_id: "tb-new", pane_id: "t20", terminal_id: "core:t20" };
  const { app } = makeView(window, {
    herdr: "core",
    onOpenCreated: (target) => { opened.push(target); },
    run: async (value) => {
      requests.push(value);
      return { outcome: "succeeded", terminal: { ...created, workspace_id: (value as { workspace_id: string }).workspace_id } };
    },
  }, coreNewTabHome());

  // The space header's menu, the single-tab row's menu, and not Other terminals.
  const header = menuTrigger(app, "Actions for Core <img src=x>");
  header.click();
  assert.deepEqual(menuItemTexts(header), ["New tab", "Rename space", "Close space"]);
  const row = menuTrigger(app, "Actions for One");
  assert.deepEqual(menuItemTexts(row), ["New terminal", "Close terminal", "New tab", "Rename space", "Close space"]);
  assert.equal(Array.from(app.querySelectorAll('[role="menuitem"]'), (item) => item.textContent).filter((text) => text === "New tab").length, 2);

  requiredMatchingElement<HTMLButtonElement>(header.parentElement!, '[role="menuitem"]', (item) => item.textContent === "New tab").click();
  const panel = requiredElement(app, ".home-action-panel");
  assert.equal(requiredElement(panel, ".home-action-title").textContent, "New tab");
  assert.equal(requiredElement(panel, ".home-action-copy").textContent, "This adds a tab at the end of Core <img src=x> and opens it.");
  assert.equal(panel.querySelectorAll("img, .home-action-warning").length, 0);
  assert.deepEqual(Array.from(panel.querySelectorAll(".home-action-buttons button"), (button) => button.textContent), ["New tab", "Cancel"]);
  requiredMatchingElement<HTMLButtonElement>(panel, ".home-action-buttons button", (button) => button.textContent === "New tab").click();
  await settle();

  row.click();
  requiredMatchingElement<HTMLButtonElement>(row.parentElement!, '[role="menuitem"]', (item) => item.textContent === "New tab").click();
  requiredMatchingElement<HTMLButtonElement>(app, ".home-action-buttons button", (button) => button.textContent === "New tab").click();
  await settle();
  assert.deepEqual(requests.map((request) => JSON.stringify(request)), [
    '{"herdr":"core","action":"new_tab","workspace_id":"sp-core"}',
    '{"herdr":"core","action":"new_tab","workspace_id":"sp-one"}',
  ]);
  assert.deepEqual(opened, [created, { ...created, workspace_id: "sp-one" }]);
  window.close();
});

test("Herdr Core starts New tab in Herdr Core while the app window is closed, with a warning, and says what failed", async () => {
  const window = new Window({ url: "http://localhost/" });
  const requests: RunWorkspaceActionRequest[] = [];
  let opened: TerminalActionTarget | undefined;
  const created = { workspace_id: "sp-core", tab_id: "tb-new", pane_id: "t20", terminal_id: "core:t20" };
  const outcomes: RunWorkspaceActionResponse[] = [
    { outcome: "succeeded", terminal: created },
    { outcome: "succeeded" },
    { outcome: "unknown" },
    { outcome: "refused", reason: "folder_missing" },
    { outcome: "refused", reason: "not_found" },
    { outcome: "refused", reason: "app_closed" },
  ];
  const { app } = makeView(window, {
    herdr: "core",
    onOpenCreated: (target) => { opened = target; },
    run: async (value) => {
      requests.push(value);
      return outcomes.shift()!;
    },
  }, coreNewTabHome(true));
  const results: string[] = [];
  for (let index = 0; index < 6; index++) {
    const trigger = menuTrigger(app, "Actions for Core <img src=x>");
    trigger.click();
    if (index === 0) assert.deepEqual(menuItemTexts(trigger), ["New tab", "Close space"]);
    requiredMatchingElement<HTMLButtonElement>(trigger.parentElement!, '[role="menuitem"]', (item) => item.textContent === "New tab").click();
    const panel = requiredElement(app, ".home-action-panel");
    if (index === 0) {
      assert.equal(requiredElement(panel, ".home-action-copy").textContent, "This adds a tab at the end of Core <img src=x> and opens it.");
      assert.equal(
        requiredElement(panel, ".home-action-warning").textContent,
        "The Herdr app is closed. Shepherdr will start this tab in Herdr Core, and the app will add it when it next opens. Its place among the space's tabs may change then.",
      );
    }
    requiredMatchingElement<HTMLButtonElement>(panel, ".home-action-buttons button", (button) => button.textContent === "New tab").click();
    await settle();
    if (index > 0) {
      results.push(requiredElement(panel, ".home-action-copy").textContent ?? "");
      requiredMatchingElement<HTMLButtonElement>(panel, "button", (button) => button.textContent === "Done").click();
    }
  }
  assert.equal(JSON.stringify(requests[0]), '{"herdr":"core","action":"new_tab","workspace_id":"sp-core","best_effort":true}');
  assert.deepEqual(opened, created);
  assert.deepEqual(results, [
    "Check Home before starting another tab.",
    "Check Home before starting another tab.",
    "This folder doesn't exist.",
    "This space is no longer here.",
    "The Herdr app window is closed. Open the action again to start it in Herdr Core.",
  ]);
  window.close();
});

// Home on Herdr Core with Rename tab: Core lists its tabs (a split tab and a
// one-terminal tab), and One has a single tab.
function coreRenameTabHome(): Home {
  const home = coreNewTabHome();
  const space = home.workspaces[0];
  space.actions = ["new_tab", "rename_space", "rename_tab", "close_workspace"];
  space.tabs[0].label = "fix <i>login</i> 🐑";
  space.tabs[0].manual_name = "fix <i>login</i> 🐑";
  return home;
}

test("Herdr Core renames a tab from its row's menu, starting from the name it was given", async () => {
  const window = new Window({ url: "http://localhost/" });
  const requests: RunWorkspaceActionRequest[] = [];
  const outcomes: RunWorkspaceActionResponse[] = [
    { outcome: "succeeded" },
    { outcome: "unknown" },
    { outcome: "refused", reason: "herdr_refused", detail: 'no tab "tb-two"' },
    { outcome: "refused", reason: "not_found" },
  ];
  const { app } = makeView(window, {
    herdr: "core",
    run: async (value) => {
      requests.push(value);
      return outcomes.shift()!;
    },
  }, coreRenameTabHome());

  // Each tab row's menu has it; the space's menus and a single-tab space's row don't.
  assert.deepEqual(menuItemTexts(menuTrigger(app, "Actions for Core <img src=x>")), ["New tab", "Rename space", "Close space"]);
  assert.deepEqual(menuItemTexts(menuTrigger(app, "Actions for One")), ["New terminal", "Close terminal", "New tab", "Rename space", "Close space"]);
  const split = menuTrigger(app, "Actions for fix <i>login</i> 🐑");
  assert.deepEqual(menuItemTexts(split), ["Rename tab"]);
  const single = menuTrigger(app, "Actions for 2");
  assert.deepEqual(menuItemTexts(single), ["New terminal", "Close terminal", "Rename tab"]);

  const results: string[] = [];
  const rename = async (trigger: HTMLButtonElement, name: string | undefined, check?: (panel: HTMLElement, input: HTMLInputElement) => void) => {
    trigger.click();
    requiredMatchingElement<HTMLButtonElement>(trigger.parentElement!, '[role="menuitem"]', (item) => item.textContent === "Rename tab").click();
    const panel = requiredElement(app, ".home-action-panel");
    const input = requiredElement<HTMLInputElement>(panel, 'input[name="tab_name"]');
    check?.(panel, input);
    if (name !== undefined) input.value = name;
    requiredElement<HTMLFormElement>(panel, ".home-action-form")
      .dispatchEvent(new window.Event("submit", { bubbles: true, cancelable: true }));
    await settle();
    assert.equal(requiredElement(panel, ".home-action-title").className, "home-action-title");
    results.push([
      requiredElement(panel, ".home-action-title").textContent,
      requiredElement(panel, ".home-action-copy").textContent,
      panel.querySelector(".home-action-detail")?.textContent ?? "",
    ].join(" | "));
    requiredMatchingElement<HTMLButtonElement>(panel, "button", (button) => button.textContent === "Done").click();
  };

  await rename(split, "", (panel, input) => {
    assert.equal(requiredElement(panel, ".home-action-title").textContent, "Rename fix <i>login</i> 🐑");
    // The untrusted name is held to two lines; result titles aren't.
    assert.equal(requiredElement(panel, ".home-action-title").className, "home-action-title home-action-title-name");
    assert.equal(panel.querySelectorAll("i, b, img").length, 0);
    assert.equal(input.value, "fix <i>login</i> 🐑");
    assert.equal(requiredElement(panel, ".home-action-help").textContent, "Leave the name blank to use the automatic name.");
    assert.equal(requiredElement(panel, ".home-action-primary").textContent, "Rename tab");
  });
  await rename(single, "<b>review</b>", (panel, input) => {
    // A tab called by its number has no name to start from.
    assert.equal(requiredElement(panel, ".home-action-title").textContent, "Rename 2");
    assert.equal(input.value, "");
  });
  await rename(single, "x");
  await rename(single, "y");
  assert.deepEqual(requests.map((request) => JSON.stringify(request)), [
    '{"herdr":"core","action":"rename_tab","workspace_id":"sp-core","tab_id":"tb-main","name":""}',
    '{"herdr":"core","action":"rename_tab","workspace_id":"sp-core","tab_id":"tb-two","name":"<b>review</b>"}',
    '{"herdr":"core","action":"rename_tab","workspace_id":"sp-core","tab_id":"tb-two","name":"x"}',
    '{"herdr":"core","action":"rename_tab","workspace_id":"sp-core","tab_id":"tb-two","name":"y"}',
  ]);
  assert.deepEqual(results, [
    "Tab renamed | The new name is ready on Home. | ",
    "Result unknown | Result unknown. Check Home before renaming this tab again. | ",
    'Action not completed | Herdr refused this action. | no tab "tb-two"',
    "Action not completed | This tab is no longer here. | ",
  ]);
  window.close();
});

test("Herdr Core offers no Rename tab while the app window is closed", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app } = makeView(window, { herdr: "core" }, coreNewTabHome(true));
  assert.equal(
    Array.from(app.querySelectorAll(".workspace-menu-trigger"), (node) => node.getAttribute("aria-label")).includes("Actions for main"),
    false,
  );
  assert.deepEqual(menuItemTexts(menuTrigger(app, "Actions for 2")), ["New terminal", "Close terminal"]);
  assert.equal(Array.from(app.querySelectorAll('[role="menuitem"]'), (item) => item.textContent).includes("Rename tab"), false);
  window.close();
});

test("Herdr Core doesn't reopen a split tab's menu by itself when Rename tab comes back", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app, view } = makeView(window, { herdr: "core" }, coreRenameTabHome());
  const render = (home: Home) => view.render({
    actionsAvailable: true, herdr: "core", mode: "all", reachability: "current", state: state(home), suggestedFolders: [],
  });
  const label = "Actions for fix <i>login</i> 🐑";
  menuTrigger(app, label).click();
  assert.equal(menuTrigger(app, label).getAttribute("aria-expanded"), "true");
  // The window closes: the menu goes with its only item, then comes back closed.
  const closed = coreRenameTabHome();
  closed.app_window = "closed";
  closed.workspaces[0].actions = ["new_tab", "close_workspace"];
  render(closed);
  assert.equal(Array.from(app.querySelectorAll(".workspace-menu-trigger"), (node) => node.getAttribute("aria-label")).includes(label), false);
  render(coreRenameTabHome());
  assert.equal(menuTrigger(app, label).getAttribute("aria-expanded"), "false");
  assert.equal(app.querySelectorAll('.workspace-menu-popover:not([hidden])').length, 0);
  window.close();
});

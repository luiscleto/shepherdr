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

function actionHome(): Home {
  return {
    blocked_count: 1,
    working_count: 2,
    workspaces: [{
      actions: ["create_worktree", "close_group"],
      agent_counts: { blocked: 1, working: 2 },
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

function state(home = actionHome()): HomeState {
  return { connection: "live", gap: 0, has_home: true, home, last_known: false };
}

interface ViewOverrides {
  prepare?: (request: { action: "close_workspace" | "close_group" | "delete_checkout"; workspace_id: string }) => Promise<PreparedWorkspaceActionResponse>;
  run?: (request: RunWorkspaceActionRequest) => Promise<RunWorkspaceActionResponse>;
}

function makeView(window: Window, overrides: ViewOverrides = {}, home = actionHome()) {
  const app = window.document.createElement("main");
  window.document.body.append(app);
  const view = new HomeView(app, {
    onFocusPane: () => undefined,
    onOpen: () => undefined,
    onReconnect: () => undefined,
    onShowAll: () => undefined,
    onShowBlocked: () => undefined,
    prepareWorkspaceAction: overrides.prepare ?? (async () => ({ outcome: "refused", reason: "not_applicable" })),
    runWorkspaceAction: overrides.run ?? (async () => ({ outcome: "succeeded" })),
  });
  view.render({ actionsAvailable: true, mode: "all", reachability: "current", state: state(home) });
  return { app, view };
}

function requiredElement<T extends HTMLElement = HTMLElement>(root: ParentNode, selector: string): T {
  const node = root.querySelector<T>(selector);
  if (!node) throw new Error(`missing ${selector}`);
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
  const row = view.row("parent-pane");
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
  assert.equal(view.row("parent-pane") === row, true);
  assert.equal(requiredElement(app, ".terminal-name").textContent, "Parent terminal");
  assert.equal(filter.value, "Parent");
  assert.equal(window.scrollY, 217);

  finish?.({ outcome: "succeeded" });
  await settle();
  assert.match(requiredElement(app, ".home-action-panel").textContent ?? "", /Space created/);
  assert.equal(view.row("parent-pane") === row, true);
  requiredElement<HTMLButtonElement>(app, ".home-action-panel button").click();
  assert.equal(window.document.activeElement === newSpace, true);
  assert.equal(filter.value, "Parent");
  assert.equal(window.scrollY, 217);
  window.close();
});

test("workspace menu is immediately after Open and keyboard reaches fresh group confirmation", async () => {
  const window = new Window({ url: "http://localhost/" });
  let runRequest: RunWorkspaceActionRequest | undefined;
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
    prepare: async () => prepared,
    run: async (request) => {
      runRequest = request;
      return { outcome: "unknown" };
    },
  });
  requiredElement<HTMLInputElement>(app, ".home-filter input").value = "";
  const disclosure = requiredElement<HTMLButtonElement>(app, ".workspace-set-disclosure");
  disclosure.click();
  const row = view.row("parent-pane");
  assert.ok(row);
  const actionRow = row.parentElement;
  assert.equal(actionRow?.className, "workspace-action-row");
  assert.equal(actionRow?.firstElementChild === row, true);
  assert.equal(actionRow?.children[1].className, "workspace-menu");

  const trigger = requiredElement<HTMLButtonElement>(actionRow, ".workspace-menu-trigger");
  trigger.click();
  const items = Array.from(actionRow.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'));
  assert.deepEqual(items.map((item) => item.textContent), ["New worktree", "Close group"]);
  assert.equal(window.document.activeElement === items[0], true);
  items[0].dispatchEvent(new window.KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
  assert.equal(window.document.activeElement === items[1], true);
  items[1].dispatchEvent(new window.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
  assert.equal(window.document.activeElement === trigger, true);
  assert.equal(requiredElement(actionRow, '[role="menu"]').hidden, true);

  trigger.click();
  items[1].click();
  await settle();
  const panel = requiredElement(app, ".home-action-panel");
  const text = panel.textContent ?? "";
  assert.match(text, /Close Parent <script>\?/);
  assert.match(text, /whole group and 7 agents/);
  assert.match(text, /Agents may be interrupted: 2 working, 1 blocked, 3 unknown\./);
  assert.doesNotMatch(text, /opaque-child|opaque-parent/);
  assert.equal(panel.querySelector("script"), null);
  assert.equal(disclosure.getAttribute("aria-expanded"), "false");

  requiredElement<HTMLButtonElement>(panel, ".home-action-primary").click();
  await settle();
  assert.equal(
    runRequest && "expected" in runRequest ? JSON.stringify(runRequest.expected) : "",
    JSON.stringify(prepared.expected),
  );
  assert.equal(view.row("parent-pane") === row, true);
  assert.match(requiredElement(app, ".home-action-panel").textContent ?? "", /Result unknown\. Check Home/);
  assert.equal(disclosure.getAttribute("aria-expanded"), "false");
  requiredElement<HTMLButtonElement>(app, ".home-action-panel button").click();
  assert.equal(window.document.activeElement === trigger, true);
  window.close();
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
  const childTrigger = Array.from(app.querySelectorAll<HTMLButtonElement>(".workspace-menu-trigger"))
    .find((button) => button.getAttribute("aria-label") === "Actions for Child <img>");
  assert.ok(childTrigger);
  childTrigger.click();
  const deleteAction = Array.from(app.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'))
    .find((button) => button.textContent === "Delete checkout");
  assert.ok(deleteAction);
  deleteAction.click();
  await settle();

  const panel = requiredElement(app, ".home-action-panel");
  assert.equal(requiredElement(panel, ".checkout-path").textContent, path);
  assert.match(panel.textContent ?? "", /Git branch remains/);
  assert.equal(panel.querySelector("img"), null);
  requiredElement<HTMLButtonElement>(panel, ".home-action-primary").click();
  await settle();
  assert.match(
    panel.textContent ?? "",
    /This folder has changes\. It was not removed\. Resolve the changes in the terminal, then try again\./,
  );
  assert.doesNotMatch(panel.textContent ?? "", /svg onload/);
  assert.equal(panel.querySelector("svg"), null);
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
  const trigger = Array.from(app.querySelectorAll<HTMLButtonElement>(".workspace-menu-trigger"))
    .find((button) => button.getAttribute("aria-label") === "Actions for Parent <script>");
  assert.ok(trigger);

  const submitWorktree = async (branchValue: string): Promise<void> => {
    trigger.click();
    const action = Array.from(app.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'))
      .find((button) => !button.hidden && button.textContent === "New worktree");
    assert.ok(action);
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
  assert.equal(app.querySelector("b"), null);
  window.close();
});

test("server refusal detail is displayed as inert text", async () => {
  const window = new Window({ url: "http://localhost/" });
  const detail = "Herdr said <img src=x onerror=run>";
  const { app } = makeView(window, {
    prepare: async () => ({ outcome: "refused", reason: "herdr_refused", detail }),
  });
  const trigger = Array.from(app.querySelectorAll<HTMLButtonElement>(".workspace-menu-trigger"))
    .find((button) => button.getAttribute("aria-label") === "Actions for Parent <script>");
  assert.ok(trigger);
  trigger.click();
  const closeGroup = Array.from(app.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'))
    .find((button) => button.textContent === "Close group");
  assert.ok(closeGroup);
  closeGroup.click();
  await settle();

  assert.equal(requiredElement(app, ".home-action-detail").textContent, detail);
  assert.equal(app.querySelector("img"), null);
  window.close();
});

test("Home never infers workspace actions from grouping or checkout paths", () => {
  const window = new Window({ url: "http://localhost/" });
  const home = actionHome();
  home.workspaces[0].actions = [];
  home.workspaces[1].actions = [];
  home.workspaces[2].actions = [];
  home.workspaces[0].worktrees![0].actions = [];
  const app = window.document.createElement("main");
  window.document.body.append(app);
  const view = new HomeView(app, {
    onFocusPane: () => undefined,
    onOpen: () => undefined,
    onReconnect: () => undefined,
    onShowAll: () => undefined,
    onShowBlocked: () => undefined,
    prepareWorkspaceAction: async () => ({ outcome: "refused", reason: "not_applicable" }),
    runWorkspaceAction: async () => ({ outcome: "succeeded" }),
  });
  view.render({ actionsAvailable: true, mode: "all", reachability: "current", state: state(home) });

  assert.equal(app.querySelectorAll(".workspace-menu-trigger").length, 0);
  assert.equal(app.querySelectorAll(".new-space-action").length, 1);
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
          { pane_id: "second-parent-pane", terminal_id: "second-parent-terminal", title: "Second terminal" },
        ];
    const { app } = makeView(window, {}, home);
    const disclosure = requiredElement<HTMLButtonElement>(app, ".workspace-set-disclosure");
    disclosure.click();
    assert.equal(disclosure.getAttribute("aria-expanded"), "false");
    assert.equal(requiredElement(app, ".workspace-set-body").hidden, true);
    const header = requiredElement(app, ".workspace-set-header");
    const trigger = requiredElement<HTMLButtonElement>(header, ".workspace-menu-trigger");
    assert.equal(trigger.getAttribute("aria-label"), "Actions for Parent <script>");
    trigger.click();
    assert.deepEqual(
      Array.from(header.querySelectorAll('[role="menuitem"]'), (node) => node.textContent),
      ["New worktree", "Close group"],
    );
    window.close();
  }
});

test("Home action controls retain phone-sized touch targets in the Home-owned stylesheet", () => {
  const styles = readFileSync(new URL("./style.css", import.meta.url), "utf8");
  assert.match(styles, /\.workspace-menu-trigger\s*\{[^}]*width:\s*44px;[^}]*min-width:\s*44px;[^}]*min-height:\s*44px;/s);
  assert.match(styles, /\.home-action-buttons button\s*\{[^}]*min-width:\s*44px;[^}]*min-height:\s*44px;/s);
  assert.match(styles, /\.workspace-set\s*\{[^}]*overflow:\s*visible;/s);
  assert.match(styles, /@media \(max-width: 520px\)[\s\S]*\.home-filter\s*\{[^}]*grid-column:\s*1 \/ -1;/);
});

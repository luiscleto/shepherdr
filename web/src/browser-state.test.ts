import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { Window } from "happy-dom";

import { HomeView, type HomeViewRender } from "./home-view";
import { automaticAllTerminalsPlace, tabRowKey, type Home, type HomeState } from "./home-model";

function flatHome(): Home {
  return {
    blocked_count: 1,
    working_count: 0,
    workspaces: [
      {
        actions: ["close_workspace"],
        id: "workspace-one",
        label: "Workspace one",
        number: 1,
        tabs: [
          {
            current: true,
            id: "tab-one",
            label: "Main",
            number: 1,
            terminals: [
              {
                actions: [],
                agent: { kind: "codex", name: "Agent one", status: "blocked" },
                pane_id: "pane-one",
                terminal_id: "terminal-one",
                title: "Workspace one",
              },
            ],
          },
        ],
      },
    ],
  };
}

function groupedHome(): Home {
  return {
    blocked_count: 1,
    working_count: 1,
    workspaces: [
      {
        actions: ["create_worktree", "close_group"],
        agent_counts: { working: 1 },
        group_agent_counts: { blocked: 1, done: 1, idle: 1, unknown: 1, working: 1 },
        id: "parent",
        label: "Main project <script>",
        number: 1,
        tabs: [
          {
            current: true,
            id: "parent-tab",
            label: "Main",
            number: 1,
            terminals: [
              {
                actions: [],
                agent: { kind: "codex", name: "Builder", status: "working" },
                pane_id: "parent-pane",
                terminal_id: "parent-terminal",
                title: "Builder",
              },
              {
                actions: [],
                pane_id: "ordinary-pane",
                terminal_id: "ordinary-terminal",
                title: "Shell & notes",
              },
            ],
          },
        ],
        worktrees: [
          {
            actions: ["close_workspace", "delete_checkout"],
            id: "blocked-worktree",
            label: "Review branch",
            number: 2,
            tabs: [
              {
                current: true,
                id: "blocked-tab",
                label: "Review",
                number: 1,
                terminals: [
                  {
                    actions: [],
                    agent: { kind: "codex", name: "Reviewer", status: "blocked" },
                    pane_id: "blocked-pane",
                    terminal_id: "blocked-terminal",
                    title: "Review branch",
                  },
                ],
              },
            ],
          },
          {
            actions: ["close_workspace", "delete_checkout"],
            id: "quiet-worktree",
            label: "Quiet branch",
            number: 3,
            tabs: [
              {
                current: true,
                id: "quiet-tab",
                label: "Quiet",
                number: 1,
                terminals: [
                  {
                    actions: [],
                    agent: { kind: "codex", name: "Finished", status: "done" },
                    pane_id: "quiet-pane",
                    terminal_id: "quiet-terminal",
                    title: "Quiet branch",
                  },
                  {
                    actions: [],
                    agent: { kind: "codex", name: "Waiting", status: "idle" },
                    pane_id: "idle-pane",
                    terminal_id: "idle-terminal",
                    title: "Waiting",
                  },
                  {
                    actions: [],
                    agent: { kind: "codex", name: "Unknown", status: "unknown" },
                    pane_id: "unknown-pane",
                    terminal_id: "unknown-terminal",
                    title: "Unknown",
                  },
                ],
              },
            ],
          },
        ],
      },
    ],
  };
}

function state(value = flatHome(), connection: HomeState["connection"] = "live"): HomeState {
  return {
    can_open_terminals: true,
    connection,
    gap: 2,
    has_home: true,
    herdr_version: "0.8.0",
    home: value,
    last_known: connection !== "live",
  };
}

function coreHome(): Home {
  const terminal = (pane: string, title: string, status?: "blocked" | "error" | "idle") => ({
    actions: [],
    ...(status ? { agent: { kind: "claude-code", name: title, status } } : {}),
    pane_id: pane,
    terminal_id: `core:${pane}`,
    title,
  });
  return {
    blocked_count: 1,
    error_count: 1,
    working_count: 0,
    workspaces: [
      {
        actions: [],
        agent_counts: { blocked: 1, error: 1, idle: 1 },
        id: "sp-alpha",
        label: "alpha <img src=x onerror=alert(1)>",
        number: 1,
        tabs: [
          {
            current: false,
            id: "tb-main",
            label: "main",
            number: 1,
            terminals: [
              terminal("t1", "Shell"),
              terminal("t2", "Asks a question", "blocked"),
            ],
          },
          {
            current: false,
            id: "tb-two",
            label: "2",
            number: 2,
            terminals: [terminal("t3", "Failed run", "error"), terminal("t4", "Waiting", "idle")],
          },
        ],
      },
      {
        actions: [],
        id: "other",
        label: "Other terminals",
        number: 2,
        tabs: [{ current: false, id: "other", label: "Other terminals", number: 1, terminals: [terminal("t9", "outside")] }],
      },
    ],
  };
}

function makeView(window: Window, actions: Partial<ConstructorParameters<typeof HomeView>[1]> = {}) {
  const app = window.document.createElement("main");
  window.document.body.append(app);
  const view = new HomeView(app, {
    isHomeActive: actions.isHomeActive ?? (() => true),
    onFocusPane: actions.onFocusPane ?? (() => undefined),
    onOpen: actions.onOpen ?? (() => undefined),
    onOpenCreated: actions.onOpenCreated ?? (() => undefined),
    onShowAll: actions.onShowAll ?? (() => undefined),
    onShowBlocked: actions.onShowBlocked ?? (() => undefined),
    onShowError: actions.onShowError,
    onShowPinned: actions.onShowPinned,
    onShowHerdr: actions.onShowHerdr,
    prepareWorkspaceAction: actions.prepareWorkspaceAction ?? (async () => ({
      outcome: "refused",
      reason: "not_applicable",
    })),
    prepareTerminalAction: actions.prepareTerminalAction ?? (async () => ({
      outcome: "refused",
      reason: "not_applicable",
    })),
    runWorkspaceAction: actions.runWorkspaceAction ?? (async () => ({ outcome: "succeeded" })),
    runTerminalAction: actions.runTerminalAction ?? (async () => ({ outcome: "succeeded" })),
  });
  return { app, view };
}

function render(view: HomeView, next: HomeState, overrides: Partial<HomeViewRender> = {}): void {
  view.render({
    actionsAvailable: next.connection === "live" && !next.last_known && next.has_home,
    herdr: "tui",
    mode: "all",
    reachability: "current",
    state: next,
    suggestedFolders: [],
    ...overrides,
  });
}

function requiredElement(root: ParentNode, selector: string): HTMLElement {
  const node = root.querySelector<HTMLElement>(selector);
  if (!node) throw new Error(`missing ${selector}`);
  return node;
}

function requiredRow(view: HomeView, paneID: string): HTMLElement {
  const row = view.row(paneID);
  if (!row) throw new Error(`missing row ${paneID}`);
  return row;
}

function requiredWorkspaceRow(view: HomeView, workspaceID: string): HTMLElement {
  const row = view.workspaceRow(workspaceID);
  if (!row) throw new Error(`missing workspace row ${workspaceID}`);
  return row;
}

test("Home shows one real initial or unavailable state with the transport-owned badge", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app, view } = makeView(window);
  const loading: HomeState = {
    can_open_terminals: false,
    connection: "reconnecting",
    gap: 0,
    has_home: false,
    herdr_version: "",
    home: { blocked_count: 0, working_count: 0, workspaces: [] },
    last_known: false,
  };

  render(view, loading, { actionsAvailable: false });
  const connection = requiredElement(app, ".home-connection");
  assert.equal(requiredElement(connection, ".connection-label").textContent, "Reconnecting");
  assert.equal(requiredElement(connection, ".connection-label").localName, "span");
  assert.equal(requiredElement(connection, ".connection-dot").className, "connection-dot connection-dot-reconnecting");
  assert.equal(connection.childElementCount, 2);
  assert.equal(connection.children.item(1)?.className, "home-connection-buttons");
  assert.deepEqual([...connection.querySelectorAll("button")].map((button) => button.getAttribute("aria-label")), ["Shared files", "Settings"]);
  assert.equal(connection.querySelectorAll("p, details").length, 0);
  assert.equal(connection.querySelectorAll("strong").length, 0);
  assert.match(app.textContent ?? "", /Loading terminals/);
  assert.equal(app.querySelectorAll(".attention-bar").length, 0);

  render(view, { ...loading, connection: "incompatible", detail: "unsupported detail" }, { actionsAvailable: false });
  assert.equal(requiredElement(connection, ".connection-label").textContent, "Cannot use this Herdr");
  assert.equal(requiredElement(connection, ".connection-dot").className, "connection-dot connection-dot-offline");
  assert.doesNotMatch(connection.textContent ?? "", /unsupported detail|Values below|fresh view|may be stale/);

  render(view, { ...loading, connection: "not_running" }, { actionsAvailable: false, reachability: "offline" });
  assert.equal(requiredElement(connection, ".connection-label").textContent, "Herdr is not running");
  assert.match(app.textContent ?? "", /Home unavailable/);
  assert.doesNotMatch(app.textContent ?? "", /Offline/);

  render(view, state());
  assert.equal(requiredElement(app, ".herdr-version").textContent, "Herdr TUI 0.8.0");
  assert.equal(requiredElement(connection, ".connection-label").textContent, "Live");
  assert.equal(requiredElement(connection, ".connection-dot").className, "connection-dot connection-dot-live");
  assert.equal(connection.childElementCount, 2);
  assert.equal(requiredRow(view, "pane-one").localName, "button");
  assert.equal(requiredRow(view, "pane-one").getAttribute("aria-disabled"), null);
  assert.doesNotMatch(app.textContent ?? "", /Home is updating|Loading terminals/);
  window.close();
});

test("zero-terminal workspaces render once without the global empty state", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app, view } = makeView(window);
  const emptyWorkspaceHome: Home = {
    blocked_count: 0,
    working_count: 0,
    workspaces: [{
      actions: ["close_workspace"],
      id: "empty-workspace",
      label: "Empty workspace",
      number: 1,
      tabs: [{ current: true, id: "empty-tab", label: "Main", number: 1, terminals: [] }],
    }],
  };

  render(view, state(emptyWorkspaceHome));

  assert.equal(app.querySelectorAll("section.workspace").length, 1);
  assert.equal(
    Array.from(app.querySelectorAll("section.workspace > h2")).filter((heading) => heading.textContent === "Empty workspace")
      .length,
    1,
  );
  assert.equal(app.querySelectorAll(".terminal-row").length, 0);
  assert.doesNotMatch(app.textContent ?? "", /No terminals/);

  render(view, state({ blocked_count: 0, working_count: 0, workspaces: [] }));
  assert.equal(app.querySelectorAll("section.workspace").length, 0);
  assert.equal(
    Array.from(app.querySelectorAll(".state-panel strong")).filter((heading) => heading.textContent === "No terminals").length,
    1,
  );
  assert.match(app.textContent ?? "", /Herdr is running, but nothing is open/);
  const order = Array.from(app.children).map((child) => child.classList.contains("home-tools")
    ? "tools"
    : child.querySelector("strong")?.textContent === "No terminals" ? "empty" : "other");
  assert.ok(order.indexOf("tools") >= 0 && order.indexOf("tools") < order.indexOf("empty"), order.join(","));
  window.close();
});

test("whole Home replacements retain rows, focus, scroll, and do nothing when unchanged", async () => {
  const window = new Window({ url: "http://localhost/" });
  const focused: string[] = [];
  const { app, view } = makeView(window, { onFocusPane: (paneID) => focused.push(paneID) });
  const first = state();
  render(view, first);
  const row = requiredRow(view, "pane-one");
  row.focus();
  window.scrollTo(0, 240);

  let mutations = 0;
  const observer = new window.MutationObserver((records) => {
    mutations += records.length;
  });
  observer.observe(app, { attributes: true, characterData: true, childList: true, subtree: true });
  render(view, first);
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(mutations, 0);

  const changed = structuredClone(first);
  const terminal = changed.home.workspaces[0].tabs[0].terminals[0];
  terminal.title = "Renamed agent";
  terminal.agent = { kind: "codex", name: "Replacement", status: "working" };
  changed.home.blocked_count = 0;
  changed.home.working_count = 1;
  render(view, changed);
  assert.equal(view.row("pane-one") === row, true);
  assert.equal(window.document.activeElement === row, true);
  assert.equal(window.scrollY, 240);
  assert.equal(row.getAttribute("aria-label"), "Open Renamed agent, workspace Workspace one, Replacement, working");
  assert.equal(requiredElement(row, ".status").textContent, "working");
  assert.deepEqual(focused, ["pane-one"]);
  observer.disconnect();
  window.close();
});

test("transport changes keep the last complete Home and its stable badge slot", () => {
  const window = new Window({ url: "http://localhost/" });
  let opens = 0;
  const { app, view } = makeView(window, {
    onOpen: () => opens++,
  });
  const current = state();
  render(view, current);
  const connection = requiredElement(app, ".home-connection");
  const version = requiredElement(app, ".herdr-version");
  assert.equal(version.localName, "p");
  assert.equal(version.textContent, "Herdr TUI 0.8.0");
  const row = requiredRow(view, "pane-one");
  assert.equal(connection.childElementCount, 2);
  assert.equal(connection.querySelectorAll("button").length, 2);
  assert.doesNotMatch(connection.textContent ?? "", /Reconnect/);
  row.click();
  assert.equal(opens, 1);

  const stale = { ...current, connection: "reconnecting" as const, last_known: true };
  render(view, stale, { actionsAvailable: false, reachability: "reconnecting" });
  assert.equal(app.querySelector(".herdr-version") === version, true);
  assert.equal(version.textContent, "");
  assert.equal(app.querySelector(".home-connection") === connection, true);
  const unavailableRow = requiredRow(view, "pane-one");
  assert.equal(unavailableRow.localName, "div");
  assert.equal(unavailableRow.tabIndex, -1);
  assert.equal(unavailableRow.getAttribute("aria-disabled"), null);
  assert.equal(app.querySelectorAll("button.terminal-row").length, 0);
  assert.equal(app.querySelectorAll(".terminal-row").length, 1);
  assert.equal(requiredElement(connection, ".connection-label").textContent, "Reconnecting");
  assert.equal(requiredElement(connection, ".connection-dot").className, "connection-dot connection-dot-reconnecting");
  assert.equal(connection.childElementCount, 2);
  assert.doesNotMatch(connection.textContent ?? "", /State below|fresh view|Values below/);
  unavailableRow.click();
  assert.equal(opens, 1);

  render(view, stale, { actionsAvailable: false, reachability: "offline" });
  assert.equal(version.textContent, "");
  assert.equal(requiredElement(connection, ".connection-label").textContent, "Offline");
  assert.equal(requiredElement(connection, ".connection-dot").className, "connection-dot connection-dot-offline");
  assert.equal(connection.childElementCount, 2);
  assert.equal(connection.querySelectorAll("button").length, 2);
  assert.doesNotMatch(connection.textContent ?? "", /Reconnect/);

  render(view, current);
  assert.equal(version.textContent, "Herdr TUI 0.8.0");
  assert.equal(app.querySelector(".home-connection") === connection, true);
  assert.equal(requiredElement(connection, ".connection-label").textContent, "Live");
  const recoveredRow = requiredRow(view, "pane-one");
  assert.equal(recoveredRow.localName, "button");
  assert.equal(requiredElement(recoveredRow, ".terminal-name").textContent, "Workspace one");
  recoveredRow.click();
  assert.equal(opens, 2);
  window.close();
});

test("Home renders the live Herdr version as text in a geometry-reserving slot", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app, view } = makeView(window);
  const current = state();
  current.herdr_version = "0.8.0<img src=x>";

  render(view, current);

  const version = requiredElement(app, ".herdr-version");
  assert.equal(version.textContent, "Herdr TUI 0.8.0<img src=x>");
  assert.equal(version.childElementCount, 0);
  render(view, { ...current, has_home: false });
  assert.equal(version.textContent, "Herdr TUI 0.8.0<img src=x>");
  render(view, { ...current, herdr_version: "" });
  assert.equal(version.textContent, "");
  const styles = readFileSync(new URL("./style.css", import.meta.url), "utf8");
  const versionRule = styles.match(/\.herdr-version\s*\{([^}]*)\}/)?.[1] ?? "";
  assert.match(versionRule, /(?:^|\n)\s*height:\s*0\.86rem;/);
  assert.match(versionRule, /(?:^|\n)\s*max-width:\s*100%;/);
  assert.match(versionRule, /(?:^|\n)\s*overflow:\s*hidden;/);
  assert.match(versionRule, /(?:^|\n)\s*text-overflow:\s*ellipsis;/);
  assert.match(versionRule, /(?:^|\n)\s*white-space:\s*nowrap;/);
  window.close();
});

test("opaque workspace and tab ids cannot collide during reconciliation", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app, view } = makeView(window);
  const collidingHome: Home = {
    blocked_count: 0,
    working_count: 0,
    workspaces: [
      {
        actions: ["close_workspace"],
        id: "left\u0000middle",
        label: "First workspace",
        number: 1,
        tabs: [
          {
            current: true,
            id: "right",
            label: "First tab",
            number: 1,
            terminals: [
              { actions: [], pane_id: "first-a", terminal_id: "first-terminal-a", title: "First A" },
              { actions: [], pane_id: "first-b", terminal_id: "first-terminal-b", title: "First B" },
            ],
          },
        ],
      },
      {
        actions: ["close_workspace"],
        id: "left",
        label: "Second workspace",
        number: 2,
        tabs: [
          {
            current: true,
            id: "middle\u0000right",
            label: "Second tab",
            number: 1,
            terminals: [
              { actions: [], pane_id: "second-a", terminal_id: "second-terminal-a", title: "Second A" },
              { actions: [], pane_id: "second-b", terminal_id: "second-terminal-b", title: "Second B" },
            ],
          },
        ],
      },
    ],
  };

  render(view, state(collidingHome));

  assert.equal(app.querySelectorAll("section.workspace").length, 2);
  assert.equal(app.querySelectorAll(".workspace-terminal-trigger").length, 2);
  assert.equal(app.querySelectorAll(".terminal-section, .terminal-section-disclosure").length, 0);
  assert.deepEqual(
    Array.from(app.querySelectorAll(".terminal-name"), (node) => node.textContent),
    ["First workspace", "Second workspace"],
  );
  requiredWorkspaceRow(view, "left\u0000middle").click();
  assert.equal(requiredElement(app, ".terminal-picker-count").textContent, "2 terminals");
  assert.deepEqual(
    Array.from(app.querySelectorAll(".terminal-picker-body .terminal-name"), (node) => node.textContent),
    ["First A", "First B"],
  );
  requiredElement<HTMLButtonElement>(app, ".terminal-picker-close").click();
  requiredWorkspaceRow(view, "left").click();
  assert.deepEqual(
    Array.from(app.querySelectorAll(".terminal-picker-body .terminal-name"), (node) => node.textContent),
    ["Second A", "Second B"],
  );
  window.close();
});

test("worktree sets keep the parent Open separate from the worktree disclosure", () => {
  const window = new Window({ url: "http://localhost/" });
  const opened: string[] = [];
  const { app, view } = makeView(window, { onOpen: (entry) => opened.push(entry.terminal.pane_id) });
  const home = groupedHome();
  home.workspaces[0].tabs[0].terminals = [home.workspaces[0].tabs[0].terminals[0]];
  const current = state(home);
  render(view, current);

  const disclosure = requiredElement(app, ".workspace-set-disclosure");
  const parentRow = requiredRow(view, "parent-pane");
  assert.equal(disclosure.getAttribute("aria-expanded"), "true");
  assert.equal(disclosure.getAttribute("aria-label"), "Collapse Main project <script> worktrees");
  assert.equal(app.querySelectorAll(".workspace-set-meta").length, 0);
  assert.deepEqual(
    Array.from(parentRow.querySelectorAll(".workspace-summary-status"), (badge) => badge.textContent),
    ["1 working", "1 blocked", "1 idle", "1 done", "1 unknown"],
  );
  assert.doesNotMatch(requiredElement(parentRow, ".workspace-set-summary").textContent ?? "", /workspace|agent/);
  assert.equal(parentRow.closest(".workspace-set-header") !== null, true);
  assert.equal(parentRow.closest(".workspace-set-body"), null);
  assert.deepEqual(
    Array.from(requiredElement(parentRow, ".terminal-main").children, (child) => child.className),
    ["terminal-name", "workspace-set-summary", "terminal-meta"],
  );
  assert.doesNotMatch(disclosure.textContent ?? "", /Open/);
  assert.equal(disclosure.textContent, "⌄");
  assert.equal(parentRow.localName, "button");
  assert.equal(
    parentRow.getAttribute("aria-label"),
    "Open Builder, workspace Main project <script>, working, workspace totals: 1 working, 1 blocked, 1 idle, 1 done, 1 unknown",
  );
  assert.equal(requiredElement(parentRow, ".status").textContent, "working");
  assert.equal(app.querySelectorAll(".terminal-row").length, 3);
  assert.equal(requiredElement(app, ".workspace-expand-action").textContent, "Collapse all");
  assert.equal(app.querySelector("script") === null, true);

  parentRow.click();
  assert.deepEqual(opened, ["parent-pane"]);
  disclosure.click();
  assert.equal(disclosure.getAttribute("aria-expanded"), "false");
  assert.equal(disclosure.getAttribute("aria-label"), "Expand Main project <script> worktrees");
  assert.equal(disclosure.textContent, "›");
  assert.equal(requiredElement(app, ".workspace-set-body").hidden, true);
  assert.equal(parentRow.closest("[hidden]") === null, true);
  assert.deepEqual(opened, ["parent-pane"]);
  assert.equal(requiredElement(app, ".workspace-expand-action").textContent, "Expand all");

  const quieter = structuredClone(current);
  quieter.home.workspaces[0].group_agent_counts = { blocked: 1 };
  const blockedBadge = requiredElement(parentRow, ".workspace-summary-blocked");
  render(view, quieter);
  assert.equal(disclosure.getAttribute("aria-expanded"), "false");
  assert.deepEqual(
    Array.from(parentRow.querySelectorAll(".workspace-summary-status"), (badge) => badge.textContent),
    ["1 blocked"],
  );
  assert.equal(requiredElement(parentRow, ".workspace-summary-blocked") === blockedBadge, true);

  requiredElement(app, ".workspace-expand-action").click();
  assert.equal(disclosure.getAttribute("aria-expanded"), "true");
  assert.equal(requiredElement(app, ".workspace-expand-action").textContent, "Collapse all");
  window.close();
});

test("multi-terminal controls work the same in a group heading and nested worktree row", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app, view } = makeView(window);
  render(view, state(groupedHome()));

  const parent = requiredWorkspaceRow(view, "parent");
  const nested = requiredWorkspaceRow(view, "quiet-worktree");
  assert.equal(parent.closest(".workspace-set-header") !== null, true);
  assert.equal(nested.closest(".workspace-set-contents") !== null, true);
  assert.equal(requiredElement(parent, ".terminal-count-badge").textContent, "2");
  assert.equal(requiredElement(nested, ".terminal-count-badge").textContent, "3");
  assert.equal(app.querySelectorAll(".terminal-section, .terminal-section-disclosure").length, 0);

  parent.click();
  assert.equal(requiredElement(app, ".terminal-picker-title").textContent, "Main project <script>");
  assert.deepEqual(
    Array.from(app.querySelectorAll(".terminal-picker-body .terminal-name"), (node) => node.textContent),
    ["Builder", "Shell & notes"],
  );
  requiredElement<HTMLButtonElement>(app, ".terminal-picker-close").click();

  requiredElement<HTMLButtonElement>(app, ".workspace-set-disclosure").click();
  assert.equal(requiredElement(app, ".workspace-set-body").hidden, true);
  assert.equal(parent.closest("[hidden]"), null);
  parent.click();
  assert.equal(requiredElement(app, ".terminal-picker-count").textContent, "2 terminals");
  requiredElement<HTMLButtonElement>(app, ".terminal-picker-close").click();

  requiredElement<HTMLButtonElement>(app, ".workspace-expand-action").click();
  nested.click();
  assert.equal(requiredElement(app, ".terminal-picker-title").textContent, "Quiet branch");
  assert.equal(app.querySelectorAll(".terminal-picker-body .terminal-row").length, 3);
  window.close();
});

test("Home filters visible names locally without changing group expansion or global attention", () => {
  const window = new Window({ url: "http://localhost/" });
  let blockedClicks = 0;
  const { app, view } = makeView(window, { onShowBlocked: () => blockedClicks++ });
  render(view, state(groupedHome()));

  const homeHeading = requiredElement(app, ".masthead h1");
  assert.equal(homeHeading.hidden, true);
  const filter = requiredElement(app, ".home-filter input") as HTMLInputElement;
  const expandAction = requiredElement(app, ".workspace-expand-action");
  const blockedAction = Array.from(app.querySelectorAll<HTMLButtonElement>(".attention-bar button"))
    .find((button) => !button.hidden && button.textContent === "1 blocked");
  assert.ok(blockedAction);
  blockedAction.click();
  assert.equal(blockedClicks, 1);

  filter.value = "Reviewer";
  filter.dispatchEvent(new window.Event("input", { bubbles: true }));
  assert.deepEqual(Array.from(app.querySelectorAll(".terminal-name"), (node) => node.textContent), ["Review branch"]);
  assert.equal(requiredElement(app, ".workspace-set-disclosure").getAttribute("aria-expanded"), "true");
  assert.equal(expandAction.hidden, true);
  assert.deepEqual(
    Array.from(app.querySelectorAll(".workspace-summary-status"), (badge) => badge.textContent),
    ["1 working", "1 blocked", "1 idle", "1 done", "1 unknown"],
  );
  assert.equal(requiredElement(app, ".attention-bar strong").textContent, "1 working");
  assert.equal(blockedAction.hidden, false);

  filter.value = "missing value";
  filter.dispatchEvent(new window.Event("input", { bubbles: true }));
  assert.equal(app.querySelectorAll(".terminal-row").length, 0);
  assert.equal(requiredElement(app, ".home-no-matches strong").textContent, "No matches");

  filter.value = "";
  filter.dispatchEvent(new window.Event("input", { bubbles: true }));
  assert.equal(app.querySelectorAll(".terminal-row").length, 3);
  assert.equal(requiredElement(app, ".workspace-set-disclosure").getAttribute("aria-expanded"), "true");
  assert.equal(expandAction.hidden, false);
  window.close();
});

test("Blocked keeps a matching multi-terminal workspace row and its complete picker", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app, view } = makeView(window);
  const home = groupedHome();
  home.blocked_count = 2;
  home.working_count = 0;
  home.workspaces[0].agent_counts = { blocked: 1 };
  home.workspaces[0].tabs[0].terminals[0].agent!.status = "blocked";

  render(view, state(home), { mode: "blocked" });

  assert.equal(app.querySelectorAll(".workspace-set-disclosure, .workspace-expand-action").length, 0);
  assert.equal(app.querySelectorAll(".terminal-row").length, 2);
  const parent = requiredWorkspaceRow(view, "parent");
  assert.equal(requiredElement(parent, ".terminal-count-badge").textContent, "2");
  assert.deepEqual(
    Array.from(parent.querySelectorAll(".workspace-summary-status"), (node) => node.textContent),
    ["1 blocked"],
  );
  parent.click();
  assert.deepEqual(
    Array.from(app.querySelectorAll(".terminal-picker-body .terminal-name"), (node) => node.textContent),
    ["Builder", "Shell & notes"],
  );
  window.close();
});

test("Blocked returns manually or automatically to the saved all-Home place", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app, view } = makeView(window);
  const current = state(groupedHome());
  render(view, current);
  const blockedRow = requiredRow(view, "blocked-pane");
  const parentRow = requiredWorkspaceRow(view, "parent");
  parentRow.focus();
  window.scrollTo(0, 333);
  requiredElement(app, ".workspace-set-disclosure").click();

  render(view, current, {
    mode: "blocked",
    restore: { focusPane: "blocked-pane", scroll: 0 },
  });
  assert.equal(app.querySelectorAll(".workspace-set-disclosure").length, 0);
  assert.equal(app.querySelectorAll(".workspace-expand-action").length, 0);
  assert.equal(app.querySelectorAll(".terminal-row").length, 1);
  assert.equal(requiredElement(app, ".terminal-name").textContent, "Review branch");
  assert.equal(view.row("blocked-pane") === blockedRow, true);
  assert.equal(window.document.activeElement === blockedRow, true);
  assert.equal(requiredElement(app, ".attention-bar button:not([hidden])").textContent, "Show all terminals");

  render(view, current, {
    mode: "all",
    restore: { focusWorkspace: "parent", scroll: 333 },
  });
  assert.equal(view.workspaceRow("parent") === parentRow, true);
  assert.equal(window.document.activeElement === parentRow, true);
  assert.equal(window.scrollY, 333);
  assert.equal(requiredElement(app, ".workspace-set-disclosure").getAttribute("aria-expanded"), "false");

  render(view, current, { mode: "blocked", restore: { focusPane: "blocked-pane", scroll: 0 } });
  const withoutBlocked = structuredClone(current);
  withoutBlocked.home.blocked_count = 0;
  withoutBlocked.home.workspaces[0].agent_counts = { done: 1, idle: 1, unknown: 1, working: 1 };
  delete withoutBlocked.home.workspaces[0].worktrees?.[0].tabs[0].terminals[0].agent;
  const automaticPlace = automaticAllTerminalsPlace("blocked", withoutBlocked.home.blocked_count, {
    focusWorkspace: "parent",
    scroll: 333,
  });
  render(view, withoutBlocked, { mode: automaticPlace ? "all" : "blocked", restore: automaticPlace });
  assert.equal(automaticPlace?.focusWorkspace, "parent");
  assert.equal(automaticPlace?.scroll, 333);
  assert.equal(window.document.activeElement === parentRow, true);
  assert.equal(window.scrollY, 333);
  assert.equal(requiredElement(app, ".workspace-set-disclosure").getAttribute("aria-expanded"), "false");
  window.close();
});

test("Home place restoration keeps a surviving row anchor without inventing focus", () => {
  const window = new Window({ url: "http://localhost/" });
  const { view } = makeView(window);
  const current = state();
  render(view, current);
  const row = requiredRow(view, "pane-one");
  Object.defineProperty(row, "getBoundingClientRect", { value: () => ({ top: 70 }) });
  render(view, current, {
    restore: { anchorTop: 50, pane: "pane-one", scroll: 200 },
  });
  assert.equal(window.scrollY, 220);
  assert.equal(window.document.activeElement === row, false);
  window.close();
});

test("With the Herdr app, Home says space, offers N error and Error, and has no space actions", () => {
  const window = new Window({ url: "http://localhost/" });
  const shown: string[] = [];
  const { app, view } = makeView(window, { onShowError: () => shown.push("error"), onShowBlocked: () => shown.push("blocked") });
  const current = state(coreHome());

  render(view, current, { herdr: "core" });
  const filter = requiredElement(app, ".home-filter input");
  assert.equal(filter.getAttribute("placeholder"), "Filter spaces and terminals");
  assert.equal(filter.getAttribute("aria-label"), "Filter spaces and terminals");
  assert.equal(requiredElement(app, ".herdr-version").textContent, "Herdr Core 0.8.0");
  assert.equal(app.querySelectorAll(".new-space-action, .workspace-menu, .terminal-menu").length, 0);
  assert.deepEqual(
    Array.from(app.querySelectorAll(".attention-bar > :not([hidden])"), (node) => node.textContent),
    ["0 working", "1 blocked", "1 error"],
  );
  assert.deepEqual(
    Array.from(requiredWorkspaceRow(view, "sp-alpha").querySelectorAll(".workspace-summary-status"), (node) => node.textContent),
    ["1 blocked", "1 error", "1 idle"],
  );
  assert.equal(requiredWorkspaceRow(view, "sp-alpha").querySelector("img"), null);
  assert.equal(requiredElement(app, ".attention-bar button:not([hidden])").textContent, "1 blocked");
  Array.from(app.querySelectorAll<HTMLElement>(".attention-bar button")).find((node) => node.textContent === "1 error")?.click();
  assert.deepEqual(shown, ["error"]);

  render(view, current, { herdr: "core", mode: "error" });
  assert.equal(requiredElement(app, "h1").textContent, "Error");
  // alpha lists its tabs; Error keeps only tab 2, which holds the error.
  assert.deepEqual(Array.from(app.querySelectorAll(".terminal-row .terminal-name"), (node) => node.textContent), ["2"]);
  assert.equal(requiredWorkspaceRow(view, tabRowKey("sp-alpha", "tb-two")).querySelector(".terminal-count-badge")?.textContent, "2");
  assert.deepEqual(
    Array.from(app.querySelectorAll(".attention-bar > :not([hidden])"), (node) => node.textContent),
    ["0 working", "Show all terminals"],
  );
  window.close();
});

test("The Herdr TUI keeps workspace words and never shows an error count", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app, view } = makeView(window);
  const home = coreHome();
  render(view, state(home));
  assert.equal(requiredElement(app, ".home-filter input").getAttribute("placeholder"), "Filter workspaces and terminals");
  assert.equal(requiredElement(app, ".herdr-version").textContent, "Herdr TUI 0.8.0");
  assert.deepEqual(
    Array.from(app.querySelectorAll(".attention-bar > :not([hidden])"), (node) => node.textContent),
    ["0 working", "1 blocked"],
  );
  window.close();
});

test("the Herdr switch shows only when asked, with fixed labels and the shown Herdr pressed", () => {
  const window = new Window({ url: "http://localhost/" });
  const picked: string[] = [];
  const { app, view } = makeView(window, { onShowHerdr: (herdr) => picked.push(herdr) });
  const current = state();
  current.herdr_version = "<b>0.9.5</b>";
  current.home.workspaces[0].label = "Herdr Core <img src=x>";

  render(view, current);
  assert.equal(app.querySelectorAll(".herdr-switch").length, 0);

  render(view, current, { herdrSwitch: true });
  const group = requiredElement(app, ".herdr-switch");
  assert.equal(app.children.item(1) === group, true, "the switch sits directly under the masthead");
  assert.equal(group.getAttribute("role"), "group");
  assert.equal(group.getAttribute("aria-label"), "Herdr");
  assert.deepEqual(
    Array.from(group.querySelectorAll("button"), (node) => [node.textContent, node.getAttribute("aria-pressed"), node.childElementCount]),
    [["Herdr TUI", "true", 0], ["Herdr Core", "false", 0]],
  );
  assert.equal(requiredElement(app, ".herdr-version").textContent, "Herdr TUI <b>0.9.5</b>");
  assert.equal(app.querySelectorAll("b, img").length, 0);

  Array.from(group.querySelectorAll<HTMLElement>("button"))[1].click();
  assert.deepEqual(picked, ["core"]);

  render(view, { ...current, connection: "not_running", last_known: true }, { herdr: "core", herdrSwitch: true, actionsAvailable: false });
  assert.deepEqual(
    Array.from(group.querySelectorAll("button"), (node) => node.getAttribute("aria-pressed")),
    ["false", "true"],
  );
  assert.equal(requiredElement(app, ".connection-label").textContent, "Herdr is not running");
  window.close();
});

test("showing the other Herdr rebuilds rows that share ids and clears the filter", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app, view } = makeView(window);
  const tui = state();
  render(view, tui, { herdrSwitch: true });
  const tuiRow = requiredRow(view, "pane-one");
  assert.equal(tuiRow.getAttribute("aria-label"), "Open Workspace one, workspace Workspace one, Agent one, blocked");
  const filter = requiredElement(app, ".home-filter input") as HTMLInputElement;
  filter.value = "Workspace";
  filter.dispatchEvent(new window.Event("input") as unknown as Event);
  assert.equal(filter.value, "Workspace");

  const core = state();
  core.home.workspaces[0].label = "alpha";
  render(view, core, { herdr: "core", herdrSwitch: true });
  const coreRow = requiredRow(view, "pane-one");
  assert.equal(coreRow === tuiRow, false);
  assert.equal(tuiRow.isConnected, false);
  assert.equal(coreRow.getAttribute("aria-label"), "Open Workspace one, space alpha, Agent one, blocked");
  assert.equal(filter.value, "");
  assert.equal(filter.getAttribute("placeholder"), "Filter spaces and terminals");
  window.close();
});

test("an agent with an empty name adds no empty part to its row or accessible name", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app, view } = makeView(window);
  const home = coreHome();
  const other = home.workspaces.find((workspace) => workspace.id === "other")!;
  const terminal = other.tabs[0].terminals[0];
  terminal.title = "Terminal";
  terminal.agent = { kind: "", name: "", status: "idle" };
  render(view, state(home), { herdr: "core" });
  const row = requiredRow(view, terminal.pane_id);
  const secondary = requiredElement(row, ".terminal-agent").textContent ?? "";
  assert.equal(secondary.includes("·"), false, secondary);
  assert.equal(secondary.trim(), secondary);
  const name = row.getAttribute("aria-label") ?? "";
  assert.equal(name.split(", ").some((part) => part.trim() === ""), false, name);
  assert.equal(name, "Open Terminal, space Other terminals, idle");
  assert.equal(app.querySelectorAll(".status-idle").length > 0, true);
  window.close();
});

function douroHome(): Home {
  const claude = (pane: string, status: "working" | "blocked" | "done" | "idle") => ({
    actions: ["split_terminal" as const, "close_terminal" as const],
    agent: { kind: "claude-code", name: "claude-code", status },
    pane_id: pane,
    terminal_id: `core:${pane}`,
    title: "claude-code",
  });
  const tab = (id: string, label: string, number: number, terminals: ReturnType<typeof claude>[]) => ({
    current: number === 1, id, label, number, terminals,
  });
  return {
    blocked_count: 0,
    error_count: 0,
    working_count: 1,
    workspaces: [
      {
        actions: ["rename_space", "close_workspace"],
        agent_counts: { working: 1, done: 4 },
        id: "sp-douro",
        label: "Douro",
        number: 1,
        tabs: [
          tab("tb-a", "Software 001 devenv", 1, [claude("t1", "done")]),
          tab("tb-b", "Ops setup", 2, [claude("t2", "done")]),
          tab("tb-c", "4", 3, [claude("t3", "working")]),
          tab("tb-d", "Hardware backplane", 4, [claude("t4", "done"), claude("t5", "done")]),
        ],
      },
      {
        actions: ["rename_space", "close_workspace"],
        agent_counts: { done: 1 },
        id: "sp-quiet",
        label: "Quiet",
        number: 2,
        tabs: [
          tab("tb-q1", "one", 1, [claude("t6", "done")]),
          tab("tb-q2", "two", 2, [{ ...claude("t7", "done"), agent: undefined, title: "zsh" } as unknown as ReturnType<typeof claude>]),
        ],
      },
      {
        actions: ["rename_space", "close_workspace"],
        agent_counts: { idle: 2 },
        id: "sp-split",
        label: "Split",
        number: 3,
        tabs: [tab("tb-s", "main", 1, [claude("t8", "idle"), claude("t9", "idle")])],
      },
    ],
  } as Home;
}

test("Herdr Core lists a space's several tabs under the space; one tab keeps its row", () => {
  const window = new Window({ url: "http://localhost/" });
  const opened: string[] = [];
  const { app, view } = makeView(window, { onOpen: (entry) => opened.push(entry.terminal.pane_id) });
  render(view, state(douroHome()), { herdr: "core" });

  const douro = requiredWorkspaceRow(view, "sp-douro");
  assert.equal(douro.getAttribute("aria-expanded"), "true");
  assert.equal(douro.getAttribute("aria-label"), "Collapse Douro tabs, 1 working, 4 done");
  assert.deepEqual(Array.from(douro.querySelectorAll(".workspace-summary-status"), (node) => node.textContent), ["1 working", "4 done"]);
  assert.equal(douro.parentElement?.querySelector(".workspace-menu-trigger")?.getAttribute("aria-label"), "Actions for Douro");
  const section = douro.closest(".space-tabs")!;
  assert.deepEqual(
    Array.from(section.querySelectorAll(".space-tabs-list .terminal-row"), (row) => [
      row.querySelector(".terminal-name")?.textContent,
      row.querySelector(".terminal-agent")?.textContent,
      row.querySelector(".status")?.textContent ?? "",
      row.querySelector(".terminal-count-badge")?.textContent ?? "",
      row.getAttribute("aria-label"),
    ]),
    [
      ["Software 001 devenv", "claude-code", "done", "", "Open Software 001 devenv, space Douro, claude-code, done"],
      ["Ops setup", "claude-code", "done", "", "Open Ops setup, space Douro, claude-code, done"],
      ["4", "claude-code", "working", "", "Open 4, space Douro, claude-code, working"],
      ["Hardware backplane", undefined, "", "2", "Choose a terminal in Hardware backplane, space Douro, 2 terminals, 2 done"],
    ],
  );
  // A tab's one terminal keeps its own actions; a split tab has no menu of its own.
  assert.deepEqual(
    Array.from(section.querySelectorAll(".space-tabs-list .workspace-menu-trigger"), (node) => node.getAttribute("aria-label")),
    ["Actions for Software 001 devenv", "Actions for Ops setup", "Actions for 4"],
  );
  requiredRow(view, "t3").click();
  assert.deepEqual(opened, ["t3"]);

  // The split tab's picker lists only that tab's terminals.
  requiredWorkspaceRow(view, tabRowKey("sp-douro", "tb-d")).click();
  assert.equal(requiredElement(app, ".terminal-picker-title").textContent, "Douro");
  assert.equal(requiredElement(app, ".terminal-picker-count").textContent, "2 terminals");
  assert.deepEqual(
    Array.from(app.querySelectorAll(".terminal-picker-body h3, .terminal-picker-body .terminal-name"), (node) => node.textContent),
    ["Hardware backplane", "claude-code", "claude-code"],
  );
  requiredElement(app, ".terminal-picker-close").click();

  // Only nonzero counts; a tab's plain terminal shows its title and no status.
  const quiet = requiredWorkspaceRow(view, "sp-quiet");
  assert.equal(quiet.getAttribute("aria-expanded"), "false");
  assert.equal(quiet.closest(".space-tabs")!.querySelector<HTMLElement>(".workspace-set-body")!.hidden, true);
  assert.equal(requiredRow(view, "t7").querySelector(".terminal-agent")?.textContent, "zsh");
  assert.equal(requiredRow(view, "t7").querySelector<HTMLElement>(".terminal-meta > :last-child")!.hidden, true);

  // One split tab: today's row and its whole-space picker.
  const split = requiredWorkspaceRow(view, "sp-split");
  assert.equal(split.classList.contains("workspace-terminal-trigger"), true);
  assert.equal(requiredElement(split, ".terminal-count-badge").textContent, "2");
  assert.equal(app.querySelectorAll(".space-tabs").length, 2);
  window.close();
});

test("Herdr Core tab lists open and close by choice, Expand all, the filter and Blocked", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app, view } = makeView(window);
  const current = state(douroHome());
  render(view, current, { herdr: "core" });
  const expandAction = requiredElement(app, ".workspace-expand-action");
  assert.equal(expandAction.hidden, false);
  assert.equal(expandAction.textContent, "Expand all");

  requiredWorkspaceRow(view, "sp-douro").click();
  assert.equal(requiredWorkspaceRow(view, "sp-douro").getAttribute("aria-expanded"), "false");
  render(view, current, { herdr: "core" });
  assert.equal(requiredWorkspaceRow(view, "sp-douro").getAttribute("aria-expanded"), "false");

  expandAction.click();
  assert.deepEqual(
    ["sp-douro", "sp-quiet"].map((id) => requiredWorkspaceRow(view, id).getAttribute("aria-expanded")),
    ["true", "true"],
  );
  assert.equal(expandAction.textContent, "Collapse all");
  expandAction.click();
  assert.deepEqual(
    ["sp-douro", "sp-quiet"].map((id) => requiredWorkspaceRow(view, id).getAttribute("aria-expanded")),
    ["false", "false"],
  );

  // A tab name match shows its closed space open with only that row, and keeps the choice.
  const filter = requiredElement(app, ".home-filter input") as HTMLInputElement;
  filter.value = "ops";
  filter.dispatchEvent(new window.Event("input", { bubbles: true }));
  assert.equal(requiredWorkspaceRow(view, "sp-douro").getAttribute("aria-expanded"), "true");
  assert.deepEqual(Array.from(app.querySelectorAll(".terminal-row .terminal-name"), (node) => node.textContent), ["Ops setup"]);
  assert.equal(expandAction.hidden, true);
  filter.value = "";
  filter.dispatchEvent(new window.Event("input", { bubbles: true }));
  assert.equal(requiredWorkspaceRow(view, "sp-douro").getAttribute("aria-expanded"), "false");

  // Blocked shows the space open with only the tabs that hold a blocked agent.
  const blocked = douroHome();
  blocked.blocked_count = 1;
  blocked.workspaces[0].agent_counts = { working: 1, blocked: 1, done: 3 };
  blocked.workspaces[0].tabs[1].terminals[0].agent!.status = "blocked";
  render(view, state(blocked), { herdr: "core", mode: "blocked" });
  // There the header is a plain heading: nothing to open or close.
  const header = requiredElement(app, ".space-tabs-header");
  assert.equal(header.querySelectorAll("button").length, 0);
  assert.equal(header.querySelectorAll(".workspace-set-marker").length, 0);
  assert.deepEqual(
    [requiredElement(header, ".workspace-set-title").textContent, ...Array.from(header.querySelectorAll(".workspace-summary-status"), (node) => node.textContent)],
    ["Douro", "1 working", "1 blocked", "3 done"],
  );
  assert.equal(requiredElement(app, ".space-tabs .workspace-set-body").hidden, false);
  assert.deepEqual(Array.from(app.querySelectorAll(".terminal-row .terminal-name"), (node) => node.textContent), ["Ops setup"]);
  assert.equal(app.querySelectorAll(".workspace-menu").length, 0);
  // Leaving Blocked keeps Douro closed, as it was chosen.
  render(view, state(blocked), { herdr: "core" });
  assert.equal(requiredWorkspaceRow(view, "sp-douro").getAttribute("aria-expanded"), "false");
  assert.equal(requiredWorkspaceRow(view, "sp-douro").isConnected, true);
  window.close();
});

function focusedName(window: Window): string {
  const active = window.document.activeElement;
  return active ? `${active.localName}:${active.getAttribute("aria-label") ?? ""}` : "none";
}

function douroTabLabels(app: HTMLElement): Array<string | null | undefined> {
  return Array.from(app.querySelectorAll(".space-tabs-list .terminal-name"), (node) => node.textContent);
}

test("Herdr Core keeps focus on a tab's row when the tab gains or loses a pane", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app, view } = makeView(window);
  const home = douroHome();
  render(view, state(home), { herdr: "core" });
  requiredRow(view, "t2").focus();
  assert.equal(focusedName(window), "button:Open Ops setup, space Douro, claude-code, done");

  // Ops setup gains a pane: focus moves to the tab's new picker row.
  const split = douroHome();
  const opsTab = split.workspaces[0].tabs[1];
  opsTab.terminals.push({ ...opsTab.terminals[0], agent: undefined, pane_id: "t10", terminal_id: "core:t10", title: "zsh" });
  render(view, state(split), { herdr: "core" });
  assert.equal(focusedName(window), "button:Choose a terminal in Ops setup, space Douro, 2 terminals, 1 done");

  // It drops back to one terminal with its picker closed: focus moves to the terminal's row.
  render(view, state(home), { herdr: "core" });
  assert.equal(focusedName(window), "button:Open Ops setup, space Douro, claude-code, done");

  // Focus on the tab's menu button stays there while nothing changes the row.
  requiredElement(requiredRow(view, "t2").parentElement!, ".workspace-menu-trigger").focus();
  render(view, state(home), { herdr: "core" });
  assert.equal(focusedName(window), "button:Actions for Ops setup");
  render(view, state(split), { herdr: "core" });
  assert.equal(focusedName(window), "button:Choose a terminal in Ops setup, space Douro, 2 terminals, 1 done");
  window.close();
});

test("Herdr Core returns a picker's focus to the space's row after the space gains or loses tabs", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app, view } = makeView(window);
  // Split has one split tab: its row opens the whole-space picker.
  const oneTab = douroHome();
  render(view, state(oneTab), { herdr: "core" });
  requiredWorkspaceRow(view, "sp-split").click();
  assert.equal(requiredElement(app, ".terminal-picker-layer").hidden, false);

  // It gains a working tab: the picker closes and focus goes to the split tab's row.
  const twoTabs = douroHome();
  twoTabs.workspaces[2].agent_counts = { idle: 2, working: 1 };
  twoTabs.workspaces[2].tabs.push({
    current: false,
    id: "tb-s2",
    label: "second",
    number: 2,
    terminals: [{ ...twoTabs.workspaces[2].tabs[0].terminals[0], agent: { kind: "claude-code", name: "claude-code", status: "working" }, pane_id: "t11", terminal_id: "core:t11" }],
  });
  render(view, state(twoTabs), { herdr: "core" });
  assert.equal(requiredElement(app, ".terminal-picker-layer").hidden, true);
  assert.equal(focusedName(window), "button:Choose a terminal in main, space Split, 2 terminals, 2 idle");

  // Its tab picker is open when the space drops back to one tab: the picker
  // stays, and closing it goes to the space's row.
  requiredWorkspaceRow(view, tabRowKey("sp-split", "tb-s")).click();
  assert.equal(focusedName(window), "button:Close terminal picker");
  render(view, state(oneTab), { herdr: "core" });
  assert.equal(requiredElement(app, ".terminal-picker-layer").hidden, false);
  assert.equal(focusedName(window), "button:Close terminal picker");
  assert.deepEqual(
    Array.from(app.querySelectorAll(".terminal-picker-body .terminal-name"), (node) => node.textContent),
    ["claude-code", "claude-code"],
  );
  requiredElement(app, ".terminal-picker-close").click();
  assert.equal(focusedName(window), "button:Choose a terminal in Split, 2 terminals, 2 idle");

  // A closed space that gains a tab while its picker is open returns focus to its header.
  const quietTwoTabs = douroHome();
  const quietOne = douroHome();
  quietOne.workspaces[1].tabs = [quietOne.workspaces[1].tabs[0]];
  quietOne.workspaces[1].tabs[0].terminals.push({ ...quietOne.workspaces[1].tabs[0].terminals[0], pane_id: "t12", terminal_id: "core:t12" });
  render(view, state(quietOne), { herdr: "core" });
  requiredWorkspaceRow(view, "sp-quiet").click();
  quietTwoTabs.workspaces[1].tabs[0].terminals.push({ ...quietTwoTabs.workspaces[1].tabs[0].terminals[0], pane_id: "t12", terminal_id: "core:t12" });
  render(view, state(quietTwoTabs), { herdr: "core" });
  assert.equal(requiredElement(app, ".terminal-picker-layer").hidden, true);
  assert.equal(focusedName(window), "button:Expand Quiet tabs, 1 done");
  window.close();
});

test("Herdr Core keeps focus with a space whose row changes form as it gains or loses tabs", () => {
  const window = new Window({ url: "http://localhost/" });
  const { view } = makeView(window);
  const twoTabs = douroHome();
  const oneTab = douroHome();
  oneTab.workspaces[1].tabs = [oneTab.workspaces[1].tabs[0]];
  render(view, state(twoTabs), { herdr: "core" });

  // The header holds focus when Quiet drops to one tab: focus goes to its one row.
  requiredWorkspaceRow(view, "sp-quiet").focus();
  render(view, state(oneTab), { herdr: "core" });
  assert.equal(focusedName(window), "button:Open claude-code, space Quiet, done");

  // That row holds focus when Quiet gains its tab back, closed: focus goes to the header.
  render(view, state(twoTabs), { herdr: "core" });
  assert.equal(focusedName(window), "button:Expand Quiet tabs, 1 done");

  // Opened, with the first tab's row focused, the space moves to the top and keeps it.
  requiredWorkspaceRow(view, "sp-quiet").click();
  requiredRow(view, "t6").focus();
  const moved = douroHome();
  moved.workspaces = [moved.workspaces[1], moved.workspaces[0], moved.workspaces[2]];
  render(view, state(moved), { herdr: "core" });
  assert.equal(focusedName(window), "button:Open one, space Quiet, claude-code, done");
  window.close();
});

test("Herdr Core follows renamed and reordered tabs while a space is open", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app, view } = makeView(window);
  render(view, state(douroHome()), { herdr: "core" });
  requiredRow(view, "t2").focus();

  const changed = douroHome();
  const tabs = changed.workspaces[0].tabs;
  tabs[1].label = "Ops review";
  changed.workspaces[0].tabs = [tabs[1], tabs[0], tabs[2], tabs[3]];
  render(view, state(changed), { herdr: "core" });
  assert.deepEqual(douroTabLabels(app).slice(0, 4), ["Ops review", "Software 001 devenv", "4", "Hardware backplane"]);
  assert.equal(focusedName(window), "button:Open Ops review, space Douro, claude-code, done");

  // Moving the focused tab down the list keeps focus on it.
  changed.workspaces[0].tabs = [tabs[0], tabs[2], tabs[3], tabs[1]];
  render(view, state(changed), { herdr: "core" });
  assert.deepEqual(douroTabLabels(app).slice(0, 4), ["Software 001 devenv", "4", "Hardware backplane", "Ops review"]);
  assert.equal(focusedName(window), "button:Open Ops review, space Douro, claude-code, done");
  window.close();
});

test("The Herdr TUI keeps one row with a picker for a workspace with several tabs", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app, view } = makeView(window);
  render(view, state(douroHome()));
  assert.equal(app.querySelectorAll(".space-tabs").length, 0);
  assert.equal(requiredElement(requiredWorkspaceRow(view, "sp-douro"), ".terminal-count-badge").textContent, "5");
  assert.equal(requiredElement(app, ".workspace-expand-action").hidden, true);
  window.close();
});

function pinnedDouroHome(): Home {
  const home = douroHome();
  for (const workspace of home.workspaces) {
    for (const tab of workspace.tabs) {
      for (const terminal of tab.terminals) {
        if (!terminal.agent) continue;
        terminal.actions = [["t2", "t5", "t8"].includes(terminal.pane_id) ? "unpin_agent" : "pin_agent", ...terminal.actions];
        if (["t2", "t5", "t8"].includes(terminal.pane_id)) terminal.agent.pinned = true;
      }
    }
  }
  return home;
}

test("Herdr Core marks pinned agents, counts them, and Pinned lists only them under plain space headings", () => {
  const window = new Window({ url: "http://localhost/" });
  const shown: string[] = [];
  const opened: string[] = [];
  const { app, view } = makeView(window, {
    onShowPinned: () => shown.push("pinned"),
    onOpen: (entry) => opened.push(entry.terminal.pane_id),
  });
  render(view, state(pinnedDouroHome()), { herdr: "core" });

  const mark = requiredRow(view, "t2").querySelector(".terminal-pin");
  assert.equal(mark?.getAttribute("role"), "img");
  assert.equal(mark?.getAttribute("aria-label"), "Pinned");
  assert.equal(requiredRow(view, "t2").getAttribute("aria-label"), "Open Ops setup, space Douro, claude-code, done, pinned");
  assert.equal(requiredRow(view, "t1").querySelector(".terminal-pin"), null);
  // The split tab's picker marks its pinned terminal.
  requiredWorkspaceRow(view, tabRowKey("sp-douro", "tb-d")).click();
  assert.deepEqual(["t4", "t5"].map((pane) => requiredRow(view, pane).querySelector(".terminal-pin") !== null), [false, true]);
  requiredElement(app, ".terminal-picker-close").click();
  assert.deepEqual(
    Array.from(app.querySelectorAll(".attention-bar > :not([hidden])"), (node) => node.textContent),
    ["1 working", "3 pinned"],
  );
  requiredElement(app, ".attention-pinned").click();
  assert.deepEqual(shown, ["pinned"]);

  render(view, state(pinnedDouroHome()), { herdr: "core", mode: "pinned" });
  assert.equal(requiredElement(app, "h1").textContent, "Pinned");
  assert.deepEqual(
    Array.from(app.querySelectorAll(".pinned-space"), (section) => [
      section.querySelector("h2")?.textContent,
      ...Array.from(section.querySelectorAll(".terminal-row .terminal-name"), (node) => node.textContent),
    ]),
    [["Douro", "Ops setup", "claude-code"], ["Split", "claude-code"]],
  );
  assert.equal(app.querySelectorAll(".workspace-menu-trigger, .terminal-count-badge").length, 0);
  assert.deepEqual(
    Array.from(app.querySelectorAll(".attention-bar > :not([hidden])"), (node) => node.textContent),
    ["1 working", "Show all terminals"],
  );
  requiredRow(view, "t5").click();
  assert.deepEqual(opened, ["t5"]);

  // Back on Home every terminal appears once, in Home's order.
  render(view, state(pinnedDouroHome()), { herdr: "core" });
  assert.equal(app.querySelectorAll(".pinned-space").length, 0);
  assert.equal(app.querySelectorAll('[data-pane-key="t2"]').length, 1);
  window.close();
});

test("Pin and Unpin run at once from an agent's menu, and the Herdr TUI shows no pins", async () => {
  const window = new Window({ url: "http://localhost/" });
  const requests: unknown[] = [];
  const { app, view } = makeView(window, {
    runTerminalAction: async (request) => { requests.push(request); return { outcome: "succeeded" }; },
  });
  render(view, state(pinnedDouroHome()), { herdr: "core" });
  const menuItems = (pane: string) => {
    const trigger = requiredRow(view, pane).parentElement!.querySelector<HTMLElement>(".workspace-menu-trigger")!;
    trigger.click();
    return Array.from(trigger.parentElement!.querySelectorAll<HTMLElement>("[role=menuitem]"));
  };
  const items = menuItems("t1");
  assert.deepEqual(items.map((node) => node.textContent), ["Pin", "New terminal", "Close terminal"]);
  items[0].click();
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.deepEqual(requests, [{
    herdr: "core", action: "pin_agent", workspace_id: "sp-douro", tab_id: "tb-a", pane_id: "t1", terminal_id: "core:t1",
  }]);
  assert.equal(app.querySelector(".home-action-layer:not([hidden])"), null);
  assert.equal(menuItems("t2")[0].textContent, "Unpin");

  const tui = pinnedDouroHome();
  for (const workspace of tui.workspaces) {
    for (const tab of workspace.tabs) for (const terminal of tab.terminals) {
      terminal.actions = terminal.actions.filter((action) => action !== "pin_agent" && action !== "unpin_agent");
    }
  }
  render(view, state(tui), { herdr: "tui" });
  assert.equal(app.querySelectorAll(".attention-pinned").length, 0);
  window.close();
});

import assert from "node:assert/strict";
import test from "node:test";

import { Window } from "happy-dom";

import { HomeView, type HomeViewRender } from "./home-view";
import { automaticAllTerminalsPlace, type Home, type HomeState } from "./home-model";

function flatHome(): Home {
  return {
    blocked_count: 1,
    working_count: 0,
    workspaces: [
      {
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
        agent_counts: { blocked: 1, done: 1, idle: 1, unknown: 1, working: 1 },
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
                agent: { kind: "codex", name: "Builder", status: "working" },
                pane_id: "parent-pane",
                terminal_id: "parent-terminal",
                title: "Builder",
              },
              {
                pane_id: "ordinary-pane",
                terminal_id: "ordinary-terminal",
                title: "Shell & notes",
              },
            ],
          },
        ],
        worktrees: [
          {
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
                    agent: { kind: "codex", name: "Finished", status: "done" },
                    pane_id: "quiet-pane",
                    terminal_id: "quiet-terminal",
                    title: "Quiet branch",
                  },
                  {
                    agent: { kind: "codex", name: "Waiting", status: "idle" },
                    pane_id: "idle-pane",
                    terminal_id: "idle-terminal",
                    title: "Waiting",
                  },
                  {
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
    connection,
    gap: 2,
    has_home: true,
    home: value,
    last_known: connection !== "live",
  };
}

function makeView(window: Window, actions: Partial<ConstructorParameters<typeof HomeView>[1]> = {}) {
  const app = window.document.createElement("main");
  window.document.body.append(app);
  const view = new HomeView(app, {
    onFocusPane: actions.onFocusPane ?? (() => undefined),
    onOpen: actions.onOpen ?? (() => undefined),
    onReconnect: actions.onReconnect ?? (() => undefined),
    onShowAll: actions.onShowAll ?? (() => undefined),
    onShowBlocked: actions.onShowBlocked ?? (() => undefined),
  });
  return { app, view };
}

function render(view: HomeView, next: HomeState, overrides: Partial<HomeViewRender> = {}): void {
  view.render({
    actionsAvailable: next.connection === "live" && !next.last_known && next.has_home,
    mode: "all",
    reachability: "current",
    state: next,
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

test("Home shows one real initial or unavailable state with the transport-owned badge", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app, view } = makeView(window);
  const loading: HomeState = {
    connection: "reconnecting",
    gap: 0,
    has_home: false,
    home: { blocked_count: 0, working_count: 0, workspaces: [] },
    last_known: false,
  };

  render(view, loading, { actionsAvailable: false });
  const connection = requiredElement(app, ".home-connection");
  assert.equal(requiredElement(connection, ".connection-label").textContent, "Reconnecting");
  assert.equal(requiredElement(connection, ".connection-label").localName, "span");
  assert.equal(requiredElement(connection, ".connection-dot").className, "connection-dot connection-dot-reconnecting");
  assert.equal(connection.childElementCount, 2);
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
  let reconnects = 0;
  const { app, view } = makeView(window, {
    onOpen: () => opens++,
    onReconnect: () => reconnects++,
  });
  const current = state();
  render(view, current);
  const connection = requiredElement(app, ".home-connection");
  const reconnectAction = requiredElement(connection, "button") as HTMLButtonElement;
  const row = requiredRow(view, "pane-one");
  assert.equal(connection.childElementCount, 2);
  assert.equal(reconnectAction.hidden, true);
  row.click();
  assert.equal(opens, 1);

  const stale = { ...current, connection: "reconnecting" as const, last_known: true };
  render(view, stale, { actionsAvailable: false, reachability: "reconnecting" });
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
  assert.equal(reconnectAction.hidden, true);
  assert.doesNotMatch(connection.textContent ?? "", /State below|fresh view|Values below/);
  unavailableRow.click();
  assert.equal(opens, 1);

  render(view, stale, { actionsAvailable: false, reachability: "offline" });
  assert.equal(requiredElement(connection, ".connection-label").textContent, "Offline");
  assert.equal(requiredElement(connection, ".connection-dot").className, "connection-dot connection-dot-offline");
  assert.equal(connection.childElementCount, 2);
  assert.equal(reconnectAction.hidden, false);
  reconnectAction.click();
  assert.equal(reconnects, 1);
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
              { pane_id: "first-a", terminal_id: "first-terminal-a", title: "First A" },
              { pane_id: "first-b", terminal_id: "first-terminal-b", title: "First B" },
            ],
          },
        ],
      },
      {
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
              { pane_id: "second-a", terminal_id: "second-terminal-a", title: "Second A" },
              { pane_id: "second-b", terminal_id: "second-terminal-b", title: "Second B" },
            ],
          },
        ],
      },
    ],
  };

  render(view, state(collidingHome));

  assert.equal(app.querySelectorAll("section.workspace").length, 2);
  assert.equal(app.querySelectorAll(".terminal-row").length, 4);
  assert.deepEqual(
    Array.from(app.querySelectorAll(".terminal-name"), (node) => node.textContent),
    ["First A", "First B", "Second A", "Second B"],
  );
  window.close();
});

test("worktree sets disclose exact ordered totals and manual choices win for the visit", () => {
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
  assert.equal(disclosure.getAttribute("aria-label"), "Collapse Main project <script> workspaces");
  assert.equal(
    requiredElement(app, ".workspace-set-meta").textContent,
    "3 workspaces · 1 working · 1 blocked · 1 idle · 1 done · 1 unknown",
  );
  assert.doesNotMatch(disclosure.textContent ?? "", /Open/);
  assert.equal(disclosure.textContent, "⌄");
  assert.equal(parentRow.localName, "button");
  assert.equal(parentRow.getAttribute("aria-label"), "Open Builder, workspace Main project <script>, working");
  assert.equal(requiredElement(parentRow, ".status").textContent, "working");
  assert.equal(app.querySelectorAll(".terminal-row").length, 5);
  assert.equal(requiredElement(app, ".workspace-expand-action").textContent, "Collapse all");
  assert.equal(app.querySelector("script") === null, true);

  parentRow.click();
  assert.deepEqual(opened, ["parent-pane"]);
  disclosure.click();
  assert.equal(disclosure.getAttribute("aria-expanded"), "false");
  assert.equal(disclosure.getAttribute("aria-label"), "Expand Main project <script> workspaces");
  assert.equal(requiredElement(app, ".workspace-set-contents").hidden, true);
  assert.equal(parentRow.closest("[hidden]") === null, true);
  assert.deepEqual(opened, ["parent-pane"]);
  assert.equal(requiredElement(app, ".workspace-expand-action").textContent, "Expand all");

  const quieter = structuredClone(current);
  quieter.home.workspaces[0].agent_counts = { done: 5 };
  render(view, quieter);
  assert.equal(disclosure.getAttribute("aria-expanded"), "false");
  assert.equal(requiredElement(app, ".workspace-set-meta").textContent, "3 workspaces · 5 done");

  requiredElement(app, ".workspace-expand-action").click();
  assert.equal(disclosure.getAttribute("aria-expanded"), "true");
  assert.equal(requiredElement(app, ".workspace-expand-action").textContent, "Collapse all");
  window.close();
});

test("Blocked returns manually or automatically to the saved all-Home place", () => {
  const window = new Window({ url: "http://localhost/" });
  const { app, view } = makeView(window);
  const current = state(groupedHome());
  render(view, current);
  const blockedRow = requiredRow(view, "blocked-pane");
  const ordinaryRow = requiredRow(view, "ordinary-pane");
  ordinaryRow.focus();
  window.scrollTo(0, 333);
  requiredElement(app, ".workspace-set-disclosure").click();

  render(view, current, {
    mode: "blocked",
    restore: { focusPane: "blocked-pane", scroll: 0 },
  });
  assert.equal(app.querySelectorAll(".workspace-set-disclosure").length, 0);
  assert.equal(app.querySelectorAll(".workspace-expand-action").length, 0);
  assert.equal(app.querySelectorAll(".terminal-row").length, 1);
  assert.equal(app.querySelectorAll("section.workspace > .workspace-title").length, 1);
  assert.equal(requiredElement(app, "section.workspace > .workspace-title").textContent, "Review branch");
  assert.equal(view.row("blocked-pane") === blockedRow, true);
  assert.equal(window.document.activeElement === blockedRow, true);
  assert.equal(requiredElement(app, ".attention-bar button:not([hidden])").textContent, "Show all terminals");

  render(view, current, {
    mode: "all",
    restore: { focusPane: "ordinary-pane", scroll: 333 },
  });
  assert.equal(view.row("ordinary-pane") === ordinaryRow, true);
  assert.equal(window.document.activeElement === ordinaryRow, true);
  assert.equal(window.scrollY, 333);
  assert.equal(requiredElement(app, ".workspace-set-disclosure").getAttribute("aria-expanded"), "false");

  render(view, current, { mode: "blocked", restore: { focusPane: "blocked-pane", scroll: 0 } });
  const withoutBlocked = structuredClone(current);
  withoutBlocked.home.blocked_count = 0;
  withoutBlocked.home.workspaces[0].agent_counts = { done: 1, idle: 1, unknown: 1, working: 1 };
  delete withoutBlocked.home.workspaces[0].worktrees?.[0].tabs[0].terminals[0].agent;
  const automaticPlace = automaticAllTerminalsPlace("blocked", withoutBlocked.home.blocked_count, {
    focusPane: "ordinary-pane",
    scroll: 333,
  });
  render(view, withoutBlocked, { mode: automaticPlace ? "all" : "blocked", restore: automaticPlace });
  assert.equal(automaticPlace?.focusPane, "ordinary-pane");
  assert.equal(automaticPlace?.scroll, 333);
  assert.equal(window.document.activeElement === ordinaryRow, true);
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

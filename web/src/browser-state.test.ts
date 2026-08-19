import assert from "node:assert/strict";
import test from "node:test";

import { Window } from "happy-dom";

import { HomeView } from "./home-view";
import { type Home, type HomeState } from "./home-model";
import { returningToHome } from "./terminal-route";

function home(): Home {
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

function state(connection: HomeState["connection"] = "live", value = home()): HomeState {
  return { connection, gap: 2, home: value, last_known: connection !== "live" };
}

test("current Home truth is not overridden by a false offline hint", () => {
  const paneID = "pane-one";
  const window = new Window({ url: "http://localhost/" });
  const app = window.document.createElement("main");
  window.document.body.append(app);
  const view = new HomeView(app, {
    onFocusPane: () => undefined,
    onOpen: () => undefined,
    onReconnect: () => undefined,
    onShowAll: () => undefined,
    onShowBlocked: () => undefined,
  });
  view.render({
    actionsAvailable: true,
    mode: "all",
    reachability: "current",
    receivedHome: true,
    state: state(),
  });
  assert.match(app.textContent ?? "", /Live/);
  assert.doesNotMatch(app.textContent ?? "", /Offline|Live actions are unavailable/);
  assert.equal(view.row(paneID)?.tagName, "BUTTON");
  assert.equal(app.querySelectorAll('[role="status"]').length, 1);
  window.close();
});

test("Home reserves its connection region without status copy until the first complete frame", () => {
  const window = new Window({ url: "http://localhost/" });
  const app = window.document.createElement("main");
  window.document.body.append(app);
  const view = new HomeView(app, {
    onFocusPane: () => undefined,
    onOpen: () => undefined,
    onReconnect: () => undefined,
    onShowAll: () => undefined,
    onShowBlocked: () => undefined,
  });
  view.render({
    actionsAvailable: false,
    mode: "all",
    reachability: "current",
    receivedHome: false,
    state: {
      connection: "reconnecting",
      gap: 0,
      home: { blocked_count: 0, working_count: 0, workspaces: [] },
      last_known: false,
    },
  });

  const connection = app.querySelector(".home-connection");
  assert.ok(connection);
  assert.doesNotMatch(connection.textContent ?? "", /Reconnecting/);
  assert.equal(connection.querySelector("strong")?.textContent, "");
  assert.equal(connection.querySelector("p")?.textContent, "");

  view.render({
    actionsAvailable: true,
    mode: "all",
    reachability: "current",
    receivedHome: true,
    state: state(),
  });
  assert.equal(app.querySelector(".home-connection"), connection);
  assert.match(connection.textContent ?? "", /^Live/);
  window.close();
});

test("Home transport states keep one stable connection region and stale values", async () => {
  const window = new Window({ url: "http://localhost/" });
  const app = window.document.createElement("main");
  window.document.body.append(app);
  let opens = 0;
  let reconnects = 0;
  const view = new HomeView(app, {
    onFocusPane: () => undefined,
    onOpen: () => opens++,
    onReconnect: () => reconnects++,
    onShowAll: () => undefined,
    onShowBlocked: () => undefined,
  });
  const current = state();
  const render = (reachability: "current" | "offline" | "reconnecting", next = current) =>
    view.render({
      actionsAvailable: reachability === "current" && next.connection === "live" && !next.last_known,
      mode: "all",
      reachability,
      receivedHome: true,
      state: next,
    });

  render("current");
  const connection = app.querySelector(".home-connection");
  assert.ok(connection);
  assert.match(connection.textContent ?? "", /^Live/);
  const row = view.row("pane-one");
  assert.ok(row);
  assert.equal(row.tagName, "BUTTON");
  assert.equal(row.getAttribute("aria-disabled"), "false");
  row.focus();
  row.click();
  assert.equal(opens, 1);

  for (let now = 2_000; now <= 120_000; now += 2_000) {
    const changing = structuredClone(current);
    const agent = changing.home.workspaces[0].tabs[0].terminals[0].agent;
    assert.ok(agent);
    agent.status = now % 4_000 === 0 ? "working" : "blocked";
    changing.home.blocked_count = agent.status === "blocked" ? 1 : 0;
    changing.home.working_count = agent.status === "working" ? 1 : 0;
    render("current", changing);
    assert.match(connection.textContent ?? "", /^Live/);
    assert.doesNotMatch(connection.textContent ?? "", /Reconnecting/);
    assert.equal(view.row("pane-one"), row);
  }

  const stale = { ...current, connection: "reconnecting" as const, last_known: true };
  render("reconnecting", stale);
  assert.equal(app.querySelector(".home-connection"), connection);
  assert.equal(app.querySelectorAll(".home-connection").length, 1);
  assert.match(connection.textContent ?? "", /ReconnectingState below may be stale\./);
  assert.equal(view.row("pane-one"), row, "transport changes must retain the keyed row");
  assert.equal(row.getAttribute("aria-disabled"), "true");
  assert.equal(window.document.activeElement, row, "transport changes must not discard row focus");
  row.click();
  assert.equal(opens, 1, "stale rows cannot open a terminal");

  let mutations = 0;
  const observer = new window.MutationObserver((records) => {
    mutations += records.length;
  });
  observer.observe(app, { attributes: true, characterData: true, childList: true, subtree: true });
  render("reconnecting", stale);
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(app.querySelector(".home-connection"), connection, "another attempt must not replace the notice");
  assert.equal(view.row("pane-one"), row, "another attempt must retain the stale row");
  assert.equal(row.getAttribute("aria-disabled"), "true", "another attempt must not make stale values live");
  assert.equal(mutations, 0, "another attempt must not mutate the stable Home view");
  observer.disconnect();

  render("current", stale);
  assert.match(connection.textContent ?? "", /Reconnecting/, "heartbeat-only traffic cannot recover stale Home truth");
  assert.equal(row.getAttribute("aria-disabled"), "true");

  render("offline", stale);
  assert.match(connection.textContent ?? "", /OfflineState below may be stale\.Reconnect/);
  const reconnect = Array.from(connection.querySelectorAll("button")).find((node) => node.textContent === "Reconnect");
  assert.ok(reconnect);
  reconnect.click();
  assert.equal(reconnects, 1);

  render("current", current);
  assert.match(connection.textContent ?? "", /^Live/);
  assert.equal(view.row("pane-one"), row);
  assert.equal(row.getAttribute("aria-disabled"), "false");
  row.click();
  assert.equal(opens, 2, "real recovery restores the existing row action");
  window.close();
});

test("browser Back from a pane route restores Home place", () => {
  assert.equal(returningToHome("pane-one", undefined), true);
  assert.equal(returningToHome("pane-one", "pane-two"), false);
  assert.equal(returningToHome(undefined, undefined), false);
});

test("keyed Home updates preserve rows, DOM focus, scroll, and unchanged render stability", async () => {
  const window = new Window({ url: "http://localhost/" });
  const app = window.document.createElement("main");
  window.document.body.append(app);
  const focused: string[] = [];
  const view = new HomeView(app, {
    onFocusPane: (paneID) => focused.push(paneID),
    onOpen: () => undefined,
    onReconnect: () => undefined,
    onShowAll: () => undefined,
    onShowBlocked: () => undefined,
  });
  const render = (next: HomeState, restore?: { focusPane?: string; scroll: number }) =>
    view.render({
      actionsAvailable: true,
      mode: "all",
      reachability: "current",
      receivedHome: true,
      restore,
      state: next,
    });

  const firstState = state();
  render(firstState);
  const originalRow = view.row("pane-one");
  assert.ok(originalRow);
  originalRow.focus();
  window.scrollTo(0, 240);
  assert.equal(window.document.activeElement, originalRow);

  let mutations = 0;
  const mutationDetails: string[] = [];
  const observer = new window.MutationObserver((records) => {
    mutations += records.length;
    mutationDetails.push(...records.map((record) => `${record.type}:${record.attributeName ?? record.target.nodeName}`));
  });
  observer.observe(app, { attributes: true, characterData: true, childList: true, subtree: true });
  render(firstState);
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(mutations, 0, `an unchanged semantic render should not mutate Home: ${mutationDetails.join(", ")}`);

  const semantic = structuredClone(firstState);
  const changed = semantic.home.workspaces[0].tabs[0].terminals[0];
  changed.title = "Renamed agent";
  changed.agent = { kind: "codex", name: "Replacement", status: "working" };
  semantic.home.blocked_count = 0;
  semantic.home.working_count = 1;
  render(semantic);
  assert.equal(view.row("pane-one"), originalRow);
  assert.equal(window.document.activeElement, originalRow);
  assert.equal(window.scrollY, 240);
  assert.match(originalRow.textContent ?? "", /Renamed agent.*Replacement.*working/);
  assert.equal(originalRow.getAttribute("aria-label"), "Open Renamed agent, workspace Workspace one, Replacement, working");
  assert.equal(originalRow.querySelector(".status")?.textContent, "working");

  originalRow.blur();
  const topology = structuredClone(semantic);
  topology.home.workspaces[0].tabs[0].terminals.push({
    pane_id: "pane-two",
    terminal_id: "terminal-two",
    title: "Ordinary shell",
  });
  render(topology);
  assert.equal(view.row("pane-one"), originalRow, "topology changes must move rather than recreate the existing row");
  assert.equal(window.scrollY, 240);
  const ordinaryRow = view.row("pane-two");
  assert.ok(ordinaryRow);
  assert.equal(ordinaryRow.querySelector(".status"), null, "ordinary terminals must not expose status decoration");
  assert.doesNotMatch(ordinaryRow.textContent ?? "", /\b(?:working|blocked|idle|done|unknown)\b/);
  assert.deepEqual(focused, ["pane-one"]);
  observer.disconnect();
  window.close();
});

test("blocked/all reuse restores the exact keyed row, focus, and scroll synchronously", () => {
  const window = new Window({ url: "http://localhost/" });
  const app = window.document.createElement("main");
  window.document.body.append(app);
  const value = home();
  value.workspaces[0].tabs[0].terminals.push({
    pane_id: "pane-two",
    terminal_id: "terminal-two",
    title: "Ordinary shell",
  });
  const current = state("live", value);
  const view = new HomeView(app, {
    onFocusPane: () => undefined,
    onOpen: () => undefined,
    onReconnect: () => undefined,
    onShowAll: () => undefined,
    onShowBlocked: () => undefined,
  });
  const base = {
    actionsAvailable: true,
    reachability: "current" as const,
    receivedHome: true,
    state: current,
  };
  view.render({ ...base, mode: "all" });
  const blockedRow = view.row("pane-one");
  const ordinaryRow = view.row("pane-two");
  assert.ok(blockedRow && ordinaryRow);
  ordinaryRow.focus();
  window.scrollTo(0, 333);

  view.render({ ...base, mode: "blocked", restore: { focusPane: "pane-one", scroll: 0 } });
  assert.equal(view.row("pane-one"), blockedRow);
  assert.equal(window.document.activeElement, blockedRow);
  assert.equal(window.scrollY, 0);

  view.render({ ...base, mode: "all", restore: { focusPane: "pane-two", scroll: 333 } });
  assert.equal(view.row("pane-one"), blockedRow);
  assert.equal(view.row("pane-two"), ordinaryRow);
  assert.equal(window.document.activeElement, ordinaryRow);
  assert.equal(window.scrollY, 333);
  window.close();
});

test("Home place restoration preserves a surviving row anchor without inventing focus", () => {
  const window = new Window({ url: "http://localhost/" });
  const app = window.document.createElement("main");
  window.document.body.append(app);
  const view = new HomeView(app, {
    onFocusPane: () => undefined,
    onOpen: () => undefined,
    onReconnect: () => undefined,
    onShowAll: () => undefined,
    onShowBlocked: () => undefined,
  });
  const current = state();
  view.render({ actionsAvailable: true, mode: "all", reachability: "current", receivedHome: true, state: current });
  const row = view.row("pane-one");
  assert.ok(row);
  Object.defineProperty(row, "getBoundingClientRect", {
    value: () => ({ top: 70 }),
  });
  view.render({
    actionsAvailable: true,
    mode: "all",
    reachability: "current",
    receivedHome: true,
    restore: { anchorTop: 50, pane: "pane-one", scroll: 200 },
    state: current,
  });
  assert.equal(window.scrollY, 220);
  assert.notEqual(window.document.activeElement, row, "returning must not manufacture list focus");
  window.close();
});

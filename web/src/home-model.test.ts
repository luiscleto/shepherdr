import assert from "node:assert/strict";
import test from "node:test";

import {
  accessibleTerminalName,
  allTerminals,
  automaticAllTerminalsPlace,
  chooseHerdr,
  emptyHomeState,
  findTerminal,
  herdrPresent,
  herdrSettled,
  herdrSwitchShown,
  readRememberedHerdr,
  rememberHerdr,
  showTabHeadings,
  suggestedFolders,
  terminalCount,
  visibleTabs,
  type Home,
  type HomeState,
} from "./home-model.ts";
import { parseTerminalRoute, terminalHash } from "./terminal-route.ts";

const home: Home = {
  blocked_count: 1,
  working_count: 0,
  workspaces: [
    {
      actions: ["close_workspace"],
      id: "workspace/<script>",
      label: "Helper & tools",
      number: 1,
      tabs: [
        {
          current: true,
          id: "tab/one",
          label: "Main",
          number: 1,
          terminals: [
            {
              actions: ["split_terminal", "rename_terminal", "close_terminal"],
              agent: { kind: "codex", name: "Gate helper", status: "blocked" },
              pane_id: "pane/agent?<script>",
              terminal_id: "term-one",
              title: "Review 1",
            },
            { actions: [], pane_id: "pane/ordinary#two", terminal_id: "term-two", title: "Shell" },
          ],
        },
        {
          current: false,
          id: "tab/two",
          label: "Other",
          number: 2,
          terminals: [{ actions: [], pane_id: "pane/three", terminal_id: "term-three", title: "Review 2" }],
        },
      ],
    },
  ],
};

test("models every canonical terminal independently of agent decoration", () => {
  assert.equal(allTerminals(home).length, 3);
  assert.equal(terminalCount(home.workspaces[0]), 3);
  assert.equal(findTerminal(home, "pane/ordinary#two")?.terminal.title, "Shell");
  assert.equal(showTabHeadings(home.workspaces[0]), true);
});

test("blocked mode reuses only real blocked terminal rows without renumbering titles", () => {
  const tabs = visibleTabs(home.workspaces[0], "blocked");
  assert.equal(tabs.length, 1);
  assert.deepEqual(tabs[0].terminals.map((terminal) => terminal.title), ["Review 1"]);
});

test("accessible output keeps the displayed title, place, agent, and exact status but no ids", () => {
  const entry = findTerminal(home, "pane/agent?<script>");
  assert.ok(entry);
  const name = accessibleTerminalName(entry, true);
  assert.equal(name, "Open Review 1, workspace Helper & tools, tab Main, Gate helper, blocked");
  assert.doesNotMatch(name, /pane\/agent|workspace\/<script>|term-one/);
});

test("losing the last blocked agent returns the saved all-Home place", () => {
  const place = automaticAllTerminalsPlace("blocked", 0, { focusPane: "pane/ordinary#two", scroll: 218 });

  assert.equal(place?.focusPane, "pane/ordinary#two");
  assert.equal(place?.scroll, 218);
  assert.equal(automaticAllTerminalsPlace("blocked", 1, { scroll: 218 }), undefined);
});

function herdrState(connection: HomeState["connection"], hasHome: boolean): HomeState {
  return { ...emptyHomeState(), connection, has_home: hasHome, last_known: hasHome && connection !== "live" };
}

test("a Herdr is settled after its first attempt, and present once it had a Home or can't be used", () => {
  const cases: Array<[HomeState["connection"], boolean, boolean, boolean]> = [
    ["reconnecting", false, false, false],
    ["reconnecting", true, true, true],
    ["not_running", false, true, false],
    ["not_running", true, true, true],
    ["incompatible", false, true, true],
    ["live", true, true, true],
  ];
  for (const [connection, hasHome, settled, present] of cases) {
    const state = herdrState(connection, hasHome);
    assert.equal(herdrSettled(state), settled, `${connection} ${hasHome} settled`);
    assert.equal(herdrPresent(state), present, `${connection} ${hasHome} present`);
  }
});

test("Home waits for both Herdrs, then shows the remembered, the only, or Herdr Core", () => {
  const live = herdrState("live", true);
  const stopped = herdrState("not_running", true);
  const absent = herdrState("not_running", false);
  const first = herdrState("reconnecting", false);
  const incompatible = herdrState("incompatible", false);
  const cases: Array<[string, HomeState, HomeState, "tui" | "core" | undefined, "tui" | "core" | undefined, boolean]> = [
    ["both on their first attempt", first, first, undefined, undefined, false],
    ["Herdr TUI live, core on its first attempt", live, first, "tui", undefined, false],
    ["both live, nothing remembered", live, live, undefined, "core", true],
    ["both live, Herdr TUI remembered", live, live, "tui", "tui", true],
    ["both live, Herdr Core remembered", live, live, "core", "core", true],
    ["only the Herdr TUI", live, absent, "core", "tui", false],
    ["only Herdr Core", absent, live, "tui", "core", false],
    ["neither", absent, absent, "core", "tui", false],
    ["Herdr Core stopped after a Home", live, stopped, undefined, "core", true],
    ["Herdr Core can't be used", live, incompatible, "core", "core", true],
  ];
  for (const [name, tui, core, remembered, shown, switchShown] of cases) {
    assert.equal(chooseHerdr({ core, tui }, remembered), shown, name);
    assert.equal(herdrSwitchShown({ core, tui }), switchShown, name);
  }
});

test("the remembered Herdr survives storage that is missing, throws, or holds other values", () => {
  const values = new Map<string, string>();
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value); },
  } as unknown as Storage;
  assert.equal(readRememberedHerdr(() => storage), undefined);
  rememberHerdr(() => storage, "tui");
  assert.equal(values.get("shepherdr.home.herdr"), "tui");
  assert.equal(readRememberedHerdr(() => storage), "tui");
  for (const other of ["", "app", "TUI", "core "]) {
    values.set("shepherdr.home.herdr", other);
    assert.equal(readRememberedHerdr(() => storage), undefined, other);
  }

  const throwing = {
    getItem: () => { throw new Error("SecurityError"); },
    setItem: () => { throw new Error("QuotaExceededError"); },
  } as unknown as Storage;
  assert.equal(readRememberedHerdr(() => throwing), undefined);
  assert.doesNotThrow(() => rememberHerdr(() => throwing, "core"));
  const unavailable = (): Storage => { throw new Error("SecurityError"); };
  assert.equal(readRememberedHerdr(unavailable), undefined);
  assert.doesNotThrow(() => rememberHerdr(unavailable, "core"));
});

test("terminal links name their Herdr; a link without one is from the Herdr TUI", () => {
  assert.deepEqual(parseTerminalRoute("#terminal=w1%3Ap1&terminal_id=term-1"), { herdr: "tui", paneID: "w1:p1", terminalID: "term-1" });
  assert.deepEqual(parseTerminalRoute("#terminal=w1%3Ap1&herdr=tui"), { herdr: "tui", paneID: "w1:p1" });
  assert.deepEqual(parseTerminalRoute("#terminal=t7&terminal_id=core-1%3At7&herdr=core"), { herdr: "core", paneID: "t7", terminalID: "core-1:t7" });
  for (const other of ["", "app", "CORE", "tui,core"]) {
    assert.equal(parseTerminalRoute(`#terminal=t7&herdr=${encodeURIComponent(other)}`)?.herdr, "unknown", other);
  }
  assert.equal(parseTerminalRoute("#terminal=&herdr=core"), undefined);
  assert.equal(terminalHash("core", "t7", "core-1:t7"), "terminal=t7&terminal_id=core-1%3At7&herdr=core");
  assert.equal(terminalHash("tui", "w1:p1"), "terminal=w1%3Ap1&herdr=tui");
  assert.deepEqual(parseTerminalRoute(`#${terminalHash("tui", "w1:p1", "a&b=c")}`), { herdr: "tui", paneID: "w1:p1", terminalID: "a&b=c" });
});

test("New space suggests each live Herdr's repository folders once, the Herdr TUI's first", () => {
  const side = (paths: Array<string | undefined>, connection: HomeState["connection"] = "live", lastKnown = false): HomeState => ({
    ...emptyHomeState(),
    connection,
    has_home: true,
    home: {
      blocked_count: 0, error_count: 0, working_count: 0,
      workspaces: paths.map((path, index) => ({
        actions: [], id: `w${index}`, label: `w${index}`, number: index + 1, tabs: [],
        ...(path === undefined ? {} : { checkout_path: path }),
      })),
    },
    last_known: lastKnown,
  });
  const tui = side(["/a", undefined, "/c"]);
  const core = side(["/b", "/a", undefined, "/b"]);
  assert.deepEqual(suggestedFolders({ tui, core }), ["/a", "/c", "/b"]);
  assert.deepEqual(suggestedFolders({ tui: side(["/a", "/c"], "not_running", true), core }), ["/b", "/a"]);
  assert.deepEqual(suggestedFolders({ tui, core: side(["/b"], "reconnecting", true) }), ["/a", "/c"]);
  assert.deepEqual(suggestedFolders({ tui: emptyHomeState(), core: emptyHomeState() }), []);
});

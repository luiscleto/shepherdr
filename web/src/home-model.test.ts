import assert from "node:assert/strict";
import test from "node:test";

import {
  accessibleTerminalName,
  allTerminals,
  automaticAllTerminalsPlace,
  findTerminal,
  showTabHeadings,
  terminalCount,
  visibleTabs,
  type Home,
} from "./home-model.ts";

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
              agent: { kind: "codex", name: "Gate helper", status: "blocked" },
              pane_id: "pane/agent?<script>",
              terminal_id: "term-one",
              title: "Review 1",
            },
            { pane_id: "pane/ordinary#two", terminal_id: "term-two", title: "Shell" },
          ],
        },
        {
          current: false,
          id: "tab/two",
          label: "Other",
          number: 2,
          terminals: [{ pane_id: "pane/three", terminal_id: "term-three", title: "Review 2" }],
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
  const tabs = visibleTabs(home.workspaces[0], true);
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

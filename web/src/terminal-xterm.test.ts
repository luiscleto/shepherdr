import assert from "node:assert/strict";
import test from "node:test";

import { initialTerminalWheelState, localScrollMovesArea, localScrollRows, personInputFilter, terminalWheelRows, touchScrollsArea } from "./terminal/adapter";

test("desktop wheel and trackpad deltas become bounded terminal history rows", () => {
  const initial = initialTerminalWheelState();
  assert.equal(terminalWheelRows(-50, 0, 25, 24, initial).lines, -2);
  assert.equal(terminalWheelRows(2, 1, 17, 24, initial).lines, 2);
  assert.equal(terminalWheelRows(-1, 2, 17, 24, initial).lines, -24);
  assert.equal(terminalWheelRows(20_000, 1, 17, 24, initial).lines, 1_000);
});

test("likely trackpad pixels damp application events without losing host history rows", () => {
  let state = initialTerminalWheelState();
  for (let event = 0; event < 3; event += 1) {
    const result = terminalWheelRows(-16, 0, 16, 24, state);
    assert.equal(result.lines, 0);
    state = result.state;
  }
  const routed = terminalWheelRows(-16, 0, 16, 24, state);
  assert.equal(routed.lines, -4);
  assert.equal(routed.state.pendingHistoryLines, 0);
});

test("Alt applies xterm's fast-scroll scale before likely-trackpad damping", () => {
  const routed = terminalWheelRows(-16, 0, 16, 24, initialTerminalWheelState(), true);
  assert.equal(routed.lines, -1);
  assert.equal(routed.state.applicationRemainder, -0.5);
  assert.equal(routed.state.historyRemainder, 0);
});

test("the likely-trackpad boundary matches xterm mouse-report and alternate-scroll routing", () => {
  const initial = initialTerminalWheelState();
  const damped = terminalWheelRows(-49, 0, 49, 24, initial);
  assert.equal(damped.lines, 0);
  assert.equal(damped.state.pendingHistoryLines, -1);
  assert.equal(damped.state.applicationRemainder, -0.3);

  assert.equal(terminalWheelRows(-50, 0, 50, 24, initial).lines, -1);
});

test("a core terminal's wheel scrolls whole local rows and keeps the part of a row left over", () => {
  assert.deepEqual(localScrollRows(-120, 0, 20, 24, 0), { lines: -6, remainder: 0 });
  assert.deepEqual(localScrollRows(3, 1, 17, 24, 0), { lines: 3, remainder: 0 });
  assert.deepEqual(localScrollRows(-1, 2, 17, 24, 0), { lines: -24, remainder: 0 });
  const first = localScrollRows(10, 0, 16, 24, 0);
  assert.deepEqual([first.lines, first.remainder], [0, 0.625]);
  const second = localScrollRows(10, 0, 16, 24, first.remainder);
  assert.deepEqual([second.lines, second.remainder], [1, 0.25]);
  assert.equal(localScrollRows(-20_000, 1, 17, 24, 0).lines, -1_000);
  assert.deepEqual(localScrollRows(Number.NaN, 0, 16, 24, 0.5), { lines: 0, remainder: 0 });
});

test("a core terminal's area and scrollback scroll as one page", () => {
  assert.equal(localScrollMovesArea(true, 40, 2_000, 2_000, true), true);
  assert.equal(localScrollMovesArea(true, 0, 2_000, 2_000, true), false);
  assert.equal(localScrollMovesArea(false, 0, 1_500, 2_000, true), false);
  assert.equal(localScrollMovesArea(false, 0, 2_000, 2_000, true), true);
  // At the latest output in an area that fits, a swipe stays the terminal's
  // and is cancelled, so it never bounces or pans the page.
  assert.equal(localScrollMovesArea(false, 0, 2_000, 2_000, false), false);
});

test("a swipe scrolls the terminal area only along an axis the terminal overflows", () => {
  assert.equal(touchScrollsArea(-40, 3, true, false), true);
  assert.equal(touchScrollsArea(-40, 3, false, false), false);
  assert.equal(touchScrollsArea(2, -30, true, false), false);
  assert.equal(touchScrollsArea(2, -30, false, true), true);
  assert.equal(touchScrollsArea(2, -30, false, false), false);
});

test("a keydown and the beforeinput or paste it causes are one person's input", () => {
  const input = personInputFilter();
  const seen = (["keydown", "beforeinput", "keyup", "keydown", "keyup", "beforeinput", "beforeinput", "keydown", "paste", "keyup", "paste"] as const)
    .map(type => input(type));
  assert.deepEqual(seen, [true, false, false, true, false, true, true, true, false, false, true]);
});

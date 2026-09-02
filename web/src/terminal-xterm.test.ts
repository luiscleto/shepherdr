import assert from "node:assert/strict";
import test from "node:test";

import { initialTerminalWheelState, terminalWheelRows } from "./terminal/adapter";

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

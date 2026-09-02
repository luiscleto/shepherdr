import assert from "node:assert/strict";
import test from "node:test";

import { terminalWheelRows } from "./terminal/adapter";

test("desktop wheel and trackpad deltas become bounded terminal history rows", () => {
  assert.deepEqual(terminalWheelRows(-34, 0, 17, 24, 0), { lines: -2, remainder: 0 });
  assert.deepEqual(terminalWheelRows(2, 1, 17, 24, 0), { lines: 2, remainder: 0 });
  assert.deepEqual(terminalWheelRows(-1, 2, 17, 24, 0), { lines: -24, remainder: 0 });
  assert.deepEqual(terminalWheelRows(20_000, 1, 17, 24, 0), { lines: 1_000, remainder: 0 });

  const first = terminalWheelRows(-4, 0, 16, 24, 0);
  assert.deepEqual(first, { lines: 0, remainder: -0.25 });
  assert.deepEqual(terminalWheelRows(-12, 0, 16, 24, first.remainder), { lines: -1, remainder: 0 });
});

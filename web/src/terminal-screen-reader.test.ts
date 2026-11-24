import assert from "node:assert/strict";
import test from "node:test";

import { Window } from "happy-dom";

import {
  fullTerminalScreenReader,
  onTerminalScreenReaderChange,
  readTerminalScreenReader,
  saveTerminalScreenReader,
} from "./terminal/screen-reader";

test("the full terminal keeps screen-reader support on desktop and leaves it off by default on touch", () => {
  const storage = new Window().localStorage;
  assert.equal(fullTerminalScreenReader(false, () => storage), true);
  assert.equal(fullTerminalScreenReader(true, () => storage), false);
});

test("turning the setting on restores screen-reader support on touch, stored in this browser only", () => {
  const first = new Window().localStorage;
  const other = new Window().localStorage;
  let changes = 0;
  const stop = onTerminalScreenReaderChange(() => { changes++; });

  saveTerminalScreenReader(true, () => first);
  assert.equal(readTerminalScreenReader(() => first), true);
  assert.equal(fullTerminalScreenReader(true, () => first), true);
  assert.equal(fullTerminalScreenReader(true, () => other), false);

  saveTerminalScreenReader(false, () => first);
  assert.equal(fullTerminalScreenReader(true, () => first), false);
  assert.equal(fullTerminalScreenReader(false, () => first), true);
  assert.equal(changes, 2);

  stop();
  saveTerminalScreenReader(true, () => first);
  assert.equal(changes, 2);
  saveTerminalScreenReader(false, () => first);
});

test("without storage the choice still applies for this visit, and a later stored choice replaces it", () => {
  const broken = (): Storage => { throw new Error("blocked"); };
  assert.equal(readTerminalScreenReader(broken), false);
  assert.equal(fullTerminalScreenReader(true, broken), false);
  saveTerminalScreenReader(true, broken);
  assert.equal(readTerminalScreenReader(broken), true);
  assert.equal(fullTerminalScreenReader(true, broken), true);
  saveTerminalScreenReader(false, broken);
  assert.equal(fullTerminalScreenReader(true, broken), false);
  assert.equal(fullTerminalScreenReader(false, broken), true);

  const working = new Window().localStorage;
  saveTerminalScreenReader(true, broken);
  saveTerminalScreenReader(false, () => working);
  assert.equal(fullTerminalScreenReader(true, () => working), false);
});

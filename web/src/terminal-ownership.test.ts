import assert from "node:assert/strict";
import test from "node:test";

import { nextTerminalOwnership, terminalOwnershipAction, type TerminalOwnership } from "./terminal/ownership";

test("desktop ownership opens as observer, acquires without takeover, releases, and reacquires", () => {
  let state: TerminalOwnership = "waiting";
  state = nextTerminalOwnership(state, "observer-ready");
  assert.equal(state, "observing");
  assert.equal(terminalOwnershipAction(state), "control");

  state = nextTerminalOwnership(state, "request");
  assert.equal(state, "requesting");
  assert.equal(terminalOwnershipAction(state), undefined);
  state = nextTerminalOwnership(state, "acquired");
  assert.equal(state, "controlling");
  assert.equal(terminalOwnershipAction(state), "release");

  state = nextTerminalOwnership(state, "release");
  assert.equal(state, "observing");
  state = nextTerminalOwnership(state, "request");
  state = nextTerminalOwnership(state, "acquired");
  assert.equal(state, "controlling");
});

test("an occupied controller remains observing until an explicit takeover request", () => {
  let state: TerminalOwnership = "observing";
  state = nextTerminalOwnership(state, "request");
  state = nextTerminalOwnership(state, "occupied");
  assert.equal(state, "occupied");
  assert.equal(terminalOwnershipAction(state), "takeover");
  state = nextTerminalOwnership(state, "request");
  assert.equal(state, "requesting");
  state = nextTerminalOwnership(state, "acquired");
  assert.equal(state, "controlling");
});

test("observer loss removes every ownership action until current output returns", () => {
  const state = nextTerminalOwnership("controlling", "observer-lost");
  assert.equal(state, "waiting");
  assert.equal(terminalOwnershipAction(state), undefined);
});

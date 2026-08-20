import assert from "node:assert/strict";
import test from "node:test";

import { TerminalFrameSequence } from "./terminal/session";

function frame(full: boolean, seq: number, extra: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    bytes: Buffer.from("output").toString("base64"),
    encoding: "ansi",
    full,
    height: 24,
    seq,
    type: "terminal.frame",
    width: 80,
    ...extra,
  };
}

test("terminal frames require a full first frame and a strictly increasing sequence", () => {
  assert.throws(() => new TerminalFrameSequence().accept(frame(false, 1)), /first terminal frame/);

  const sequence = new TerminalFrameSequence();
  assert.equal(sequence.accept(frame(true, 1)).frame.seq, 1);
  assert.equal(sequence.accept(frame(false, 2)).frame.seq, 2);
  assert.throws(() => sequence.accept(frame(false, 2)), /not monotonic/);
  assert.throws(() => sequence.accept(frame(false, 1)), /not monotonic/);
});

test("only a full frame may recover a missing sequence", () => {
  const sequence = new TerminalFrameSequence();
  sequence.accept(frame(true, 1));
  assert.throws(() => sequence.accept(frame(false, 3)), /sequence gap/);
  assert.equal(sequence.accept(frame(true, 3)).frame.seq, 3);
  assert.equal(sequence.accept(frame(false, 4)).frame.seq, 4);
});

test("a reconnect has a fresh frame fence while hostile frame shapes are rejected", () => {
  const firstConnection = new TerminalFrameSequence();
  firstConnection.accept(frame(true, 40));

  const reconnected = new TerminalFrameSequence();
  assert.equal(reconnected.accept(frame(true, 1)).frame.full, true);
  assert.throws(() => reconnected.accept(frame(false, 2, { extra: true })), /fields are invalid/);
  assert.throws(() => reconnected.accept(frame(false, 2, { bytes: "not base64" })), /frame bytes/);
  assert.throws(() => reconnected.accept(frame(false, 2, { width: 0 })), /fields are invalid/);
});

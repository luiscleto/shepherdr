import assert from "node:assert/strict";
import test from "node:test";

import { TerminalFrameSequence, TerminalSession } from "./terminal/session";

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

test("a size is sent once unless the page asks for it again, in view or out of view", (t) => {
  const sent: string[] = [];
  class Socket extends EventTarget {
    static OPEN = 1;
    static CLOSED = 3;
    readyState = 1;
    send(data: string): void { sent.push(data); }
    close(): void { this.readyState = 3; }
  }
  const globals = globalThis as Record<string, unknown>;
  const previous = { WebSocket: globals.WebSocket, location: globals.location, window: globals.window };
  globals.window = { setTimeout: () => 0, clearTimeout: () => undefined };
  globals.WebSocket = Socket;
  globals.location = { protocol: "http:", host: "127.0.0.1" };
  t.after(() => Object.assign(globals, previous));
  const noop = (): void => undefined;
  const session = new TerminalSession("control", {
    onFrame: noop, onInputForwarded: noop, onKeyResult: noop, onLog: noop, onStatus: noop,
  }, { endpoint: "/api/terminal", herdr: "core" });
  session.connect("t1", { cols: 46, rows: 41 });
  session.resize({ cols: 46, rows: 41 });
  session.resize({ cols: 46, rows: 35 });
  session.resize({ cols: 46, rows: 35 });
  session.resizeAgain({ cols: 46, rows: 35 }, true);
  session.resizeAgain({ cols: 46, rows: 35 }, false);
  assert.deepEqual(sent.map(message => JSON.parse(message).rows), [35, 35, 35]);
  assert.deepEqual(sent.map(message => String(JSON.parse(message).keep)), ["undefined", "true", "false"]);
  session.disconnect();
});

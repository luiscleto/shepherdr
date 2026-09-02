import assert from "node:assert/strict";
import test from "node:test";

import { Window } from "happy-dom";

import { ReaderInputQueue } from "./terminal/reader-input";
import type { SessionMode, TerminalFrame, TerminalSessionEvents, TerminalSessionLike } from "./terminal/session";

class FakeTerminalSession implements TerminalSessionLike {
  connected = false;
  connectedDimensions: { cols: number; rows: number } | undefined;
  disconnected = false;
  events: TerminalSessionEvents;
  mode: SessionMode;
  nextRequestID = 10;
  resized: Array<{ cols: number; rows: number }> = [];
  sent: string[][] = [];

  constructor(mode: SessionMode, events: TerminalSessionEvents) {
    this.mode = mode;
    this.events = events;
  }

  connect(_pane: string, dimensions: { cols: number; rows: number }): void {
    this.connected = true;
    this.connectedDimensions = dimensions;
  }

  disconnect(): void {
    this.disconnected = true;
  }

  input(): number | undefined {
    return undefined;
  }

  inputBatch(chunks: string[]): number | undefined {
    this.sent.push(chunks);
    this.nextRequestID += 1;
    return this.nextRequestID;
  }

  resize(dimensions: { cols: number; rows: number }): void {
    this.resized.push(dimensions);
  }

  acquire(): void {
    const frame: TerminalFrame = {
      bytes: "",
      encoding: "ansi",
      full: true,
      height: 24,
      seq: 1,
      type: "terminal.frame",
      width: 80,
    };
    this.events.onFrame(frame, new Uint8Array());
  }

  forward(requestID = this.nextRequestID): void {
    this.events.onInputForwarded?.(requestID);
  }

  close(status: string): void {
    this.events.onStatus(status);
    this.events.onLog("session.close");
  }
}

function withBrowser(t: test.TestContext): void {
  const browser = new Window({ url: "http://localhost/" });
  const previous = (globalThis as { window?: unknown }).window;
  Object.assign(globalThis, { window: browser });
  t.after(() => {
    Object.assign(globalThis, { window: previous });
    browser.close();
  });
}

test("Reader input matches the forwarded request ID before releasing control", (t) => {
  withBrowser(t);
  const sessions: FakeTerminalSession[] = [];
  let forwarded = 0;
  const queue = new ReaderInputQueue({
    onFailed: () => assert.fail("input unexpectedly failed"),
    onForwarded: (count) => forwarded += count,
    onLog: () => undefined,
    onOccupied: () => assert.fail("input was unexpectedly occupied"),
    onSending: () => undefined,
    onUncertain: () => assert.fail("input unexpectedly became uncertain"),
  }, {
    endpoint: "/api/terminal",
    createSession: (mode, events) => {
      const session = new FakeTerminalSession(mode, events);
      sessions.push(session);
      return session;
    },
  });
  queue.setTarget("pane-1", { cols: 80, rows: 24 }, "term-1");
  assert.equal(queue.enqueueBatch(["paste", "\r"]), true);
  sessions[0].acquire();
  assert.deepEqual(sessions[0].sent, [["paste", "\r"]]);

  sessions[0].forward(999);
  assert.equal(forwarded, 0, "a mismatched acknowledgement must not complete input");
  sessions[0].forward();
  assert.equal(forwarded, 1);
  assert.equal(queue.state(), "ready");
  assert.equal(sessions[0].disconnected, true, "transient Reader control must release after forwarding");
});

test("Reader input uses the latest measured viewport while its control is active", (t) => {
  withBrowser(t);
  const sessions: FakeTerminalSession[] = [];
  const queue = new ReaderInputQueue({
    onFailed: () => assert.fail("input unexpectedly failed"),
    onForwarded: () => undefined,
    onLog: () => undefined,
    onOccupied: () => assert.fail("input was unexpectedly occupied"),
    onSending: () => undefined,
    onUncertain: () => assert.fail("input unexpectedly became uncertain"),
  }, {
    endpoint: "/api/terminal",
    createSession: (mode, events) => {
      const session = new FakeTerminalSession(mode, events);
      sessions.push(session);
      return session;
    },
  });
  queue.setTarget("pane-1", { cols: 80, rows: 24 }, "term-1");
  queue.resize({ cols: 45, rows: 24 });
  assert.equal(queue.enqueue("message"), true);
  assert.deepEqual(sessions[0].connectedDimensions, { cols: 45, rows: 24 });

  queue.resize({ cols: 40, rows: 24 });
  assert.deepEqual(sessions[0].resized, [{ cols: 40, rows: 24 }]);
  sessions[0].acquire();
  sessions[0].forward();
});

test("Reader input stays queued when occupied and offers only explicit takeover", (t) => {
  withBrowser(t);
  const sessions: FakeTerminalSession[] = [];
  let occupied = "";
  let forwarded = 0;
  const queue = new ReaderInputQueue({
    onFailed: () => assert.fail("occupied control is not an ordinary failure"),
    onForwarded: (count) => forwarded += count,
    onLog: () => undefined,
    onOccupied: (message) => occupied = message,
    onSending: () => undefined,
    onUncertain: () => assert.fail("occupied input is not ambiguous"),
  }, {
    endpoint: "/api/terminal",
    createSession: (mode, events) => {
      const session = new FakeTerminalSession(mode, events);
      sessions.push(session);
      return session;
    },
  });
  queue.setTarget("pane-1", { cols: 80, rows: 24 }, "term-1");
  queue.enqueue("\x03");
  assert.equal(sessions[0].mode, "control");
  sessions[0].close("another terminal already has an attached client");
  assert.match(occupied, /Someone else/);
  assert.equal(sessions.length, 1, "ordinary failure must not steal control");
  assert.equal(queue.enqueue("\x03"), false, "the queued batch cannot be duplicated while awaiting a decision");

  queue.retry(false);
  assert.equal(sessions.length, 1, "occupied recovery must not permit an ordinary retry");
  queue.retry(true);
  assert.equal(sessions[1].mode, "takeover");
  sessions[1].acquire();
  sessions[1].forward();
  assert.equal(forwarded, 1);
});

test("ambiguous Reader input is never automatically queued or sent again", (t) => {
  withBrowser(t);
  const sessions: FakeTerminalSession[] = [];
  let forwarded = 0;
  let uncertain = "";
  const queue = new ReaderInputQueue({
    onFailed: () => assert.fail("ambiguous input must not be described as unsent"),
    onForwarded: () => forwarded += 1,
    onLog: () => undefined,
    onOccupied: () => assert.fail("ambiguous input must not be described as occupied"),
    onSending: () => undefined,
    onUncertain: (message) => uncertain = message,
  }, {
    endpoint: "/api/terminal",
    createSession: (mode, events) => {
      const session = new FakeTerminalSession(mode, events);
      sessions.push(session);
      return session;
    },
  });
  queue.setTarget("pane-1", { cols: 80, rows: 24 }, "term-1");
  assert.equal(queue.enqueueBatch(["paste", "\r"]), true);
  assert.equal(queue.enqueue("\x03"), false, "a second batch cannot enter while control is unresolved");
  sessions[0].acquire();
  assert.equal(queue.enqueue("\x04"), false, "a second batch cannot enter while acknowledgement is unresolved");
  sessions[0].close("connection lost");
  assert.match(uncertain, /could not be confirmed/);
  assert.equal(forwarded, 0);
  assert.equal(queue.state(), "uncertain");
  assert.equal(queue.enqueue("\x04"), false, "uncertain delivery must be acknowledged before another batch");

  queue.retry(false);
  queue.retry(true);
  assert.equal(sessions.length, 1, "an ambiguous batch must require a new human send action");
  queue.dismissUncertain();
  assert.equal(queue.state(), "ready");
  assert.equal(queue.enqueue("\x04"), true);
  sessions[1].acquire();
  sessions[1].forward();
  assert.equal(forwarded, 1);
});

test("ordinary acquisition failure offers only an ordinary retry", (t) => {
  withBrowser(t);
  const sessions: FakeTerminalSession[] = [];
  let failed = "";
  const queue = new ReaderInputQueue({
    onFailed: (message) => failed = message,
    onForwarded: () => undefined,
    onLog: () => undefined,
    onOccupied: () => assert.fail("ordinary failure must not claim another controller exists"),
    onSending: () => undefined,
    onUncertain: () => assert.fail("input was never handed to the connection"),
  }, {
    endpoint: "/api/terminal",
    createSession: (mode, events) => {
      const session = new FakeTerminalSession(mode, events);
      sessions.push(session);
      return session;
    },
  });
  queue.setTarget("pane-1", { cols: 80, rows: 24 }, "term-1");
  assert.equal(queue.enqueue("\x03"), true);
  sessions[0].close("connection failed");
  assert.match(failed, /not sent/);

  queue.retry(true);
  assert.equal(sessions.length, 1, "ordinary failure must not permit takeover");
  queue.retry(false);
  assert.equal(sessions[1].mode, "control");
  sessions[1].acquire();
  sessions[1].forward();
});

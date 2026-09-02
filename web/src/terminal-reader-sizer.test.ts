import assert from "node:assert/strict";
import test from "node:test";

import { ReaderTerminalSizer } from "./terminal/reader-sizer";
import type { SessionMode, TerminalFrame, TerminalSessionEvents, TerminalSessionLike } from "./terminal/session";

class FakeSizingSession implements TerminalSessionLike {
  connectedDimensions: { cols: number; rows: number } | undefined;
  disconnected = false;
  readonly events: TerminalSessionEvents;
  readonly mode: SessionMode;

  constructor(mode: SessionMode, events: TerminalSessionEvents) {
    this.mode = mode;
    this.events = events;
  }

  connect(_pane: string, dimensions: { cols: number; rows: number }): void {
    this.connectedDimensions = dimensions;
  }

  disconnect(): void {
    this.disconnected = true;
  }

  input(): number | undefined { return undefined; }
  inputBatch(): number | undefined { return undefined; }
  resize(): void {}

  acquire(): void {
    const dimensions = this.connectedDimensions ?? { cols: 80, rows: 24 };
    const frame: TerminalFrame = {
      bytes: "",
      encoding: "ansi",
      full: true,
      height: dimensions.rows,
      seq: 1,
      type: "terminal.frame",
      width: dimensions.cols,
    };
    this.events.onFrame(frame, new Uint8Array());
  }

  close(status: string): void {
    this.events.onStatus(status);
    this.events.onLog("session.close");
  }
}

function sizingHarness(): {
  active: boolean[];
  sessions: FakeSizingSession[];
  settled: () => number;
  sizer: ReaderTerminalSizer;
} {
  const active: boolean[] = [];
  const sessions: FakeSizingSession[] = [];
  let settled = 0;
  const sizer = new ReaderTerminalSizer("pane-1", "term-1", {
    onActive: (value) => active.push(value),
    onSettled: () => settled += 1,
  }, {
    endpoint: "/api/terminal",
    createSession: (mode, events) => {
      const session = new FakeSizingSession(mode, events);
      sessions.push(session);
      return session;
    },
  });
  return { active, sessions, settled: () => settled, sizer };
}

test("Reader sizing uses the current observer rows on open and reconnect", () => {
  const { sessions, sizer } = sizingHarness();
  sizer.viewportChanged({ cols: 45, rows: 24 });
  sizer.setAvailable(true);
  sizer.observerFrame({ cols: 80, rows: 35 });
  assert.deepEqual(sessions.map(({ connectedDimensions, mode }) => ({ connectedDimensions, mode })), [{
    connectedDimensions: { cols: 45, rows: 35 },
    mode: "control",
  }]);
  sessions[0].acquire();

  sizer.setAvailable(false);
  sizer.observerLost();
  sizer.observerFrame({ cols: 120, rows: 51 });
  assert.equal(sessions.length, 1, "reconnect must wait until Reader control is available");
  sizer.setAvailable(true);
  assert.deepEqual(sessions[1].connectedDimensions, { cols: 45, rows: 51 });
  assert.equal(sessions[0].disconnected, true);
  sizer.destroy();
});

test("Reader sizing never takes over or retries an occupied controller", () => {
  const { active, sessions, settled, sizer } = sizingHarness();
  sizer.viewportChanged({ cols: 45, rows: 24 });
  sizer.setAvailable(true);
  sizer.observerFrame({ cols: 80, rows: 35 });
  sessions[0].close("another terminal already has an attached client");
  sizer.observerFrame({ cols: 80, rows: 35 });

  assert.deepEqual(sessions.map(({ mode }) => mode), ["control"]);
  assert.deepEqual(active, [true, false]);
  assert.equal(settled(), 1);
  assert.equal(sizer.active(), false);
  sizer.destroy();
});

test("Reader sizing coalesces viewport changes and destruction stops pending work", () => {
  const { sessions, sizer } = sizingHarness();
  sizer.viewportChanged({ cols: 45, rows: 24 });
  sizer.setAvailable(true);
  sizer.observerFrame({ cols: 80, rows: 35 });
  sizer.viewportChanged({ cols: 44, rows: 24 });
  sizer.viewportChanged({ cols: 43, rows: 24 });
  assert.equal(sessions.length, 1);

  sessions[0].acquire();
  assert.equal(sessions.length, 2);
  assert.deepEqual(sessions[1].connectedDimensions, { cols: 43, rows: 35 });
  sessions[1].acquire();
  sizer.viewportChanged({ cols: 43, rows: 24 });
  sizer.observerFrame({ cols: 80, rows: 35 });
  assert.equal(sessions.length, 2, "unchanged geometry must not oscillate");

  sizer.request();
  assert.equal(sessions.length, 3);
  sizer.destroy();
  assert.equal(sessions[2].disconnected, true);
  sizer.request();
  sizer.observerFrame({ cols: 80, rows: 50 });
  assert.equal(sessions.length, 3);
});

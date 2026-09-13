import assert from "node:assert/strict";
import test from "node:test";
import { ReaderInputQueue } from "./terminal/reader-input";
import type { TerminalSessionLike } from "./terminal/session";

function setup(t: test.TestContext, occupied = false) {
  const previous = Object.getOwnPropertyDescriptor(globalThis, "window");
  Object.defineProperty(globalThis, "window", { value: globalThis, configurable: true });
  let sent = 0;
  let acquired = 0;
  let released = 0;
  let forwarded = 0;
  let uncertain = 0;
  let current: TerminalSessionLike | undefined;
  const session: TerminalSessionLike = {
    connect() {}, disconnect() { released++; }, input() { return undefined; },
    inputBatch() { return ++sent; }, resize() {}, scroll() { return false; },
  };
  if (!occupied) current = session;
  const input = new ReaderInputQueue({
    onFailed() {}, onForwarded() { forwarded++; }, onLog() {}, onOccupied() {}, onSending() {},
    onUncertain() { uncertain++; },
  }, {
    session: () => current, occupied: () => occupied,
    acquire: async () => { acquired++; occupied = false; current = session; return true; },
  });
  t.after(() => {
    input.clearTarget();
    if (previous) Object.defineProperty(globalThis, "window", previous);
    else Reflect.deleteProperty(globalThis, "window");
  });
  return { input, session, counts: () => ({ sent, acquired, released, forwarded, uncertain }) };
}

test("Reader acknowledges exactly one request without acquiring or releasing its retained stream", (t) => {
  const { input, session, counts } = setup(t);
  assert.equal(input.enqueueBatch(["paste", "\r"]), true);
  assert.equal(input.enqueue("duplicate"), false);
  input.forwarded(session, 99);
  assert.equal(counts().forwarded, 0);
  input.forwarded(session, 1);
  input.forwarded(session, 1);
  assert.equal(counts().forwarded, 1);
  assert.equal(counts().sent, 1);
  assert.equal(counts().acquired, 0);
  assert.equal(counts().released, 0);
});

test("occupied Reader sends only its explicitly confirmed action after takeover", async (t) => {
  const { input, counts } = setup(t, true);
  assert.equal(input.enqueue("confirmed action"), true);
  assert.equal(input.state(), "occupied");
  await input.retry(false);
  assert.equal(counts().sent, 0);
  assert.equal(counts().acquired, 0);
  await input.retry(true);
  assert.equal(counts().sent, 1);
  assert.equal(counts().acquired, 1);
  assert.equal(input.enqueue("second action"), false);
});

test("disconnect leaves unconfirmed input unknown with no automatic repeat", async (t) => {
  const { input, session, counts } = setup(t);
  assert.equal(input.enqueue("one action"), true);
  input.disconnected(session);
  input.forwarded(session, 1);
  await input.retry(false);
  await input.retry(true);
  assert.equal(input.state(), "uncertain");
  assert.equal(counts().uncertain, 1);
  assert.equal(counts().sent, 1);
  assert.equal(counts().forwarded, 0);
  assert.equal(counts().acquired, 0);
  input.dismissUncertain();
  assert.equal(counts().sent, 1);
});

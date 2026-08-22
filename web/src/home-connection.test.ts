import assert from "node:assert/strict";
import test from "node:test";

import {
  HOME_RECOVERY_LIMIT_MS,
  HOME_RETRY_DELAY_MS,
  HOME_STALE_AFTER_MS,
  homeReachability,
  nextHomeCheckDelay,
  resumeHomeConnection,
} from "./home-connection";

test("a complete live frame is current immediately", () => {
  assert.equal(homeReachability(10_000, 10_000), "current");
});

test("normal two-second frames stay continuously current through unrelated transport events", () => {
  let lastValidHomeFrameAt = 0;
  for (let now = 0; now <= 120_000; now += 2_000) {
    // Socket open/close/error/online events do not write this timestamp.
    if (now % 10_000 === 0) {
      assert.equal(homeReachability(lastValidHomeFrameAt, now), "current");
    }
    lastValidHomeFrameAt = now; // heartbeat or complete changing state frame
    assert.equal(homeReachability(lastValidHomeFrameAt, now), "current");
  }
});

test("genuine frame silence moves once to Reconnecting and then Offline", () => {
  const lastValidHomeFrameAt = 5_000;
  assert.equal(homeReachability(lastValidHomeFrameAt, 24_999), "current");
  assert.equal(homeReachability(lastValidHomeFrameAt, 25_000), "reconnecting");
  assert.equal(homeReachability(lastValidHomeFrameAt, 49_999), "reconnecting");
  assert.equal(homeReachability(lastValidHomeFrameAt, 50_000), "offline");
});

test("one check schedules retries and the two frame-age deadlines", () => {
  const lastValidHomeFrameAt = 10_000;
  assert.equal(nextHomeCheckDelay(lastValidHomeFrameAt, 11_000, false), HOME_RETRY_DELAY_MS);
  assert.equal(
    nextHomeCheckDelay(lastValidHomeFrameAt, 11_000, true),
    HOME_STALE_AFTER_MS - 1_000,
  );
  assert.equal(
    nextHomeCheckDelay(lastValidHomeFrameAt, 10_000 + HOME_STALE_AFTER_MS, true),
    HOME_RECOVERY_LIMIT_MS - HOME_STALE_AFTER_MS,
  );
  assert.equal(
    nextHomeCheckDelay(lastValidHomeFrameAt, 10_000 + HOME_RECOVERY_LIMIT_MS, false),
    HOME_RETRY_DELAY_MS,
  );
});

test("Offline keeps the existing restrained retry cadence", () => {
  const lastValidHomeFrameAt = 10_000;
  const offlineAt = 10_000 + HOME_RECOVERY_LIMIT_MS;
  assert.equal(homeReachability(lastValidHomeFrameAt, offlineAt), "offline");
  assert.equal(nextHomeCheckDelay(lastValidHomeFrameAt, offlineAt, false), HOME_RETRY_DELAY_MS);
  assert.equal(nextHomeCheckDelay(lastValidHomeFrameAt, offlineAt, true), HOME_RETRY_DELAY_MS);
});

test("returning to view restarts recovery without claiming Live before valid evidence", () => {
  let lastValidHomeFrameAt = 10_000;
  const now = lastValidHomeFrameAt + HOME_RECOVERY_LIMIT_MS;
  let reconnects = 0;
  const reconnect = () => reconnects++;

  resumeHomeConnection("hidden", reconnect);
  assert.equal(reconnects, 0);
  assert.equal(homeReachability(lastValidHomeFrameAt, now), "offline");

  resumeHomeConnection("visible", reconnect);
  assert.equal(reconnects, 1);
  assert.equal(homeReachability(lastValidHomeFrameAt, now), "offline");

  lastValidHomeFrameAt = now; // valid current Home frame or heartbeat
  assert.equal(homeReachability(lastValidHomeFrameAt, now), "current");
});

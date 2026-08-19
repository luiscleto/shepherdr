import assert from "node:assert/strict";
import test from "node:test";

import {
  HOME_RECOVERY_LIMIT_MS,
  HOME_RETRY_DELAY_MS,
  HOME_STALE_AFTER_MS,
  homeReachability,
  nextHomeCheckDelay,
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
    1,
  );
});

test("the due Offline check settles without scheduling another check", () => {
  const lastValidHomeFrameAt = 10_000;
  let scheduledChecks = 1;
  const runScheduledCheck = (now: number) => {
    if (homeReachability(lastValidHomeFrameAt, now) === "offline") return;
    scheduledChecks++;
  };

  assert.equal(nextHomeCheckDelay(lastValidHomeFrameAt, 10_000 + HOME_RECOVERY_LIMIT_MS, false), 1);
  runScheduledCheck(10_000 + HOME_RECOVERY_LIMIT_MS + 1);
  assert.equal(scheduledChecks, 1);
});

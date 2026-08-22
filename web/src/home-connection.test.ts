import assert from "node:assert/strict";
import test from "node:test";

import {
  HomeConnectionOwner,
  HOME_RECOVERY_LIMIT_MS,
  HOME_RETRY_DELAY_MS,
  HOME_STALE_AFTER_MS,
  homeConnectionAllowed,
  homeReachability,
  maintainHomeConnection,
  nextHomeCheckDelay,
  resumeHomeConnection,
} from "./home-connection";
import { parseTerminalRoute } from "./terminal-route";

type FakeReadyState = "closed" | "connecting" | "open";

class FakeHomeConnection {
  readonly #closeListeners: Array<() => void> = [];
  closes = 0;

  constructor(readonly id: number, public readyState: FakeReadyState) {}

  addCloseListener(listener: () => void): void {
    this.#closeListeners.push(listener);
  }

  close(): void {
    this.closes++;
    this.readyState = "closed";
    for (const listener of this.#closeListeners) listener();
  }
}

function fakeConnectionActive(connection: FakeHomeConnection): boolean {
  return connection.readyState === "open" || connection.readyState === "connecting";
}

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

test("an Offline due check replaces one OPEN or CONNECTING silent socket without stale duplicates", () => {
  for (const readyState of ["open", "connecting"] as const) {
    const owner = new HomeConnectionOwner<FakeHomeConnection>();
    let created = 0;
    let ownedCloses = 0;
    const create = () => new FakeHomeConnection(++created, readyState);
    const activate = (connection: FakeHomeConnection) => {
      connection.addCloseListener(() => {
        if (!owner.release(connection)) return;
        ownedCloses++;
      });
    };
    const first = owner.replace(create, activate);

    const outcome = maintainHomeConnection(
      "offline",
      owner,
      fakeConnectionActive,
      create,
      activate,
    );

    assert.equal(outcome, "replaced");
    assert.equal(first.closes, 1);
    assert.equal(created, 2);
    assert.equal(owner.current?.id, 2);
    assert.equal(owner.current?.readyState, readyState);
    assert.equal(ownedCloses, 0);
    assert.equal(owner.release(first), false);
    assert.equal(owner.current?.id, 2);
  }
});

test("an Offline due check connects once when no socket is owned", () => {
  const owner = new HomeConnectionOwner<FakeHomeConnection>();
  let created = 0;
  const outcome = maintainHomeConnection(
    "offline",
    owner,
    fakeConnectionActive,
    () => new FakeHomeConnection(++created, "connecting"),
    () => undefined,
  );

  assert.equal(outcome, "connected");
  assert.equal(created, 1);
  assert.equal(owner.current?.id, 1);
  assert.equal(owner.current?.readyState, "connecting");
});

test("protected unauthenticated and invitation states never create Home", () => {
  for (const [mode, authorityReady] of [
    ["checking", false],
    ["signed-out", false],
    ["trust", false],
    ["active", false],
  ] as const) {
    const owner = new HomeConnectionOwner<FakeHomeConnection>();
    let created = 0;
    const outcome = maintainHomeConnection(
      "offline",
      owner,
      fakeConnectionActive,
      () => new FakeHomeConnection(++created, "connecting"),
      () => undefined,
      false,
      homeConnectionAllowed(mode, authorityReady),
    );

    assert.equal(outcome, "stopped");
    assert.equal(created, 0);
    assert.equal(owner.current, undefined);
  }
});

test("authenticated and sign-in-off Home both retry automatically", () => {
  for (const [mode, authorityReady] of [["active", true], ["sign-in-off", false]] as const) {
    const owner = new HomeConnectionOwner<FakeHomeConnection>();
    let created = 0;
    const create = () => new FakeHomeConnection(++created, "connecting");
    const first = maintainHomeConnection(
      "offline",
      owner,
      fakeConnectionActive,
      create,
      () => undefined,
      false,
      homeConnectionAllowed(mode, authorityReady),
    );
    const retried = maintainHomeConnection(
      "offline",
      owner,
      fakeConnectionActive,
      create,
      () => undefined,
      false,
      homeConnectionAllowed(mode, authorityReady),
    );

    assert.equal(first, "connected");
    assert.equal(retried, "replaced");
    assert.equal(created, 2);
    assert.equal(owner.current?.id, 2);
  }
});

test("sign-out, revocation, reset, expiry, and invitation entry stop old Home ownership", () => {
  for (const [mode, authorityReady] of [
    ["signed-out", false],
    ["active", false],
    ["trust", false],
  ] as const) {
    const owner = new HomeConnectionOwner<FakeHomeConnection>();
    let created = 0;
    const create = () => new FakeHomeConnection(++created, "open");
    const oldAuthority = owner.replace(create, () => undefined);
    const stopped = maintainHomeConnection(
      "offline",
      owner,
      fakeConnectionActive,
      create,
      () => undefined,
      false,
      homeConnectionAllowed(mode, authorityReady),
    );
    const stillStopped = maintainHomeConnection(
      "offline",
      owner,
      fakeConnectionActive,
      create,
      () => undefined,
      false,
      homeConnectionAllowed(mode, authorityReady),
    );

    assert.equal(stopped, "stopped");
    assert.equal(stillStopped, "stopped");
    assert.equal(oldAuthority.closes, 1);
    assert.equal(created, 1);
    assert.equal(owner.current, undefined);
  }
});

test("visibility recovery preserves an active Terminal route, identity, and draft", () => {
  const hash = "#terminal=pane%2Fone&terminal_id=term-1";
  const route = parseTerminalRoute(hash);
  const terminalPage = {
    destroys: 0,
    draft: "unsent text",
    paneID: route?.paneID,
    terminalID: route?.terminalID,
  };
  const owner = new HomeConnectionOwner<FakeHomeConnection>();
  let created = 0;
  const create = () => new FakeHomeConnection(++created, "connecting");
  const activate = (connection: FakeHomeConnection) => {
    connection.addCloseListener(() => owner.release(connection));
  };
  const first = owner.replace(create, activate);
  const renderHome = () => {
    terminalPage.destroys++;
    terminalPage.draft = "";
  };
  let reconnectOutcome = "not-run";
  const reconnect = () => {
    reconnectOutcome = maintainHomeConnection(
      "current",
      owner,
      fakeConnectionActive,
      create,
      activate,
      true,
    );
  };

  const hiddenResumed = resumeHomeConnection(
    "hidden",
    route === undefined,
    renderHome,
    reconnect,
  );
  assert.equal(hiddenResumed, false);
  assert.equal(created, 1);
  assert.equal(reconnectOutcome, "not-run");

  const visibleResumed = resumeHomeConnection(
    "visible",
    route === undefined,
    renderHome,
    reconnect,
  );

  const currentRoute = parseTerminalRoute(hash);
  assert.equal(visibleResumed, true);
  assert.equal(reconnectOutcome, "replaced");
  assert.equal(first.closes, 1);
  assert.equal(created, 2);
  assert.equal(owner.current?.id, 2);
  assert.equal(terminalPage.destroys, 0);
  assert.equal(terminalPage.draft, "unsent text");
  assert.equal(terminalPage.paneID, "pane/one");
  assert.equal(terminalPage.terminalID, "term-1");
  assert.equal(currentRoute?.paneID, "pane/one");
  assert.equal(currentRoute?.terminalID, "term-1");
});

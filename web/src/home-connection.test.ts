import assert from "node:assert/strict";
import test from "node:test";

import {
  HomeConnectionOwner,
  HomeResumeTracker,
  HOME_FIRST_VALID_FRAME_WINDOW_MS,
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

test("an Offline attempt gets the full bounded window for a delayed first frame", () => {
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
    const offlineAt = HOME_RECOVERY_LIMIT_MS;
    const first = owner.replace(create, activate, offlineAt);

    assert.equal(
      nextHomeCheckDelay(0, offlineAt, true, owner.attemptStartedAt),
      HOME_FIRST_VALID_FRAME_WINDOW_MS,
    );

    const insideWindow = maintainHomeConnection(
      "offline",
      owner,
      fakeConnectionActive,
      create,
      activate,
      offlineAt + HOME_FIRST_VALID_FRAME_WINDOW_MS - 1,
    );

    assert.equal(insideWindow, "waiting");
    assert.equal(first.closes, 0);
    assert.equal(created, 1);

    const atDeadline = maintainHomeConnection(
      "offline",
      owner,
      fakeConnectionActive,
      create,
      activate,
      offlineAt + HOME_FIRST_VALID_FRAME_WINDOW_MS,
    );

    assert.equal(atDeadline, "replaced");
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
    HOME_RECOVERY_LIMIT_MS,
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
      HOME_RECOVERY_LIMIT_MS,
      "when-needed",
      homeConnectionAllowed(mode, authorityReady),
    );

    assert.equal(outcome, "stopped");
    assert.equal(created, 0);
    assert.equal(owner.current, undefined);
  }
});

test("authenticated and sign-in-off Home both retry automatically when no viable attempt exists", () => {
  for (const [mode, authorityReady] of [["active", true], ["sign-in-off", false]] as const) {
    const owner = new HomeConnectionOwner<FakeHomeConnection>();
    let created = 0;
    const create = () => new FakeHomeConnection(++created, "connecting");
    const activate = (connection: FakeHomeConnection) => {
      connection.addCloseListener(() => owner.release(connection));
    };
    const first = maintainHomeConnection(
      "offline",
      owner,
      fakeConnectionActive,
      create,
      activate,
      HOME_RECOVERY_LIMIT_MS,
      "when-needed",
      homeConnectionAllowed(mode, authorityReady),
    );
    owner.current?.close();
    const retried = maintainHomeConnection(
      "offline",
      owner,
      fakeConnectionActive,
      create,
      activate,
      HOME_RECOVERY_LIMIT_MS + HOME_RETRY_DELAY_MS,
      "when-needed",
      homeConnectionAllowed(mode, authorityReady),
    );

    assert.equal(first, "connected");
    assert.equal(retried, "connected");
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
    const oldAuthority = owner.replace(create, () => undefined, 0);
    const stopped = maintainHomeConnection(
      "offline",
      owner,
      fakeConnectionActive,
      create,
      () => undefined,
      HOME_RECOVERY_LIMIT_MS,
      "when-needed",
      homeConnectionAllowed(mode, authorityReady),
    );
    const stillStopped = maintainHomeConnection(
      "offline",
      owner,
      fakeConnectionActive,
      create,
      () => undefined,
      HOME_RECOVERY_LIMIT_MS,
      "when-needed",
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
  const first = owner.replace(create, activate, 0);
  owner.recordValidFrame(first);
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
      HOME_RECOVERY_LIMIT_MS,
      "after-attempt-window",
    );
  };

  const hiddenResumed = resumeHomeConnection(
    false,
    route === undefined,
    renderHome,
    reconnect,
  );
  assert.equal(hiddenResumed, false);
  assert.equal(created, 1);
  assert.equal(reconnectOutcome, "not-run");

  const visibleResumed = resumeHomeConnection(
    true,
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

test("the first return event replaces a young pre-return attempt and clustered events preserve it", () => {
  const owner = new HomeConnectionOwner<FakeHomeConnection>();
  const resumeTracker = new HomeResumeTracker();
  let now = 5_000;
  let created = 0;
  let renders = 0;
  const outcomes: string[] = [];
  const create = () => new FakeHomeConnection(++created, "connecting");
  const activate = (connection: FakeHomeConnection) => {
    connection.addCloseListener(() => owner.release(connection));
  };
  const old = owner.replace(create, activate, now - 1_000);
  const reconnect = () => {
    outcomes.push(maintainHomeConnection(
      "current",
      owner,
      fakeConnectionActive,
      create,
      activate,
      now,
      resumeTracker.resumeReplacement(),
    ));
  };

  resumeTracker.markNonForeground();
  assert.equal(resumeHomeConnection(true, true, () => {
    renders++;
  }, reconnect), true);
  const resumed = owner.current;
  assert.equal(resumed?.id, 2);
  assert.equal(old.closes, 1);

  if (resumed) {
    resumed.readyState = "open";
    owner.recordValidFrame(resumed);
  }
  now += 25;
  assert.equal(resumeHomeConnection(true, true, () => {
    renders++;
  }, reconnect), true);
  now += 25;
  assert.equal(resumeHomeConnection(true, true, () => {
    renders++;
  }, reconnect), true);

  assert.equal(created, 2);
  assert.equal(resumed?.closes, 0);
  assert.equal(owner.current?.id, 2);
  assert.equal(renders, 3);
  assert.deepEqual(outcomes, ["replaced", "waiting", "waiting"]);
});

test("a valid frame immediately restores current evidence and keeps its socket", () => {
  const owner = new HomeConnectionOwner<FakeHomeConnection>();
  let created = 0;
  let lastValidHomeFrameAt = 0;
  const offlineAt = HOME_RECOVERY_LIMIT_MS;
  const create = () => new FakeHomeConnection(++created, "open");
  const socket = owner.replace(create, () => undefined, offlineAt);

  assert.equal(homeReachability(lastValidHomeFrameAt, offlineAt), "offline");
  const frameAt = offlineAt + 500;
  assert.equal(owner.recordValidFrame(socket), true);
  lastValidHomeFrameAt = frameAt;
  assert.equal(homeReachability(lastValidHomeFrameAt, frameAt), "current");

  const outcome = maintainHomeConnection(
    homeReachability(lastValidHomeFrameAt, frameAt),
    owner,
    fakeConnectionActive,
    create,
    () => undefined,
    frameAt,
  );
  assert.equal(outcome, "waiting");
  assert.equal(created, 1);
  assert.equal(socket.closes, 0);
});

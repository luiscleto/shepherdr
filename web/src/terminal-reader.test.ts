import assert from "node:assert/strict";
import test from "node:test";

import { Window } from "happy-dom";

import { terminalReaderForDevice } from "./terminal/device";
import { readerActionAvailability } from "./terminal/reader-availability";
import { pauseReaderLiveRefresh, ReaderView, readerAtLatest } from "./terminal/reader-view";

class TestIntersectionObserver {
  readonly root = null;
  readonly rootMargin = "";
  readonly thresholds: number[] = [];

  constructor(_callback: IntersectionObserverCallback) {}
  disconnect(): void {}
  observe(): void {}
  takeRecords(): IntersectionObserverEntry[] { return []; }
  unobserve(): void {}
}

function withReaderBrowser(t: test.TestContext): { browser: Window; host: HTMLElement } {
  const browser = new Window({ url: "http://localhost/" });
  const previous = {
    document: globalThis.document,
    HTMLElement: globalThis.HTMLElement,
    HTMLSpanElement: globalThis.HTMLSpanElement,
    IntersectionObserver: globalThis.IntersectionObserver,
    window: globalThis.window,
  };
  Object.assign(globalThis, {
    document: browser.document,
    HTMLElement: browser.HTMLElement,
    HTMLSpanElement: browser.HTMLSpanElement,
    IntersectionObserver: TestIntersectionObserver,
    window: browser,
  });
  const host = browser.document.createElement("div") as unknown as HTMLElement;
  browser.document.body.append(host);
  t.after(() => {
    Object.assign(globalThis, previous);
    browser.close();
  });
  return { browser, host };
}

function snapshot(ansi: string): Response {
  return new Response(JSON.stringify({ ansi, cols: 80, generation: 1, rows: 24, terminal_id: "term-1" }), {
    headers: { "Content-Type": "application/json" },
    status: 200,
  });
}

test("Reader pauses moving output while someone is reading away from latest", () => {
  assert.equal(readerAtLatest(2_000, 400, 600), false);
  assert.equal(readerAtLatest(2_000, 1_400, 600), true);
  assert.equal(readerAtLatest(2_000, 1_375, 600), true, "a small bottom tolerance avoids status flapping");
  assert.equal(readerAtLatest(2_000, 1_360, 600), false);
  assert.equal(pauseReaderLiveRefresh(false, false), true, "live replacement pauses away from latest");
  assert.equal(pauseReaderLiveRefresh(true, true), true, "live replacement pauses during selection");
  assert.equal(pauseReaderLiveRefresh(true, false), false, "returning to latest permits one fresh snapshot");
});

test("the production Reader follows the primary coarse pointer even when another fine pointer exists", () => {
  const media = {
    matchMedia(query: string) {
      return { matches: query === "(pointer: coarse)" } as MediaQueryList;
    },
  } as Pick<Window, "matchMedia">;
  assert.equal(terminalReaderForDevice(media), true);

  const desktop = {
    matchMedia() {
      return { matches: false } as MediaQueryList;
    },
  } as Pick<Window, "matchMedia">;
  assert.equal(terminalReaderForDevice(desktop), false);
});

test("Reader availability composes observer readiness with every unresolved send state", () => {
  assert.deepEqual(readerActionAvailability(false, "ready"), { observerReady: false, recover: false, send: false });
  assert.deepEqual(readerActionAvailability(true, "ready"), { observerReady: true, recover: false, send: true });
  for (const state of ["requesting", "forwarding", "failed", "occupied", "uncertain"] as const) {
    assert.equal(readerActionAvailability(true, state).send, false, `${state} must keep the command row unavailable after reconnect`);
  }
  assert.equal(readerActionAvailability(false, "failed").recover, false);
  assert.equal(readerActionAvailability(true, "failed").recover, true);
  assert.equal(readerActionAvailability(false, "occupied").recover, false);
  assert.equal(readerActionAvailability(true, "occupied").recover, true);
  assert.equal(readerActionAvailability(false, "uncertain").recover, true, "local dismissal does not require a terminal connection");
});

test("collapsed Reader keeps recovery visible but gates remote actions across disconnect and reconnect", (t) => {
  const { host } = withReaderBrowser(t);
  const reader = new ReaderView(host, {
    onLog: () => undefined,
    onStatus: () => undefined,
    onSubmit: () => true,
  }, { collapsibleComposer: true, endpoint: "/api/terminal/read" });
  const composer = host.querySelector(".reader-composer") as HTMLDivElement;
  const feedback = host.querySelector(".reader-send-feedback") as HTMLDivElement;
  const buttons = Array.from(feedback.querySelectorAll("button")) as HTMLButtonElement[];
  const retry = buttons.find((button) => button.textContent === "Try again")!;
  const takeover = buttons.find((button) => button.textContent === "Take over and send")!;

  let takeoverRequested = false;
  reader.setActionAvailability(readerActionAvailability(false, "occupied"));
  reader.inputOccupied("Someone else is controlling this terminal", () => takeoverRequested = true);
  assert.equal(composer.hidden, true);
  assert.equal(feedback.hidden, false, "recovery feedback must remain visible while disconnected");
  assert.equal(retry.hidden, true, "occupied recovery must not offer ordinary retry");
  assert.equal(takeover.hidden, false);
  assert.equal(takeover.disabled, true, "takeover must not be usable while the observer is disconnected");
  takeover.click();
  assert.equal(takeoverRequested, false);

  reader.setActionAvailability(readerActionAvailability(true, "occupied"));
  assert.equal(takeover.disabled, false);
  assert.equal((host.querySelector("textarea") as HTMLTextAreaElement).disabled, true, "reconnect must not enable a new batch during recovery");
  takeover.click();
  assert.equal(takeoverRequested, true);

  let retryRequested = false;
  reader.setActionAvailability(readerActionAvailability(false, "failed"));
  reader.inputFailed("Control could not be acquired", () => retryRequested = true);
  assert.equal(retry.hidden, false, "ordinary acquisition failure may offer ordinary retry");
  assert.equal(retry.disabled, true, "ordinary retry must not be usable while disconnected");
  assert.equal(takeover.hidden, true, "ordinary failure must not offer takeover");
  retry.click();
  assert.equal(retryRequested, false);
  reader.setActionAvailability(readerActionAvailability(true, "failed"));
  assert.equal(retry.disabled, false);
  retry.click();
  assert.equal(retryRequested, true);

  let dismissed = false;
  reader.setActionAvailability(readerActionAvailability(false, "uncertain"));
  reader.inputUncertain("Delivery could not be confirmed", () => dismissed = true);
  assert.equal(composer.hidden, true, "shortcut recovery must not open the text composer");
  assert.equal(retry.textContent, "Dismiss");
  assert.equal(retry.disabled, false);
  retry.click();
  assert.equal(dismissed, true);
  assert.equal(feedback.hidden, true);
  reader.destroy();
});

test("Reader preserves a native selection made while history pagination is in flight", async (t) => {
  const { browser, host } = withReaderBrowser(t);
  const previousFetch = globalThis.fetch;
  let fetchCount = 0;
  let finishPagination: ((response: Response) => void) | undefined;
  globalThis.fetch = async () => {
    fetchCount += 1;
    if (fetchCount === 1) return snapshot("hello");
    return await new Promise<Response>((resolve) => {
      finishPagination = resolve;
    });
  };
  t.after(() => {
    globalThis.fetch = previousFetch;
  });

  const reader = new ReaderView(host, {
    onLog: () => undefined,
    onStatus: () => undefined,
    onSubmit: () => true,
  }, { endpoint: "/api/terminal/read" });
  await reader.open("pane-1", "term-1");
  const pagination = reader.refresh(true, true);
  assert.equal(fetchCount, 2);

  const output = host.querySelector(".reader-output") as HTMLDivElement;
  const text = output.querySelector("span")?.firstChild;
  assert.ok(text);
  const range = browser.document.createRange();
  range.setStart(text, 0);
  range.setEnd(text, 5);
  const selection = browser.getSelection()!;
  selection.removeAllRanges();
  selection.addRange(range);

  finishPagination?.(snapshot("older\nhello"));
  await pagination;
  assert.equal(selection.toString(), "hello");
  assert.equal(output.contains(selection.anchorNode), true, "selected DOM must not be replaced after pagination resolves");
  assert.equal(output.textContent, "hello");
  reader.destroy();
});

import assert from "node:assert/strict";
import test from "node:test";

import { Window } from "happy-dom";

import { terminalReaderForDevice } from "./terminal/device";
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

test("collapsed Reader shows shortcut recovery with only the truthful action", (t) => {
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

  reader.inputUncertain("Delivery could not be confirmed");
  assert.equal(composer.hidden, true, "shortcut recovery must not open the text composer");
  assert.equal(feedback.hidden, false, "uncertain shortcut feedback must be visible");
  assert.equal(retry.hidden, true);
  assert.equal(takeover.hidden, true);

  let takeoverRequested = false;
  reader.inputOccupied("Someone else is controlling this terminal", () => takeoverRequested = true);
  assert.equal(composer.hidden, true);
  assert.equal(retry.hidden, true, "occupied recovery must not offer ordinary retry");
  assert.equal(takeover.hidden, false);
  takeover.click();
  assert.equal(takeoverRequested, true);

  reader.inputFailed("Control could not be acquired", () => undefined);
  assert.equal(retry.hidden, false, "ordinary acquisition failure may offer ordinary retry");
  assert.equal(takeover.hidden, true, "ordinary failure must not offer takeover");
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

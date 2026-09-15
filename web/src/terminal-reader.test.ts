import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { Window } from "happy-dom";

import { terminalReaderForDevice } from "./terminal/device";
import { readerActionAvailability } from "./terminal/reader-availability";
import { pauseReaderLiveRefresh, ReaderView, readerAtLatest } from "./terminal/reader-view";

class TestIntersectionObserver {
  static latest: TestIntersectionObserver;
  readonly root = null;
  readonly rootMargin = "";
  readonly thresholds: number[] = [];

  constructor(readonly callback: IntersectionObserverCallback) { TestIntersectionObserver.latest = this; }
  intersect(): void {
    this.callback([{ isIntersecting: true } as IntersectionObserverEntry], this as unknown as IntersectionObserver);
  }
  disconnect(): void {}
  observe(): void {}
  takeRecords(): IntersectionObserverEntry[] { return []; }
  unobserve(): void {}
}

function withReaderBrowser(t: test.TestContext, beforeClose = () => {}): { browser: Window; host: HTMLElement } {
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
    beforeClose();
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

test("Reader history banner requires upward intent, reuses a confirmed result, and clears stale evidence", async (t) => {
  let reader: ReaderView;
  const { host, browser } = withReaderBrowser(t, () => reader?.destroy());
  let ansi = "retained";
  let reads = 0;
  let opens = 0;
  t.mock.method(globalThis, "fetch", async () => { reads++; return snapshot(ansi); });
  reader = new ReaderView(host, {
    onLog() {}, onStatus() {}, onSubmit: () => true, onOpenTerminal() { opens++; },
  }, { endpoint: "/api/terminal/read" });
  const hint = host.querySelector<HTMLElement>(".reader-history-banner")!;
  const scroll = host.querySelector<HTMLElement>(".reader-scroll")!;
  const upward = () => scroll.dispatchEvent(new browser.WheelEvent("wheel", { deltaY: -10 }) as unknown as Event);
  await reader.open("pane-1", "term-1");
  assert.equal(hint.hidden, true);
  Object.defineProperties(scroll, { scrollHeight: { value: 1200 }, clientHeight: { value: 300 } });
  await reader.refresh(false, false);
  assert.equal(hint.hidden, true, "ordinary live output cannot show the hint");
  TestIntersectionObserver.latest.intersect();
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(hint.hidden, true, "automatic pagination/layout cannot show the hint");
  const before = reads;
  scroll.scrollTop = 400;
  upward();
  assert.equal(hint.hidden, true, "only the top history region qualifies");
  scroll.scrollTop = 0;
  const zoom = new browser.WheelEvent("wheel", { deltaY: -10 });
  // happy-dom's WheelEvent does not inherit MouseEvent's modifier properties.
  Object.defineProperty(zoom, "ctrlKey", { value: true });
  scroll.dispatchEvent(zoom as unknown as Event);
  scroll.dispatchEvent(new browser.WheelEvent("wheel", { deltaY: 10 }) as unknown as Event);
  assert.equal(hint.hidden, true, "zoom and downward input do not qualify");
  upward();
  upward();
  assert.equal(hint.hidden, false);
  assert.equal(hint.querySelector("span")?.textContent, "More history may be available in Terminal.");
  assert.equal(hint.parentElement === host, true);
  assert.equal(host.querySelectorAll(".reader-history-banner").length, 1);
  assert.equal(reads, before, "the hint must not initiate another read");
  hint.querySelector<HTMLButtonElement>("button")!.click();
  assert.equal(opens, 1);
  ansi = "older\nretained";
  await reader.refresh(true, true);
  assert.equal(hint.hidden, true, "additional output invalidates the hint");
  upward();
  assert.equal(hint.hidden, true, "growth invalidates the previous no-growth result");
  await new Promise(resolve => setTimeout(resolve, 0));
  upward();
  assert.equal(hint.hidden, false);
  await reader.open("pane-2", "term-1");
  upward();
  assert.equal(hint.hidden, true, "a new pane has no confirmed no-growth result");
});

test("Reader history banner qualifies an in-flight older result, but not failures or selection drags", async (t) => {
  let reader: ReaderView;
  const { host, browser } = withReaderBrowser(t, () => reader?.destroy());
  t.mock.method(globalThis, "fetch", async () => snapshot("retained"));
  reader = new ReaderView(host, {
    onLog() {}, onStatus() {}, onSubmit: () => true, onOpenTerminal() {},
  }, { endpoint: "/api/terminal/read" });
  await reader.open("pane-1", "term-1");
  const hint = host.querySelector<HTMLElement>(".reader-history-banner")!;
  const scroll = host.querySelector<HTMLElement>(".reader-scroll")!;
  const upward = () => scroll.dispatchEvent(new browser.WheelEvent("wheel", { deltaY: -10 }) as unknown as Event);
  let finish!: (response: Response) => void;
  t.mock.method(globalThis, "fetch", () => new Promise<Response>(resolve => { finish = resolve; }));
  const failed = reader.refresh(true, true);
  upward();
  finish(new Response("unavailable", { status: 503 }));
  await failed;
  assert.equal(hint.hidden, true);
  t.mock.method(globalThis, "fetch", async () => snapshot("retained"));
  await reader.refresh(false, false);
  await reader.refresh(true, true);
  assert.equal(hint.hidden, true, "failed request intent cannot leak into later reads");
  await reader.open("pane-live", "term-1");
  t.mock.method(globalThis, "fetch", () => new Promise<Response>(resolve => { finish = resolve; }));
  const liveFailure = reader.refresh(false, false);
  upward();
  finish(new Response("unavailable", { status: 503 }));
  await liveFailure;
  t.mock.method(globalThis, "fetch", async () => snapshot("retained"));
  await reader.refresh(true, true);
  assert.equal(hint.hidden, true, "intent during a failed live read cannot qualify a later older read");
  const range = browser.document.createRange();
  range.selectNodeContents(host.querySelector(".reader-output")!);
  browser.getSelection()!.addRange(range);
  upward();
  assert.equal(hint.hidden, true, "a selection gesture cannot reveal the hint");
  browser.getSelection()!.removeAllRanges();
  await reader.open("pane-2", "term-1");
  t.mock.method(globalThis, "fetch", () => new Promise<Response>(resolve => { finish = resolve; }));
  const unchanged = reader.refresh(true, true);
  upward();
  finish(snapshot("retained"));
  await unchanged;
  assert.equal(hint.hidden, false, "upward intent during an older read qualifies its success");
});

test("Reader history banner handles passive touch and keyboard intent but never the depth cap alone", async (t) => {
  let reader: ReaderView;
  const { host, browser } = withReaderBrowser(t, () => reader?.destroy());
  let sequence = 0;
  t.mock.method(globalThis, "fetch", async () => snapshot(`output ${sequence++}`));
  reader = new ReaderView(host, {
    onLog() {}, onStatus() {}, onSubmit: () => true, onOpenTerminal() {},
  }, { endpoint: "/api/terminal/read" });
  await reader.open("pane-1", "term-1");
  const hint = host.querySelector<HTMLElement>(".reader-history-banner")!;
  const scroll = host.querySelector<HTMLElement>(".reader-scroll")!;
  for (let page = 0; page < 40; page++) {
    TestIntersectionObserver.latest.intersect();
    await new Promise(resolve => setTimeout(resolve, 0));
  }
  const key = new browser.KeyboardEvent("keydown", { key: "PageUp", cancelable: true });
  scroll.dispatchEvent(key as unknown as Event);
  assert.equal(key.defaultPrevented, false);
  assert.equal(hint.hidden, true, "the configured depth limit is not a no-growth result");
  assert.equal(sequence, 40, "history reads stop at 20,000 lines");
  t.mock.method(globalThis, "fetch", async () => snapshot("retained"));
  await reader.open("pane-2", "term-1");
  await reader.refresh(true, true);
  scroll.dispatchEvent(new browser.KeyboardEvent("keydown", { key: "PageUp" }) as unknown as Event);
  assert.equal(hint.hidden, false);
  await reader.open("pane-3", "term-1");
  await reader.refresh(true, true);
  const touch = (type: string, positions: number[]) => {
    const event = new browser.Event(type, { cancelable: true });
    Object.defineProperty(event, "touches", { value: positions.map(clientY => ({ clientX: 20, clientY })) });
    scroll.dispatchEvent(event as unknown as Event);
    assert.equal(event.defaultPrevented, false);
  };
  touch("touchstart", [50, 60]);
  touch("touchmove", [80, 90]);
  assert.equal(hint.hidden, true, "multitouch cannot qualify");
  touch("touchstart", [50]);
  touch("touchmove", [80]);
  assert.equal(hint.hidden, false);
});

test("Reader upward intent during a live read checks older output once after success", async (t) => {
  let reader: ReaderView;
  const { host, browser } = withReaderBrowser(t, () => reader?.destroy());
  t.mock.method(globalThis, "fetch", async () => snapshot("retained"));
  reader = new ReaderView(host, {
    onLog() {}, onStatus() {}, onSubmit: () => true, onOpenTerminal() {},
  }, { endpoint: "/api/terminal/read" });
  await reader.open("pane-1", "term-1");
  let finish!: (response: Response) => void;
  const depths: number[] = [];
  t.mock.method(globalThis, "fetch", (url: string) => {
    depths.push(Number(new URL(url, "http://localhost").searchParams.get("lines")));
    return depths.length === 1 ? new Promise<Response>(resolve => { finish = resolve; }) : Promise.resolve(snapshot("retained"));
  });
  const live = reader.refresh(false, false);
  const scroll = host.querySelector<HTMLElement>(".reader-scroll")!;
  for (let attempt = 0; attempt < 5; attempt++) {
    scroll.dispatchEvent(new browser.WheelEvent("wheel", { deltaY: -10 }) as unknown as Event);
  }
  const banner = host.querySelector<HTMLElement>(".reader-history-banner")!;
  assert.equal(banner.hidden, true);
  assert.equal(depths.join(","), "500");
  finish(snapshot("retained"));
  await live;
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(depths.join(","), "500,1000", "one attempt is not lost or multiplied by an in-flight live read");
  assert.equal(banner.hidden, false);
});

test("Reader checks nonoverflow upward touch intent without another intersection or scroll event", async (t) => {
  let reader: ReaderView;
  const { host, browser } = withReaderBrowser(t, () => reader?.destroy());
  const depths: number[] = [];
  let ansi = "alternate screen";
  t.mock.method(globalThis, "fetch", async (url: string) => {
    depths.push(Number(new URL(url, "http://localhost").searchParams.get("lines")));
    return snapshot(ansi);
  });
  reader = new ReaderView(host, {
    onLog() {}, onStatus() {}, onSubmit: () => true, onOpenTerminal() {},
  }, { endpoint: "/api/terminal/read" });
  const banner = host.querySelector<HTMLElement>(".reader-history-banner")!;
  const scroll = host.querySelector<HTMLElement>(".reader-scroll")!;
  Object.defineProperties(scroll, { scrollHeight: { value: 300 }, clientHeight: { value: 300 } });
  const touch = (type: string, y: number, x = 20) => {
    const event = new browser.Event(type, { cancelable: true });
    Object.defineProperty(event, "touches", { value: [{ clientX: x, clientY: y }] });
    scroll.dispatchEvent(event as unknown as Event);
    assert.equal(event.defaultPrevented, false);
  };
  TestIntersectionObserver.latest.intersect();
  await reader.open("pane-1", "term-1");
  scroll.scrollTop = 0; // happy-dom does not clamp scrollTop to the nonoverflow extent.
  touch("touchstart", 80);
  touch("touchmove", 40);
  touch("touchstart", 40);
  touch("touchmove", 44);
  touch("touchmove", 45, 80);
  assert.equal(depths.join(","), "500", "downward movement, jitter and horizontal swipes do not read history");
  assert.equal(banner.hidden, true);
  touch("touchstart", 40);
  touch("touchmove", 80);
  touch("touchmove", 110);
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(depths.join(","), "500,1000");
  assert.equal(banner.hidden, false);
  assert.equal(scroll.scrollTop, 0);
  const close = banner.querySelector<HTMLButtonElement>('[aria-label="Dismiss history hint"]')!;
  assert.equal(close.querySelector("svg") !== null, true);
  close.click();
  for (let attempt = 0; attempt < 5; attempt++) {
    touch("touchstart", 40);
    touch("touchmove", 80);
  }
  assert.equal(banner.hidden, true);
  assert.equal(depths.length, 2, "unchanged exhaustion stops repeated reads");
  ansi = "updated alternate screen";
  await reader.refresh(false, false);
  scroll.scrollTop = 0;
  reader.setPresentation(false);
  touch("touchstart", 40);
  touch("touchmove", 80);
  assert.equal(depths.length, 3, "full Terminal cannot request Reader history");
  reader.setPresentation(true);
  touch("touchstart", 40);
  touch("touchmove", 80);
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(depths.length, 4);
  assert.equal(banner.hidden, true, "dismissal survives output and view changes for this visit");
  await reader.open("pane-2", "term-1");
  scroll.scrollTop = 0;
  touch("touchstart", 40);
  touch("touchmove", 80);
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(banner.hidden, false, "a new visit can show the banner again");
});

test("ten view toggles retain Reader nodes, position, draft, selection and files without hidden reads", async (t) => {
  const { host, browser } = withReaderBrowser(t);
  const previousFetch = globalThis.fetch;
  let reads = 0;
  globalThis.fetch = async () => { reads++; return snapshot("retained output"); };
  t.after(() => { globalThis.fetch = previousFetch; });
  const reader = new ReaderView(host, { onLog() {}, onStatus() {}, onSubmit: () => true }, { endpoint: "/api/terminal/read", collapsibleComposer: true });
  await reader.open("pane-1", "term-1");
  reader.setActionAvailability({ edit: true, observerReady: true, recover: false, send: true });
  reader.setFileSelectionAvailable(true);
  reader.showComposer();
  const editor = host.querySelector("textarea")!;
  const output = host.querySelector(".reader-output")!;
  const scroll = host.querySelector<HTMLElement>(".reader-scroll")!;
  editor.value = "keep this draft";
  editor.setSelectionRange(2, 5);
  scroll.scrollTop = 120;
  const fileInput = host.querySelector<HTMLInputElement>(".reader-file-input")!;
  Object.defineProperty(fileInput, "files", { value: [new browser.File(["tiny"], "trial.txt")], configurable: true });
  fileInput.dispatchEvent(new browser.Event("change") as unknown as Event);
  await new Promise(resolve => setTimeout(resolve, 0));
  const fileCard = host.querySelector(".reader-file-chip");
  assert.equal(Boolean(fileCard), true);
  const text = output.querySelector("span")?.firstChild ?? output.firstChild!;
  const range = document.createRange();
  range.selectNodeContents(text);
  window.getSelection()!.removeAllRanges();
  window.getSelection()!.addRange(range);
  const selection = window.getSelection()!.toString();
  for (let count = 0; count < 10; count++) {
    reader.setPresentation(false);
    reader.refreshSoon();
    await reader.refresh(true, true);
    reader.setPresentation(true);
  }
  assert.equal(reads, 1);
  assert.equal(host.querySelector(".reader-output") === output, true);
  assert.equal(host.querySelector("textarea") === editor, true);
  assert.equal(host.querySelector(".reader-file-chip") === fileCard, true);
  assert.equal(editor.value, "keep this draft");
  assert.equal(editor.selectionStart, 2);
  assert.equal(editor.selectionEnd, 5);
  assert.equal(scroll.scrollTop, 120);
  assert.equal(window.getSelection()!.toString(), selection);
  assert.equal(host.querySelector<HTMLElement>(".reader-composer")!.hidden, false);
  reader.destroy();
});

test("Reader does not restore a saved selection after the person changes it in the other view", async (t) => {
  const { host } = withReaderBrowser(t);
  const previousFetch = globalThis.fetch;
  globalThis.fetch = async () => snapshot("old selection");
  t.after(() => { globalThis.fetch = previousFetch; });
  const reader = new ReaderView(host, { onLog() {}, onStatus() {}, onSubmit: () => true }, { endpoint: "/api/terminal/read" });
  await reader.open("pane-1", "term-1");
  const range = document.createRange();
  range.selectNodeContents(host.querySelector(".reader-output")!);
  window.getSelection()!.addRange(range);
  reader.setPresentation(false);
  const other = document.createElement("p");
  other.textContent = "new selection";
  document.body.append(other);
  const next = document.createRange();
  next.selectNodeContents(other);
  window.getSelection()!.removeAllRanges();
  window.getSelection()!.addRange(next);
  document.dispatchEvent(new (window as unknown as { Event: typeof Event }).Event("selectionchange"));
  window.getSelection()!.removeAllRanges();
  reader.setPresentation(true);
  assert.equal(window.getSelection()!.toString(), "");
  reader.destroy();
});

test("files-only presentation reuses controls and preserves the Reader draft on insert and cancel", async t => {
  const { host, browser } = withReaderBrowser(t);
  let inserts = 0, submissions = 0;
  const reader = new ReaderView(host, {
    onLog() {}, onStatus() {}, onSubmit() { submissions++; return true; },
    onInsertFiles() { inserts++; return true; },
  }, { endpoint: "/api/terminal/read", collapsibleComposer: true });
  reader.setActionAvailability({ edit: true, observerReady: true, recover: false, send: true });
  reader.setFileSelectionAvailable(true);
  reader.showComposer();
  const editor = host.querySelector("textarea")!;
  editor.value = "unsent Reader text";
  editor.setSelectionRange(2, 5);
  const picker = host.querySelector<HTMLInputElement>(".reader-file-input")!;
  Object.defineProperty(picker, "files", { value: [new browser.File(["tiny"], "trial.txt")], configurable: true });
  picker.dispatchEvent(new browser.Event("change") as unknown as Event);
  await new Promise(resolve => setTimeout(resolve, 0));
  const file = host.querySelector(".reader-file-chip");
  const send = host.querySelector<HTMLButtonElement>(".reader-send")!;
  reader.setPresentation(false);
  reader.showFileInsertion();
  assert.equal(Boolean(host.querySelector(".terminal-file-insertion")), true);
  assert.equal(editor.hidden, true);
  assert.equal(send.textContent, "Insert files");
  assert.equal(host.querySelector(".reader-file-chip") === file, true);
  send.click();
  assert.equal(inserts, 1);
  assert.equal(submissions, 0);
  reader.inputForwarded(false, true);
  assert.equal(editor.value, "unsent Reader text");
  host.querySelector<HTMLButtonElement>('[aria-label="Cancel file insertion"]')!.click();
  assert.equal(host.querySelector<HTMLElement>(".reader-file-menu")!.hidden, true);
  reader.setPresentation(true);
  assert.equal(Boolean(host.querySelector(".terminal-file-insertion")), false);
  assert.equal(editor.hidden, false);
  assert.equal(editor.selectionStart, 2);
  assert.equal(editor.selectionEnd, 5);
  assert.equal(host.querySelector("textarea") === editor, true);
  assert.equal(send.textContent, "");
  assert.equal(send.getAttribute("aria-label"), "Send text");
  reader.destroy();
});

test("Reader pauses moving output while someone is reading away from latest", () => {
  assert.equal(readerAtLatest(2_000, 400, 600), false);
  assert.equal(readerAtLatest(2_000, 1_400, 600), true);
  assert.equal(readerAtLatest(2_000, 1_375, 600), true, "a small bottom tolerance avoids status flapping");
  assert.equal(readerAtLatest(2_000, 1_360, 600), false);
  assert.equal(pauseReaderLiveRefresh(false, false), true, "live replacement pauses away from latest");
  assert.equal(pauseReaderLiveRefresh(true, true), true, "live replacement pauses during selection");
  assert.equal(pauseReaderLiveRefresh(true, false), false, "returning to latest permits one fresh snapshot");
});

test("Reader uses snapshot dimensions only for reading", async (t) => {
  const { host } = withReaderBrowser(t);
  const previousFetch = globalThis.fetch;
  globalThis.fetch = async () => snapshot("output");
  t.after(() => globalThis.fetch = previousFetch);
  const reader = new ReaderView(host, {
    onLog: () => undefined,
    onStatus: () => undefined,
    onSubmit: () => true,
  }, { endpoint: "/api/terminal/read" });

  await reader.open("pane-1", "term-1");
  assert.equal(reader.dimensions().rows, 24);
  await reader.refresh(true, false);
  assert.equal(reader.dimensions().rows, 24);
  reader.destroy();
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
  assert.equal(JSON.stringify(readerActionAvailability(false, "ready")), JSON.stringify({ edit: true, observerReady: false, recover: false, send: false }));
  assert.equal(JSON.stringify(readerActionAvailability(true, "ready")), JSON.stringify({ edit: true, observerReady: true, recover: false, send: true }));
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

test("an open Reader composer remains the same local editor across sizing and observer reconnects", (t) => {
  const { browser, host } = withReaderBrowser(t);
  let submissions = 0;
  const reader = new ReaderView(host, {
    onLog: () => undefined,
    onStatus: () => undefined,
    onSubmit: () => {
      submissions += 1;
      return true;
    },
  }, { collapsibleComposer: true, endpoint: "/api/terminal/read" });
  const composer = host.querySelector(".reader-composer") as HTMLDivElement;
  const input = composer.querySelector("textarea") as HTMLTextAreaElement;
  const send = composer.querySelector('button[aria-label="Send text"]') as HTMLButtonElement;
  const close = composer.querySelector('button[aria-label="Close composer"]') as HTMLButtonElement;

  reader.setActionAvailability(readerActionAvailability(true, "ready"));
  reader.showComposer();
  input.value = "keep this reconnect draft";
  input.setSelectionRange(5, 19, "backward");
  input.focus();

  reader.setActionAvailability(readerActionAvailability(false, "ready"));
  assert.equal(composer.hidden, false);
  assert.equal(input.disabled, false, "sizing or observer loss must leave the open textarea locally editable");
  assert.equal(send.disabled, true);
  assert.equal(input.value, "keep this reconnect draft");
  assert.equal(input.selectionStart, 5);
  assert.equal(input.selectionEnd, 19);
  assert.equal(input.selectionDirection, "backward");
  assert.equal(browser.document.activeElement === input, true);
  assert.equal(host.querySelector(".reader-composer") === composer, true);
  assert.equal(host.querySelector("textarea") === input, true);
  send.dispatchEvent(new browser.Event("click", { bubbles: true }));
  assert.equal(submissions, 0, "a dispatched click must not bypass unavailable sending");

  input.setRangeText("local", 5, 9, "end");
  const editedDraft = input.value;
  const editedSelectionStart = input.selectionStart;
  const editedSelectionEnd = input.selectionEnd;
  reader.setActionAvailability(readerActionAvailability(true, "ready"));
  assert.equal(send.disabled, false);
  assert.equal(input.value, editedDraft);
  assert.equal(input.selectionStart, editedSelectionStart);
  assert.equal(input.selectionEnd, editedSelectionEnd);
  assert.equal(browser.document.activeElement === input, true);
  assert.equal(host.querySelector("textarea") === input, true);

  reader.setActionAvailability(readerActionAvailability(false, "ready"));
  browser.document.body.focus();
  reader.setActionAvailability(readerActionAvailability(true, "ready"));
  assert.equal(browser.document.activeElement === input, false, "reconnect must not refocus an unfocused composer");
  reader.setActionAvailability(readerActionAvailability(false, "ready"));
  close.click();
  assert.equal(composer.hidden, true, "Close remains a local action while sending is unavailable");
  reader.setActionAvailability(readerActionAvailability(true, "ready"));
  assert.equal(composer.hidden, true, "reconnect must not reopen a closed composer");
  assert.equal(browser.document.activeElement === input, false);
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

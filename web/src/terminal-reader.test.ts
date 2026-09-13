import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
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

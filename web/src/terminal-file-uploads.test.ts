import assert from "node:assert/strict";
import test from "node:test";

import { Window } from "happy-dom";

import { readerActionAvailability } from "./terminal/reader-availability";
import { sendTerminalFiles } from "./terminal/file-uploads";
import { ReaderView } from "./terminal/reader-view";

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

function withFileBrowser(t: test.TestContext): { browser: Window; host: HTMLElement; revoked: string[] } {
  const browser = new Window({ url: "http://localhost/" });
  const previous = {
    document: globalThis.document,
    HTMLElement: globalThis.HTMLElement,
    HTMLSpanElement: globalThis.HTMLSpanElement,
    IntersectionObserver: globalThis.IntersectionObserver,
    window: globalThis.window,
  };
  const revoked: string[] = [];
  let nextURL = 0;
  Object.defineProperty(browser.URL, "createObjectURL", {
    configurable: true,
    value: () => `blob:http://localhost/preview-${++nextURL}`,
  });
  Object.defineProperty(browser.URL, "revokeObjectURL", {
    configurable: true,
    value: (value: string) => revoked.push(value),
  });
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
  return { browser, host, revoked };
}

function chooseFiles(browser: Window, input: HTMLInputElement, files: File[]): void {
  Object.defineProperty(input, "files", { configurable: true, value: files });
  input.dispatchEvent(new browser.Event("change", { bubbles: true }));
  Object.defineProperty(input, "files", { configurable: true, value: [] });
}

test("recognized-agent file controls keep drafts, removable chips, and scoped thumbnails", (t) => {
  const { browser, host, revoked } = withFileBrowser(t);
  const submissions: Array<{ files: number; text: string }> = [];
  const reader = new ReaderView(host, {
    onLog: () => undefined,
    onStatus: () => undefined,
    onSubmit: (text, files) => {
      submissions.push({ files: files.length, text });
      return true;
    },
  }, { collapsibleComposer: true, endpoint: "/api/terminal/read" });
  reader.setActionAvailability(readerActionAvailability(true, "ready"));
  reader.showComposer();
  const add = Array.from(host.querySelectorAll("button")).find((button) => button.textContent === "Add files") as HTMLButtonElement;
  const input = host.querySelector('input[type="file"]') as HTMLInputElement;
  const textarea = host.querySelector("textarea") as HTMLTextAreaElement;
  textarea.value = "keep this text";
  assert.equal(add.hidden, true, "an ordinary terminal must not show a placeholder file action");

  reader.setFileSelectionAvailable(true);
  assert.equal(add.hidden, false);
  input.dispatchEvent(new browser.Event("change", { bubbles: true }));
  assert.equal(textarea.value, "keep this text", "picker cancel must leave the text draft intact");
  assert.equal(host.querySelectorAll(".reader-file-chip").length, 0);

  const image = new browser.File(["image"], "photo.png", { type: "image/png" }) as unknown as File;
  const documentFile = new browser.File(["notes"], "notes.txt", { type: "text/plain" }) as unknown as File;
  chooseFiles(browser, input, [image, documentFile]);
  assert.equal(host.querySelectorAll(".reader-file-chip").length, 2);
  assert.match(host.querySelector(".reader-file-list")?.textContent ?? "", /photo\.png · 5 B/);
  assert.match(host.querySelector(".reader-file-list")?.textContent ?? "", /notes\.txt · 5 B/);
  assert.equal(host.querySelector(".reader-file-chip img")?.getAttribute("src"), "blob:http://localhost/preview-1");

  reader.setFileSelectionAvailable(false);
  assert.equal(add.hidden, true);
  assert.equal(host.querySelectorAll(".reader-file-chip").length, 2, "disconnect must preserve pending files");
  assert.equal(textarea.value, "keep this text");
  reader.setFileSelectionAvailable(true);
  const remove = host.querySelector('.reader-file-chip button[aria-label="Remove photo.png"]') as HTMLButtonElement;
  remove.click();
  assert.equal(host.querySelectorAll(".reader-file-chip").length, 1);
  assert.deepEqual(revoked, ["blob:http://localhost/preview-1"]);

  const send = Array.from(host.querySelectorAll("button")).find((button) => button.textContent === "Send") as HTMLButtonElement;
  send.click();
  assert.deepEqual(submissions, [{ files: 1, text: "keep this text" }]);
  reader.filesSending();
  const pendingRemove = host.querySelector('.reader-file-chip button[aria-label="Remove notes.txt"]') as HTMLButtonElement;
  const close = Array.from(host.querySelectorAll("button")).find((button) => button.textContent === "Close") as HTMLButtonElement;
  assert.equal(pendingRemove.disabled, true, "the captured batch must not change while it is being sent");
  assert.equal(close.disabled, true);
  pendingRemove.click();
  assert.equal(reader.pendingFiles().length, 1);
  reader.inputFailed("Files were not sent.", () => undefined);
  assert.equal((host.querySelector('.reader-file-chip button[aria-label="Remove notes.txt"]') as HTMLButtonElement).disabled, false);
  assert.equal(close.disabled, false);
  assert.equal(reader.pendingFiles().length, 1, "definite failure must preserve the pending file");
  reader.inputUncertain("Check the terminal.", () => undefined);
  assert.equal(reader.pendingFiles().length, 1, "unknown result must preserve the pending file");
  chooseFiles(browser, input, [image]);
  reader.destroy();
  assert.deepEqual(revoked, ["blob:http://localhost/preview-1", "blob:http://localhost/preview-2"]);
});

test("file-only and combined sends use base64 JSON and only deliberate takeover retries", async (t) => {
  const browser = new Window({ url: "http://localhost/" });
  t.after(() => browser.close());
  const file = new browser.File([new Uint8Array([0, 1, 255])], "opaque.bin", { type: "application/octet-stream" }) as unknown as File;
  const requests: Array<Record<string, unknown>> = [];
  const replies = [
    { message: "Controlled elsewhere.", result: "occupied" },
    { message: "Terminal input was sent.", result: "forwarded" },
  ];
  const fetcher = async (_input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    requests.push(JSON.parse(String(init?.body)) as Record<string, unknown>);
    return new Response(JSON.stringify(replies.shift()), { headers: { "Content-Type": "application/json" }, status: requests.length === 1 ? 409 : 200 });
  };
  const target = { paneID: "pane-1", terminalID: "term-1", workspaceID: "workspace-1" };
  const first = await sendTerminalFiles(target, "", [file], false, new AbortController().signal, fetcher);
  assert.equal(first.result, "occupied");
  assert.equal(requests.length, 1, "occupied response must not retry automatically");
  assert.equal(requests[0].text, "");
  assert.deepEqual(requests[0].files, [{ data: "AAH/", name: "opaque.bin" }]);

  const second = await sendTerminalFiles(target, "Compare this", [file], true, new AbortController().signal, fetcher);
  assert.equal(second.result, "forwarded");
  assert.equal(requests.length, 2);
  assert.equal(requests[1].takeover, true);
  assert.equal(requests[1].text, "Compare this");
});

test("file send distinguishes definite server failure from an unconfirmed connection result", async (t) => {
  const browser = new Window({ url: "http://localhost/" });
  t.after(() => browser.close());
  const file = new browser.File(["small"], "small.txt") as unknown as File;
  const target = { paneID: "pane-1", terminalID: "term-1", workspaceID: "workspace-1" };
  const definite = await sendTerminalFiles(target, "", [file], false, new AbortController().signal, async () =>
    new Response(JSON.stringify({ message: "Files were not sent.", result: "not_sent" }), { status: 409 }));
  assert.equal(definite.result, "not_sent");
  const unknown = await sendTerminalFiles(target, "", [file], false, new AbortController().signal, async () => {
    throw new Error("connection lost");
  });
  assert.equal(unknown.result, "unknown");
  assert.match(unknown.message, /could not be confirmed/);
  const revokedSession = await sendTerminalFiles(target, "", [file], false, new AbortController().signal, async () =>
    new Response("Sign in again.", { status: 401 }));
  assert.equal(revokedSession.result, "unknown", "session revocation may be observed after the terminal batch was forwarded");
});

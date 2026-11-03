import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { Window } from "happy-dom";

import { readerActionAvailability, type ReaderInputState } from "./terminal/reader-availability";
import { sendTerminalFiles, uploadStateAfterLastFileRemoved } from "./terminal/file-uploads";
import { ReaderView, readerMessageAction } from "./terminal/reader-view";

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

function selectFiles(browser: Window, input: HTMLInputElement, files: File[]): void {
  Object.defineProperty(input, "files", { configurable: true, value: files });
  input.dispatchEvent(new browser.Event("change", { bubbles: true }));
  Object.defineProperty(input, "files", { configurable: true, value: [] });
}

function cssRule(source: string, selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`${escaped}\\s*\\{([^}]*)\\}`).exec(source)?.[1] ?? "";
}

test("mobile attachments use a contained horizontal strip and the Reader action says Message", (t) => {
  const styles = readFileSync(new URL("./terminal-reader.css", import.meta.url), "utf8");
  const list = cssRule(styles, ".reader-file-list");
  assert.match(list, /display:\s*flex/);
  assert.match(list, /max-width:\s*100%/);
  assert.match(list, /overflow-x:\s*auto/);
  assert.match(list, /overscroll-behavior-x:\s*contain/);
  const chip = cssRule(styles, ".reader-file-chip");
  assert.match(chip, /position:\s*relative/);
  assert.match(chip, /flex:\s*0 0 min\(/);
  assert.match(chip, /grid-template-columns:\s*auto minmax\(0, 1fr\)/);
  assert.match(chip, /overflow:\s*hidden/);
  const remove = cssRule(styles, ".reader-file-remove");
  assert.match(remove, /position:\s*absolute/);
  assert.match(remove, /right:\s*0/);
  assert.match(remove, /min-width:\s*44px/);
  assert.match(remove, /min-height:\s*44px/);
  const removeFocus = cssRule(styles, ".reader-file-remove:focus-visible");
  assert.match(removeFocus, /outline:\s*0/);
  assert.match(removeFocus, /box-shadow:\s*inset\s+0\s+0\s+0\s+3px/);
  assert.match(removeFocus, /#b85c32/i);

  const { browser } = withFileBrowser(t);
  let opened = false;
  const message = readerMessageAction(() => opened = true);
  assert.equal(message.textContent, "Message");
  assert.equal(message.getAttribute("aria-label"), "Message");
  assert.equal(message.title, "Message");
  assert.equal(message.className, "terminal-write-text");
  message.dispatchEvent(new browser.Event("click", { bubbles: true }));
  assert.equal(opened, true);
});

test("recognized-agent file controls keep drafts, removable chips, and scoped thumbnails", async (t) => {
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
  const add = host.querySelector('button[aria-label="Add files"]') as HTMLButtonElement;
  const input = host.querySelector(".reader-file-input") as HTMLInputElement;
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
  selectFiles(browser, input, [image, documentFile]);
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

  const send = host.querySelector('button[aria-label="Send text and files"]') as HTMLButtonElement;
  send.click();
  assert.deepEqual(submissions, [{ files: 1, text: "keep this text" }]);
  reader.filesSending();
  const pendingRemove = host.querySelector('.reader-file-chip button[aria-label="Remove notes.txt"]') as HTMLButtonElement;
  const close = host.querySelector('button[aria-label="Close composer"]') as HTMLButtonElement;
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
  selectFiles(browser, input, [image]);
  reader.destroy();
  assert.deepEqual(revoked, ["blob:http://localhost/preview-1", "blob:http://localhost/preview-2"]);
});

test("mobile composer icons and picker choices add the chosen files", async (t) => {
  const { browser, host } = withFileBrowser(t);
  const reader = new ReaderView(host, {
    onLog: () => undefined,
    onStatus: () => undefined,
    onSubmit: () => true,
  }, { collapsibleComposer: true, endpoint: "/api/terminal/read" });
  reader.setActionAvailability(readerActionAvailability(true, "ready"));
  reader.setFileSelectionAvailable(true);
  reader.showComposer();

  const textarea = host.querySelector("textarea") as HTMLTextAreaElement;
  const add = host.querySelector('button[aria-label="Add files"]') as HTMLButtonElement;
  const send = host.querySelector('button[aria-label="Send text"]') as HTMLButtonElement;
  const close = host.querySelector('button[aria-label="Close composer"]') as HTMLButtonElement;
  for (const [button, label] of [[add, "Add files"], [send, "Send text"], [close, "Close composer"]] as const) {
    assert.equal(button.textContent, "", `${label} must remain icon-only`);
    assert.equal(button.title, label);
    assert.equal(button.querySelector("svg")?.getAttribute("aria-hidden"), "true");
  }

  textarea.value = "keep this draft";
  textarea.dispatchEvent(new browser.Event("input", { bubbles: true }));
  assert.equal(send.disabled, false);
  add.click();
  const menu = host.querySelector(".reader-file-menu") as HTMLDivElement;
  const choices = Array.from(menu.querySelectorAll("button")) as HTMLButtonElement[];
  assert.equal(menu.hidden, false);
  assert.equal(add.getAttribute("aria-expanded"), "true");
  assert.equal(add.hasAttribute("aria-haspopup"), false);
  assert.equal(menu.getAttribute("role"), "group");
  assert.deepEqual(choices.map((button) => button.textContent), ["Photos", "Files"]);
  assert.equal(choices.every((button) => button.hasAttribute("role") === false), true);
  assert.equal(browser.document.activeElement === choices[0], true);

  const photoInput = host.querySelector(".reader-photo-input") as HTMLInputElement;
  const fileInput = host.querySelector(".reader-file-input") as HTMLInputElement;
  assert.equal(photoInput.accept, "image/*");
  assert.equal(fileInput.accept, "");
  let photoPickerOpened = 0;
  Object.defineProperty(photoInput, "click", { configurable: true, value: () => photoPickerOpened += 1 });
  choices[0].click();
  assert.equal(photoPickerOpened, 1);
  assert.equal(menu.hidden, true);
  assert.equal(browser.document.activeElement === add, true, "a picker choice must move focus out of the hidden group");
  photoInput.dispatchEvent(new browser.Event("cancel"));
  assert.equal(textarea.value, "keep this draft");
  assert.equal(browser.document.activeElement === add, true, "picker cancel must restore focus to the paperclip");

  add.click();
  menu.dispatchEvent(new browser.KeyboardEvent("keydown", { bubbles: true, key: "Escape" }));
  assert.equal(menu.hidden, true);
  assert.equal(browser.document.activeElement === add, true, "Escape dismissal must restore focus");
  add.click();
  choices[1].focus();
  send.focus();
  assert.equal(menu.hidden, true, "natural focus navigation must dismiss the source group");
  assert.equal(browser.document.activeElement === send, true);
  add.click();
  textarea.dispatchEvent(new browser.Event("pointerdown", { bubbles: true }));
  assert.equal(menu.hidden, true, "an outside pointer must dismiss the source menu");
  assert.equal(menu.contains(browser.document.activeElement), false);

  add.click();
  reader.setFileSelectionAvailable(false);
  assert.equal(menu.hidden, true);
  assert.equal(add.hidden, true);
  assert.equal(browser.document.activeElement === textarea, true, "availability loss must move focus to the surviving editor");
  reader.setFileSelectionAvailable(true);

  let filePickerOpened = 0;
  Object.defineProperty(fileInput, "click", { configurable: true, value: () => filePickerOpened += 1 });
  add.click();
  choices[1].click();
  assert.equal(filePickerOpened, 1);
  const file = new browser.File(["notes"], "notes.txt", { type: "text/plain" }) as unknown as File;
  selectFiles(browser, fileInput, [file]);
  assert.equal(host.querySelector(".reader-file-preparing"), null, "a chosen file is ready at once, with no reading step");
  assert.equal(host.querySelectorAll(".reader-file-chip").length, 1);
  assert.equal(send.disabled, false);
  assert.equal(send.getAttribute("aria-label"), "Send text and files");
  assert.equal(send.title, "Send text and files");
  textarea.value = "";
  textarea.dispatchEvent(new browser.Event("input", { bubbles: true }));
  assert.equal(send.disabled, false, "a prepared file must enable file-only send");
  assert.equal(send.getAttribute("aria-label"), "Send files");
  assert.equal(send.title, "Send files");
  const remove = host.querySelector('button[aria-label="Remove notes.txt"]') as HTMLButtonElement;
  assert.equal(remove.textContent, "");
  assert.equal(remove.title, "Remove notes.txt");
  assert.equal(remove.querySelector("svg")?.getAttribute("aria-hidden"), "true");
  reader.destroy();
});

test("removing the last recovery file restores text, shortcuts, and file selection", async (t) => {
  const { browser, host } = withFileBrowser(t);
  for (const stable of ["ready", "requesting", "forwarding"] as const) {
    assert.equal(uploadStateAfterLastFileRemoved(stable), stable, `${stable} is not file recovery`);
  }
  for (const recovery of ["failed", "occupied", "uncertain"] as const) {
    let uploadState: ReaderInputState = recovery;
    let reader: ReaderView;
    reader = new ReaderView(host, {
      onLog: () => undefined,
      onPendingFilesEmpty: () => {
        const nextState = uploadStateAfterLastFileRemoved(uploadState);
        if (nextState === uploadState) return;
        uploadState = nextState;
        reader.clearInputRecovery();
        reader.setActionAvailability(readerActionAvailability(true, uploadState));
        reader.setFileSelectionAvailable(uploadState === "ready");
      },
      onStatus: () => undefined,
      onSubmit: () => true,
    }, { collapsibleComposer: true, endpoint: "/api/terminal/read" });
    reader.setActionAvailability(readerActionAvailability(true, "ready"));
    reader.setFileSelectionAvailable(true);
    reader.showComposer();
    const textarea = host.querySelector("textarea") as HTMLTextAreaElement;
    textarea.value = `keep ${recovery} text`;
    textarea.dispatchEvent(new browser.Event("input", { bubbles: true }));
    const fileInput = host.querySelector(".reader-file-input") as HTMLInputElement;
    selectFiles(browser, fileInput, [new browser.File(["file"], `${recovery}.txt`) as unknown as File]);
    reader.filesSending();
    if (recovery === "failed") reader.inputFailed("Files were not sent.", () => undefined);
    else if (recovery === "occupied") reader.inputOccupied("Controlled elsewhere.", () => undefined);
    else reader.inputUncertain("Delivery uncertain.", () => undefined);
    reader.setActionAvailability(readerActionAvailability(true, recovery));
    reader.setFileSelectionAvailable(false);

    const remove = host.querySelector(`button[aria-label="Remove ${recovery}.txt"]`) as HTMLButtonElement;
    remove.click();
    assert.equal(uploadState, "ready");
    assert.equal(textarea.value, `keep ${recovery} text`, `${recovery} removal must preserve typed text`);
    assert.equal((host.querySelector(".reader-send-feedback") as HTMLDivElement).hidden, true);
    const add = host.querySelector('button[aria-label="Add files"]') as HTMLButtonElement;
    assert.equal(add.hidden, false);
    assert.equal(add.disabled, false);
    const sendText = host.querySelector('button[aria-label="Send text"]') as HTMLButtonElement;
    assert.equal(sendText.disabled, false);
    assert.equal(readerActionAvailability(true, uploadState).send, true, `${recovery} removal must restore shortcut sending`);
    reader.destroy();
  }
});

test("file-only and combined sends stream raw file bytes after one request line and only deliberate takeover retries", async () => {
  const file = new File([new Uint8Array([0, 1, 255])], "opaque.bin", { type: "application/octet-stream" });
  const requests: Array<Record<string, unknown>> = [];
  const fileBytes: number[][] = [];
  const types: string[] = [];
  const urls: string[] = [];
  const replies = [
    { message: "Controlled elsewhere.", result: "occupied" },
    { message: "Terminal input was sent.", result: "forwarded" },
  ];
  const fetcher = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    urls.push(String(input));
    types.push(String(new Headers(init?.headers).get("Content-Type")));
    assert.equal(init?.body instanceof Blob, true, "the body must be a Blob that streams the chosen files");
    const bytes = new Uint8Array(await (init?.body as Blob).arrayBuffer());
    const newline = bytes.indexOf(10);
    requests.push(JSON.parse(new TextDecoder().decode(bytes.subarray(0, newline))) as Record<string, unknown>);
    fileBytes.push(Array.from(bytes.subarray(newline + 1)));
    return new Response(JSON.stringify(replies.shift()), { headers: { "Content-Type": "application/json" }, status: requests.length === 1 ? 409 : 200 });
  };
  const target = { herdr: "tui" as const, paneID: "pane-1", terminalID: "term-1", workspaceID: "workspace-1" };
  const first = await sendTerminalFiles(target, "", [file], false, new AbortController().signal, fetcher);
  assert.equal(first.result, "occupied");
  assert.equal(requests.length, 1, "occupied response must not retry automatically");
  assert.equal(requests[0].text, "");
  assert.deepEqual(requests[0].files, [{ name: "opaque.bin", size: 3 }]);
  assert.deepEqual(fileBytes[0], [0, 1, 255]);

  const second = await sendTerminalFiles({ ...target, herdr: "core" }, "Compare\nthis", [file, file], true, new AbortController().signal, fetcher);
  assert.equal(second.result, "forwarded");
  assert.equal(requests.length, 2);
  assert.deepEqual(urls, ["/api/terminal/files?herdr=tui", "/api/terminal/files?herdr=core"]);
  assert.deepEqual(types, ["application/octet-stream", "application/octet-stream"]);
  assert.equal(requests[1].takeover, true);
  assert.equal(requests[1].text, "Compare\nthis");
  assert.deepEqual(fileBytes[1], [0, 1, 255, 0, 1, 255]);
});

test("a file that can no longer be read is not sent", async (t) => {
  const browser = new Window({ url: "http://localhost/" });
  t.after(() => browser.close());
  const file = new browser.File(["gone"], "gone.txt") as unknown as File;
  Object.defineProperty(file, "slice", { configurable: true, value: () => ({ arrayBuffer: () => Promise.reject(new Error("NotReadableError")) }) });
  let fetched = 0;
  const outcome = await sendTerminalFiles({ herdr: "tui", paneID: "pane-1", terminalID: "term-1", workspaceID: "workspace-1" }, "", [file], false,
    new AbortController().signal, async () => {
      fetched += 1;
      return new Response("{}");
    });
  assert.equal(outcome.result, "not_sent");
  assert.equal(outcome.message, "The selected files could not be read, so nothing was sent.");
  assert.equal(fetched, 0);
});

test("file send distinguishes definite server failure from an unconfirmed connection result", async (t) => {
  const browser = new Window({ url: "http://localhost/" });
  t.after(() => browser.close());
  const file = new browser.File(["small"], "small.txt") as unknown as File;
  const target = { herdr: "tui" as const, paneID: "pane-1", terminalID: "term-1", workspaceID: "workspace-1" };
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

test("malformed gateway response leaves file delivery unconfirmed", async (t) => {
  const browser = new Window({ url: "http://localhost/" });
  t.after(() => browser.close());
  const file = new browser.File(["small"], "small.txt") as unknown as File;
  const outcome = await sendTerminalFiles(
    { herdr: "tui", paneID: "pane-1", terminalID: "term-1", workspaceID: "workspace-1" },
    "",
    [file],
    false,
    new AbortController().signal,
    async () => new Response("<html>Bad gateway</html>", { status: 502 }),
  );
  assert.equal(outcome.result, "unknown");
  assert.match(outcome.message, /could not be confirmed/);
});

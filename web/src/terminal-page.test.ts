import assert from "node:assert/strict";
import test from "node:test";
import { build } from "esbuild";
import { Window } from "happy-dom";
import type { TerminalAdapterEvents } from "./terminal/adapter";

// Bundle the real page and adapter as the browser does: xterm's published UMD
// package cannot be loaded as named Node ESM imports. Only geometry is stubbed.
// Browsers have no Node Buffer. The grapheme addon decodes its tables through
// Buffer when it exists and misreads Node's pooled bytes, which can loop forever.
const bundle = await build({
  stdin: { contents: 'export { closeTerminalWhileCached, TerminalPage } from "./terminal-page"; export { XTermAdapter } from "./terminal/xterm-adapter"; export { Terminal } from "@xterm/xterm";', resolveDir: `${process.cwd()}/src` },
  bundle: true, format: "esm", write: false, define: { Buffer: "undefined" },
});
const { closeTerminalWhileCached, Terminal, TerminalPage, XTermAdapter } = await import(`data:text/javascript;base64,${Buffer.from(bundle.outputFiles[0].contents).toString("base64")}`);

const tick = () => new Promise(resolve => setTimeout(resolve, 0));
// Node's own async work, such as reading a file or a reply's body, can take
// many turns on a slow machine, so wait for the real condition, within a bound.
const until = async (ready: () => boolean, ms = 10_000) => {
  for (const deadline = Date.now() + ms; !ready() && Date.now() < deadline;) await tick();
};

test("logical keys leave the Reader draft alone and require deliberate recovery without reconnecting", async t => {
  const { host, browser, sockets } = pageBrowser(t, true);
  await tick(); sockets[0].frame();
  const byText = (text: string) => Array.from(host.querySelectorAll<HTMLButtonElement>("button")).find(button => button.textContent === text)!;
  assert.equal(Array.from(host.querySelectorAll(".terminal-command-bar button"), button => button.getAttribute("aria-label") ?? button.textContent).join("|"), "Message|Send keys|Release|Esc|Enter|↑|↓|Backspace|Ctrl + c");
  assert.equal(host.querySelector('.terminal-command-bar [aria-label="Backspace"]')!.textContent, "⌫");
  byText("Message").click();
  const editor = host.querySelector<HTMLTextAreaElement>("textarea")!;
  editor.value = "unsent message"; editor.setSelectionRange(2, 5);
  editor.dispatchEvent(new browser.Event("input", { bubbles: true }));
  const picker = host.querySelector<HTMLInputElement>(".reader-file-input")!;
  Object.defineProperty(picker, "files", { value: [new browser.File(["pending bytes"], "unsent.txt")] });
  picker.dispatchEvent(new browser.Event("change"));
  await tick();
  byText("Send keys").click();
  assert.equal(byText("Send keys").querySelectorAll("svg").length, 0);
  assert.equal(byText("Send keys").nextElementSibling?.textContent, "Release");
  const sheet = host.querySelector<HTMLDialogElement>(".keys-sheet")!;
  const pick = (text: string) => Array.from(sheet.querySelectorAll<HTMLButtonElement>("button")).find(button => button.textContent === text)!;
  pick("Ctrl").click(); pick("Backspace").click(); pick("Pin shortcut").click();
  assert.equal(host.querySelector('.terminal-command-bar [aria-label="Ctrl + Backspace"]')!.textContent, "Ctrl + ⌫");
  assert.equal(sockets[0].commands.length, 0);
  assert.equal(sheet.open, true);
  pick("Send Ctrl + Backspace").click();
  assert.equal(JSON.stringify(sockets[0].commands), JSON.stringify([{ type: "terminal.send-key", key: { base: "backspace", ctrl: true }, request_id: 1 }]));
  assert.equal(editor.value, "unsent message");
  sockets[0].message({ type: "terminal.input-forwarded", request_id: 1 });
  assert.equal(pick("Send Ctrl + Backspace").disabled, true);
  assert.equal(sheet.open, true);
  sockets[0].message({ type: "terminal.key-result", request_id: 1, result: "accepted" });
  assert.equal(sheet.open, false);
  assert.equal(sheet.querySelector(".keys-feedback")!.textContent, "");
  assert.equal(pick("Send Ctrl + Backspace").disabled, false);
  assert.equal(editor.value, "unsent message");
  assert.equal(editor.selectionStart, 2);
  assert.equal(host.querySelectorAll('[aria-label="Remove unsent.txt"]').length, 1);
  assert.equal(host.querySelector<HTMLElement>(".reader-composer")!.hidden, false);
  assert.equal(browser.document.activeElement === editor, false);
  assert.equal(sockets.length, 1);
  byText("Send keys").click();
  pick("Send Ctrl + Backspace").click();
  sockets[0].message({ type: "terminal.key-result", request_id: 2, result: "not_sent" });
  assert.equal(sheet.open, true);
  assert.equal(sheet.querySelector(".keys-feedback")!.textContent, "Key not sent. Check control and try again.");
  assert.equal(sockets.length, 1);
  pick("Send Ctrl + Backspace").click();
  sockets[0].message({ type: "terminal.key-result", request_id: 3, result: "unknown" });
  assert.equal(sheet.open, true);
  assert.equal(sheet.querySelector(".keys-feedback")!.textContent, "Could not confirm the key. Check the terminal before sending again.");
  assert.equal(pick("Dismiss").hidden, false);
  assert.equal(pick("Send Ctrl + Backspace").disabled, true);
  pick("Dismiss").click();
  assert.equal(sheet.querySelector(".keys-feedback")!.textContent, "");
  assert.equal(pick("Send Ctrl + Backspace").disabled, false);
  assert.equal(sockets[0].commands.length, 3);
  assert.equal(editor.value, "unsent message");
  sheet.close();
  host.querySelector<HTMLButtonElement>('[aria-label="Enter"]')!.click();
  assert.equal(JSON.stringify(sockets[0].commands[3].key), JSON.stringify({ base: "enter" }));
  sockets[0].message({ type: "terminal.key-result", request_id: 4, result: "accepted" });
  assert.equal(sheet.open, false);
  assert.equal(sheet.querySelector(".keys-feedback")!.textContent, "");
  host.querySelector<HTMLButtonElement>(".terminal-saved-shortcuts button")!.click();
  assert.equal(sheet.open, false);
  sockets[0].message({ type: "terminal.key-result", request_id: 5, result: "accepted" });
  assert.equal(sheet.open, false);
  host.querySelector<HTMLButtonElement>(".terminal-control-action")!.click();
  await tick(); sockets[1].frame();
  assert.equal(host.querySelector<HTMLButtonElement>('[aria-label="Enter"]')!.disabled, true);
  byText("Send keys").click(); pick("Tab").click(); pick("Pin shortcut").click();
  assert.equal(sockets[1].commands.length, 0);
});

test("fixed Enter keeps saved shortcut order and survives an empty list and Restore defaults", async t => {
  const saved = '[{"base":"tab"},{"base":"esc"}]';
  const { host, browser, sockets } = pageBrowser(t, true, saved);
  await tick(); sockets[0].frame();
  const labels = () => Array.from(host.querySelectorAll(".terminal-command-bar button"), button => button.getAttribute("aria-label") ?? button.textContent).join("|");
  const click = (text: string) => Array.from(host.querySelectorAll<HTMLButtonElement>("button")).find(button => button.textContent === text)!.click();
  assert.equal(labels(), "Message|Send keys|Release|Tab|Enter|Esc");
  assert.equal(browser.localStorage.getItem("shepherdr.terminal.shortcuts"), saved);
  click("Send keys"); click("Edit shortcuts");
  host.querySelector<HTMLButtonElement>('[aria-label="Remove Tab"]')!.click();
  host.querySelector<HTMLButtonElement>('[aria-label="Remove Esc"]')!.click();
  assert.equal(labels(), "Message|Send keys|Release|Enter");
  assert.equal(browser.localStorage.getItem("shepherdr.terminal.shortcuts"), "[]");
  click("Restore defaults");
  assert.equal(labels(), "Message|Send keys|Release|Esc|Enter|↑|↓|Backspace|Ctrl + c");
  assert.equal(host.querySelectorAll('.terminal-command-bar [aria-label="Enter"]').length, 1);
  assert.equal(sockets[0].commands.length, 0);
  host.querySelector<HTMLButtonElement>('.terminal-command-bar [aria-label="Backspace"]')!.click();
  assert.equal(JSON.stringify(sockets[0].commands[0]), JSON.stringify({ type: "terminal.send-key", key: { base: "backspace" }, request_id: 1 }));
});

test("xterm waits for both font outcomes and departure prevents delayed resources", async t => {
  const browser = new Window({ url: "http://localhost/" });
  const originalDocument = globalThis.document;
  const requests: string[] = [];
  let finishBold!: (faces: FontFace[]) => void;
  Object.defineProperty(browser.document, "fonts", { value: {
    load(font: string) {
      requests.push(font);
      return requests.length === 1
        ? Promise.reject(new Error("regular unavailable"))
        : new Promise<FontFace[]>(resolve => { finishBold = resolve; });
    },
  } });
  globalThis.document = browser.document as unknown as Document;
  const adapter = new XTermAdapter();
  t.after(() => {
    adapter.destroy();
    globalThis.document = originalDocument;
    browser.close();
  });
  const host = browser.document.createElement("div");
  let completed = false;
  let resizeCalls = 0;
  const mounted = adapter.mount(host, { onData() {}, onResize() { resizeCalls++; } })
    .then(() => { completed = true; });
  await tick();
  assert.equal(requests.join(";"), 'normal 400 14px "IBM Plex Mono";normal 700 14px "IBM Plex Mono"');
  assert.equal(completed, false);
  assert.equal(host.childElementCount, 0);
  adapter.destroy();
  finishBold([]);
  await mounted;
  assert.equal(host.childElementCount, 0);
  assert.equal(resizeCalls, 0);
  assert.equal(adapter.paste("probe"), false);
});

test("the terminal counts a grapheme cluster as one character, as Herdr does", async t => {
  const browser = new Window({ url: "http://localhost/" });
  const originals = { document: globalThis.document, ResizeObserver: globalThis.ResizeObserver };
  Object.defineProperty(browser.document, "fonts", { value: { load: async () => [] } });
  globalThis.document = browser.document as unknown as Document;
  globalThis.ResizeObserver = class { observe() {} disconnect() {} } as unknown as typeof ResizeObserver;
  // The real xterm loads the real grapheme addon; only drawing is stubbed.
  // Without allowProposedApi the addon cannot reach xterm's unicode API.
  t.mock.method(Terminal.prototype, "open", () => {});
  let terminal: { options: { allowProposedApi?: boolean }; unicode: { activeVersion: string } } | undefined;
  const loadAddon = Terminal.prototype.loadAddon;
  t.mock.method(Terminal.prototype, "loadAddon", function (this: typeof terminal, addon: unknown) {
    terminal = this;
    return loadAddon.call(this, addon);
  });
  const adapter = new XTermAdapter();
  t.after(() => {
    adapter.destroy();
    Object.assign(globalThis, originals);
    browser.close();
  });
  const host = browser.document.createElement("div");
  browser.document.body.append(host);
  await adapter.mount(host, { onData() {}, onResize() {} });
  assert.equal(terminal?.options.allowProposedApi, true);
  assert.equal(terminal?.unicode.activeVersion, "15-graphemes");
});

function pageBrowser(t: test.TestContext, mobile: boolean, savedShortcuts?: string, herdr: "tui" | "core" = "tui", options: Record<string, unknown> = {}) {
  const browser = new Window({ url: "http://localhost/" });
  if (savedShortcuts !== undefined) browser.localStorage.setItem("shepherdr.terminal.shortcuts", savedShortcuts);
  const originals = new Map<string, unknown>();
  const sockets: Socket[] = [];
  class Socket extends EventTarget {
    static OPEN = 1;
    readyState = 1;
    commands: Array<Record<string, unknown>> = [];
    constructor(readonly url: string) { super(); sockets.push(this); }
    send(text: string) { this.commands.push(JSON.parse(text)); }
    close() { this.readyState = 3; this.dispatchEvent(new Event("close")); }
    message(value: unknown) { this.dispatchEvent(new MessageEvent("message", { data: JSON.stringify(value) })); }
    frame(cols = 40, rows = 20) {
      this.message({ type: "terminal.frame", encoding: "ansi", full: true, seq: 1, width: cols, height: rows, bytes: btoa("output") });
    }
  }
  class Observer { observe() {} disconnect() {} }
  let historyIntersection!: () => void;
  class HistoryObserver extends Observer {
    constructor(callback: IntersectionObserverCallback) {
      super();
      historyIntersection = () => callback([{ isIntersecting: true } as IntersectionObserverEntry], this as unknown as IntersectionObserver);
    }
  }
  browser.matchMedia = () => ({ matches: mobile }) as ReturnType<Window["matchMedia"]>;
  const globals = {
    window: browser, document: browser.document, location: browser.location,
    HTMLElement: browser.HTMLElement, HTMLSpanElement: browser.HTMLSpanElement,
    ResizeObserver: Observer, IntersectionObserver: HistoryObserver, WebSocket: Socket,
    fetch: async () => new Response(JSON.stringify({ ansi: "retained", cols: 40, rows: 20, generation: 1, terminal_id: "term-trial" })),
  };
  for (const [key, value] of Object.entries(globals)) {
    originals.set(key, (globalThis as Record<string, unknown>)[key]);
    (globalThis as Record<string, unknown>)[key] = value;
  }
  let events: TerminalAdapterEvents;
  for (const method of ["mount", "fit", "destroy", "replace", "write", "focus", "receiveDimensions", "setInputEnabled"]) {
    t.mock.method(XTermAdapter.prototype, method, method === "mount" ? async (_host: HTMLElement, callbacks: TerminalAdapterEvents) => {
      events = callbacks;
      callbacks.onResize({ cols: 40, rows: 20 });
    } : () => {});
  }
  const host = browser.document.createElement("div");
  browser.document.body.append(host);
  const page = new TerminalPage(host, { herdr, paneID: "trial:p1", terminalID: "term-trial", workspaceID: "trial", title: "Trial", agentStatus: "idle" }, { onHome() {}, ...options });
  t.after(() => {
    page.destroy();
    for (const [key, value] of originals) (globalThis as Record<string, unknown>)[key] = value;
    browser.close();
  });
  return { host, browser, sockets, events: () => events, page, historyIntersection: () => historyIntersection() };
}

test("a Herdr Core terminal opens controlling, releases to observing without Take over, and hides keys core can't send", async t => {
  const { host, sockets, events } = pageBrowser(t, false, '[{"base":"esc"},{"base":"+","ctrl":true},{"base":"A"}]', "core");
  await tick();
  const url = new URL(sockets[0].url);
  assert.equal(`${url.pathname} ${url.searchParams.get("herdr")} ${url.searchParams.get("mode")}`, "/api/terminal core control");
  sockets[0].frame();
  const control = host.querySelector<HTMLButtonElement>(".terminal-control-action")!;
  assert.equal(`${control.hidden} ${control.textContent}`, "false Release");
  const shortcuts = Array.from(host.querySelectorAll(".terminal-saved-shortcuts button")).map(button => button.textContent).join(",");
  assert.equal(shortcuts, "Esc,Enter,A");
  assert.equal(host.textContent?.includes("Take over"), false);
  // Herdr Core has no scroll command: the wheel stays local while controlling.
  assert.equal(events().onScroll?.({ direction: "up", lines: 3, column: 1, row: 1, modifiers: 0 }), false);
  assert.equal(sockets[0].commands.some(command => command.type === "terminal.scroll"), false);
  control.click();
  await tick();
  assert.equal(new URL(sockets[1].url).searchParams.get("mode"), "observe");
  sockets[1].frame();
  assert.equal(control.textContent, "Control");
  assert.equal(host.textContent?.includes("Take over"), false);
});

test("a Herdr Core page keeps its size while in view, lets it go out of view, and Release says so", async t => {
  const { host, browser, sockets } = pageBrowser(t, false, undefined, "core");
  await tick(); sockets[0].frame();
  const control = host.querySelector<HTMLButtonElement>(".terminal-control-action")!;
  assert.equal(control.title, "While you control here, the Herdr app window shows this screen's size. Release gives it back.");
  let visibility = "visible";
  Object.defineProperty(browser.document, "visibilityState", { configurable: true, get: () => visibility });
  visibility = "hidden";
  browser.document.dispatchEvent(new browser.Event("visibilitychange"));
  visibility = "visible";
  browser.document.dispatchEvent(new browser.Event("visibilitychange"));
  const resizes = sockets[0].commands.filter(command => command.type === "terminal.resize");
  assert.equal(resizes.map(command => `${command.cols}x${command.rows}:${String(command.keep)}`).join(","), "40x20:true,40x20:false,40x20:true");
  control.click();
  await tick(); sockets[1].frame();
  assert.equal(`${control.textContent} ${control.title}`, "Control ");
});

test("a key combination Herdr Core refuses says the program can't receive it, and other not-sent keys still say Check control", async t => {
  const { host, sockets } = pageBrowser(t, true, undefined, "core");
  await tick(); sockets[0].frame();
  Array.from(host.querySelectorAll<HTMLButtonElement>("button")).find(button => button.textContent === "Send keys")!.click();
  const sheet = host.querySelector<HTMLDialogElement>(".keys-sheet")!;
  const pick = (text: string) => Array.from(sheet.querySelectorAll<HTMLButtonElement>("button")).find(button => button.textContent === text)!;
  pick("Ctrl").click(); pick("Backspace").click();
  pick("Send Ctrl + Backspace").click();
  sockets[0].message({ type: "terminal.key-result", request_id: 1, result: "refused" });
  assert.equal(sheet.open, true);
  assert.equal(sheet.querySelector(".keys-feedback")!.textContent, "This program can't receive that key combination right now.");
  assert.equal(pick("Send Ctrl + Backspace").disabled, false);
  pick("Send Ctrl + Backspace").click();
  sockets[0].message({ type: "terminal.key-result", request_id: 2, result: "not_sent" });
  assert.equal(sheet.querySelector(".keys-feedback")!.textContent, "Key not sent. Check control and try again.");
  assert.equal(sockets.length, 1);
});

test("a page kept for Back releases control and closes its terminal, and reopens it on return", async t => {
  const { host, browser, sockets, page } = pageBrowser(t, true);
  await tick(); sockets[0].frame();
  let reopened = 0;
  closeTerminalWhileCached(browser, () => page.destroy(), () => reopened++);
  const transition = (type: string, persisted: boolean) => {
    const event = new browser.Event(type);
    Object.defineProperty(event, "persisted", { value: persisted });
    browser.dispatchEvent(event);
  };
  transition("pagehide", false);
  transition("pageshow", false);
  assert.equal(`${sockets[0].readyState} ${sockets[0].commands.length} ${reopened}`, "1 0 0");
  transition("pagehide", true);
  assert.equal(`${sockets[0].readyState} ${JSON.stringify(sockets[0].commands)}`, '3 [{"type":"terminal.release"}]');
  assert.equal(host.childElementCount, 0);
  transition("pageshow", true);
  assert.equal(`${reopened} ${sockets.length}`, "1 1");
});

test("Reader history banner opens the same Terminal view and remembers it without stream commands", async t => {
  const { host, browser, sockets, historyIntersection } = pageBrowser(t, true);
  await tick();
  sockets[0].frame();
  historyIntersection();
  await tick();
  const hint = host.querySelector<HTMLElement>(".reader-history-banner")!;
  assert.equal(hint.hidden, true);
  host.querySelector(".reader-scroll")!.dispatchEvent(new browser.WheelEvent("wheel", { deltaY: -10 }));
  assert.equal(hint.hidden, false);
  const output = host.querySelector(".reader-output");
  const editor = host.querySelector<HTMLTextAreaElement>("textarea")!;
  editor.value = "keep draft";
  hint.querySelector<HTMLButtonElement>("button")!.click();
  assert.equal(host.querySelector(".terminal-view-toggle")!.getAttribute("aria-label"), "Reader");
  assert.equal(host.querySelector<HTMLElement>(".terminal-full-mount")!.style.visibility, "visible");
  assert.equal(browser.localStorage.getItem("shepherdr.terminal.view"), "terminal");
  assert.equal(hint.hidden, true, "the banner is suppressed in full Terminal");
  host.querySelector<HTMLButtonElement>(".terminal-view-toggle")!.click();
  assert.equal(hint.hidden, true, "returning to Reader alone cannot reveal the banner");
  assert.equal(sockets.length, 1);
  assert.equal(sockets[0].commands.length, 0);
  assert.equal(host.querySelector(".reader-output") === output, true);
  assert.equal(editor.value, "keep draft");
});

for (const mobile of [true, false]) {
  test(`${mobile ? "mobile" : "desktop"} acquisition sends the latest measured size once after an older first frame`, async t => {
    const { sockets, events } = pageBrowser(t, mobile);
    await tick();
    assert.equal(sockets.length, 1);
    assert.equal(new URL(sockets[0].url).searchParams.get("cols"), "40");
    events().onResize({ cols: 52, rows: 16 });
    assert.equal(sockets[0].commands.length, 0);
    sockets[0].frame();
    assert.equal(JSON.stringify(sockets[0].commands), JSON.stringify([{ type: "terminal.resize", cols: 52, rows: 16 }]));
    events().onResize({ cols: 52, rows: 16 });
    assert.equal(sockets[0].commands.length, 1);
  });
}

test("Release preserves local drafting and ten toggles keep the page's stream and nodes", async t => {
  const { host, sockets } = pageBrowser(t, true);
  await tick();
  sockets[0].frame();
  const message = host.querySelector<HTMLButtonElement>(".reader-message-action") ?? host.querySelector<HTMLButtonElement>('[aria-label="Message"]')!;
  message.click();
  const editor = host.querySelector<HTMLTextAreaElement>("textarea")!;
  editor.value = "keep draft";
  const output = host.querySelector(".reader-output");
  for (let i = 0; i < 10; i++) host.querySelector<HTMLButtonElement>(".terminal-view-toggle")!.click();
  assert.equal(sockets.length, 1);
  assert.equal(sockets[0].commands.length, 0);
  assert.equal(host.querySelector(".reader-output") === output, true);
  assert.equal(host.querySelector("textarea") === editor, true);
  host.querySelector<HTMLButtonElement>(".terminal-control-action")!.click();
  await tick();
  assert.equal(sockets[0].readyState, 3);
  assert.equal(new URL(sockets[1].url).searchParams.get("mode"), "observe");
  sockets[1].frame();
  assert.equal(message.disabled, false);
  assert.equal(editor.disabled, false);
  assert.equal(editor.value, "keep draft");
  host.querySelector<HTMLButtonElement>('.reader-composer-close')!.click();
  assert.equal(host.querySelector<HTMLElement>('.reader-composer')!.hidden, true);
  message.click();
  assert.equal(host.querySelector<HTMLElement>('.reader-composer')!.hidden, false);
  assert.equal(editor.disabled, false);
  assert.equal(host.querySelector<HTMLButtonElement>('[aria-label="Enter"]')!.disabled, true);
});

test("mobile view toggle has icon-only named terminal and phone actions beside Settings", async t => {
  const { host, sockets } = pageBrowser(t, true);
  await tick();
  sockets[0].frame();
  const toggle = host.querySelector<HTMLButtonElement>(".terminal-view-toggle")!;
  assert.equal(toggle.textContent, "");
  assert.equal(toggle.getAttribute("aria-label"), "Full terminal");
  assert.equal(toggle.nextElementSibling?.classList.contains("terminal-notifications"), true);
  assert.equal(toggle.querySelector("svg")?.getAttribute("aria-hidden"), "true");
  const terminalPath = toggle.querySelector("path")?.getAttribute("d");
  toggle.click();
  assert.equal(toggle.textContent, "");
  assert.equal(toggle.getAttribute("aria-label"), "Reader");
  assert.equal(toggle.querySelector("path")?.getAttribute("d") !== terminalPath, true);
  toggle.click();
  assert.equal(toggle.getAttribute("aria-label"), "Full terminal");
  assert.equal(sockets.length, 1);
  assert.equal(sockets[0].commands.length, 0);
});

test("removing the last file cannot hide uncertain keyboard input recovery", async t => {
  const { host, browser, sockets, events } = pageBrowser(t, true);
  await tick();
  sockets[0].frame();
  host.querySelector<HTMLButtonElement>('[aria-label="Message"]')!.click();
  const picker = host.querySelector<HTMLInputElement>(".reader-file-input")!;
  Object.defineProperty(picker, "files", { value: [new browser.File(["tiny"], "trial.txt")] });
  picker.dispatchEvent(new browser.Event("change"));
  await tick();
  host.querySelector<HTMLButtonElement>(".terminal-view-toggle")!.click();
  events().onData("x");
  assert.equal(sockets[0].commands.length, 1);
  sockets[0].message({ type: "terminal.status", message: "taken over" });
  sockets[0].close();
  await tick();
  sockets[1].frame();
  host.querySelector<HTMLButtonElement>(".terminal-view-toggle")!.click();
  host.querySelector<HTMLButtonElement>('[aria-label="Remove trial.txt"]')!.click();
  assert.equal(host.querySelector<HTMLElement>(".reader-send-feedback")!.hidden, false);
  const dismiss = [...host.querySelectorAll<HTMLButtonElement>("button")].find(button => button.textContent === "Dismiss")!;
  assert.equal(dismiss.disabled, false);
  dismiss.click();
  assert.equal(host.querySelector<HTMLElement>(".reader-send-feedback")!.hidden, true);
  assert.equal(sockets.reduce((count, socket) => count + socket.commands.filter(command => command.type === "terminal.input").length, 0), 1);
});

test("text forwarding locks the editor until ack; an unknown result retains the draft without replay", async t => {
  const { host, browser, sockets } = pageBrowser(t, true);
  await tick();
  sockets[0].frame();
  host.querySelector<HTMLButtonElement>('[aria-label="Message"]')!.click();
  const editor = host.querySelector<HTMLTextAreaElement>("textarea")!;
  editor.value = "draft A";
  editor.dispatchEvent(new browser.Event("input"));
  host.querySelector<HTMLButtonElement>('.reader-send')!.click();
  assert.equal(editor.disabled, true);
  assert.equal(JSON.stringify(sockets[0].commands[0].chunks), JSON.stringify(["\x1b[200~draft A\x1b[201~", "\r"]));
  sockets[0].message({ type: "terminal.input-forwarded", request_id: 1 });
  assert.equal(editor.value, "");
  host.querySelector<HTMLButtonElement>('[aria-label="Message"]')!.click();
  editor.value = "draft B";
  editor.dispatchEvent(new browser.Event("input"));
  host.querySelector<HTMLButtonElement>('.reader-send')!.click();
  assert.equal(editor.disabled, true);
  sockets[0].close();
  assert.equal(editor.value, "draft B");
  assert.equal(editor.disabled, false);
  editor.value = "retained local draft";
  sockets[0].message({ type: "terminal.input-forwarded", request_id: 2 });
  assert.equal(editor.value, "retained local draft", "late acknowledgement cannot clear the uncertain draft");
  const dismiss = [...host.querySelectorAll<HTMLButtonElement>("button")].find(button => button.textContent === "Dismiss")!;
  dismiss.click();
  assert.equal(editor.disabled, false);
  assert.equal(sockets[0].commands.length, 2, "dismissal never replays input");
});

test("occupied text recovery keeps the editor locked through confirmed takeover and acknowledgement", async t => {
  const { host, browser, sockets } = pageBrowser(t, true);
  await tick();
  sockets[0].message({ type: "terminal.status", message: "already has an attached client" });
  sockets[0].close();
  await tick();
  sockets[1].frame();
  host.querySelector<HTMLButtonElement>('[aria-label="Message"]')!.click();
  const editor = host.querySelector<HTMLTextAreaElement>("textarea")!;
  editor.value = "draft A";
  editor.dispatchEvent(new browser.Event("input"));
  host.querySelector<HTMLButtonElement>('.reader-send')!.click();
  assert.equal(editor.disabled, true);
  assert.equal(sockets[1].commands.length, 0);
  const takeover = [...host.querySelectorAll<HTMLButtonElement>("button")].find(button => button.textContent === "Take over and send")!;
  browser.confirm = () => true;
  takeover.click();
  assert.equal(editor.disabled, true, "requesting control retains the submitted text");
  await tick();
  assert.equal(new URL(sockets[2].url).searchParams.get("mode"), "takeover");
  sockets[2].frame();
  await tick();
  assert.equal(editor.disabled, true, "forwarding retains the submitted text");
  assert.equal(JSON.stringify(sockets[2].commands[0].chunks), JSON.stringify(["\x1b[200~draft A\x1b[201~", "\r"]));
  sockets[2].message({ type: "terminal.input-forwarded", request_id: 1 });
  assert.equal(editor.value, "");
  assert.equal(sockets[2].commands.length, 1);
});

test("disconnect without a text submission leaves an open local draft editable", async t => {
  const { host, sockets } = pageBrowser(t, true);
  await tick();
  sockets[0].frame();
  host.querySelector<HTMLButtonElement>('[aria-label="Message"]')!.click();
  const editor = host.querySelector<HTMLTextAreaElement>("textarea")!;
  editor.value = "local draft";
  sockets[0].close();
  assert.equal(editor.disabled, false);
  assert.equal(editor.value, "local draft");
  assert.equal(sockets[0].commands.length, 0);
});

test("desktop controller failure falls back to observation and explicit Control", async t => {
  const { host, sockets } = pageBrowser(t, false);
  await tick();
  sockets[0].frame();
  sockets[0].message({ type: "terminal.status", message: "Could not control terminal" });
  sockets[0].close();
  await tick();
  assert.equal(sockets.length, 2);
  assert.equal(new URL(sockets[1].url).searchParams.get("mode"), "observe");
  sockets[1].frame();
  const control = host.querySelector<HTMLButtonElement>(".terminal-control-action")!;
  assert.equal(control.textContent, "Control");
  control.click();
  await tick();
  assert.equal(new URL(sockets[2].url).searchParams.get("mode"), "control");
  assert.equal(sockets[2].commands.length, 0);
});

test("the Shared from here notice counts files from the first list as already here", async t => {
  const shared = (id: string) => ({
    id: id.repeat(32), kind: "image", name: `${id}.png`, seen: false, shared_at: 1_000, size: 10, tag: "PNG",
    from: { herdr: "tui", place: "trial", source: "a".repeat(24), current: { pane_id: "trial:p1", terminal_id: "term-trial", workspace_id: "trial" } },
  });
  // A fresh load opens the page before the list arrives.
  const { host, page } = pageBrowser(t, false, undefined, "tui", { onShared() {} });
  await tick();
  const notice = host.querySelector<HTMLElement>(".shared-here-banner")!;
  page.setShared([shared("a"), shared("b")]);
  assert.equal(notice.hidden, true);
  assert.equal(host.querySelector(".terminal-shared")?.getAttribute("aria-label"), "Shared files from this terminal, 2 new");
  page.setShared([shared("a"), shared("b"), shared("c")]);
  assert.equal(notice.hidden, false);
  assert.equal(notice.querySelector("span")?.textContent, "Shared from here: c.png");
});

test("a terminal's earlier draft comes back as it was, without opening a closed composer", async t => {
  const { host, page } = pageBrowser(t, true, undefined, "tui", { draft: { files: [], open: false, text: "half a thought" } });
  await tick();
  assert.equal(host.querySelector<HTMLTextAreaElement>("textarea")!.value, "half a thought");
  assert.equal(host.querySelector<HTMLElement>(".reader-composer")!.hidden, true);
  assert.deepEqual(page.draft(), { files: [], open: false, text: "half a thought" });
});

test("a ready message goes after the terminal's earlier draft", async t => {
  const { host, page } = pageBrowser(t, true, undefined, "tui", {
    draft: { files: [], open: true, text: "Make the pricing chart." },
    message: "Please share it.",
  });
  await tick();
  assert.equal(host.querySelector<HTMLTextAreaElement>("textarea")!.value, "Make the pricing chart. Please share it.");
  assert.equal(host.querySelector<HTMLElement>(".reader-composer")!.hidden, false);
  assert.deepEqual(page.draft(), { files: [], open: true, text: "Make the pricing chart. Please share it." });
  host.querySelector<HTMLTextAreaElement>("textarea")!.value = "";
  assert.equal(page.draft(), undefined);
});

test("a terminal's earlier draft brings back its picked files", async t => {
  const chart = new File(["chart bytes"], "chart.txt");
  const { host, page } = pageBrowser(t, true, undefined, "tui", { draft: { files: [chart], open: true, text: "Compare these." } });
  await tick();
  assert.equal(host.querySelectorAll('[aria-label="Remove chart.txt"]').length, 1);
  assert.equal(host.querySelector(".reader-composer [aria-label^=Send]")?.getAttribute("aria-label"), "Send text and files");
  assert.equal(page.draft()?.files[0] === chart, true);
  host.querySelector<HTMLTextAreaElement>("textarea")!.value = "";
  assert.equal(page.draft()?.files.length, 1);
  host.querySelector<HTMLButtonElement>('[aria-label="Remove chart.txt"]')!.click();
  assert.equal(page.draft(), undefined);
});

test("Shared files and Show files wait while files are sending, as the view switch does, and the page keeps no draft", async t => {
  const shared = (id: string) => ({
    id: id.repeat(32), kind: "image", name: `${id}.png`, seen: false, shared_at: 1_000, size: 10, tag: "PNG",
    from: { herdr: "tui", place: "trial", source: "a".repeat(24), current: { pane_id: "trial:p1", terminal_id: "term-trial", workspace_id: "trial" } },
  });
  const { host, browser, sockets, page } = pageBrowser(t, true, undefined, "tui", { onShared() {} });
  await tick();
  sockets[0].frame();
  sockets[0].message({ type: "terminal.controller", handle: "c".repeat(64) });
  page.setShared([]);
  page.setShared([shared("a")]);
  host.querySelector<HTMLButtonElement>('[aria-label="Message"]')!.click();
  const picker = host.querySelector<HTMLInputElement>(".reader-file-input")!;
  Object.defineProperty(picker, "files", { value: [new File(["tiny"], "trial.txt")] });
  picker.dispatchEvent(new browser.Event("change"));
  await tick();
  let reply!: (response: Response) => void;
  (globalThis as Record<string, unknown>).fetch = () => new Promise<Response>(resolve => { reply = resolve; });
  const sharedButton = host.querySelector<HTMLButtonElement>(".terminal-shared")!;
  const show = [...host.querySelectorAll<HTMLButtonElement>(".shared-here-banner button")].find(button => button.textContent === "Show files")!;
  const toggle = host.querySelector<HTMLButtonElement>(".terminal-view-toggle")!;
  assert.deepEqual([sharedButton.disabled, show.disabled, toggle.disabled], [false, false, false]);
  assert.equal(page.draft()?.files.length, 1);
  host.querySelector<HTMLButtonElement>(".reader-send")!.click();
  await until(() => Boolean(reply));
  assert.deepEqual([sharedButton.disabled, show.disabled, toggle.disabled], [true, true, true]);
  // Browser Back can still leave for Shared files; the send's result is not known yet.
  assert.equal(page.draft(), undefined);
  reply(new Response(JSON.stringify({ message: "Terminal input was sent.", result: "forwarded" })));
  await until(() => !sharedButton.disabled && !show.disabled && !toggle.disabled);
  assert.deepEqual([sharedButton.disabled, show.disabled, toggle.disabled], [false, false, false]);
});

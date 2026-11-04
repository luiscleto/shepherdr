import assert from "node:assert/strict";
import test from "node:test";

import { Window } from "happy-dom";

import {
  filesFor,
  formatShared,
  formatSize,
  fromText,
  groupByAgent,
  parseSharedMessage,
  parseSharedRoute,
  sharedHash,
  sortFiles,
  unseenCount,
  type SharedFile,
  type SharedRoute,
} from "./shared-model.ts";
import { SharedFilesAction, SharedPage, type SharedPageModel } from "./shared-view.ts";

const epoch = "0123456789abcdef0123456789abcdef";
const site = "aaaaaaaaaaaaaaaaaaaaaaaa";
const review = "bbbbbbbbbbbbbbbbbbbbbbbb";

function file(id: string, overrides: Partial<SharedFile> = {}): SharedFile {
  return { id: id.repeat(32).slice(0, 32), kind: "image", name: `${id}.png`, seen: false, shared_at: 1_000, size: 10, tag: "PNG", ...overrides };
}

const siteCurrent = { pane_id: "t3", terminal_id: "core:t3", workspace_id: "spc-site" };

function files(): SharedFile[] {
  return [
    file("a", { from: { herdr: "core", place: "site", source: site, agent: "claude-code", current: siteCurrent }, shared_at: 5_000, size: 300 }),
    file("b", { from: { herdr: "core", place: "site", source: site, agent: "claude-code", current: siteCurrent }, shared_at: 4_000, size: 100, seen: true }),
    file("c", { from: { herdr: "core", place: "billing-api", tab: "review", source: review, agent: "codex" }, kind: "text", name: "notes.md", tag: "MD", shared_at: 3_000, size: 200 }),
    file("d", { kind: "other", name: "site.zip", tag: "ZIP", shared_at: 2_000, size: 50 }),
  ];
}

test("the shared list parses strictly and rejects a bad entry whole", () => {
  const list = files();
  assert.equal(parseSharedMessage({ type: "shared", epoch, files: list })?.length, 4);
  assert.equal(parseSharedMessage({ type: "shared", epoch, files: [{ ...list[0], id: "../home" }] }), undefined);
  assert.equal(parseSharedMessage({ type: "shared", epoch, files: [{ ...list[0], extra: 1 }] }), undefined);
  assert.equal(parseSharedMessage({ type: "shared", epoch, files: [{ ...list[0], kind: "html" }] }), undefined);
  assert.equal(parseSharedMessage({ type: "home.heartbeat", epoch }), undefined);
});

test("unseen counts, terminal filters and sorting", () => {
  const list = files();
  const terminal = { herdr: "core" as const, paneID: "t3", terminalID: "core:t3" };
  assert.equal(unseenCount(list), 3);
  assert.equal(unseenCount(list, { herdr: "core", terminal }), 1);
  assert.deepEqual(filesFor(list, { herdr: "core", terminal }).map((entry) => entry.name), ["a.png", "b.png"]);
  // A later terminal with the same IDs is not current for these files.
  assert.deepEqual(filesFor(list, { herdr: "core", terminal: { ...terminal, terminalID: "core:t9" } }), []);
  assert.deepEqual(filesFor(list, { herdr: "core", source: review }).map((entry) => entry.name), ["notes.md"]);
  assert.deepEqual(sortFiles(list, "newest").map((entry) => entry.name), ["a.png", "b.png", "notes.md", "site.zip"]);
  assert.deepEqual(sortFiles(list, "oldest").map((entry) => entry.name), ["site.zip", "notes.md", "b.png", "a.png"]);
  assert.deepEqual(sortFiles(list, "largest").map((entry) => entry.size), [300, 200, 100, 50]);
  assert.deepEqual(sortFiles(list, "smallest").map((entry) => entry.size), [50, 100, 200, 300]);
  assert.deepEqual(groupByAgent(list).map((group) => `${group.key}:${group.files.length}`), [`${site}:2`, `${review}:1`, "outside:1"]);
});

test("labels, sizes and times read as the page shows them", () => {
  const list = files();
  assert.equal(fromText(list[0].from, false), "site · claude-code");
  assert.equal(fromText(list[2].from, true), "billing-api / review · codex · Herdr Core");
  assert.equal(fromText(undefined, true), "Outside Herdr");
  assert.equal(formatSize(96_000), "96 KB");
  assert.equal(formatSize(8_400_000), "8.4 MB");
  const now = new Date(2026, 10, 4, 12, 0);
  assert.equal(formatShared(now.getTime() - 20_000, now), "Just now");
  assert.equal(formatShared(now.getTime() - 2 * 60_000, now), "2 min ago");
  assert.equal(formatShared(now.getTime() - 70 * 60_000, now), "1 h ago");
  assert.equal(formatShared(new Date(2026, 10, 3, 18, 40).getTime(), now), "Yesterday 18:40");
  assert.equal(formatShared(new Date(2026, 9, 28, 9, 5).getTime(), now), "Oct 28 09:05");
});

test("shared routes round-trip and ignore anything malformed", () => {
  const terminal = { herdr: "core" as const, paneID: "t3", terminalID: "core:t3" };
  const routes: SharedRoute[] = [
    {},
    { file: "a".repeat(32) },
    { back: terminal, filter: { herdr: "core", terminal } },
    { filter: { herdr: "tui", source: site } },
  ];
  for (const route of routes) assert.deepEqual(parseSharedRoute(sharedHash(route)), route);
  assert.equal(sharedHash({}), "#shared");
  assert.deepEqual(parseSharedRoute("#shared=../../etc&herdr=x&source=<b>"), {});
  assert.equal(parseSharedRoute("#terminal=t3&herdr=core"), undefined);
});

// Every page and window closes after its test, even a failing one, so no
// timer keeps the run alive.
const opened: Array<() => void> = [];
test.afterEach(() => {
  for (const close of opened.splice(0)) close();
});

function page(model: Partial<SharedPageModel> = {}) {
  const window = new Window({ url: "http://localhost/" });
  const host = window.document.createElement("main");
  window.document.body.append(host);
  const calls: string[] = [];
  const view = new SharedPage(host as unknown as HTMLElement, {
    askForFile: () => calls.push("ask"),
    back: () => calls.push("back"),
    navigate: (route) => calls.push(`navigate ${sharedHash(route)}`),
    openTerminal: (herdr, target) => calls.push(`terminal ${herdr} ${target.pane_id}`),
    remove: async (ids) => { calls.push(`remove ${ids.length}`); return true; },
    sort: (sort) => calls.push(`sort ${sort}`),
  });
  const full: SharedPageModel = { canAsk: false, files: files(), markHerdr: () => false, route: {}, sort: "newest", ...model };
  view.update(full);
  opened.push(() => { view.destroy(); window.close(); });
  return { calls, host, view, window, full };
}

function texts(root: ParentNode, selector: string): string[] {
  return [...root.querySelectorAll(selector)].map((node) => node.textContent ?? "");
}

test("the page lists every file with its From line and offers only real actions", () => {
  const { calls, host, view, window } = page();
  assert.deepEqual(texts(host, ".shared-name"), ["a.png", "b.png", "notes.md", "site.zip"]);
  assert.deepEqual(texts(host, ".shared-from"), ["site · claude-code", "site · claude-code", "billing-api / review · codex", "Outside Herdr"]);
  assert.equal(host.querySelector(".terminal-connection")?.textContent, "4 files · 650 B");
  assert.equal(host.querySelector(".terminal-home")?.textContent, "Home");
  // Open terminal only for a file whose terminal is current.
  const triggers = [...host.querySelectorAll<HTMLButtonElement>(".shared-item .workspace-menu-trigger")];
  triggers[0].click();
  assert.deepEqual(texts(host, ".shared-item .workspace-menu-popover button"), ["Open", "Download", "Open terminal", "Delete"]);
  triggers[2].click();
  assert.deepEqual(texts(host, ".shared-item .workspace-menu-popover button"), ["Open", "Download", "Delete"]);
  // The page menu offers Delete all shown, and no Ask for a file without a terminal.
  host.querySelector<HTMLButtonElement>(".shared-page-menu .workspace-menu-trigger")?.click();
  assert.deepEqual(texts(host, ".shared-page-menu .workspace-menu-popover button"), ["Delete all shown (4)"]);
  host.querySelector<HTMLButtonElement>(".shared-open")?.click();
  assert.equal(calls.at(-1), `navigate #shared=${"a".repeat(32)}`);
  view.destroy();
  window.close();
});

test("a terminal's filtered list, its empty state and Ask for a file", () => {
  const terminal = { herdr: "core" as const, paneID: "t3", terminalID: "core:t3" };
  const route = { back: terminal, filter: { herdr: "core" as const, terminal } };
  const { calls, host, view, window, full } = page({ canAsk: true, route });
  assert.deepEqual(texts(host, ".shared-name"), ["a.png", "b.png"]);
  assert.equal(host.querySelector(".shared-filter p")?.textContent, "From site · claude-code");
  assert.equal(host.querySelector(".terminal-connection")?.textContent, "2 of 4 files · 400 B");
  assert.equal(host.querySelector(".terminal-home")?.textContent, "Back");
  host.querySelector<HTMLButtonElement>(".shared-page-menu .workspace-menu-trigger")?.click();
  assert.deepEqual(texts(host, ".shared-page-menu .workspace-menu-popover button"), ["Ask for a file", "Delete all shown (2)"]);
  host.querySelector<HTMLButtonElement>(".shared-page-menu .workspace-menu-popover button")?.click();
  assert.equal(calls.at(-1), "ask");
  // The terminal's files are deleted elsewhere: the list empties in place.
  view.update({ ...full, files: full.files.slice(2), terminalLabel: { herdr: "core", place: "site", source: "" } });
  assert.equal(host.querySelectorAll(".shared-item").length, 0);
  assert.equal(host.querySelector(".shared-empty h2")?.textContent, "Nothing shared from site");
  assert.equal(host.querySelector(".shared-filter button")?.textContent, "Show all 2");
  view.destroy();
  window.close();
});

test("delete asks first and Delete all shown removes exactly what is shown", async () => {
  const { calls, host, view, window } = page({ route: { filter: { herdr: "core", source: review } } });
  host.querySelector<HTMLButtonElement>(".shared-page-menu .workspace-menu-trigger")?.click();
  host.querySelector<HTMLButtonElement>(".shared-page-menu .workspace-menu-popover button")?.click();
  assert.equal(host.querySelector(".shared-sheet .home-action-title")?.textContent, "Delete all 1 shown?");
  host.querySelector<HTMLButtonElement>(".shared-sheet .home-action-primary")?.click();
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(calls.at(-1), "remove 1");
  assert.equal(host.querySelectorAll(".shared-sheet").length, 0);
  view.destroy();
  window.close();
});

test("the empty page says how to share, and the tray button shows only a nonzero count", () => {
  const { host, view, window } = page({ files: [] });
  assert.equal(host.querySelector(".shared-empty h2")?.textContent, "Nothing shared yet");
  assert.equal(host.querySelector(".shared-empty code")?.textContent, "shepherdr share <file>");
  assert.equal(host.querySelector<HTMLElement>(".shared-page-menu")?.hidden, true);
  const action = new SharedFilesAction(window.document as unknown as Document, () => undefined, "home-shared");
  assert.equal(action.element.getAttribute("aria-label"), "Shared files");
  assert.equal(action.element.querySelector<HTMLElement>(".shared-count-badge")?.hidden, true);
  action.setCount(3);
  assert.equal(action.element.getAttribute("aria-label"), "Shared files, 3 new");
  assert.equal(action.element.querySelector(".shared-count-badge")?.textContent, "3");
  view.destroy();
  window.close();
});

test("the viewer shows a file's actions and closes when it is deleted", () => {
  const id = "a".repeat(32);
  const { calls, host, view, window, full } = page({ route: { file: id } });
  assert.equal(host.querySelector(".viewer .viewer-name")?.textContent, "a.png");
  assert.equal(host.querySelector<HTMLImageElement>(".viewer-stage img")?.getAttribute("src"), `/api/shared/${id}`);
  assert.deepEqual(texts(host, ".viewer-foot button"), ["Open terminal", "Download", "Delete"]);
  view.update({ ...full, files: full.files.slice(1) });
  assert.equal(host.querySelectorAll(".viewer").length, 0);
  assert.equal(calls.at(-1), "navigate #shared");
  view.destroy();
  window.close();
});

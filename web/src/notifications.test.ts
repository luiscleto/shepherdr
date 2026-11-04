import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { runInNewContext } from "node:vm";

import { Window } from "happy-dom";

import { HomeView } from "./home-view.ts";
import {
  defaultNotificationEvents,
  NotificationsController,
  type BrowserPushSubscription,
  type NotificationAPI,
  type NotificationConfig,
  type NotificationEvents,
  type NotificationPlatform,
  type PushSubscriptionData,
} from "./notifications.ts";
import { parseTerminalRoute, terminalRouteOutcome } from "./terminal-route.ts";

class FakeSubscription implements BrowserPushSubscription {
  unsubscribed = false;

  data(): PushSubscriptionData {
    return {
      endpoint: "https://push.example/browser-one",
      expirationTime: null,
      keys: { auth: "auth-key", p256dh: "p256dh-key" },
    };
  }

  async unsubscribe(): Promise<boolean> {
    this.unsubscribed = true;
    return true;
  }
}

class FakePlatform implements NotificationPlatform {
  dismissedValue = false;
  ios = false;
  permissionValue: NotificationPermission = "default";
  requestResult: NotificationPermission = "granted";
  requestCount = 0;
  workerRefreshCount = 0;
  subscription: FakeSubscription | undefined;
  supportedValue = true;

  dismissed(): boolean { return this.dismissedValue; }
  dismiss(): void { this.dismissedValue = true; }
  iosBrowserTab(): boolean { return this.ios; }
  permission(): NotificationPermission { return this.permissionValue; }
  supported(): boolean { return this.supportedValue; }
  async existingSubscription(): Promise<BrowserPushSubscription | undefined> { return this.subscription; }
  async requestPermission(): Promise<NotificationPermission> {
    this.requestCount++;
    this.permissionValue = this.requestResult;
    return this.requestResult;
  }
  async refreshWorker(): Promise<void> { this.workerRefreshCount++; }
  async subscribe(): Promise<BrowserPushSubscription> {
    this.subscription = new FakeSubscription();
    return this.subscription;
  }
}

class FakeAPI implements NotificationAPI {
  configValue: NotificationConfig = { setup: false };
  enabled = false;
  events = { ...defaultNotificationEvents };
  saved: Array<{ events: NotificationEvents; subscription: PushSubscriptionData }> = [];

  async config(): Promise<NotificationConfig> { return this.configValue; }
  async read(): Promise<{ enabled: boolean; events: NotificationEvents }> {
    return { enabled: this.enabled, events: { ...this.events } };
  }
  async remove(): Promise<void> { this.enabled = false; }
  async save(subscription: PushSubscriptionData, events: NotificationEvents): Promise<void> {
    this.enabled = true;
    this.events = { ...events };
    this.saved.push({ events: { ...events }, subscription });
  }
}

async function settle(): Promise<void> {
  for (let index = 0; index < 8; index++) await Promise.resolve();
}

function notificationView() {
  const window = new Window({ url: "https://shepherdr.example/" });
  const root = window.document.createElement("div");
  window.document.body.append(root);
  const api = new FakeAPI();
  const platform = new FakePlatform();
  const controller = new NotificationsController(root, api, platform);
  return { api, controller, platform, root, window };
}

function buttonWithText(root: ParentNode, text: string): HTMLButtonElement {
  const button = Array.from(root.querySelectorAll<HTMLButtonElement>("button"))
    .find((candidate) => candidate.textContent === text);
  if (!button) throw new Error(`missing button ${text}`);
  return button;
}

function serviceWorkerHarness(homeClientPresent = true) {
  type Listener = (event: Record<string, unknown>) => void;
  const listeners = new Map<string, Listener>();
  let closed = 0;
  let focused = 0;
  let matched = 0;
  let navigated = "";
  let opened = "";
  let shownBody = "";
  let shownDestination = "";
  let shownTitle = "";
  let claimed = 0;
  let skipped = 0;
  const client = {
    url: "https://shepherdr.example/",
    async focus() { focused++; return client; },
    async navigate(destination: string) { navigated = destination; return client; },
  };
  const scope = {
    addEventListener(name: string, listener: Listener) { listeners.set(name, listener); },
    async skipWaiting() { skipped++; },
    clients: {
      async claim() { claimed++; },
      async matchAll() { matched++; return homeClientPresent ? [client] : []; },
      async openWindow(destination: string) { opened = destination; return client; },
    },
    location: { origin: "https://shepherdr.example" },
    registration: {
      async showNotification(title: string, options: { body: string; data: { destination: string } }) {
        shownTitle = title;
        shownBody = options.body;
        shownDestination = options.data.destination;
      },
    },
  };
  runInNewContext(readFileSync(new URL("./service-worker.js", import.meta.url), "utf8"), {
    self: scope,
    URL,
    URLSearchParams,
  });

  async function lifecycle(name: "activate" | "install"): Promise<void> {
    let work: Promise<unknown> | undefined;
    listeners.get(name)?.({ waitUntil(value: Promise<unknown>) { work = value; } });
    if (!work) throw new Error(`${name} handler did not register work`);
    await work;
  }

  async function push(payload: unknown): Promise<void> {
    let work: Promise<unknown> | undefined;
    const event: Record<string, unknown> = {
      data: { json: () => payload },
      waitUntil(value: Promise<unknown>) { work = value; },
    };
    listeners.get("push")?.(event);
    if (!work) throw new Error("push handler did not register work");
    await work;
  }

  async function click(destination: unknown): Promise<void> {
    let work: Promise<unknown> | undefined;
    listeners.get("notificationclick")?.({
      notification: {
        close() { closed++; },
        data: { destination },
      },
      waitUntil(value: Promise<unknown>) { work = value; },
    });
    if (!work) throw new Error("notification click handler did not register work");
    await work;
  }

  return {
    click,
    claimed: () => claimed,
    closed: () => closed,
    focused: () => focused,
    listenerNames: () => Array.from(listeners.keys()).sort().join(","),
    lifecycle,
    matched: () => matched,
    navigated: () => navigated,
    opened: () => opened,
    push,
    shownBody: () => shownBody,
    shownDestination: () => shownDestination,
    shownTitle: () => shownTitle,
    skipped: () => skipped,
  };
}

test("missing local setup hides the invitation and settings explain the CLI action without permission", async () => {
  const { controller, platform, root } = notificationView();
  await controller.init();
  assert.equal(platform.workerRefreshCount, 1);
  assert.equal(root.querySelectorAll(".notification-invitation").length, 0);

  controller.openSettings();
  await settle();
  assert.equal(root.querySelector("h2")?.textContent, "Settings");
  assert.equal(buttonWithText(root, "Notifications").getAttribute("aria-expanded"), "false");
  assert.equal(root.textContent?.includes("-vapid-contact"), false);
  buttonWithText(root, "Notifications").click();
  assert.equal(buttonWithText(root, "Notifications").getAttribute("aria-expanded"), "true");
  assert.match(root.textContent ?? "", /On the machine running Shepherdr, start it with -vapid-contact and a contact email or website\./);
  assert.equal(platform.requestCount, 0);
  assert.equal(root.querySelectorAll(".notification-primary").length, 0);
});

test("first eligible invitation remembers Not now locally", async () => {
  const { api, controller, platform, root } = notificationView();
  api.configValue = { contact: "mailto:operator@example.com", public_key: "public", setup: true };
  await controller.init();
  assert.equal(root.querySelectorAll(".notification-invitation").length, 1);
  assert.equal(buttonWithText(root, "Turn on notifications").textContent, "Turn on notifications");

  buttonWithText(root, "Not now").click();
  assert.equal(platform.dismissedValue, true);
  assert.equal(root.childElementCount, 0);
  assert.equal(platform.requestCount, 0);
});

test("explicit enable tap requests permission and saves blocked and done defaults", async () => {
  const { api, controller, platform, root } = notificationView();
  api.configValue = { contact: "mailto:operator@example.com", public_key: "public", setup: true };
  await controller.init();
  buttonWithText(root, "Turn on notifications").click();
  await settle();

  assert.equal(platform.requestCount, 1);
  assert.equal(api.saved.length, 1);
  assert.deepEqual(api.saved[0]?.events, defaultNotificationEvents);
  assert.equal(root.querySelector<HTMLInputElement>('input[name="blocked"]')?.checked, true);
  assert.equal(root.querySelector<HTMLInputElement>('input[name="done"]')?.checked, true);
  assert.equal(root.querySelector<HTMLInputElement>('input[name="trusted_sign_in_added"]')?.checked, true);
  assert.equal(root.querySelector<HTMLInputElement>('input[name="trusted_sign_in_removed"]')?.checked, true);
  assert.equal(root.querySelector<HTMLInputElement>('input[name="working"]')?.checked, false);
  assert.equal(root.querySelector<HTMLInputElement>('input[name="workspace_opened"]')?.checked, false);
  assert.match(root.textContent ?? "", /These settings apply only to this browser or installed app\./);
});

test("Devices is reached inside Settings only when sign-in is protected", async () => {
  const { api, controller, root, window } = notificationView();
  api.configValue = { contact: "mailto:operator@example.com", public_key: "public", setup: true };
  let opened = 0;
  let closed = 0;
  controller.setDeviceSettingsHandler((host) => {
    opened++;
    host.append(window.document.createTextNode("Trusted sign-in controls"));
  }, () => { closed++; });
  await controller.init();

  const settingsAction = window.document.createElement("button");
  window.document.body.append(settingsAction);
  settingsAction.focus();
  controller.setDeviceSettingsAvailable(false);
  controller.openSettings();
  await settle();
  assert.equal(root.querySelector(".settings-devices") === null, true);
  buttonWithText(root, "Close").click();

  settingsAction.focus();
  controller.setDeviceSettingsAvailable(true);
  controller.openSettings();
  await settle();
  assert.equal(root.querySelector(".settings-devices h3")?.textContent, "Devices");
  assert.equal(buttonWithText(root, "Notifications").getAttribute("aria-expanded"), "false");
  assert.equal(buttonWithText(root, "Devices").getAttribute("aria-expanded"), "false");
  assert.equal(root.textContent?.includes("Trusted sign-in controls"), false);
  buttonWithText(root, "Notifications").click();
  const scrolledPanel = root.querySelector<HTMLElement>(".notification-panel");
  if (scrolledPanel) scrolledPanel.scrollTop = 240;
  buttonWithText(root, "Devices").click();
  assert.equal(opened, 1);
  assert.equal(buttonWithText(root, "Notifications").getAttribute("aria-expanded"), "true");
  assert.equal(buttonWithText(root, "Devices").getAttribute("aria-expanded"), "true");
  assert.equal(root.querySelector<HTMLElement>(".notification-panel")?.scrollTop, 240);
  assert.equal(window.document.activeElement?.getAttribute("data-settings-section"), "devices");
  assert.equal(window.document.activeElement?.getAttribute("aria-expanded"), "true");
  assert.equal(root.textContent?.includes("Trusted sign-in controls"), true);
  assert.equal(root.querySelector(".notification-panel") !== null, true);
  assert.equal(root.querySelector(".access-layer") === null, true);
  buttonWithText(root, "Close").click();
  assert.equal(closed, 1);
  assert.equal(window.document.activeElement === settingsAction, true);
  assert.equal(root.querySelector(".notification-panel") === null, true);
});

test("denied permission shows browser guidance without subscribing", async () => {
  const { api, controller, platform, root } = notificationView();
  api.configValue = { contact: "https://operator.example", public_key: "public", setup: true };
  platform.requestResult = "denied";
  await controller.init();
  buttonWithText(root, "Turn on notifications").click();
  await settle();

  assert.equal(platform.requestCount, 1);
  assert.equal(platform.subscription, undefined);
  assert.equal(api.saved.length, 0);
  assert.match(root.textContent ?? "", /Notifications are blocked in this browser\. Allow them in browser settings\./);
});

test("Home keeps its icon-only Settings action below the connection indicator", () => {
  const window = new Window({ url: "https://shepherdr.example/" });
  const app = window.document.createElement("main");
  window.document.body.append(app);
  let opened = 0;
  const view = new HomeView(app, {
    isHomeActive: () => true,
    onFocusPane: () => undefined,
    onNotifications: () => { opened++; },
    onOpen: () => undefined,
    onShowAll: () => undefined,
    onShowBlocked: () => undefined,
    prepareWorkspaceAction: async () => ({ outcome: "refused", reason: "not_applicable" }),
    runWorkspaceAction: async () => ({ outcome: "succeeded" }),
  });
  view.render({
    actionsAvailable: true,
    mode: "all",
    reachability: "current",
    state: {
      connection: "live",
      gap: 0,
      has_home: true,
      herdr_version: "0.8.0",
      home: { blocked_count: 0, working_count: 0, workspaces: [] },
      last_known: false,
    },
  });
  const settings = app.querySelector<HTMLButtonElement>("button.home-notifications");
  assert.equal(settings?.getAttribute("aria-label"), "Settings");
  assert.equal(settings?.classList.contains("settings-action"), true);
  assert.equal(settings?.textContent, "");
  assert.equal(settings?.querySelectorAll("svg").length, 1);
  // Settings sits beside Shared files, under the connection badge.
  assert.equal(settings?.parentElement?.className, "home-connection-buttons");
  assert.equal(settings?.parentElement?.parentElement?.className, "state-panel home-connection");
  assert.equal(settings?.parentElement?.parentElement?.children.item(0)?.className, "connection-indicator");
  assert.equal(settings?.parentElement?.children.item(0)?.getAttribute("aria-label"), "Shared files");
  assert.equal(settings?.parentElement?.children.item(1)?.getAttribute("aria-label"), "Settings");
  assert.equal(settings?.parentElement?.childElementCount, 2);
  settings?.click();
  assert.equal(opened, 1);
  window.close();
});

test("exact notification route prefers valid current state over stale selection", () => {
  const route = parseTerminalRoute("#terminal=w1%3Ap1&terminal_id=term-1");
  assert.equal(route?.paneID, "w1:p1");
  assert.equal(route?.terminalID, "term-1");
  assert.equal(terminalRouteOutcome({
    currentStateAvailable: true,
    currentTerminalID: "term-1",
    expectedTerminalID: route?.terminalID,
    selectedTerminalID: "older-terminal-for-same-pane",
  }), "current");
  assert.equal(terminalRouteOutcome({
    currentStateAvailable: true,
    currentTerminalID: "replacement",
    expectedTerminalID: route?.terminalID,
    selectedTerminalID: "term-1",
  }), "unavailable");
  assert.equal(terminalRouteOutcome({
    currentStateAvailable: false,
    currentTerminalID: "term-1",
    expectedTerminalID: route?.terminalID,
  }), "waiting");
});

test("push worker shows plain workspace names and safely handles payload fallback", async () => {
  const worker = serviceWorkerHarness();
  assert.equal(worker.listenerNames(), "activate,install,notificationclick,push");
  await worker.lifecycle("install");
  await worker.lifecycle("activate");
  assert.equal(worker.skipped(), 1);
  assert.equal(worker.claimed(), 1);

  await worker.push({
    destination: "/#terminal=w1%3Ap1&terminal_id=term-1",
    kind: "status",
    pane_id: "w1:p1",
    status: "blocked",
    terminal_id: "term-1",
    workspace_name: "Review <workspace>",
  });
  assert.equal(worker.shownTitle(), "Agent is blocked");
  assert.equal(worker.shownBody(), "Review <workspace>");
  assert.equal(worker.shownDestination(), "/#terminal=w1%3Ap1&terminal_id=term-1");

  await worker.push({
    destination: "/#terminal=w1%3Ap1&terminal_id=term-1",
    kind: "status",
    pane_id: "w1:p1",
    status: "done",
    terminal_id: "term-1",
    workspace_name: "   ",
  });
  assert.equal(worker.shownTitle(), "Shepherdr");
  assert.equal(worker.shownBody(), "A workspace finished.");
  assert.equal(worker.shownDestination(), "/#terminal=w1%3Ap1&terminal_id=term-1");

  await worker.push({ destination: "/", kind: "workspace_opened", workspace_name: "Temporary" });
  assert.equal(worker.shownTitle(), "Workspace opened");
  assert.equal(worker.shownBody(), "Temporary");
  assert.equal(worker.shownDestination(), "/");

  await worker.push({ destination: "/", kind: "workspace_closed", workspace_name: "Temporary" });
  assert.equal(worker.shownTitle(), "Workspace closed");
  assert.equal(worker.shownBody(), "Temporary");
  assert.equal(worker.shownDestination(), "/");

  await worker.push({ destination: "/", kind: "trusted_sign_in_added", trust_label: "Phone <script>" });
  assert.equal(worker.shownTitle(), "Trusted sign-in added");
  assert.equal(worker.shownBody(), "Label: Phone <script>");
  assert.equal(worker.shownDestination(), "/");

  await worker.push({ destination: "/", kind: "trusted_sign_in_removed", trust_label: "   " });
  assert.equal(worker.shownTitle(), "Trusted sign-in removed");
  assert.equal(worker.shownBody(), "A trusted sign-in was removed.");
  assert.equal(worker.shownDestination(), "/");

  await worker.push({ destination: "/", kind: "workspace_opened", workspace_name: "Pay\u202emoc\nexe\u2066 \u200bnow\ufeff" });
  assert.equal(worker.shownBody(), "Paymoc exe now");

  await worker.push({ destination: "/", kind: "trusted_sign_in_added", trust_label: "\u202bTablet\u0007\u2028two\u202c" });
  assert.equal(worker.shownBody(), "Label: Tablet two");

  await worker.push({ destination: "/", kind: "workspace_opened", workspace_name: "   " });
  assert.equal(worker.shownTitle(), "Shepherdr");
  assert.equal(worker.shownBody(), "A workspace opened.");
  assert.equal(worker.shownDestination(), "/");

  await worker.push({
    destination: "/#terminal=wrong&terminal_id=term-1",
    kind: "status",
    pane_id: "w1:p1",
    status: "done",
    terminal_id: "term-1",
  });
  assert.equal(worker.shownTitle(), "Shepherdr");
  assert.equal(worker.shownBody(), "A workspace changed.");
  assert.equal(worker.shownDestination(), "/");

  await worker.click("https://outside.example/terminal");
  assert.equal(worker.closed(), 1);
  assert.equal(worker.opened(), "https://shepherdr.example/");
  assert.equal(worker.matched(), 0);
  assert.equal(worker.navigated(), "");
  assert.equal(worker.focused(), 0);

  const styles = readFileSync(new URL("./style.css", import.meta.url), "utf8");
  assert.match(styles, /\.notification-invitation-actions button,[\s\S]*min-height:\s*44px;/);
});

test("named status pushes use exact Herdr status titles and workspace bodies", async () => {
  const worker = serviceWorkerHarness();
  const destination = "/#terminal=w1%3Ap1&terminal_id=term-1";
  for (const [status, title] of [
    ["working", "Agent is working"],
    ["blocked", "Agent is blocked"],
    ["idle", "Agent is idle"],
    ["done", "Agent is done"],
    ["unknown", "Agent status is unknown"],
  ] as const) {
    await worker.push({
      destination,
      kind: "status",
      pane_id: "w1:p1",
      status,
      terminal_id: "term-1",
      workspace_name: "Review workspace",
    });
    assert.equal(worker.shownTitle(), title);
    assert.equal(worker.shownBody(), "Review workspace");
    assert.equal(worker.shownDestination(), destination);
  }
});

test("status pushes lead with a name the person gave, for every status and both Herdrs", async () => {
  const worker = serviceWorkerHarness();
  const core = { destination: "/#terminal=t3&terminal_id=core%3At3&herdr=core", herdr: "core", pane_id: "t3", terminal_id: "core:t3" };
  const tui = { destination: "/#terminal=w1%3Ap1&terminal_id=term-1", pane_id: "w1:p1", terminal_id: "term-1" };
  for (const status of ["blocked", "error", "done", "idle", "working", "unknown"]) {
    await worker.push({ ...core, kind: "status", name: "Review PR17 CI", status, workspace_name: "Douro" });
    assert.equal(worker.shownTitle(), `Review PR17 CI is ${status}`);
    assert.equal(worker.shownBody(), "Douro");
    assert.equal(worker.shownDestination(), core.destination);
  }
  for (const status of ["blocked", "done", "idle", "working", "unknown"]) {
    await worker.push({ ...tui, kind: "status", name: "Scout P3 Queiroz cite", status, workspace_name: "Douro" });
    assert.equal(worker.shownTitle(), `Scout P3 Queiroz cite is ${status}`);
    assert.equal(worker.shownBody(), "Douro");
    assert.equal(worker.shownDestination(), tui.destination);
  }

  await worker.push({ ...core, kind: "status", status: "done", workspace_name: "Douro" });
  assert.equal(worker.shownTitle(), "Agent is done");
  assert.equal(worker.shownBody(), "Douro");

  // Without a usable space name, the generic wording stays and the name is not shown.
  await worker.push({ ...core, kind: "status", name: "Review PR17 CI", status: "done", workspace_name: "x".repeat(161) });
  assert.equal(worker.shownTitle(), "Shepherdr");
  assert.equal(worker.shownBody(), "A space finished.");

  // A name the server would not send is not shown.
  for (const name of ["x".repeat(61), "bad\u001b]0;title\u0007", "  ", 7, { text: "Review" }]) {
    await worker.push({ ...core, kind: "status", name, status: "blocked", workspace_name: "Douro" });
    assert.equal(worker.shownTitle(), "Agent is blocked");
    assert.equal(worker.shownBody(), "Douro");
  }

  // HTML-looking names stay plain text; a name never picks the link.
  await worker.push({ ...core, kind: "status", name: "<img src=x onerror=alert(1)>", status: "done", workspace_name: "Douro" });
  assert.equal(worker.shownTitle(), "<img src=x onerror=alert(1)> is done");
  assert.equal(worker.shownDestination(), core.destination);
  await worker.push({ ...core, destination: "/#terminal=t9&terminal_id=core%3At3&herdr=core", kind: "status", name: "Review PR17 CI", status: "done", workspace_name: "Douro" });
  assert.equal(worker.shownTitle(), "Shepherdr");
  assert.equal(worker.shownDestination(), "/");

  // Space notices never take a name.
  await worker.push({ destination: "/", herdr: "core", kind: "workspace_opened", name: "Review PR17 CI", workspace_name: "Douro" });
  assert.equal(worker.shownTitle(), "Space opened");
  assert.equal(worker.shownBody(), "Douro");
});

test("invalid lifecycle destinations keep generic copy and do not expose workspace names", async () => {
  const worker = serviceWorkerHarness();
  for (const [kind, destination] of [
    ["workspace_opened", "https://outside.example/terminal"],
    ["workspace_closed", "/not-home"],
  ] as const) {
    await worker.push({ destination, kind, workspace_name: "Private workspace" });
    assert.equal(worker.shownTitle(), "Shepherdr");
    assert.equal(worker.shownBody(), "A workspace changed.");
    assert.equal(worker.shownBody().includes("Private workspace"), false);
    assert.equal(worker.shownDestination(), "/");
  }

  await worker.push({ destination: "https://outside.example/", kind: "trusted_sign_in_added", trust_label: "Private phone" });
  assert.equal(worker.shownTitle(), "Shepherdr");
  assert.equal(worker.shownBody(), "A workspace changed.");
  assert.equal(worker.shownBody().includes("Private phone"), false);
  assert.equal(worker.shownDestination(), "/");
});

test("an unvalidated workspace name keeps generic status copy", async () => {
  const worker = serviceWorkerHarness();
  const destination = "/#terminal=w1%3Ap1&terminal_id=term-1";
  await worker.push({
    destination,
    kind: "status",
    pane_id: "w1:p1",
    status: "done",
    terminal_id: "term-1",
    workspace_name: "x".repeat(161),
  });

  assert.equal(worker.shownTitle(), "Shepherdr");
  assert.equal(worker.shownBody(), "A workspace finished.");
  assert.equal(worker.shownDestination(), destination);
});

test("status click opens the exact terminal URL instead of reusing same-origin Home", async () => {
  const worker = serviceWorkerHarness(true);
  await worker.click("/#terminal=w1%3Ap1&terminal_id=term-1");

  assert.equal(worker.closed(), 1);
  assert.equal(worker.opened(), "https://shepherdr.example/#terminal=w1%3Ap1&terminal_id=term-1");
  assert.equal(worker.matched(), 0);
  assert.equal(worker.navigated(), "");
  assert.equal(worker.focused(), 0);
});

test("status click without an existing client opens the exact terminal URL", async () => {
  const worker = serviceWorkerHarness(false);
  await worker.click("/#terminal=w1%3Ap1&terminal_id=term-1");

  assert.equal(worker.closed(), 1);
  assert.equal(worker.opened(), "https://shepherdr.example/#terminal=w1%3Ap1&terminal_id=term-1");
  assert.equal(worker.matched(), 0);
});

test("lifecycle click opens Home", async () => {
  const worker = serviceWorkerHarness(true);
  await worker.click("/");

  assert.equal(worker.closed(), 1);
  assert.equal(worker.opened(), "https://shepherdr.example/");
  assert.equal(worker.matched(), 0);
});

async function submitSettings(window: Window, root: ParentNode): Promise<void> {
  const nodeFormData = globalThis.FormData;
  globalThis.FormData = window.FormData as unknown as typeof FormData;
  try {
    root.querySelector<HTMLFormElement>("form")!.requestSubmit();
    await settle();
  } finally {
    globalThis.FormData = nodeFormData;
  }
}

test("with only Herdr Core, settings say space and offer error, which starts on", async () => {
  const { api, controller, root, window } = notificationView();
  api.configValue = { contact: "mailto:operator@example.com", public_key: "public", setup: true };
  controller.setPresent({ core: true, tui: false });
  await controller.init();
  assert.match(root.textContent ?? "", /Get an alert when a space needs attention or finishes\./);
  buttonWithText(root, "Turn on notifications").click();
  await settle();

  assert.equal(api.saved[0]?.events.error, true);
  assert.equal(root.querySelector<HTMLInputElement>('input[name="error"]')?.checked, true);
  const labels = Array.from(root.querySelectorAll(".notification-choice span"), (node) => node.textContent);
  assert.deepEqual(labels.slice(0, 7), ["working", "blocked", "error", "idle", "done", "unknown", "Space opened"]);
  assert.equal(labels[7], "Space closed");

  root.querySelector<HTMLInputElement>('input[name="error"]')!.checked = false;
  await submitSettings(window, root);
  assert.equal(api.saved.at(-1)?.events.error, false);
});

test("with only the Herdr TUI, or neither, settings keep workspace words and keep the hidden error choice", async () => {
  const { api, controller, platform, root, window } = notificationView();
  api.configValue = { contact: "mailto:operator@example.com", public_key: "public", setup: true };
  api.events = { ...defaultNotificationEvents, error: false };
  api.enabled = true;
  platform.permissionValue = "granted";
  platform.subscription = new FakeSubscription();
  controller.setPresent({ core: false, tui: true });
  await controller.init();
  controller.openSettings();
  await settle();
  buttonWithText(root, "Notifications").click();
  await settle();

  assert.equal(root.querySelectorAll('input[name="error"]').length, 0);
  const labels = Array.from(root.querySelectorAll(".notification-choice span"), (node) => node.textContent);
  assert.equal(labels.includes("Workspace opened"), true);
  await submitSettings(window, root);
  assert.equal(api.saved.at(-1)?.events.error, false);

  controller.setPresent({ core: false, tui: false });
  await settle();
  assert.equal(root.querySelectorAll('input[name="error"]').length, 0);
  assert.equal(Array.from(root.querySelectorAll(".notification-choice span"), (node) => node.textContent).includes("Workspace closed"), true);
});

test("with both Herdrs present, one set of settings names workspaces and spaces", async () => {
  const { api, controller, root, window } = notificationView();
  api.configValue = { contact: "mailto:operator@example.com", public_key: "public", setup: true };
  controller.setPresent({ core: true, tui: true });
  await controller.init();
  assert.match(root.textContent ?? "", /Get an alert when a workspace or space needs attention or finishes\./);
  buttonWithText(root, "Turn on notifications").click();
  await settle();

  const labels = Array.from(root.querySelectorAll(".notification-choice span"), (node) => node.textContent);
  assert.deepEqual(labels.slice(0, 8), ["working", "blocked", "error", "idle", "done", "unknown", "Workspace or space opened", "Workspace or space closed"]);
  root.querySelector<HTMLInputElement>('input[name="error"]')!.checked = false;
  await submitSettings(window, root);
  assert.equal(api.saved.at(-1)?.events.error, false);
});

test("with the Herdr app, pushes say space and error uses the blocked wording", async () => {
  const worker = serviceWorkerHarness();
  const destination = "/#terminal=t3&terminal_id=core%3At3&herdr=core";
  await worker.push({ destination: "/", herdr: "core", kind: "workspace_opened", workspace_name: "alpha" });
  assert.equal(worker.shownTitle(), "Space opened");
  assert.equal(worker.shownBody(), "alpha");
  await worker.push({ destination: "/", herdr: "core", kind: "workspace_closed" });
  assert.equal(worker.shownBody(), "A space closed.");
  for (const [status, title] of [["blocked", "Agent is blocked"], ["error", "Agent status is error"]] as const) {
    await worker.push({ destination, herdr: "core", kind: "status", pane_id: "t3", status, terminal_id: "core:t3", workspace_name: "alpha" });
    assert.equal(worker.shownTitle(), title);
    assert.equal(worker.shownBody(), "alpha");
    await worker.push({ destination, herdr: "core", kind: "status", pane_id: "t3", status, terminal_id: "core:t3" });
    assert.equal(worker.shownBody(), "A space needs attention.");
  }

  await worker.push({ destination, kind: "status", pane_id: "t3", status: "error", terminal_id: "core:t3", workspace_name: "alpha" });
  assert.equal(worker.shownTitle(), "Shepherdr");
  assert.equal(worker.shownBody(), "A workspace changed.");
});

test("with the Herdr app, pushes for a terminal in no space say terminal", async () => {
  const worker = serviceWorkerHarness();
  const destination = "/#terminal=t4&terminal_id=core%3At4&herdr=core";
  const bodies = Object.entries({
    blocked: "A terminal needs attention.",
    error: "A terminal needs attention.",
    done: "A terminal finished.",
    idle: "A terminal is idle.",
    working: "A terminal is working.",
  });
  for (const [status, body] of bodies) {
    await worker.push({ destination, herdr: "core", kind: "status", other_terminals: true, pane_id: "t4", status, terminal_id: "core:t4" });
    assert.equal(worker.shownTitle(), "Shepherdr");
    assert.equal(worker.shownBody(), body);
  }

  const tui = "/#terminal=w1%3Ap1&terminal_id=term-1";
  await worker.push({ destination: tui, kind: "status", other_terminals: true, pane_id: "w1:p1", status: "blocked", terminal_id: "term-1" });
  assert.equal(worker.shownBody(), "A workspace needs attention.");
});

test("a status push opens only a link to the same Herdr", async () => {
  const worker = serviceWorkerHarness();
  const tui = { kind: "status", pane_id: "w1:p1", status: "blocked", terminal_id: "term-1", workspace_name: "api" };
  for (const [destination, herdr, opens] of [
    ["/#terminal=w1%3Ap1&terminal_id=term-1&herdr=tui", "tui", true],
    ["/#terminal=w1%3Ap1&terminal_id=term-1", "tui", true],
    ["/#terminal=w1%3Ap1&terminal_id=term-1&herdr=tui", undefined, true],
    ["/#terminal=w1%3Ap1&terminal_id=term-1", undefined, true],
    ["/#terminal=w1%3Ap1&terminal_id=term-1&herdr=core", "tui", false],
    ["/#terminal=w1%3Ap1&terminal_id=term-1", "core", false],
    ["/#terminal=w1%3Ap1&terminal_id=term-1&herdr=tui", "core", false],
    ["/#terminal=w1%3Ap1&terminal_id=term-1&herdr=app", "tui", false],
  ] as const) {
    await worker.push({ ...tui, destination, ...(herdr ? { herdr } : {}) });
    assert.equal(worker.shownTitle(), opens ? "Agent is blocked" : "Shepherdr", `${destination} ${herdr}`);
    await worker.click(worker.shownDestination());
    assert.equal(worker.opened(), opens ? `https://shepherdr.example${destination}` : "https://shepherdr.example/", `${destination} ${herdr}`);
  }
  await worker.push({ ...tui, destination: "/#terminal=w1%3Ap1&terminal_id=term-1&herdr=tui", herdr: "tui" });
  assert.equal(worker.shownBody(), "api");
});

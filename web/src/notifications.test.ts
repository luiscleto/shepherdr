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

function serviceWorkerHarness() {
  type Listener = (event: Record<string, unknown>) => void;
  const listeners = new Map<string, Listener>();
  let closed = 0;
  let focused = 0;
  let navigated = "";
  let opened = "";
  let shownBody = "";
  let shownDestination = "";
  const client = {
    url: "https://shepherdr.example/#terminal=old",
    async focus() { focused++; return client; },
    async navigate(destination: string) { navigated = destination; return client; },
  };
  const scope = {
    addEventListener(name: string, listener: Listener) { listeners.set(name, listener); },
    clients: {
      async matchAll() { return [client]; },
      async openWindow(destination: string) { opened = destination; return client; },
    },
    location: { origin: "https://shepherdr.example" },
    registration: {
      async showNotification(_title: string, options: { body: string; data: { destination: string } }) {
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
    closed: () => closed,
    focused: () => focused,
    listenerNames: () => Array.from(listeners.keys()).sort().join(","),
    navigated: () => navigated,
    opened: () => opened,
    push,
    shownBody: () => shownBody,
    shownDestination: () => shownDestination,
  };
}

test("missing local setup hides the invitation and settings explain the CLI action without permission", async () => {
  const { controller, platform, root } = notificationView();
  await controller.init();
  assert.equal(root.querySelectorAll(".notification-invitation").length, 0);

  controller.openSettings();
  await settle();
  assert.equal(root.querySelector("h2")?.textContent, "Notifications aren't set up");
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
  assert.equal(root.querySelector<HTMLInputElement>('input[name="working"]')?.checked, false);
  assert.equal(root.querySelector<HTMLInputElement>('input[name="workspace_opened"]')?.checked, false);
  assert.match(root.textContent ?? "", /These settings apply only to this browser or installed app\./);
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
    onReconnect: () => undefined,
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
      home: { blocked_count: 0, working_count: 0, workspaces: [] },
      last_known: false,
    },
  });
  const settings = app.querySelector<HTMLButtonElement>("button.home-notifications");
  assert.equal(settings?.getAttribute("aria-label"), "Settings");
  assert.equal(settings?.classList.contains("settings-action"), true);
  assert.equal(settings?.textContent, "");
  assert.equal(settings?.querySelectorAll("svg").length, 1);
  assert.equal(settings?.parentElement?.className, "state-panel home-connection");
  assert.equal(settings?.parentElement?.children.item(0)?.className, "connection-indicator");
  assert.equal(settings?.parentElement?.children.item(1)?.textContent, "Reconnect");
  assert.equal(settings?.parentElement?.children.item(2)?.getAttribute("aria-label"), "Settings");
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

test("push worker derives generic exact notices and safely handles click fallback", async () => {
  const worker = serviceWorkerHarness();
  assert.equal(worker.listenerNames(), "notificationclick,push");

  await worker.push({
    destination: "/#terminal=w1%3Ap1&terminal_id=term-1",
    kind: "status",
    pane_id: "w1:p1",
    status: "blocked",
    terminal_id: "term-1",
  });
  assert.equal(worker.shownBody(), "A workspace needs attention.");
  assert.equal(worker.shownDestination(), "/#terminal=w1%3Ap1&terminal_id=term-1");

  await worker.push({
    destination: "/#terminal=wrong&terminal_id=term-1",
    kind: "status",
    pane_id: "w1:p1",
    status: "done",
    terminal_id: "term-1",
  });
  assert.equal(worker.shownBody(), "A workspace changed.");
  assert.equal(worker.shownDestination(), "/");

  await worker.click("https://outside.example/terminal");
  assert.equal(worker.closed(), 1);
  assert.equal(worker.navigated(), "https://shepherdr.example/");
  assert.equal(worker.focused(), 1);
  assert.equal(worker.opened(), "");

  const styles = readFileSync(new URL("./style.css", import.meta.url), "utf8");
  assert.match(styles, /\.notification-invitation-actions button,[\s\S]*min-height:\s*44px;/);
});

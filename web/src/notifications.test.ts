import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

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
import { exactTerminalMatches, parseTerminalRoute } from "./terminal-route.ts";

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

test("Home settings action and exact notification destination preserve existing unavailable behavior", () => {
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
  buttonWithText(app, "Notifications").click();
  assert.equal(opened, 1);

  const route = parseTerminalRoute("#terminal=w1%3Ap1&terminal_id=term-1");
  assert.deepEqual(route, { paneID: "w1:p1", terminalID: "term-1" });
  assert.equal(exactTerminalMatches("term-1", route?.terminalID), true);
  assert.equal(exactTerminalMatches("replacement", route?.terminalID), false);
});

test("push worker remains push-only and derives generic exact-link notices", () => {
  const worker = readFileSync(new URL("./service-worker.js", import.meta.url), "utf8");
  assert.equal(worker.includes('addEventListener("fetch"'), false);
  assert.equal(worker.includes('addEventListener("sync"'), false);
  assert.equal(worker.includes("A workspace needs attention."), true);
  assert.equal(worker.includes("A workspace finished."), true);
  assert.equal(worker.includes('parameters.get("terminal_id") === terminalID'), true);
  assert.equal(worker.includes("workspace_name"), false);

  const styles = readFileSync(new URL("./style.css", import.meta.url), "utf8");
  assert.match(styles, /\.notification-invitation-actions button,[\s\S]*min-height:\s*44px;/);
  assert.match(styles, /\.terminal-notifications,/);
});

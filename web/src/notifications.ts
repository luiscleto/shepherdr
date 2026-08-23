export interface NotificationEvents {
  blocked: boolean;
  done: boolean;
  idle: boolean;
  trusted_sign_in_added: boolean;
  trusted_sign_in_removed: boolean;
  unknown: boolean;
  working: boolean;
  workspace_closed: boolean;
  workspace_opened: boolean;
}

export interface PushSubscriptionData {
  endpoint: string;
  expirationTime: number | null;
  keys: { auth: string; p256dh: string };
}

export interface NotificationConfig {
  contact?: string;
  public_key?: string;
  setup: boolean;
  unavailable?: boolean;
}

export interface NotificationSettings {
  enabled: boolean;
  events: NotificationEvents;
}

export interface BrowserPushSubscription {
  data(): PushSubscriptionData;
  unsubscribe(): Promise<boolean>;
}

export interface NotificationPlatform {
  dismissed(): boolean;
  dismiss(): void;
  existingSubscription(): Promise<BrowserPushSubscription | undefined>;
  iosBrowserTab(): boolean;
  permission(): NotificationPermission;
  requestPermission(): Promise<NotificationPermission>;
  refreshWorker(): Promise<void>;
  subscribe(publicKey: string): Promise<BrowserPushSubscription>;
  supported(): boolean;
}

export interface NotificationAPI {
  config(): Promise<NotificationConfig>;
  read(endpoint: string): Promise<NotificationSettings>;
  remove(endpoint: string): Promise<void>;
  save(subscription: PushSubscriptionData, events: NotificationEvents): Promise<void>;
}

const dismissedKey = "shepherdr.notifications.not-now";

export const defaultNotificationEvents: NotificationEvents = {
  blocked: true,
  done: true,
  idle: false,
  trusted_sign_in_added: true,
  trusted_sign_in_removed: true,
  unknown: false,
  working: false,
  workspace_closed: false,
  workspace_opened: false,
};

export function invitationEligible(
  config: NotificationConfig,
  supported: boolean,
  permission: NotificationPermission,
  dismissed: boolean,
): boolean {
  return config.setup && !config.unavailable && supported && permission === "default" && !dismissed;
}

class BrowserNotificationAPI implements NotificationAPI {
  async config(): Promise<NotificationConfig> {
    const response = await notificationRequest("/api/notifications/config", "POST", {});
    const value = asObject(response);
    return {
      setup: value.setup === true,
      ...(typeof value.contact === "string" ? { contact: value.contact } : {}),
      ...(typeof value.public_key === "string" ? { public_key: value.public_key } : {}),
      ...(value.unavailable === true ? { unavailable: true } : {}),
    };
  }

  async read(endpoint: string): Promise<NotificationSettings> {
    const value = asObject(await notificationRequest("/api/notifications/settings/read", "POST", { endpoint }));
    return { enabled: value.enabled === true, events: parseEvents(value.events) };
  }

  async save(subscription: PushSubscriptionData, events: NotificationEvents): Promise<void> {
    await notificationRequest("/api/notifications/settings", "POST", { events, subscription });
  }

  async remove(endpoint: string): Promise<void> {
    await notificationRequest("/api/notifications/settings", "DELETE", { endpoint });
  }
}

class BrowserNotificationPlatform implements NotificationPlatform {
  dismissed(): boolean {
    try {
      return window.localStorage.getItem(dismissedKey) === "1";
    } catch {
      return false;
    }
  }

  dismiss(): void {
    try {
      window.localStorage.setItem(dismissedKey, "1");
    } catch {
      // The invitation remains dismissed for this rendered visit.
    }
  }

  supported(): boolean {
    return "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
  }

  permission(): NotificationPermission {
    return "Notification" in window ? Notification.permission : "denied";
  }

  requestPermission(): Promise<NotificationPermission> {
    return Notification.requestPermission();
  }

  async refreshWorker(): Promise<void> {
    const registration = await navigator.serviceWorker.register("/service-worker.js", { scope: "/" });
    await registration.update();
  }

  iosBrowserTab(): boolean {
    const ios = /iPad|iPhone|iPod/.test(navigator.userAgent) ||
      (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1);
    const standalone = (navigator as Navigator & { standalone?: boolean }).standalone === true;
    return ios && !standalone;
  }

  async existingSubscription(): Promise<BrowserPushSubscription | undefined> {
    const registration = await navigator.serviceWorker.getRegistration("/");
    const subscription = await registration?.pushManager.getSubscription();
    return subscription ? wrapSubscription(subscription) : undefined;
  }

  async subscribe(publicKey: string): Promise<BrowserPushSubscription> {
    const registration = await navigator.serviceWorker.register("/service-worker.js", { scope: "/" });
    await navigator.serviceWorker.ready;
    const subscription = await registration.pushManager.subscribe({
      applicationServerKey: decodePublicKey(publicKey),
      userVisibleOnly: true,
    });
    return wrapSubscription(subscription);
  }
}

export class NotificationsController {
  readonly #api: NotificationAPI;
  readonly #document: Document;
  readonly #platform: NotificationPlatform;
  readonly #root: HTMLElement;

  #config: NotificationConfig | undefined;
  #busy = false;
  #dialogOpen = false;
  #dismissedForVisit = false;
  #events = { ...defaultNotificationEvents };
  #devicesAvailable = false;
  #devicesExpanded = false;
  #notificationsExpanded = false;
  #openDevices: ((host: HTMLElement, closeSettings: () => void) => void) | undefined;
  #closeDevices: (() => void) | undefined;
  #focusSection: "devices" | "notifications" | undefined;
  #returnFocus: HTMLElement | undefined;
  #subscription: BrowserPushSubscription | undefined;

  constructor(
    root: HTMLElement,
    api: NotificationAPI = new BrowserNotificationAPI(),
    platform: NotificationPlatform = new BrowserNotificationPlatform(),
  ) {
    this.#root = root;
    this.#document = root.ownerDocument;
    this.#api = api;
    this.#platform = platform;
  }

  async init(): Promise<void> {
    const workerRefresh = this.#platform.supported()
      ? this.#platform.refreshWorker().catch(() => undefined)
      : Promise.resolve();
    try {
      this.#config = await this.#api.config();
    } catch {
      this.#config = { setup: false, unavailable: true };
    }
    this.#render();
    await workerRefresh;
  }

  setDeviceSettingsHandler(
    open: (host: HTMLElement, closeSettings: () => void) => void,
    close: () => void,
  ): void {
    this.#openDevices = open;
    this.#closeDevices = close;
  }

  setDeviceSettingsAvailable(available: boolean): void {
    if (!available && this.#devicesExpanded) {
      this.#devicesExpanded = false;
      this.#closeDevices?.();
    }
    this.#devicesAvailable = available;
    if (this.#dialogOpen) this.#renderSettings();
  }

  openSettings(): void {
    const HTMLElementConstructor = this.#document.defaultView?.HTMLElement;
    this.#returnFocus = HTMLElementConstructor && this.#document.activeElement instanceof HTMLElementConstructor
      ? this.#document.activeElement
      : undefined;
    this.#notificationsExpanded = false;
    this.#devicesExpanded = false;
    this.#dialogOpen = true;
    this.#renderSettings();
    void this.#loadSettings();
  }

  #closeSettings(): void {
    this.#dialogOpen = false;
    this.#notificationsExpanded = false;
    const devicesWereExpanded = this.#devicesExpanded;
    this.#devicesExpanded = false;
    if (devicesWereExpanded) this.#closeDevices?.();
    this.#render();
    this.#returnFocus?.focus({ preventScroll: true });
    this.#returnFocus = undefined;
  }

  async #loadSettings(): Promise<void> {
    if (!this.#config) await this.init();
    if (!this.#dialogOpen || !this.#config?.setup || this.#config.unavailable ||
      !this.#platform.supported() || this.#platform.iosBrowserTab()) {
      this.#renderSettings();
      return;
    }
    if (this.#platform.permission() !== "granted") {
      this.#renderSettings();
      return;
    }
    try {
      this.#subscription = await this.#platform.existingSubscription();
      if (this.#subscription) {
        const settings = await this.#api.read(this.#subscription.data().endpoint);
        if (settings.enabled) this.#events = settings.events;
        else this.#subscription = undefined;
      }
      this.#renderSettings();
    } catch {
      this.#subscription = undefined;
      this.#renderSettings("Could not load notification settings. Try again.");
    }
  }

  async #turnOn(): Promise<void> {
    if (this.#busy) return;
    this.#busy = true;
    this.#notificationsExpanded = true;
    let permission: NotificationPermission;
    try {
      permission = await this.#platform.requestPermission();
    } catch {
      this.#busy = false;
      this.#dialogOpen = true;
      this.#renderSettings("Notifications could not be turned on. Try again.");
      return;
    }
    if (permission !== "granted") {
      this.#busy = false;
      this.#dialogOpen = true;
      this.#renderSettings();
      return;
    }
    if (!this.#config?.public_key) {
      this.#busy = false;
      this.#dialogOpen = true;
      this.#renderSettings("Notifications are not ready. Try again after Shepherdr restarts.");
      return;
    }
    this.#dialogOpen = true;
    this.#renderSettings("Turning on notifications…");
    let subscription: BrowserPushSubscription | undefined;
    try {
      const existing = await this.#platform.existingSubscription();
      if (existing) {
        const prior = await this.#api.read(existing.data().endpoint);
        if (prior.enabled) {
          subscription = existing;
          this.#events = prior.events;
        } else {
          await existing.unsubscribe();
        }
      }
      subscription ??= await this.#platform.subscribe(this.#config.public_key);
      await this.#api.save(subscription.data(), this.#events);
      this.#subscription = subscription;
      this.#busy = false;
      this.#renderSettings();
    } catch {
      if (subscription && subscription !== this.#subscription) await subscription.unsubscribe().catch(() => false);
      this.#busy = false;
      this.#renderSettings("Notifications could not be turned on. Try again.");
    }
  }

  async #save(form: HTMLFormElement): Promise<void> {
    if (!this.#subscription || this.#busy) return;
    const next = eventsFromForm(form);
    this.#busy = true;
    this.#renderSettings("Saving notification settings…");
    try {
      await this.#api.save(this.#subscription.data(), next);
      this.#events = next;
      this.#busy = false;
      this.#renderSettings("Notification settings saved.");
    } catch {
      this.#busy = false;
      this.#renderSettings("Settings were not saved. Your previous choices are still active.");
    }
  }

  async #turnOff(): Promise<void> {
    if (!this.#subscription || this.#busy) return;
    const subscription = this.#subscription;
    this.#busy = true;
    this.#renderSettings("Turning off notifications…");
    try {
      await this.#api.remove(subscription.data().endpoint);
    } catch {
      this.#busy = false;
      this.#renderSettings("Notifications were not turned off. Try again.");
      return;
    }
    this.#subscription = undefined;
    this.#busy = false;
    try {
      await subscription.unsubscribe();
      this.#renderSettings("Notifications are off for this browser.");
    } catch {
      this.#renderSettings("Notifications are off in Shepherdr, but this browser could not finish unsubscribing.");
    }
  }

  #render(): void {
    if (this.#dialogOpen) {
      this.#renderSettings();
      return;
    }
    const config = this.#config;
    const eligible = config && invitationEligible(
      config,
      this.#platform.supported() && !this.#platform.iosBrowserTab(),
      this.#platform.permission(),
      this.#dismissedForVisit || this.#platform.dismissed(),
    );
    if (!eligible) {
      this.#root.replaceChildren();
      return;
    }
    const invitation = element(this.#document, "aside", "notification-invitation");
    invitation.setAttribute("aria-label", "Notifications invitation");
    const copy = element(this.#document, "div");
    copy.append(
      element(this.#document, "strong", undefined, "Stay close to the work"),
      element(this.#document, "p", undefined, "Get an alert when a workspace needs attention or finishes."),
    );
    const actions = element(this.#document, "div", "notification-invitation-actions");
    actions.append(
      action(this.#document, "Turn on notifications", () => void this.#turnOn()),
      action(this.#document, "Not now", () => {
        this.#dismissedForVisit = true;
        this.#platform.dismiss();
        this.#render();
      }),
    );
    invitation.append(copy, actions);
    this.#root.replaceChildren(invitation);
  }

  #renderSettings(message?: string): void {
    if (!this.#dialogOpen) return;
    const layer = element(this.#document, "div", "notification-layer");
    const panel = element(this.#document, "section", "notification-panel");
    panel.setAttribute("role", "dialog");
    panel.setAttribute("aria-modal", "true");
    panel.setAttribute("aria-labelledby", "settings-title");
    panel.tabIndex = -1;
    const heading = element(this.#document, "h2", undefined, "Settings");
    heading.id = "settings-title";
    const close = action(this.#document, "Close", () => this.#closeSettings(), "notification-close");
    const header = element(this.#document, "header", "notification-panel-header");
    header.append(heading, close);
    panel.append(header);
    const notificationSection = element(this.#document, "section", "settings-section");
    const notificationHeading = element(this.#document, "h3");
    const notificationToggle = action(
      this.#document,
      "Notifications",
      () => this.#toggleSection("notifications"),
      "settings-section-toggle",
    );
    notificationToggle.setAttribute("aria-expanded", String(this.#notificationsExpanded));
    notificationToggle.setAttribute("aria-controls", "settings-notifications-content");
    notificationToggle.dataset.settingsSection = "notifications";
    notificationHeading.append(notificationToggle);
    notificationSection.append(notificationHeading);

    if (this.#notificationsExpanded) {
      const notificationContent = element(this.#document, "div", "settings-section-content");
      notificationContent.id = "settings-notifications-content";
      notificationContent.append(element(
        this.#document,
        "p",
        "notification-scope",
        "These settings apply only to this browser or installed app.",
      ));

      const config = this.#config;
      if (!config) {
        notificationContent.append(element(this.#document, "p", undefined, message ?? "Loading notification settings…"));
      } else if (config.unavailable) {
        notificationContent.append(element(this.#document, "p", undefined, "Notifications unavailable. Check Shepherdr on the machine where it is running."));
      } else if (!config.setup) {
        const setup = element(this.#document, "p");
        setup.append(
          "Notifications aren't set up. On the machine running Shepherdr, start it with ",
          element(this.#document, "code", undefined, "-vapid-contact"),
          " and a contact email or website.",
        );
        notificationContent.append(setup);
      } else if (this.#platform.iosBrowserTab()) {
        notificationContent.append(element(this.#document, "p", undefined, "Add Shepherdr to your Home Screen, then open it there."));
      } else if (!this.#platform.supported()) {
        notificationContent.append(element(
          this.#document,
          "p",
          undefined,
          "Notifications aren't available in this browser.",
        ));
      } else if (this.#platform.permission() === "denied") {
        notificationContent.append(element(
          this.#document,
          "p",
          undefined,
          "Notifications are blocked in this browser. Allow them in browser settings.",
        ));
      } else if (!this.#subscription) {
        const turnOn = action(this.#document, "Turn on notifications", () => void this.#turnOn(), "notification-primary");
        turnOn.disabled = this.#busy;
        notificationContent.append(
          element(this.#document, "p", undefined, message ?? "Choose which events matter after notifications are on."),
          turnOn,
        );
      } else {
        const form = element(this.#document, "form", "notification-form");
        const fields = element(this.#document, "fieldset");
        fields.append(element(this.#document, "legend", undefined, "Notify me when"));
        for (const [name, label] of eventChoices) fields.append(eventCheckbox(this.#document, name, label, this.#events[name]));
        const buttons = element(this.#document, "div", "notification-form-actions");
        const save = action(this.#document, "Save", () => undefined, "notification-primary");
        save.type = "submit";
        buttons.append(save, action(this.#document, "Turn off notifications", () => void this.#turnOff()));
        form.append(fields, buttons);
        for (const control of Array.from(form.querySelectorAll<HTMLInputElement | HTMLButtonElement>("input, button"))) {
          control.disabled = this.#busy;
        }
        form.addEventListener("submit", (event) => {
          event.preventDefault();
          void this.#save(form);
        });
        notificationContent.append(form);
        if (message) notificationContent.append(element(this.#document, "p", "notification-feedback", message));
      }
      notificationSection.append(notificationContent);
    }

    panel.append(notificationSection);
    let devicesHost: HTMLElement | undefined;
    if (this.#devicesAvailable && this.#openDevices) {
      const devicesSection = element(this.#document, "section", "settings-section settings-devices");
      const devicesHeading = element(this.#document, "h3");
      const devicesToggle = action(
        this.#document,
        "Devices",
        () => this.#toggleSection("devices"),
        "settings-section-toggle",
      );
      devicesToggle.setAttribute("aria-expanded", String(this.#devicesExpanded));
      devicesToggle.setAttribute("aria-controls", "settings-devices-content");
      devicesToggle.dataset.settingsSection = "devices";
      devicesHeading.append(devicesToggle);
      devicesSection.append(devicesHeading);
      if (this.#devicesExpanded) {
        devicesHost = element(this.#document, "div", "settings-section-content");
        devicesHost.id = "settings-devices-content";
        devicesSection.append(devicesHost);
      }
      panel.append(devicesSection);
    }

    layer.append(panel);
    layer.addEventListener("click", (event) => {
      if (event.target === layer) this.#closeSettings();
    });
    layer.addEventListener("keydown", (event) => {
      if (event.key === "Escape") this.#closeSettings();
    });
    this.#root.replaceChildren(layer);
    if (devicesHost) this.#openDevices?.(devicesHost, () => this.#closeSettings());
    const sectionToFocus = this.#focusSection;
    this.#focusSection = undefined;
    if (sectionToFocus) {
      panel.querySelector<HTMLButtonElement>(`[data-settings-section="${sectionToFocus}"]`)?.focus({ preventScroll: true });
    } else {
      panel.focus({ preventScroll: true });
    }
  }

  #toggleSection(section: "devices" | "notifications"): void {
    if (section === "notifications") {
      this.#notificationsExpanded = !this.#notificationsExpanded;
    } else {
      this.#devicesExpanded = !this.#devicesExpanded;
      if (!this.#devicesExpanded) this.#closeDevices?.();
    }
    this.#focusSection = section;
    this.#renderSettings();
  }
}

const eventChoices: ReadonlyArray<[keyof NotificationEvents, string]> = [
  ["working", "working"],
  ["blocked", "blocked"],
  ["idle", "idle"],
  ["done", "done"],
  ["unknown", "unknown"],
  ["workspace_opened", "Workspace opened"],
  ["workspace_closed", "Workspace closed"],
  ["trusted_sign_in_added", "Trusted sign-in added"],
  ["trusted_sign_in_removed", "Trusted sign-in removed"],
];

function eventCheckbox(
  document: Document,
  name: keyof NotificationEvents,
  label: string,
  checked: boolean,
): HTMLLabelElement {
  const field = element(document, "label", "notification-choice");
  const input = element(document, "input");
  input.type = "checkbox";
  input.name = name;
  input.checked = checked;
  field.append(input, element(document, "span", undefined, label));
  return field;
}

function eventsFromForm(form: HTMLFormElement): NotificationEvents {
  const data = new FormData(form);
  return {
    blocked: data.has("blocked"),
    done: data.has("done"),
    idle: data.has("idle"),
    trusted_sign_in_added: data.has("trusted_sign_in_added"),
    trusted_sign_in_removed: data.has("trusted_sign_in_removed"),
    unknown: data.has("unknown"),
    working: data.has("working"),
    workspace_closed: data.has("workspace_closed"),
    workspace_opened: data.has("workspace_opened"),
  };
}

function parseEvents(value: unknown): NotificationEvents {
  const object = asObject(value);
  const result = { ...defaultNotificationEvents };
  for (const [name] of eventChoices) {
    if (typeof object[name] !== "boolean") throw new Error("invalid notification settings");
    result[name] = object[name];
  }
  return result;
}

function wrapSubscription(subscription: PushSubscription): BrowserPushSubscription {
  return {
    data: () => {
      const value = subscription.toJSON();
      const endpoint = value.endpoint;
      const auth = value.keys?.auth;
      const p256dh = value.keys?.p256dh;
      if (typeof endpoint !== "string" || typeof auth !== "string" || typeof p256dh !== "string") {
        throw new Error("invalid browser push subscription");
      }
      return {
        endpoint,
        expirationTime: typeof value.expirationTime === "number" ? value.expirationTime : null,
        keys: { auth, p256dh },
      };
    },
    unsubscribe: () => subscription.unsubscribe(),
  };
}

function decodePublicKey(value: string): Uint8Array<ArrayBuffer> {
  const padding = "=".repeat((4 - value.length % 4) % 4);
  const decoded = window.atob((value + padding).replace(/-/g, "+").replace(/_/g, "/"));
  const bytes = new Uint8Array(decoded.length);
  for (let index = 0; index < decoded.length; index++) bytes[index] = decoded.charCodeAt(index);
  return bytes;
}

async function notificationRequest(path: string, method: "POST" | "DELETE", body: unknown): Promise<unknown> {
  const response = await fetch(path, {
    body: JSON.stringify(body),
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    method,
  });
  const value: unknown = await response.json();
  if (!response.ok) throw new Error("notification request failed");
  return value;
}

function asObject(value: unknown): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Error("invalid notification response");
  return value as Record<string, unknown>;
}

function element<K extends keyof HTMLElementTagNameMap>(
  document: Document,
  tag: K,
  className?: string,
  text?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function action(document: Document, text: string, run: () => void, className?: string): HTMLButtonElement {
  const button = element(document, "button", className, text);
  button.type = "button";
  button.addEventListener("click", run);
  return button;
}

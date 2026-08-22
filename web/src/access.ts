export type AccessProbe = "protected" | "sign-in-off" | "signed-out" | "unavailable";

interface AccessActions {
  onSessionRotated(): void;
  onSessionRotating(active: boolean): void;
  onSignedIn(): void;
  onSignedOut(): void;
}

interface Device {
  backup_eligible: boolean;
  backup_observed_at: string;
  backup_state: boolean;
  created_at: string;
  label: string;
  last_used_at: string;
  trust_id: string;
}

interface DevicesResponse {
  can_revoke: boolean;
  current_trust_id: string;
  devices: Device[];
}

interface InvitationResponse {
  expires_at: string;
  link: string;
  qr: string;
}

interface PublicKeyOptions {
  publicKey: Record<string, unknown>;
}

const unusableInvitationMessage =
  "This invitation can't be used. Create a new invitation on the machine running Shepherdr or from a trusted device.";

class AccessRequestError extends Error {
  constructor(readonly status: number, message: string) {
    super(message);
  }
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
  const node = element(document, "button", className, text);
  node.type = "button";
  node.addEventListener("click", run);
  return node;
}

export function invitationToken(hash: string): string | undefined {
  if (!hash.startsWith("#")) return undefined;
  const entries = Array.from(new URLSearchParams(hash.slice(1)).entries());
  if (entries.length !== 1 || entries[0][0] !== "trust") return undefined;
  const value = entries[0][1];
  return value && /^[A-Za-z0-9_-]{43}$/.test(value) ? value : undefined;
}

export function hasInvitationFragment(hash: string): boolean {
  if (!hash.startsWith("#")) return false;
  return Array.from(new URLSearchParams(hash.slice(1)).keys()).some((key) => key === "trust");
}

export class AccessController {
  readonly #actions: AccessActions;
  readonly #document: Document;
  readonly #root: HTMLElement;
  #busy = false;
  #returnFocus: HTMLElement | undefined;

  constructor(root: HTMLElement, actions: AccessActions) {
    this.#root = root;
    this.#document = root.ownerDocument;
    this.#actions = actions;
  }

  async probe(): Promise<AccessProbe> {
    try {
      const response = await fetch("/api/devices", {
        cache: "no-store",
        credentials: "same-origin",
        headers: { Accept: "application/json" },
      });
      if (response.status === 200) return "protected";
      if (response.status === 401) return "signed-out";
      if (response.status === 404) return "sign-in-off";
      return "unavailable";
    } catch {
      return "unavailable";
    }
  }

  renderChecking(host: HTMLElement): void {
    host.className = "access-screen";
    const panel = this.#panel("Shepherdr", "Connecting");
    panel.append(element(this.#document, "p", undefined, "Opening Shepherdr."));
    host.replaceChildren(panel);
  }

  renderSignIn(host: HTMLElement, message?: string): void {
    host.className = "access-screen";
    const panel = this.#panel("Shepherdr", "Sign in");
    panel.append(
      element(this.#document, "p", undefined, "Use your passkey to open Shepherdr."),
      action(this.#document, "Sign in with a passkey", () => void this.#signIn(host), "access-primary"),
    );
    if (message) panel.append(element(this.#document, "p", "access-feedback", message));
    host.replaceChildren(panel);
  }

  renderTrust(host: HTMLElement, token: string, message?: string): void {
    host.className = "access-screen";
    if (!token) {
      const panel = element(this.#document, "section", "access-panel");
      panel.append(element(this.#document, "p", "access-feedback", unusableInvitationMessage));
      host.replaceChildren(panel);
      return;
    }
    const panel = this.#panel("Shepherdr", "Trust this device");
    panel.append(element(this.#document, "p", undefined, "Create a passkey to trust this browser."));
    const label = element(this.#document, "label", "access-field");
    label.append(
      element(this.#document, "span", undefined, "Label"),
      element(this.#document, "span", "access-field-help", "Shown on Devices. Not an account."),
    );
    const input = element(this.#document, "input");
    input.name = "device-label";
    input.maxLength = 160;
    input.autocomplete = "off";
    input.value = "Trusted sign-in";
    label.append(input);
    const trust = action(this.#document, "Trust this device", () => void this.#trust(host, token, input.value), "access-primary");
    panel.append(label, trust);
    if (message) panel.append(element(this.#document, "p", "access-feedback", message));
    host.replaceChildren(panel);
  }

  async openDevices(): Promise<void> {
    if (this.#busy) return;
    const HTMLElementConstructor = this.#document.defaultView?.HTMLElement;
    this.#returnFocus = HTMLElementConstructor && this.#document.activeElement instanceof HTMLElementConstructor
      ? this.#document.activeElement
      : undefined;
    this.#renderDevices(undefined, "Loading trusted sign-ins…");
    try {
      const devices = await accessRequest<DevicesResponse>("/api/devices", "GET");
      this.#renderDevices(devices);
    } catch (error) {
      if (error instanceof AccessRequestError && error.status === 401) {
        this.#closeDevices();
        this.#actions.onSignedOut();
        return;
      }
      this.#renderDevices(undefined, "Could not load trusted sign-ins. Try again.");
    }
  }

  close(): void {
    this.#root.replaceChildren();
  }

  async #signIn(host: HTMLElement): Promise<void> {
    if (this.#busy) return;
    this.#busy = true;
    this.renderSignIn(host, "Waiting for your passkey…");
    try {
      const begin = await accessRequest<PublicKeyOptions>("/api/auth/sign-in/begin", "POST", {});
      const credential = await getCredential(begin);
      await accessRequest("/api/auth/sign-in/finish", "POST", credential);
      this.#busy = false;
      this.#actions.onSignedIn();
    } catch {
      this.#busy = false;
      this.renderSignIn(host, "Sign in again.");
    }
  }

  async #trust(host: HTMLElement, token: string, label: string): Promise<void> {
    if (this.#busy) return;
    this.#busy = true;
    this.renderTrust(host, token, "Waiting for your passkey…");
    try {
      const begin = await accessRequest<PublicKeyOptions>("/api/auth/trust/begin", "POST", { label, token });
      const credential = await createCredential(begin);
      await accessRequest("/api/auth/trust/finish", "POST", credential);
      this.#busy = false;
      this.#actions.onSignedIn();
    } catch (error) {
      this.#busy = false;
      if (error instanceof AccessRequestError && error.status === 400 && error.message === unusableInvitationMessage) {
        this.renderTrust(host, "");
        return;
      }
      this.renderTrust(host, token, "Passkey not created. Try again.");
    }
  }

  async #reauthenticate(): Promise<void> {
    this.#actions.onSessionRotating(true);
    try {
      const begin = await accessRequest<PublicKeyOptions>("/api/auth/reauthenticate/begin", "POST", {});
      const credential = await getCredential(begin);
      await accessRequest("/api/auth/reauthenticate/finish", "POST", credential);
      this.#actions.onSessionRotated();
    } finally {
      this.#actions.onSessionRotating(false);
    }
  }

  async #freshMutation<T>(operation: () => Promise<T>): Promise<T> {
    try {
      return await operation();
    } catch (error) {
      if (!(error instanceof AccessRequestError) || error.status !== 428) throw error;
      await this.#reauthenticate();
      return operation();
    }
  }

  #renderDevices(devices?: DevicesResponse, message?: string): void {
    const layer = element(this.#document, "div", "access-layer");
    const panel = element(this.#document, "section", "access-devices-panel");
    panel.setAttribute("role", "dialog");
    panel.setAttribute("aria-modal", "true");
    panel.setAttribute("aria-labelledby", "devices-title");
    panel.tabIndex = -1;
    const heading = element(this.#document, "h2", undefined, "Devices");
    heading.id = "devices-title";
    const close = action(this.#document, "Close", () => this.#closeDevices(), "access-close");
    const header = element(this.#document, "header", "access-panel-header");
    header.append(heading, close);
    panel.append(
      header,
      element(
        this.#document,
        "p",
        "access-scope",
        "A passkey may sync. Its copies share one trusted sign-in here and are revoked together.",
      ),
    );
    if (message) panel.append(element(this.#document, "p", "access-feedback", message));
    if (devices) {
      const list = element(this.#document, "ul", "device-list");
      for (const device of devices.devices) {
        const item = element(this.#document, "li", "device-row");
        const copy = element(this.#document, "div");
        copy.append(
          element(this.#document, "strong", undefined, device.label || "Trusted sign-in"),
          element(this.#document, "span", undefined, "Last used " + formatTime(device.last_used_at)),
          element(
            this.#document,
            "span",
            undefined,
            "Last reported by this passkey at " + formatTime(device.backup_observed_at) +
              (device.backup_state ? ": backup" : ": no backup"),
          ),
        );
        item.append(copy);
        if (devices.can_revoke) {
          item.append(action(this.#document, "Revoke", () => void this.#revoke(device, devices)));
        }
        list.append(item);
      }
      const actions = element(this.#document, "div", "access-device-actions");
      actions.append(
        action(this.#document, "Trust another device", () => void this.#createInvitation(devices), "access-primary"),
        action(this.#document, "Sign out", () => void this.#signOut(devices)),
      );
      panel.append(list);
      if (!devices.can_revoke) {
        panel.append(element(
          this.#document,
          "p",
          "access-scope",
          "This is the last trusted sign-in. To clear all access, stop Shepherdr and reset it on the machine.",
        ));
      }
      panel.append(actions);
    }
    layer.append(panel);
    this.#root.replaceChildren(layer);
    panel.focus();
  }

  async #createInvitation(devices: DevicesResponse): Promise<void> {
    if (this.#busy) return;
    this.#busy = true;
    this.#renderDevices(devices, "Waiting for your passkey…");
    try {
      const invitation = await this.#freshMutation(() =>
        accessRequest<InvitationResponse>("/api/devices/invitations", "POST", {})
      );
      this.#busy = false;
      this.#renderInvitation(invitation);
    } catch (error) {
      this.#busy = false;
      if (error instanceof AccessRequestError && error.status === 401) {
        this.#closeDevices();
        this.#actions.onSignedOut();
        return;
      }
      this.#renderDevices(devices, "An invitation could not be created. Try again.");
    }
  }

  #renderInvitation(invitation: InvitationResponse): void {
    const layer = element(this.#document, "div", "access-layer");
    const panel = element(this.#document, "section", "access-devices-panel access-invitation-panel");
    panel.setAttribute("role", "dialog");
    panel.setAttribute("aria-modal", "true");
    const heading = element(this.#document, "h2", undefined, "Trust another device");
    const close = action(this.#document, "Close", () => this.#closeDevices(), "access-close");
    const header = element(this.#document, "header", "access-panel-header");
    header.append(heading, close);
    const link = element(this.#document, "code", "access-invitation-link", invitation.link);
    const qr = element(this.#document, "pre", "access-qr", invitation.qr);
    qr.setAttribute("aria-label", "Invitation QR code");
    panel.append(
      header,
      element(this.#document, "p", undefined, "Open this link on that device, or scan the QR code. It expires in ten minutes."),
      link,
      action(this.#document, "Copy link", () => void navigator.clipboard?.writeText(invitation.link)),
      qr,
    );
    layer.append(panel);
    this.#root.replaceChildren(layer);
    panel.tabIndex = -1;
    panel.focus();
  }

  async #revoke(device: Device, devices: DevicesResponse): Promise<void> {
    if (this.#busy || !devices.can_revoke) return;
    if (!window.confirm("Revoke " + (device.label || "this trusted sign-in") + "? Its passkey copies will no longer open Shepherdr.")) return;
    this.#busy = true;
    this.#renderDevices(devices, "Waiting for your passkey…");
    try {
      await this.#freshMutation(() => accessRequest("/api/devices/revoke", "POST", { trust_id: device.trust_id }));
      this.#busy = false;
      if (device.trust_id === devices.current_trust_id) {
        this.#closeDevices();
        this.#actions.onSignedOut();
        return;
      }
      const updated = await accessRequest<DevicesResponse>("/api/devices", "GET");
      this.#renderDevices(updated, "Trusted sign-in revoked.");
    } catch (error) {
      this.#busy = false;
      if (error instanceof AccessRequestError && error.status === 401) {
        this.#closeDevices();
        this.#actions.onSignedOut();
        return;
      }
      this.#renderDevices(devices, "This trusted sign-in could not be revoked. Try again.");
    }
  }

  async #signOut(devices: DevicesResponse): Promise<void> {
    if (this.#busy) return;
    this.#busy = true;
    try {
      await accessRequest("/api/auth/sign-out", "POST", {});
    } catch {
      this.#busy = false;
      this.#renderDevices(devices, "Could not sign out. Try again.");
      return;
    }
    this.#busy = false;
    this.#closeDevices();
    this.#actions.onSignedOut();
  }

  #closeDevices(): void {
    this.#root.replaceChildren();
    this.#returnFocus?.focus({ preventScroll: true });
    this.#returnFocus = undefined;
  }

  #panel(eyebrow: string, heading: string): HTMLElement {
    const panel = element(this.#document, "section", "access-panel");
    panel.append(
      element(this.#document, "p", "eyebrow", eyebrow),
      element(this.#document, "h1", undefined, heading),
    );
    return panel;
  }
}

async function accessRequest<T = unknown>(endpoint: string, method: "GET" | "POST", body?: unknown): Promise<T> {
  const response = await fetch(endpoint, {
    method,
    cache: "no-store",
    credentials: "same-origin",
    headers: method === "POST"
      ? { Accept: "application/json", "Content-Type": "application/json" }
      : { Accept: "application/json" },
    ...(method === "POST" ? { body: JSON.stringify(body) } : {}),
  });
  let result: unknown;
  try {
    result = await response.json();
  } catch {
    result = undefined;
  }
  if (!response.ok) {
    const message = isObject(result) && typeof result.error === "string" ? result.error : "The request could not be completed.";
    throw new AccessRequestError(response.status, message);
  }
  return result as T;
}

async function createCredential(options: PublicKeyOptions): Promise<Record<string, unknown>> {
  if (!navigator.credentials?.create) throw new Error("Passkeys are unavailable");
  const publicKey = creationOptions(options.publicKey);
  const credential = await navigator.credentials.create({ publicKey }) as PublicKeyCredential | null;
  if (!credential || !(credential.response instanceof AuthenticatorAttestationResponse)) throw new Error("Passkey creation stopped");
  const response = credential.response;
  const credentialPublicKey = response.getPublicKey?.();
  const publicKeyAlgorithm = response.getPublicKeyAlgorithm?.();
  return {
    id: credential.id,
    rawId: encodeBase64URL(credential.rawId),
    type: credential.type,
    response: {
      attestationObject: encodeBase64URL(response.attestationObject),
      clientDataJSON: encodeBase64URL(response.clientDataJSON),
      transports: response.getTransports?.() ?? [],
      ...(response.getAuthenticatorData ? { authenticatorData: encodeBase64URL(response.getAuthenticatorData()) } : {}),
      ...(credentialPublicKey ? { publicKey: encodeBase64URL(credentialPublicKey) } : {}),
      ...(publicKeyAlgorithm !== null && publicKeyAlgorithm !== undefined ? { publicKeyAlgorithm } : {}),
    },
    authenticatorAttachment: credential.authenticatorAttachment ?? undefined,
    clientExtensionResults: credential.getClientExtensionResults(),
  };
}

async function getCredential(options: PublicKeyOptions): Promise<Record<string, unknown>> {
  if (!navigator.credentials?.get) throw new Error("Passkeys are unavailable");
  const publicKey = requestOptions(options.publicKey);
  const credential = await navigator.credentials.get({ publicKey }) as PublicKeyCredential | null;
  if (!credential || !(credential.response instanceof AuthenticatorAssertionResponse)) throw new Error("Sign in stopped");
  const response = credential.response;
  return {
    id: credential.id,
    rawId: encodeBase64URL(credential.rawId),
    type: credential.type,
    response: {
      authenticatorData: encodeBase64URL(response.authenticatorData),
      clientDataJSON: encodeBase64URL(response.clientDataJSON),
      signature: encodeBase64URL(response.signature),
      ...(response.userHandle ? { userHandle: encodeBase64URL(response.userHandle) } : {}),
    },
    authenticatorAttachment: credential.authenticatorAttachment ?? undefined,
    clientExtensionResults: credential.getClientExtensionResults(),
  };
}

function creationOptions(value: Record<string, unknown>): PublicKeyCredentialCreationOptions {
  const user = isObject(value.user) ? value.user : {};
  return {
    ...value,
    challenge: decodeBase64URL(String(value.challenge ?? "")),
    user: { ...user, id: decodeBase64URL(String(user.id ?? "")) },
    excludeCredentials: Array.isArray(value.excludeCredentials)
      ? value.excludeCredentials.map(credentialDescriptor)
      : [],
  } as PublicKeyCredentialCreationOptions;
}

function requestOptions(value: Record<string, unknown>): PublicKeyCredentialRequestOptions {
  return {
    ...value,
    challenge: decodeBase64URL(String(value.challenge ?? "")),
    allowCredentials: Array.isArray(value.allowCredentials)
      ? value.allowCredentials.map(credentialDescriptor)
      : [],
  } as PublicKeyCredentialRequestOptions;
}

function credentialDescriptor(value: unknown): PublicKeyCredentialDescriptor {
  const descriptor = isObject(value) ? value : {};
  return { ...descriptor, id: decodeBase64URL(String(descriptor.id ?? "")) } as PublicKeyCredentialDescriptor;
}

function decodeBase64URL(value: string): ArrayBuffer {
  const base64 = value.replace(/-/g, "+").replace(/_/g, "/") + "=".repeat((4 - value.length % 4) % 4);
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index);
  return bytes.buffer;
}

function encodeBase64URL(value: ArrayBuffer): string {
  const bytes = new Uint8Array(value);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function formatTime(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? "at an unknown time" : date.toLocaleString();
}

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

interface NavigatorPlatformInformation {
  platform?: string;
  userAgent?: string;
  userAgentData?: { platform?: string };
}

export function suggestedDeviceLabel(information?: NavigatorPlatformInformation): string {
  const hint = `${information?.userAgentData?.platform ?? ""} ${information?.platform ?? ""} ${information?.userAgent ?? ""}`;
  if (/Android/i.test(hint)) return "Android device";
  if (/Windows|Win32|Win64/i.test(hint)) return "Windows device";
  if (/iPad|iPhone|iPod|\biOS\b/i.test(hint)) return "iOS device";
  if (/CrOS/i.test(hint)) return "ChromeOS device";
  if (/Mac/i.test(hint)) return "Mac device";
  if (/Linux/i.test(hint)) return "Linux device";
  return "This device";
}

export class AccessController {
  readonly #actions: AccessActions;
  readonly #document: Document;
  readonly #root: HTMLElement;
  #busy = false;
  #closeDeviceSettings: (() => void) | undefined;
  #deviceHost: HTMLElement | undefined;
  #deviceLoadGeneration = 0;
  #deviceLoading = false;
  #deviceMessage: string | undefined;
  #devices: DevicesResponse | undefined;
  #invitation: InvitationResponse | undefined;

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

  renderTrust(host: HTMLElement, token: string, message?: string, currentLabel = ""): void {
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
    label.append(element(this.#document, "span", undefined, "Give this device a name"));
    const input = element(this.#document, "input");
    input.name = "device-label";
    input.maxLength = 160;
    input.required = true;
    input.autocomplete = "off";
    input.value = currentLabel || suggestedDeviceLabel(this.#document.defaultView?.navigator);
    label.append(input);
    const trust = action(this.#document, "Trust this device", () => void this.#trust(host, token, input.value), "access-primary");
    panel.append(label, trust);
    if (message) panel.append(element(this.#document, "p", "access-feedback", message));
    host.replaceChildren(panel);
  }

  async openDevices(host: HTMLElement, closeSettings: () => void): Promise<void> {
    this.#deviceHost = host;
    this.#closeDeviceSettings = closeSettings;
    this.#renderDeviceSettings();
    if (this.#devices || this.#deviceLoading) return;
    const generation = this.#deviceLoadGeneration;
    this.#deviceLoading = true;
    this.#deviceMessage = "Loading trusted sign-ins…";
    this.#renderDeviceSettings();
    try {
      const devices = await accessRequest<DevicesResponse>("/api/devices", "GET");
      if (generation !== this.#deviceLoadGeneration) return;
      this.#devices = devices;
      this.#deviceMessage = undefined;
      this.#renderDeviceSettings();
    } catch (error) {
      if (generation !== this.#deviceLoadGeneration) return;
      if (error instanceof AccessRequestError && error.status === 401) {
        this.#closeDeviceSettings?.();
        this.#actions.onSignedOut();
        return;
      }
      this.#deviceMessage = "Could not load trusted sign-ins. Try again.";
      this.#renderDeviceSettings();
    } finally {
      if (generation === this.#deviceLoadGeneration) this.#deviceLoading = false;
    }
  }

  closeDevices(): void {
    this.#deviceLoadGeneration++;
    this.#deviceLoading = false;
    this.#deviceMessage = undefined;
    this.#devices = undefined;
    this.#invitation = undefined;
    this.#deviceHost = undefined;
    this.#closeDeviceSettings = undefined;
  }

  close(): void {
    this.closeDevices();
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
    label = label.trim();
    if (!label) {
      this.renderTrust(host, token, "Enter a short label for this trusted sign-in.");
      return;
    }
    this.#busy = true;
    this.renderTrust(host, token, "Waiting for your passkey…", label);
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
      const DOMExceptionConstructor = this.#document.defaultView?.DOMException;
      if (DOMExceptionConstructor && error instanceof DOMExceptionConstructor && error.name === "InvalidStateError") {
        this.renderTrust(host, token, "Remove this device’s old Shepherdr passkey, then try again.", label);
        return;
      }
      this.renderTrust(host, token, "Passkey not created. Try again.", label);
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

  #renderDeviceSettings(): void {
    const host = this.#deviceHost;
    if (!host) return;
    if (this.#invitation) {
      this.#renderInvitation(host, this.#invitation);
      return;
    }
    const content = element(this.#document, "div", "access-devices");
    content.append(element(
      this.#document,
      "p",
      "access-scope",
      "A passkey may sync. Its copies share one trusted sign-in here and are revoked together.",
    ));
    if (this.#deviceMessage) content.append(element(this.#document, "p", "access-feedback", this.#deviceMessage));
    const devices = this.#devices;
    if (devices) {
      const list = element(this.#document, "ul", "device-list");
      for (const device of devices.devices) {
        const item = element(this.#document, "li", "device-row");
        const copy = element(this.#document, "div");
        copy.append(
          element(this.#document, "strong", undefined, trustedSignInLabel(device.label)),
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
      content.append(list);
      if (!devices.can_revoke) {
        content.append(element(
          this.#document,
          "p",
          "access-scope",
          "This is the last trusted sign-in. To clear all access, stop Shepherdr and reset it on the machine.",
        ));
      }
      content.append(actions);
    }
    host.replaceChildren(content);
  }

  async #createInvitation(devices: DevicesResponse): Promise<void> {
    if (this.#busy) return;
    this.#busy = true;
    this.#deviceMessage = "Waiting for your passkey…";
    this.#renderDeviceSettings();
    try {
      const invitation = await this.#freshMutation(() =>
        accessRequest<InvitationResponse>("/api/devices/invitations", "POST", {})
      );
      this.#busy = false;
      this.#invitation = invitation;
      this.#deviceMessage = undefined;
      this.#renderDeviceSettings();
    } catch (error) {
      this.#busy = false;
      if (error instanceof AccessRequestError && error.status === 401) {
        this.#closeDeviceSettings?.();
        this.#actions.onSignedOut();
        return;
      }
      this.#deviceMessage = "An invitation could not be created. Try again.";
      this.#renderDeviceSettings();
    }
  }

  #renderInvitation(host: HTMLElement, invitation: InvitationResponse): void {
    const content = element(this.#document, "div", "access-invitation");
    const link = element(this.#document, "code", "access-invitation-link", invitation.link);
    const qr = element(this.#document, "img", "access-qr");
    qr.src = invitation.qr;
    qr.alt = "Invitation QR code";
    qr.width = 320;
    qr.height = 320;
    content.append(
      element(this.#document, "h4", undefined, "Trust another device"),
      element(this.#document, "p", undefined, "Open this link on that device, or scan the QR code. It expires in ten minutes."),
      link,
      action(this.#document, "Copy link", () => void navigator.clipboard?.writeText(invitation.link)),
      qr,
    );
    host.replaceChildren(content);
  }

  async #revoke(device: Device, devices: DevicesResponse): Promise<void> {
    if (this.#busy || !devices.can_revoke) return;
    if (!window.confirm("Revoke " + trustedSignInLabel(device.label) + "? Its passkey copies will no longer open Shepherdr.")) return;
    this.#busy = true;
    this.#deviceMessage = "Waiting for your passkey…";
    this.#renderDeviceSettings();
    try {
      await this.#freshMutation(() => accessRequest("/api/devices/revoke", "POST", { trust_id: device.trust_id }));
      this.#busy = false;
      if (device.trust_id === devices.current_trust_id) {
        this.#closeDeviceSettings?.();
        this.#actions.onSignedOut();
        return;
      }
      this.#devices = await accessRequest<DevicesResponse>("/api/devices", "GET");
      this.#deviceMessage = "Trusted sign-in revoked.";
      this.#renderDeviceSettings();
    } catch (error) {
      this.#busy = false;
      if (error instanceof AccessRequestError && error.status === 401) {
        this.#closeDeviceSettings?.();
        this.#actions.onSignedOut();
        return;
      }
      this.#deviceMessage = "This trusted sign-in could not be revoked. Try again.";
      this.#renderDeviceSettings();
    }
  }

  async #signOut(devices: DevicesResponse): Promise<void> {
    if (this.#busy) return;
    this.#busy = true;
    try {
      await accessRequest("/api/auth/sign-out", "POST", {});
    } catch {
      this.#busy = false;
      this.#deviceMessage = "Could not sign out. Try again.";
      this.#renderDeviceSettings();
      return;
    }
    this.#busy = false;
    this.#closeDeviceSettings?.();
    this.#actions.onSignedOut();
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

function trustedSignInLabel(value: string): string {
  return value.trim() || "Trusted sign-in";
}

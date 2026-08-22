import assert from "node:assert/strict";
import test from "node:test";

import { Window } from "happy-dom";

import { AccessController, hasInvitationFragment, invitationToken } from "./access.ts";
import { HomeView } from "./home-view.ts";

const validInvitation = "A".repeat(43);

function accessActions() {
  return {
    onSessionRotated: () => undefined,
    onSessionRotating: () => undefined,
    onSignedIn: () => undefined,
    onSignedOut: () => undefined,
  };
}

function jsonResponse(value: unknown, status = 200): Response {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

test("invitation fragments accept only one exact bearer shape", () => {
  assert.equal(invitationToken(`#trust=${validInvitation}`), validInvitation);
  for (const hash of ["", `#view=home&trust=${validInvitation}`, "#trust=", "#trust=short", `#trust=${"A".repeat(44)}`, "#trust=%3Cscript%3E"]) {
    assert.equal(invitationToken(hash), undefined);
  }
  assert.equal(hasInvitationFragment("#trust=short"), true);
  assert.equal(hasInvitationFragment("#terminal=pane-one"), false);
});

test("sign-in and invitation recovery use the approved small passkey screens", () => {
  const window = new Window();
  const root = window.document.createElement("div");
  const host = window.document.createElement("main");
  window.document.body.append(root, host);
  const controller = new AccessController(root, accessActions());

  controller.renderSignIn(host);
  assert.equal(host.querySelector("h1")?.textContent, "Sign in");
  assert.equal(host.querySelector("button")?.textContent, "Sign in with a passkey");
  assert.equal(host.textContent?.includes("email"), false);
  assert.equal(host.textContent?.includes("account"), false);

  controller.renderTrust(host, validInvitation, "This invitation can't be used. Create a new invitation on the machine running Shepherdr or from a trusted device.");
  assert.equal(host.querySelector("h1")?.textContent, "Trust this device");
  assert.equal(host.querySelector("input")?.getAttribute("maxlength"), "160");
  assert.equal(
    host.querySelector(".access-feedback")?.textContent,
    "This invitation can't be used. Create a new invitation on the machine running Shepherdr or from a trusted device.",
  );
});

test("Devices confines hostile labels and omits Revoke for the final trusted sign-in", async () => {
  const window = new Window();
  const root = window.document.createElement("div");
  window.document.body.append(root);
  const controller = new AccessController(root, accessActions());
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => jsonResponse({
    can_revoke: false,
    current_trust_id: "one",
    devices: [{
      backup_eligible: true,
      backup_observed_at: "2026-08-22T10:00:00Z",
      backup_state: true,
      created_at: "2026-08-22T09:00:00Z",
      label: "Phone <img src=x onerror=alert(1)>",
      last_used_at: "2026-08-22T10:00:00Z",
      trust_id: "one",
    }],
  });
  try {
    await controller.openDevices();
  } finally {
    globalThis.fetch = originalFetch;
  }

  assert.equal(root.querySelector("h2")?.textContent, "Devices");
  assert.equal(root.querySelector(".device-row strong")?.textContent, "Phone <img src=x onerror=alert(1)>");
  assert.equal(root.querySelectorAll("img").length, 0);
  assert.equal(Array.from(root.querySelectorAll("button")).some((button) => button.textContent === "Revoke"), false);
  assert.equal(root.textContent?.includes("A passkey may sync."), true);
  assert.equal(root.textContent?.includes("last reported by this passkey"), true);
});

test("Devices offers Revoke only when another trusted sign-in remains", async () => {
  const window = new Window();
  const root = window.document.createElement("div");
  window.document.body.append(root);
  const controller = new AccessController(root, accessActions());
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => jsonResponse({
    can_revoke: true,
    current_trust_id: "one",
    devices: ["one", "two"].map((trustID) => ({
      backup_eligible: false,
      backup_observed_at: "2026-08-22T10:00:00Z",
      backup_state: false,
      created_at: "2026-08-22T09:00:00Z",
      label: `Trusted ${trustID}`,
      last_used_at: "2026-08-22T10:00:00Z",
      trust_id: trustID,
    })),
  });
  try {
    await controller.openDevices();
  } finally {
    globalThis.fetch = originalFetch;
  }
  const revokeCount = Array.from(root.querySelectorAll("button")).filter((button) => button.textContent === "Revoke").length;
  assert.equal(revokeCount, 2);
  assert.equal(Array.from(root.querySelectorAll("button")).some((button) => button.textContent === "Sign out"), true);
  assert.equal(Array.from(root.querySelectorAll("button")).some((button) => button.textContent === "Trust another device"), true);
});

test("access probe distinguishes protected, signed-out, and sign-in-off modes", async () => {
  const window = new Window();
  const root = window.document.createElement("div");
  const controller = new AccessController(root, accessActions());
  const statuses = [200, 401, 404, 503];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => jsonResponse({}, statuses.shift() ?? 503);
  try {
    assert.equal(await controller.probe(), "protected");
    assert.equal(await controller.probe(), "signed-out");
    assert.equal(await controller.probe(), "sign-in-off");
    assert.equal(await controller.probe(), "unavailable");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("Home quietly distinguishes sign-in-off from protected Devices", () => {
  const window = new Window();
  const app = window.document.createElement("main");
  window.document.body.append(app);
  const view = new HomeView(app, {
    isHomeActive: () => true,
    onFocusPane: () => undefined,
    onOpen: () => undefined,
    onReconnect: () => undefined,
    onShowAll: () => undefined,
    onShowBlocked: () => undefined,
    prepareWorkspaceAction: async () => ({ outcome: "refused", reason: "not_applicable" }),
    runWorkspaceAction: async () => ({ outcome: "succeeded" }),
  });
  const model = {
    actionsAvailable: true,
    mode: "all" as const,
    reachability: "current" as const,
    state: {
      connection: "live" as const,
      gap: 0,
      has_home: true,
      home: { blocked_count: 0, working_count: 0, workspaces: [] },
      last_known: false,
    },
  };

  view.render({ ...model, signInOff: true });
  assert.equal(app.querySelector(".quiet")?.textContent, "Sign-in is off.");
  assert.equal((app.querySelector(".quiet") as HTMLElement | null)?.hidden, false);
  assert.equal(app.querySelector(".home-devices"), null);

  view.render({ ...model, signInOff: false });
  assert.equal((app.querySelector(".quiet") as HTMLElement | null)?.hidden, true);
  assert.equal((app.querySelector(".home-devices") as HTMLElement | null)?.hidden, false);
  assert.equal(app.querySelector(".home-devices")?.textContent, "Devices");
  assert.equal(app.querySelectorAll(".settings-action").length, 1);
});

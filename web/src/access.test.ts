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

test("sign-in, trust labeling, and unusable invitations use the approved small screens", () => {
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

  controller.renderTrust(host, validInvitation);
  assert.equal(host.querySelector("h1")?.textContent, "Trust this device");
  assert.equal(host.querySelector("input")?.getAttribute("maxlength"), "160");
  assert.equal(host.querySelector("input")?.hasAttribute("required"), true);
  assert.equal(host.querySelector<HTMLInputElement>("input")?.value, "");
  assert.equal(host.querySelector(".access-field > span")?.textContent, "Short label");
  assert.equal(host.querySelector(".access-field-help")?.textContent, "Use a name you will recognize in Devices. Not an account.");

  controller.renderTrust(host, "");
  assert.equal(
    host.textContent,
    "This invitation can't be used. Create a new invitation on the machine running Shepherdr or from a trusted device.",
  );
  assert.equal(
    host.querySelector(".access-feedback")?.textContent,
    "This invitation can't be used. Create a new invitation on the machine running Shepherdr or from a trusted device.",
  );
  assert.equal(host.querySelectorAll("input").length, 0);
  assert.equal(host.querySelectorAll("button").length, 0);
  assert.equal(host.textContent?.includes("Create a passkey to trust this browser."), false);
});

test("Trust keeps local passkey cancellation retryable but removes a server-rejected invitation", async (t) => {
  await t.test("local cancellation", async () => {
    const window = new Window();
    const host = window.document.createElement("main");
    window.document.body.append(host);
    const controller = new AccessController(host, accessActions());
    const originalFetch = globalThis.fetch;
    const originalNavigator = Object.getOwnPropertyDescriptor(globalThis, "navigator");
    let requests = 0;
    globalThis.fetch = async () => {
      requests++;
      return jsonResponse({ publicKey: { challenge: "", user: { id: "" } } });
    };
    Object.defineProperty(globalThis, "navigator", {
      configurable: true,
      value: { credentials: { create: async () => { throw new Error("canceled"); } } },
    });
    try {
      controller.renderTrust(host, validInvitation);
      const label = host.querySelector<HTMLInputElement>('input[name="device-label"]');
      if (label) label.value = "Personal phone";
      const trust = Array.from(host.querySelectorAll("button")).find((button) => button.textContent === "Trust this device");
      trust?.click();
      await new Promise((resolve) => setTimeout(resolve, 0));
    } finally {
      globalThis.fetch = originalFetch;
      if (originalNavigator) {
        Object.defineProperty(globalThis, "navigator", originalNavigator);
      } else {
        Reflect.deleteProperty(globalThis, "navigator");
      }
    }
    assert.equal(requests, 1);
    assert.equal(host.querySelector(".access-feedback")?.textContent, "Passkey not created. Try again.");
    assert.equal(host.querySelectorAll("input").length, 1);
    assert.equal(Array.from(host.querySelectorAll("button")).some((button) => button.textContent === "Trust this device"), true);
  });

  await t.test("server invitation rejection", async () => {
    const window = new Window();
    const host = window.document.createElement("main");
    window.document.body.append(host);
    const controller = new AccessController(host, accessActions());
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async () => jsonResponse({
      error: "This invitation can't be used. Create a new invitation on the machine running Shepherdr or from a trusted device.",
    }, 400);
    try {
      controller.renderTrust(host, validInvitation);
      const label = host.querySelector<HTMLInputElement>('input[name="device-label"]');
      if (label) label.value = "Personal phone";
      const trust = Array.from(host.querySelectorAll("button")).find((button) => button.textContent === "Trust this device");
      trust?.click();
      await new Promise((resolve) => setTimeout(resolve, 0));
    } finally {
      globalThis.fetch = originalFetch;
    }
    assert.equal(
      host.querySelector(".access-feedback")?.textContent,
      "This invitation can't be used. Create a new invitation on the machine running Shepherdr or from a trusted device.",
    );
    assert.equal(host.querySelectorAll("input").length, 0);
    assert.equal(host.querySelectorAll("button").length, 0);
  });
});

test("Trust requires a human label before beginning the passkey ceremony", async () => {
  const window = new Window();
  const host = window.document.createElement("main");
  window.document.body.append(host);
  const controller = new AccessController(host, accessActions());
  const originalFetch = globalThis.fetch;
  let requests = 0;
  globalThis.fetch = async () => {
    requests++;
    return jsonResponse({});
  };
  try {
    controller.renderTrust(host, validInvitation);
    buttonWithText(host, "Trust this device").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
  } finally {
    globalThis.fetch = originalFetch;
  }
  assert.equal(requests, 0);
  assert.equal(host.querySelector(".access-feedback")?.textContent, "Enter a short label for this trusted sign-in.");
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
  assert.equal(
    root.textContent?.includes("This is the last trusted sign-in. To clear all access, stop Shepherdr and reset it on the machine."),
    true,
  );
  const backupObservation = Array.from(root.querySelectorAll(".device-row span"))
    .map((node) => node.textContent ?? "")
    .find((text) => text.startsWith("Last reported by this passkey at "));
  assert.equal(backupObservation?.endsWith(": backup"), true);
  assert.equal(backupObservation?.includes("Backup reported"), false);
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
  const backupObservations = Array.from(root.querySelectorAll(".device-row span"))
    .map((node) => node.textContent ?? "")
    .filter((text) => text.startsWith("Last reported by this passkey at "));
  assert.equal(backupObservations.length, 2);
  assert.equal(backupObservations.every((text) => text.endsWith(": no backup")), true);
  assert.equal(root.textContent?.includes("This is the last trusted sign-in."), false);
});

test("browser invitations render a square image QR", async () => {
  const window = new Window();
  const root = window.document.createElement("div");
  window.document.body.append(root);
  const controller = new AccessController(root, accessActions());
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (_input, init) => init?.method === "POST"
    ? jsonResponse({
      expires_at: "2026-08-22T10:10:00Z",
      link: `https://shepherdr.example/#trust=${validInvitation}`,
      qr: "data:image/png;base64,iVBORw0KGgo=",
    })
    : jsonResponse({
      can_revoke: false,
      current_trust_id: "one",
      devices: [{
        backup_eligible: false,
        backup_observed_at: "2026-08-22T10:00:00Z",
        backup_state: false,
        created_at: "2026-08-22T09:00:00Z",
        label: "Personal phone",
        last_used_at: "2026-08-22T10:00:00Z",
        trust_id: "one",
      }],
    });
  try {
    await controller.openDevices();
    buttonWithText(root, "Trust another device").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
  } finally {
    globalThis.fetch = originalFetch;
  }
  const qr = root.querySelector<HTMLImageElement>("img.access-qr");
  assert.equal(qr?.alt, "Invitation QR code");
  assert.equal(qr?.width, 320);
  assert.equal(qr?.height, 320);
  assert.equal(qr?.src.startsWith("data:image/png;base64,"), true);
});

test("Sign out stays truthful on failure and closes only after confirmation", async (t) => {
  for (const failure of ["transport", "server"] as const) {
    await t.test(failure, async () => {
      const window = new Window();
      const root = window.document.createElement("div");
      window.document.body.append(root);
      let signedOut = 0;
      let postAttempted = false;
      const controller = new AccessController(root, {
        ...accessActions(),
        onSignedOut: () => signedOut++,
      });
      const originalFetch = globalThis.fetch;
      globalThis.fetch = async (_input, init) => {
        if (init?.method === "POST") {
          postAttempted = true;
          if (failure === "transport") throw new Error("offline");
          return jsonResponse({ error: "unavailable" }, 503);
        }
        return jsonResponse({
          can_revoke: false,
          current_trust_id: "one",
          devices: [{
            backup_eligible: false,
            backup_observed_at: "2026-08-22T10:00:00Z",
            backup_state: false,
            created_at: "2026-08-22T09:00:00Z",
            label: "Phone",
            last_used_at: "2026-08-22T10:00:00Z",
            trust_id: "one",
          }],
        });
      };
      try {
        await controller.openDevices();
        const signOut = Array.from(root.querySelectorAll("button")).find((button) => button.textContent === "Sign out");
        signOut?.click();
        await new Promise((resolve) => setTimeout(resolve, 0));
      } finally {
        globalThis.fetch = originalFetch;
      }
      assert.equal(postAttempted, true);
      assert.equal(signedOut, 0);
      assert.equal(root.querySelector("h2")?.textContent, "Devices");
      assert.equal(root.querySelector(".access-feedback")?.textContent, "Could not sign out. Try again.");
      assert.equal(Array.from(root.querySelectorAll("button")).some((button) => button.textContent === "Sign out"), true);
    });
  }

  await t.test("confirmed", async () => {
    const window = new Window();
    const root = window.document.createElement("div");
    window.document.body.append(root);
    let signedOut = 0;
    const controller = new AccessController(root, {
      ...accessActions(),
      onSignedOut: () => signedOut++,
    });
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async (_input, init) => init?.method === "POST"
      ? jsonResponse({ signed_out: true })
      : jsonResponse({
        can_revoke: false,
        current_trust_id: "one",
        devices: [{
          backup_eligible: false,
          backup_observed_at: "2026-08-22T10:00:00Z",
          backup_state: false,
          created_at: "2026-08-22T09:00:00Z",
          label: "Phone",
          last_used_at: "2026-08-22T10:00:00Z",
          trust_id: "one",
        }],
      });
    try {
      await controller.openDevices();
      const signOut = Array.from(root.querySelectorAll("button")).find((button) => button.textContent === "Sign out");
      signOut?.click();
      await new Promise((resolve) => setTimeout(resolve, 0));
    } finally {
      globalThis.fetch = originalFetch;
    }
    assert.equal(signedOut, 1);
    assert.equal(root.childElementCount, 0);
  });
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

test("Home keeps one Settings action and no separate Devices header action", () => {
  const window = new Window();
  const app = window.document.createElement("main");
  window.document.body.append(app);
  const view = new HomeView(app, {
    isHomeActive: () => true,
    onFocusPane: () => undefined,
    onOpen: () => undefined,
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
  assert.equal(app.querySelectorAll(".settings-action").length, 1);

  view.render({ ...model, signInOff: false });
  assert.equal((app.querySelector(".quiet") as HTMLElement | null)?.hidden, true);
  assert.equal(app.querySelector(".home-devices"), null);
  assert.equal(app.querySelectorAll(".settings-action").length, 1);
});

function buttonWithText(root: ParentNode, text: string): HTMLButtonElement {
  const button = Array.from(root.querySelectorAll<HTMLButtonElement>("button"))
    .find((candidate) => candidate.textContent === text);
  if (!button) throw new Error(`missing button ${text}`);
  return button;
}

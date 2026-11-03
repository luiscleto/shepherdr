"use strict";

self.addEventListener("install", (event) => {
  event.waitUntil(self.skipWaiting());
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener("push", (event) => {
  let payload;
  try {
    payload = event.data?.json();
  } catch {
    payload = undefined;
  }
  const notice = notificationFor(payload);
  event.waitUntil(self.registration.showNotification(notice.title, {
    body: notice.body,
    data: { destination: notice.destination },
    icon: "/icon-192.png",
    badge: "/icon-192.png",
  }));
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const destination = safeDestination(event.notification.data?.destination);
  const absolute = new URL(destination, self.location.origin).href;
  event.waitUntil(self.clients.openWindow(absolute));
});

function notificationFor(value) {
  if (!value || typeof value !== "object") return fallbackNotice();
  if ((value.kind === "workspace_opened" || value.kind === "workspace_closed") && value.destination !== "/") {
    return fallbackNotice();
  }
  const destination = safeDestination(value.destination);
  const workspaceName = usableWorkspaceName(value.workspace_name);
  // For Herdr Core, Shepherdr says space, as the Herdr app does.
  const herdr = value.herdr === "core" ? "core" : "tui";
  const core = herdr === "core";
  const space = core ? "space" : "workspace";
  const Space = core ? "Space" : "Workspace";
  if (value.kind === "trusted_sign_in_added" || value.kind === "trusted_sign_in_removed") {
    if (value.destination !== "/") return fallbackNotice();
    const label = usableTrustLabel(value.trust_label);
    const added = value.kind === "trusted_sign_in_added";
    return {
      title: added ? "Trusted sign-in added" : "Trusted sign-in removed",
      body: label ? `Label: ${label}` : `A trusted sign-in was ${added ? "added" : "removed"}.`,
      destination: "/",
    };
  }
  if (value.kind === "workspace_opened") {
    return workspaceName
      ? { title: `${Space} opened`, body: workspaceName, destination: "/" }
      : { title: "Shepherdr", body: `A ${space} opened.`, destination: "/" };
  }
  if (value.kind === "workspace_closed") {
    return workspaceName
      ? { title: `${Space} closed`, body: workspaceName, destination: "/" }
      : { title: "Shepherdr", body: `A ${space} closed.`, destination: "/" };
  }
  if (value.kind !== "status" || !validOpaqueID(value.pane_id) || !validOpaqueID(value.terminal_id)) {
    return fallbackNotice();
  }
  if (!statusDestinationMatches(destination, value.pane_id, value.terminal_id, herdr)) return fallbackNotice();
  // A Herdr app terminal under Other terminals is in no space.
  const place = core && value.other_terminals === true ? "terminal" : space;
  const bodies = {
    blocked: `A ${place} needs attention.`,
    done: `A ${place} finished.`,
    idle: `A ${place} is idle.`,
    unknown: `A ${place} status changed.`,
    working: `A ${place} is working.`,
  };
  if (core) bodies.error = bodies.blocked;
  if (!Object.prototype.hasOwnProperty.call(bodies, value.status)) return fallbackNotice();
  const titles = {
    blocked: "Agent is blocked",
    error: "Agent status is error",
    done: "Agent is done",
    idle: "Agent is idle",
    unknown: "Agent status is unknown",
    working: "Agent is working",
  };
  if (!workspaceName) return { title: "Shepherdr", body: bodies[value.status], destination };
  // A name the person gave the terminal or its tab, when there is one.
  const name = usableGivenName(value.name);
  return { title: name ? `${name} is ${value.status}` : titles[value.status], body: workspaceName, destination };
}

function usableWorkspaceName(value) {
  if (typeof value !== "string") return "";
  const name = value.trim();
  return name && Array.from(name).length <= 160 ? name : "";
}

// Shepherdr sends a given name already cleaned and at most 60 characters.
// Anything else is not shown.
function usableGivenName(value) {
  if (typeof value !== "string") return "";
  const name = value.trim();
  return name && Array.from(name).length <= 60 && !/[\u0000-\u001f\u007f-\u009f]/u.test(name) ? name : "";
}

function usableTrustLabel(value) {
  if (typeof value !== "string") return "";
  const label = value.trim();
  return label && Array.from(label).length <= 160 ? label : "";
}

function fallbackNotice() {
  return { title: "Shepherdr", body: "A workspace changed.", destination: "/" };
}

function validOpaqueID(value) {
  return typeof value === "string" && value.length > 0 && value.length <= 512;
}

// A link or payload without herdr is from the Herdr TUI.
function statusDestinationMatches(destination, paneID, terminalID, herdr) {
  try {
    const parsed = new URL(destination, self.location.origin);
    const parameters = new URLSearchParams(parsed.hash.slice(1));
    return parsed.pathname === "/" && parameters.get("terminal") === paneID &&
      parameters.get("terminal_id") === terminalID && (parameters.get("herdr") ?? "tui") === herdr;
  } catch {
    return false;
  }
}

function safeDestination(value) {
  if (typeof value !== "string" || value.length > 1200 || !value.startsWith("/")) return "/";
  try {
    const parsed = new URL(value, self.location.origin);
    if (parsed.origin !== self.location.origin || parsed.pathname !== "/") return "/";
    return parsed.pathname + parsed.search + parsed.hash;
  } catch {
    return "/";
  }
}

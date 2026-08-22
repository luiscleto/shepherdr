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
  const destination = safeDestination(value.destination);
  const workspaceName = usableWorkspaceName(value.workspace_name);
  if (value.kind === "workspace_opened") {
    return workspaceName
      ? { title: "Workspace opened", body: workspaceName, destination: "/" }
      : { title: "Shepherdr", body: "A workspace opened.", destination: "/" };
  }
  if (value.kind === "workspace_closed") {
    return workspaceName
      ? { title: "Workspace closed", body: workspaceName, destination: "/" }
      : { title: "Shepherdr", body: "A workspace closed.", destination: "/" };
  }
  if (value.kind !== "status" || !validOpaqueID(value.pane_id) || !validOpaqueID(value.terminal_id)) {
    return fallbackNotice();
  }
  if (!statusDestinationMatches(destination, value.pane_id, value.terminal_id)) return fallbackNotice();
  const bodies = {
    blocked: "A workspace needs attention.",
    done: "A workspace finished.",
    idle: "A workspace is idle.",
    unknown: "A workspace status changed.",
    working: "A workspace is working.",
  };
  if (!Object.prototype.hasOwnProperty.call(bodies, value.status)) return fallbackNotice();
  const titles = {
    blocked: "Agent is blocked",
    done: "Agent is done",
    idle: "Agent is idle",
    unknown: "Agent status is unknown",
    working: "Agent is working",
  };
  return workspaceName
    ? { title: titles[value.status], body: `Workspace: ${workspaceName}`, destination }
    : { title: "Shepherdr", body: bodies[value.status], destination };
}

function usableWorkspaceName(value) {
  if (typeof value !== "string") return "";
  const name = value.trim();
  return name && Array.from(name).length <= 160 ? name : "";
}

function fallbackNotice() {
  return { title: "Shepherdr", body: "A workspace changed.", destination: "/" };
}

function validOpaqueID(value) {
  return typeof value === "string" && value.length > 0 && value.length <= 512;
}

function statusDestinationMatches(destination, paneID, terminalID) {
  try {
    const parsed = new URL(destination, self.location.origin);
    const parameters = new URLSearchParams(parsed.hash.slice(1));
    return parsed.pathname === "/" && parameters.get("terminal") === paneID &&
      parameters.get("terminal_id") === terminalID;
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

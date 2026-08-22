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
  event.waitUntil(self.registration.showNotification("Shepherdr", {
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
  event.waitUntil((async () => {
    const windows = await self.clients.matchAll({ includeUncontrolled: true, type: "window" });
    for (const client of windows) {
      if (new URL(client.url).origin !== self.location.origin) continue;
      await client.navigate(absolute);
      return client.focus();
    }
    return self.clients.openWindow(absolute);
  })());
});

function notificationFor(value) {
  if (!value || typeof value !== "object") return fallbackNotice();
  const destination = safeDestination(value.destination);
  const workspaceName = usableWorkspaceName(value.workspace_name);
  if (value.kind === "workspace_opened") {
    return { body: workspaceName ? `${workspaceName} opened.` : "A workspace opened.", destination: "/" };
  }
  if (value.kind === "workspace_closed") {
    return { body: workspaceName ? `${workspaceName} closed.` : "A workspace closed.", destination: "/" };
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
  const namedBodies = {
    blocked: "needs attention.",
    done: "finished.",
    idle: "is idle.",
    unknown: "status changed.",
    working: "is working.",
  };
  const body = workspaceName ? `${workspaceName} ${namedBodies[value.status]}` : bodies[value.status];
  return { body, destination };
}

function usableWorkspaceName(value) {
  if (typeof value !== "string") return "";
  const name = value.trim();
  return name && Array.from(name).length <= 160 ? name : "";
}

function fallbackNotice() {
  return { body: "A workspace changed.", destination: "/" };
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

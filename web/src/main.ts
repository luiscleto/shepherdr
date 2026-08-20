import "./style.css";
import { HomeView } from "./home-view";
import {
  homeReachability as deriveHomeReachability,
  nextHomeCheckDelay,
  type HomeReachability,
} from "./home-connection";
import { allTerminals, findTerminal, type HomeState, type TerminalEntry } from "./home-model";
import { TerminalPage } from "./terminal-page";
import { returningToHome } from "./terminal-route";

interface HomeHeartbeat {
  epoch: string;
  type: "home.heartbeat";
}

const serverEpochPattern = /^[0-9a-f]{32}$/;

function isHomeHeartbeat(message: unknown): message is HomeHeartbeat {
  return typeof message === "object" && message !== null &&
    "type" in message && message.type === "home.heartbeat" &&
    "epoch" in message && typeof message.epoch === "string" && serverEpochPattern.test(message.epoch);
}

function isCompleteHomeState(message: unknown): message is HomeState {
  if (typeof message !== "object" || message === null) return false;
  const value = message as Record<string, unknown>;
  const home = value.home;
  return typeof value.epoch === "string" && serverEpochPattern.test(value.epoch) &&
    ["reconnecting", "live", "not_running", "incompatible"].includes(String(value.connection)) &&
    Number.isSafeInteger(value.gap) && typeof value.last_known === "boolean" &&
    typeof home === "object" && home !== null &&
    Number.isSafeInteger((home as Record<string, unknown>).blocked_count) &&
    Number.isSafeInteger((home as Record<string, unknown>).working_count) &&
    Array.isArray((home as Record<string, unknown>).workspaces);
}

const appNode = document.querySelector<HTMLElement>("#app");
if (!appNode) throw new Error("Application root is missing");
const app: HTMLElement = appNode;

let state: HomeState = {
  connection: "reconnecting",
  gap: 0,
  home: { blocked_count: 0, working_count: 0, workspaces: [] },
  last_known: false,
};
let homeSocket: WebSocket | undefined;
let homeTimer: number | undefined;
let lastValidHomeFrameAt = performance.now();
let selectedTerminal: TerminalEntry | undefined;
let homeScroll = 0;
let homeFocusPane: string | undefined;
let homeAnchorTop: number | undefined;
let homeHadListFocus = false;
let lastFocusedHomePane: string | undefined;
let allTerminalsScroll = 0;
let allTerminalsFocusPane: string | undefined;
let homeMode: "all" | "blocked" = "all";
let restoreHomePlace = false;
let hasConnectedHome = false;
let publishedStateSignature = "";
let renderedTerminalPane: string | undefined;
let terminalPage: TerminalPage | undefined;
const homeView = new HomeView(app, {
  onFocusPane: (paneID) => {
    lastFocusedHomePane = paneID;
  },
  onOpen: openTerminal,
  onReconnect: reconnectHome,
  onShowAll: showAllTerminals,
  onShowBlocked: showBlockedTerminals,
});

function element<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
  text?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function button(text: string, action: () => void): HTMLButtonElement {
  const node = element("button", undefined, text);
  node.type = "button";
  node.addEventListener("click", action);
  return node;
}

function webSocketURL(endpoint: string): URL {
  const url = new URL(endpoint, window.location.href);
  url.protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  return url;
}

function connectHome(): void {
  if (homeReachability() === "offline") return;
  if (homeSocketActive()) {
    scheduleHomeCheck();
    return;
  }
  const socket = new WebSocket(webSocketURL("/api/home"));
  homeSocket = socket;
  socket.addEventListener("open", () => {
    if (homeSocket !== socket) return;
    scheduleHomeCheck();
  });
  socket.addEventListener("message", (event) => {
    if (homeSocket !== socket) return;
    try {
      const raw = String(event.data);
      const parsed: unknown = JSON.parse(raw);
      const now = performance.now();
      const wasCurrent = homeEvidenceCurrent();
      if (isHomeHeartbeat(parsed)) {
        if (!wasCurrent && state.connection === "live") {
          state = { ...state, connection: "reconnecting", last_known: true };
          render();
        }
        lastValidHomeFrameAt = now;
        scheduleHomeCheck();
        return;
      }
      if (!isCompleteHomeState(parsed)) throw new Error("invalid Home frame");
      lastValidHomeFrameAt = now;
      const next = parsed;
      const changed = raw !== publishedStateSignature ||
        state.connection !== next.connection || state.last_known !== next.last_known;
      state = next;
      publishedStateSignature = raw;
      hasConnectedHome = true;
      if (changed || !wasCurrent) render();
      scheduleHomeCheck();
    } catch {
      socket.close();
    }
  });
  socket.addEventListener("close", () => {
    if (homeSocket !== socket) return;
    homeSocket = undefined;
    scheduleHomeCheck();
  });
  scheduleHomeCheck();
}

function liveActionsAvailable(): boolean {
  return homeEvidenceCurrent() && state.connection === "live" && !state.last_known;
}

function homeReachability(): HomeReachability {
  return deriveHomeReachability(lastValidHomeFrameAt, performance.now());
}

function homeEvidenceCurrent(): boolean {
  return homeReachability() === "current";
}

function homeSocketActive(): boolean {
  return homeSocket?.readyState === WebSocket.OPEN || homeSocket?.readyState === WebSocket.CONNECTING;
}

function reconnectHome(): void {
  if (homeReachability() !== "offline") return;
  lastValidHomeFrameAt = performance.now();
  state = { ...state, connection: "reconnecting", last_known: hasConnectedHome || state.last_known };
  publishedStateSignature = "";
  render();
  connectHome();
}

function scheduleHomeCheck(): void {
  window.clearTimeout(homeTimer);
  homeTimer = undefined;
  const now = performance.now();
  const delay = nextHomeCheckDelay(lastValidHomeFrameAt, now, homeSocketActive());
  homeTimer = window.setTimeout(checkHomeConnection, delay);
}

function checkHomeConnection(): void {
  homeTimer = undefined;
  const now = performance.now();
  const reachability = deriveHomeReachability(lastValidHomeFrameAt, now);
  if (reachability === "offline") {
    const stale = homeSocket;
    homeSocket = undefined;
    stale?.close();
    render();
    return;
  }
  if (reachability === "reconnecting" && state.connection === "live") {
    state = { ...state, connection: "reconnecting", last_known: true };
    render();
  }
  if (reachability === "reconnecting" && homeSocketActive()) {
    const stale = homeSocket;
    homeSocket = undefined;
    stale?.close();
  }
  if (!homeSocketActive()) {
    connectHome();
    return;
  }
  scheduleHomeCheck();
}

function terminalPaneFromHash(): string | undefined {
  if (!window.location.hash.startsWith("#terminal=")) return undefined;
  try {
    return decodeURIComponent(window.location.hash.slice("#terminal=".length));
  } catch {
    return undefined;
  }
}

function render(): void {
  const paneID = terminalPaneFromHash();
  if (returningToHome(renderedTerminalPane, paneID)) restoreHomePlace = true;
  renderedTerminalPane = paneID;
  if (paneID) {
    renderTerminal(paneID);
  } else {
    renderHome();
  }
}

function renderHome(): void {
  terminalPage?.destroy();
  terminalPage = undefined;
  if (homeMode === "blocked" && state.home.blocked_count === 0) homeMode = "all";
  document.body.classList.remove("terminal-active");
  homeView.render({
    actionsAvailable: liveActionsAvailable(),
    mode: homeMode,
    reachability: homeReachability(),
    receivedHome: hasConnectedHome,
    restore: restoreHomePlace
      ? {
          anchorTop: homeAnchorTop,
          focusPane: homeHadListFocus ? homeFocusPane : undefined,
          pane: homeFocusPane,
          scroll: homeScroll,
        }
      : undefined,
    state,
  });
  homeFocusPane = undefined;
  homeAnchorTop = undefined;
  homeHadListFocus = false;
  restoreHomePlace = false;
}

function currentFocusedPane(): string | undefined {
  return document.activeElement instanceof HTMLElement ? document.activeElement.dataset.paneId : undefined;
}

function showBlockedTerminals(): void {
  allTerminalsScroll = window.scrollY;
  allTerminalsFocusPane = currentFocusedPane() ?? lastFocusedHomePane;
  const firstBlocked = allTerminals(state.home).find(({ terminal }) => terminal.agent?.status === "blocked");
  homeMode = "blocked";
  homeScroll = 0;
  homeFocusPane = firstBlocked?.terminal.pane_id;
  restoreHomePlace = true;
  renderHome();
}

function showAllTerminals(): void {
  homeMode = "all";
  homeScroll = allTerminalsScroll;
  homeFocusPane = allTerminalsFocusPane;
  restoreHomePlace = true;
  renderHome();
}

function openTerminal(entry: TerminalEntry): void {
  if (!liveActionsAvailable()) return;
  homeScroll = window.scrollY;
  homeFocusPane = entry.terminal.pane_id;
  homeAnchorTop = homeView.row(entry.terminal.pane_id)?.getBoundingClientRect().top;
  homeHadListFocus = currentFocusedPane() === entry.terminal.pane_id;
  selectedTerminal = entry;
  window.location.hash = `terminal=${encodeURIComponent(entry.terminal.pane_id)}`;
}

function renderTerminal(paneID: string): void {
  document.body.classList.add("terminal-active");
  window.scrollTo(0, 0);
  const current = findTerminal(state.home, paneID);
  const selected = selectedTerminal?.terminal.pane_id === paneID ? selectedTerminal : undefined;
  if (current && selected && current.terminal.terminal_id !== selected.terminal.terminal_id) {
    renderTerminalUnavailable();
    return;
  }
  if (!current && state.connection === "live" && !state.last_known) {
    renderTerminalUnavailable();
    return;
  }
  const entry = current ?? selected;
  if (!entry) {
    renderTerminalWaiting();
    return;
  }
  selectedTerminal = entry;
  if (terminalPage?.paneID === paneID && terminalPage.terminalID === entry.terminal.terminal_id) {
    terminalPage.updateTarget(entry.terminal.title, entry.terminal.agent?.status);
    return;
  }
  terminalPage?.destroy();
  terminalPage = new TerminalPage(app, {
    agentStatus: entry.terminal.agent?.status,
    paneID,
    terminalID: entry.terminal.terminal_id,
    title: entry.terminal.title,
  }, { onHome: leaveTerminal });
}

function renderTerminalWaiting(): void {
  terminalPage?.destroy();
  terminalPage = undefined;
  app.className = "terminal-state-screen";
  const panel = element("section", "state-panel terminal-unavailable");
  panel.append(
    element("strong", undefined, "Connecting"),
    element("p", undefined, "Waiting for this terminal."),
    button("Home", leaveTerminal),
  );
  app.replaceChildren(panel);
}

function renderTerminalUnavailable(): void {
  terminalPage?.destroy();
  terminalPage = undefined;
  app.className = "terminal-state-screen";
  const panel = element("section", "state-panel terminal-unavailable");
  panel.append(
    element("strong", undefined, "Terminal unavailable"),
    element("p", undefined, "This terminal is no longer here."),
    button("Home", leaveTerminal),
  );
  app.replaceChildren(panel);
}

function leaveTerminal(): void {
  terminalPage?.destroy();
  terminalPage = undefined;
  selectedTerminal = undefined;
  restoreHomePlace = true;
  window.location.hash = "";
}

window.addEventListener("hashchange", render);
window.addEventListener("online", connectHome);

connectHome();
render();

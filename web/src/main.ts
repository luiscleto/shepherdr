import "./style.css";
import { HomeView } from "./home-view";
import {
  homeReachability as deriveHomeReachability,
  nextHomeCheckDelay,
  type HomeReachability,
} from "./home-connection";
import {
  allTerminals,
  automaticAllTerminalsPlace,
  findTerminal,
  type HomePlace,
  type HomeState,
  type TerminalEntry,
} from "./home-model";
import { parseCompleteHomeState } from "./home-parser";
import { TerminalPage } from "./terminal-page";
import { exactTerminalMatches, parseTerminalRoute, returningToHome } from "./terminal-route";
import { WorkspaceActionsClient } from "./workspace-actions";
import { NotificationsController } from "./notifications";

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

const appNode = document.querySelector<HTMLElement>("#app");
if (!appNode) throw new Error("Application root is missing");
const app: HTMLElement = appNode;
const notificationsNode = document.querySelector<HTMLElement>("#notifications");
if (!notificationsNode) throw new Error("Notifications root is missing");
const notifications = new NotificationsController(notificationsNode);
const workspaceActions = new WorkspaceActionsClient();

let state: HomeState = {
  connection: "reconnecting",
  gap: 0,
  has_home: false,
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
let publishedStateSignature = "";
let renderedTerminalPane: string | undefined;
let terminalPage: TerminalPage | undefined;
const homeView = new HomeView(app, {
  isHomeActive: () => !terminalPaneFromHash(),
  onFocusPane: (paneID) => {
    lastFocusedHomePane = paneID;
  },
  onOpen: openTerminal,
  onNotifications: () => notifications.openSettings(),
  onReconnect: reconnectHome,
  onShowAll: showAllTerminals,
  onShowBlocked: showBlockedTerminals,
  prepareWorkspaceAction: (request) => workspaceActions.prepare(request),
  runWorkspaceAction: (request) => workspaceActions.run(request),
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
        lastValidHomeFrameAt = now;
        if (!wasCurrent) render();
        scheduleHomeCheck();
        return;
      }
      const completeState = parseCompleteHomeState(parsed);
      if (!completeState) throw new Error("invalid Home frame");
      lastValidHomeFrameAt = now;
      const next = completeState;
      const changed = raw !== publishedStateSignature ||
        state.connection !== next.connection || state.last_known !== next.last_known;
      state = next;
      publishedStateSignature = raw;
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
  return state.has_home && homeEvidenceCurrent() && state.connection === "live" && !state.last_known;
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
  state = { ...state, connection: "reconnecting", last_known: state.has_home };
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
  if (reachability === "reconnecting") render();
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
  return parseTerminalRoute(window.location.hash)?.paneID;
}

function render(): void {
  const route = parseTerminalRoute(window.location.hash);
  const paneID = route?.paneID;
  if (returningToHome(renderedTerminalPane, paneID)) restoreHomePlace = true;
  renderedTerminalPane = paneID;
  if (paneID) {
    renderTerminal(paneID, route.terminalID);
  } else {
    renderHome();
  }
}

function renderHome(): void {
  terminalPage?.destroy();
  terminalPage = undefined;
  const automaticPlace = automaticAllTerminalsPlace(homeMode, state.home.blocked_count, {
    focusPane: allTerminalsFocusPane,
    scroll: allTerminalsScroll,
  });
  if (automaticPlace) restoreAllTerminalsPlace(automaticPlace);
  document.body.classList.remove("terminal-active");
  homeView.render({
    actionsAvailable: liveActionsAvailable(),
    mode: homeMode,
    reachability: homeReachability(),
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
  restoreAllTerminalsPlace({ focusPane: allTerminalsFocusPane, scroll: allTerminalsScroll });
  renderHome();
}

function restoreAllTerminalsPlace(place: HomePlace): void {
  homeMode = "all";
  homeScroll = place.scroll;
  homeFocusPane = place.focusPane;
  restoreHomePlace = true;
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

function renderTerminal(paneID: string, expectedTerminalID?: string): void {
  document.body.classList.add("terminal-active");
  window.scrollTo(0, 0);
  if (expectedTerminalID && !liveActionsAvailable()) {
    renderTerminalWaiting();
    return;
  }
  const current = findTerminal(state.home, paneID);
  const selected = selectedTerminal?.terminal.pane_id === paneID ? selectedTerminal : undefined;
  if (current && !exactTerminalMatches(current.terminal.terminal_id, expectedTerminalID)) {
    renderTerminalUnavailable();
    return;
  }
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
  }, { onHome: leaveTerminal, onNotifications: () => notifications.openSettings() });
}

function renderTerminalWaiting(): void {
  terminalPage?.destroy();
  terminalPage = undefined;
  app.className = "terminal-state-screen";
  const panel = element("section", "state-panel terminal-unavailable");
  panel.append(
    element("strong", undefined, "Connecting"),
    element("p", undefined, "Waiting for this terminal."),
    button("Notifications", () => notifications.openSettings()),
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
    button("Notifications", () => notifications.openSettings()),
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
void notifications.init();

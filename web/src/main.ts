import "./style.css";
import { AccessController, hasInvitationFragment, invitationToken, type AccessProbe } from "./access";
import { HomeView } from "./home-view";
import {
  HomeConnectionOwner,
  homeConnectionAllowed,
  homeReachability as deriveHomeReachability,
  maintainHomeConnection,
  nextHomeCheckDelay,
  refreshHomeConnectionView,
  resumeHomeConnection,
  type HomeAccessMode,
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
import { parseTerminalRoute, returningToHome, terminalRouteOutcome } from "./terminal-route";
import { WorkspaceActionsClient } from "./workspace-actions";
import { NotificationsController } from "./notifications";
import { settingsAction } from "./settings-action";

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
const accessNode = document.querySelector<HTMLElement>("#access");
if (!accessNode) throw new Error("Access root is missing");
const workspaceActions = new WorkspaceActionsClient();

let state: HomeState = {
  connection: "reconnecting",
  gap: 0,
  has_home: false,
  home: { blocked_count: 0, working_count: 0, workspaces: [] },
  last_known: false,
};
const homeConnections = new HomeConnectionOwner<WebSocket>();
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
type AppAccessMode = HomeAccessMode;
let trustRoute = hasInvitationFragment(window.location.hash);
let trustToken = invitationToken(window.location.hash);
let accessMode: AppAccessMode = trustRoute ? "trust" : "checking";
let sessionRotating = false;
let accessProbePending = false;
let accessProbeVersion = 0;
let homeAuthorityReady = false;
if (trustRoute) history.replaceState(null, "", window.location.pathname + window.location.search);
const accessController = new AccessController(accessNode, {
  onSessionRotated: accessSessionRotated,
  onSessionRotating: (active) => { sessionRotating = active; },
  onSignedIn: accessSignedIn,
  onSignedOut: accessSignedOut,
});
notifications.setDeviceSettingsHandler((returnFocus) => void accessController.openDevices(returnFocus));
const homeView = new HomeView(app, {
  isHomeActive: homeInterfaceActive,
  onFocusPane: (paneID) => {
    lastFocusedHomePane = paneID;
  },
  onOpen: openTerminal,
  onNotifications: () => notifications.openSettings(),
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

function createHomeSocket(): WebSocket {
  return new WebSocket(webSocketURL("/api/home"));
}

function activateHomeSocket(socket: WebSocket): void {
  socket.addEventListener("open", () => {
    if (!homeConnections.owns(socket)) return;
    scheduleHomeCheck();
  });
  socket.addEventListener("message", (event) => {
    if (!homeConnections.owns(socket)) return;
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
    if (!homeConnections.release(socket)) return;
    if (!sessionRotating && accessMode === "active") {
      homeAuthorityReady = false;
      probeAccess();
      return;
    }
    if (accessMode === "sign-in-off") scheduleHomeCheck();
  });
  scheduleHomeCheck();
}

function connectHome(replaceActive = false): void {
  if (accessMode === "checking" || (accessMode === "active" && !homeAuthorityReady)) probeAccess();
  const maintenance = maintainHomeConnection(
    homeReachability(),
    homeConnections,
    homeSocketIsActive,
    createHomeSocket,
    activateHomeSocket,
    replaceActive,
    homeConnectionAllowed(accessMode, homeAuthorityReady),
  );
  if (maintenance === "waiting") scheduleHomeCheck();
}

function probeAccess(): void {
  if (accessProbePending || (accessMode !== "checking" && accessMode !== "active")) return;
  const version = accessProbeVersion;
  accessProbePending = true;
  void accessController.probe().then((mode) => {
    if (version !== accessProbeVersion) return;
    accessProbePending = false;
    applyAccessProbe(mode);
  });
}

function cancelAccessProbe(): void {
  accessProbeVersion++;
  accessProbePending = false;
}

function liveActionsAvailable(): boolean {
  return (accessMode === "active" || accessMode === "sign-in-off") && state.has_home && homeEvidenceCurrent() && state.connection === "live" && !state.last_known;
}

function homeReachability(): HomeReachability {
  return deriveHomeReachability(lastValidHomeFrameAt, performance.now());
}

function homeEvidenceCurrent(): boolean {
  return homeReachability() === "current";
}

function homeSocketActive(): boolean {
  return homeConnections.active(homeSocketIsActive);
}

function homeSocketIsActive(socket: WebSocket): boolean {
  return socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING;
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
  if (reachability !== "current") {
    refreshHomeConnectionView(homeInterfaceActive(), renderHome);
  }
  connectHome();
}

function terminalPaneFromHash(): string | undefined {
  return parseTerminalRoute(window.location.hash)?.paneID;
}

function homeInterfaceActive(): boolean {
  return (accessMode === "active" || accessMode === "sign-in-off") && !terminalPaneFromHash();
}

function render(): void {
  if (accessMode === "trust") {
    terminalPage?.destroy();
    terminalPage = undefined;
    accessController.renderTrust(app, trustToken ?? "");
    return;
  }
  if (accessMode === "signed-out") {
    terminalPage?.destroy();
    terminalPage = undefined;
    accessController.renderSignIn(app);
    return;
  }
  if (accessMode === "checking") {
    terminalPage?.destroy();
    terminalPage = undefined;
    accessController.renderChecking(app);
    return;
  }
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
    signInOff: accessMode === "sign-in-off",
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

function applyAccessProbe(mode: AccessProbe): void {
  if (mode === "protected") {
    const changed = accessMode !== "active";
    accessMode = "active";
    homeAuthorityReady = true;
    trustRoute = false;
    trustToken = undefined;
    notifications.setDeviceSettingsAvailable(true);
    if (changed) void notifications.init();
    render();
    connectHome();
    return;
  }
  if (mode === "sign-in-off") {
    const changed = accessMode !== "sign-in-off";
    accessMode = "sign-in-off";
    homeAuthorityReady = false;
    notifications.setDeviceSettingsAvailable(false);
    if (changed) void notifications.init();
    render();
    connectHome();
    return;
  }
  if (mode === "signed-out") {
    accessSignedOut();
    return;
  }
  scheduleHomeCheck();
}

function accessSignedIn(): void {
  accessController.close();
  cancelAccessProbe();
  accessMode = "checking";
  homeAuthorityReady = false;
  trustRoute = false;
  trustToken = undefined;
  homeConnections.clear();
  window.clearTimeout(homeTimer);
  homeTimer = undefined;
  lastValidHomeFrameAt = performance.now();
  render();
  probeAccess();
}

function accessSessionRotated(): void {
  cancelAccessProbe();
  homeConnections.clear();
  window.clearTimeout(homeTimer);
  homeTimer = undefined;
  homeAuthorityReady = false;
  lastValidHomeFrameAt = performance.now();
  probeAccess();
}

function accessSignedOut(): void {
  accessController.close();
  cancelAccessProbe();
  terminalPage?.destroy();
  terminalPage = undefined;
  homeConnections.clear();
  window.clearTimeout(homeTimer);
  homeTimer = undefined;
  homeAuthorityReady = false;
  state = {
    connection: "reconnecting",
    gap: 0,
    has_home: false,
    home: { blocked_count: 0, working_count: 0, workspaces: [] },
    last_known: false,
  };
  publishedStateSignature = "";
  selectedTerminal = undefined;
  notifications.setDeviceSettingsAvailable(false);
  notificationsNode!.replaceChildren();
  accessMode = "signed-out";
  window.location.hash = "";
  render();
}

function enterInvitationFromHash(): boolean {
  if (!hasInvitationFragment(window.location.hash)) return false;
  cancelAccessProbe();
  trustRoute = true;
  trustToken = invitationToken(window.location.hash);
  accessMode = "trust";
  history.replaceState(null, "", window.location.pathname + window.location.search);
  homeConnections.clear();
  window.clearTimeout(homeTimer);
  homeTimer = undefined;
  homeAuthorityReady = false;
  render();
  return true;
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
  const current = findTerminal(state.home, paneID);
  const selected = selectedTerminal?.terminal.pane_id === paneID ? selectedTerminal : undefined;
  const outcome = terminalRouteOutcome({
    currentStateAvailable: liveActionsAvailable(),
    currentTerminalID: current?.terminal.terminal_id,
    expectedTerminalID,
    selectedTerminalID: selected?.terminal.terminal_id,
  });
  if (outcome === "unavailable") {
    renderTerminalUnavailable();
    return;
  }
  if (outcome === "waiting") {
    renderTerminalWaiting();
    return;
  }
  const entry = outcome === "current" ? current : selected;
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
    settingsAction(document, () => notifications.openSettings(), "terminal-notifications"),
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
    settingsAction(document, () => notifications.openSettings(), "terminal-notifications"),
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

window.addEventListener("hashchange", () => {
  if (!enterInvitationFromHash()) render();
});
window.addEventListener("online", () => connectHome());
document.addEventListener("visibilitychange", () => {
  resumeHomeConnection(
    document.visibilityState,
    homeInterfaceActive(),
    renderHome,
    () => connectHome(true),
  );
});

render();
if (!trustRoute) probeAccess();

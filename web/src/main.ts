import "./style.css";
import { AccessController, hasInvitationFragment, invitationToken, type AccessProbe } from "./access";
import { HomeView } from "./home-view";
import {
  HomeConnectionOwner,
  HomeResumeTracker,
  homeConnectionAllowed,
  homeReachability as deriveHomeReachability,
  maintainHomeConnection,
  nextHomeCheckDelay,
  refreshHomeConnectionView,
  resumeHomeConnection,
  type HomeAccessMode,
  type HomeConnectionReplacement,
  type HomeReachability,
} from "./home-connection";
import {
  allTerminals,
  automaticAllTerminalsPlace,
  chooseHerdr,
  emptyHomeState,
  findTerminal,
  herdrPresent,
  herdrSwitchShown,
  readRememberedHerdr,
  rememberHerdr,
  terminalCount,
  viewCount,
  type Herdr,
  type HomeMode,
  type HomePlace,
  type HomeSides,
  type HomeState,
  type TerminalEntry,
} from "./home-model";
import { parseCompleteHomeState } from "./home-parser";
import { TerminalPage } from "./terminal-page";
import { parseTerminalRoute, returningToHome, terminalHash, terminalRouteOutcome, type TerminalRoute } from "./terminal-route";
import { WorkspaceActionsClient } from "./workspace-actions";
import { TerminalActionsClient, type TerminalActionTarget } from "./terminal-actions";
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
const terminalActions = new TerminalActionsClient();

// Each Home message carries both Herdrs. Home shows one of them; the other is
// kept for the switch, notification settings and terminal links.
let sides: HomeSides = { core: emptyHomeState(), tui: emptyHomeState() };
// shownHerdr stays undefined until both Herdrs are settled (Loading terminals).
let shownHerdr: Herdr | undefined;
let rememberedHerdr = readRememberedHerdr(() => window.localStorage);
const loadingState = emptyHomeState();
const homeConnections = new HomeConnectionOwner<WebSocket>();
const homeResumeTracker = new HomeResumeTracker();
let homeTimer: number | undefined;
let lastValidHomeFrameAt = performance.now();
let selectedTerminal: TerminalEntry | undefined;
let selectedTerminalHerdr: Herdr | undefined;
let homeScroll = 0;
let homeFocusPane: string | undefined;
let homeFocusWorkspace: string | undefined;
let homeAnchorTop: number | undefined;
let homeHadListFocus = false;
let lastFocusedHomePane: string | undefined;
let lastFocusedHomeWorkspace: string | undefined;
let allTerminalsScroll = 0;
let allTerminalsFocusPane: string | undefined;
let allTerminalsFocusWorkspace: string | undefined;
let homeMode: HomeMode = "all";
let restoreHomePlace = false;
let publishedStateSignature = "";
let renderedTerminalPane: string | undefined;
let renderedTerminalHerdr: TerminalRoute["herdr"] | undefined;
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
notifications.setDeviceSettingsHandler(
  (host, closeSettings) => void accessController.openDevices(host, closeSettings),
  () => accessController.closeDevices(),
);
const homeView = new HomeView(app, {
  isHomeActive: homeInterfaceActive,
  onFocusPane: (paneID) => {
    lastFocusedHomePane = paneID;
    lastFocusedHomeWorkspace = undefined;
  },
  onFocusWorkspace: (workspaceID) => {
    lastFocusedHomePane = undefined;
    lastFocusedHomeWorkspace = workspaceID;
  },
  onOpen: openTerminal,
  onOpenCreated: openCreatedTerminal,
  onNotifications: () => notifications.openSettings(),
  onShowAll: showAllTerminals,
  onShowBlocked: () => showStatusTerminals("blocked"),
  onShowError: () => showStatusTerminals("error"),
  onShowHerdr: switchHerdr,
  prepareWorkspaceAction: (request) => workspaceActions.prepare(request),
  runWorkspaceAction: (request) => workspaceActions.run(request),
  prepareTerminalAction: (request) => terminalActions.prepare(request),
  runTerminalAction: (request) => terminalActions.run(request),
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
        homeConnections.recordValidFrame(socket);
        lastValidHomeFrameAt = now;
        if (!wasCurrent) render();
        scheduleHomeCheck();
        return;
      }
      const completeState = parseCompleteHomeState(parsed);
      if (!completeState) throw new Error("invalid Home frame");
      homeConnections.recordValidFrame(socket);
      lastValidHomeFrameAt = now;
      const changed = raw !== publishedStateSignature;
      sides = { core: completeState.core, tui: completeState.tui };
      publishedStateSignature = raw;
      notifications.setPresent({ core: herdrPresent(sides.core), tui: herdrPresent(sides.tui) });
      const shownChanged = settleShownHerdr();
      if (changed || shownChanged || !wasCurrent) render();
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

function connectHome(replacement: HomeConnectionReplacement = "when-needed"): void {
  if (accessMode === "checking" || (accessMode === "active" && !homeAuthorityReady)) probeAccess();
  const maintenance = maintainHomeConnection(
    homeReachability(),
    homeConnections,
    homeSocketIsActive,
    createHomeSocket,
    activateHomeSocket,
    performance.now(),
    replacement,
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

function shownState(): HomeState {
  return shownHerdr === undefined ? loadingState : sides[shownHerdr];
}

function liveActionsAvailable(herdr: Herdr | undefined = shownHerdr): boolean {
  if (herdr === undefined) return false;
  const state = sides[herdr];
  return (accessMode === "active" || accessMode === "sign-in-off") && state.has_home && homeEvidenceCurrent() && state.connection === "live" && !state.last_known;
}

// settleShownHerdr picks the Herdr Home shows once both are settled, and again
// only if the shown one is no longer present. It reports a change.
function settleShownHerdr(): boolean {
  if (shownHerdr !== undefined && herdrPresent(sides[shownHerdr])) return false;
  const next = chooseHerdr(sides, rememberedHerdr);
  if (next === shownHerdr) return false;
  showHerdr(next);
  return true;
}

// showHerdr changes the shown Herdr. The two are different lists, so Home
// returns to all terminals and forgets places kept for the other one.
function showHerdr(herdr: Herdr | undefined): void {
  shownHerdr = herdr;
  homeMode = "all";
  allTerminalsScroll = 0;
  allTerminalsFocusPane = undefined;
  allTerminalsFocusWorkspace = undefined;
  lastFocusedHomePane = undefined;
  lastFocusedHomeWorkspace = undefined;
}

function switchHerdr(herdr: Herdr): void {
  if (herdr === shownHerdr) return;
  rememberedHerdr = herdr;
  rememberHerdr(() => window.localStorage, herdr);
  showHerdr(herdr);
  homeScroll = 0;
  homeFocusPane = undefined;
  homeFocusWorkspace = undefined;
  homeAnchorTop = undefined;
  homeHadListFocus = false;
  restoreHomePlace = true;
  renderHome();
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
  const delay = nextHomeCheckDelay(
    lastValidHomeFrameAt,
    now,
    homeSocketActive(),
    homeConnections.awaitingFirstValidFrame ? homeConnections.attemptStartedAt : undefined,
  );
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
  if (returningToHome(renderedTerminalPane, paneID)) {
    restoreHomePlace = true;
    // Home from a terminal shows that terminal's Herdr for this visit.
    const from = renderedTerminalHerdr;
    if (from !== undefined && from !== "unknown" && from !== shownHerdr && shownHerdr !== undefined && herdrPresent(sides[from])) {
      showHerdr(from);
      homeScroll = 0;
      homeFocusPane = renderedTerminalPane;
      homeFocusWorkspace = undefined;
      homeAnchorTop = undefined;
      homeHadListFocus = true;
    }
  }
  renderedTerminalPane = paneID;
  renderedTerminalHerdr = route?.herdr;
  if (route) {
    renderTerminal(route);
  } else {
    renderHome();
  }
}

function renderHome(): void {
  terminalPage?.destroy();
  terminalPage = undefined;
  const state = shownState();
  const automaticPlace = automaticAllTerminalsPlace(homeMode, viewCount(homeMode, state.home), {
    focusPane: allTerminalsFocusPane,
    focusWorkspace: allTerminalsFocusWorkspace,
    scroll: allTerminalsScroll,
  });
  if (automaticPlace) restoreAllTerminalsPlace(automaticPlace);
  document.body.classList.remove("terminal-active");
  homeView.render({
    actionsAvailable: liveActionsAvailable(),
    herdr: shownHerdr ?? "tui",
    herdrSwitch: shownHerdr !== undefined && herdrSwitchShown(sides),
    mode: homeMode,
    reachability: homeReachability(),
    signInOff: accessMode === "sign-in-off",
    restore: restoreHomePlace
      ? {
          anchorTop: homeAnchorTop,
          focusPane: homeHadListFocus ? homeFocusPane : undefined,
          focusWorkspace: homeHadListFocus ? homeFocusWorkspace : undefined,
          pane: homeFocusPane,
          scroll: homeScroll,
          workspace: homeFocusWorkspace,
        }
      : undefined,
    state,
  });
  homeFocusPane = undefined;
  homeFocusWorkspace = undefined;
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
  sides = { core: emptyHomeState(), tui: emptyHomeState() };
  showHerdr(undefined);
  publishedStateSignature = "";
  selectedTerminal = undefined;
  selectedTerminalHerdr = undefined;
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

function currentFocusedWorkspace(): string | undefined {
  return document.activeElement instanceof HTMLElement ? document.activeElement.dataset.workspaceKey : undefined;
}

function showStatusTerminals(status: "blocked" | "error"): void {
  allTerminalsScroll = window.scrollY;
  allTerminalsFocusPane = currentFocusedPane() ?? lastFocusedHomePane;
  allTerminalsFocusWorkspace = currentFocusedWorkspace() ?? lastFocusedHomeWorkspace;
  const firstBlocked = allTerminals(shownState().home).find(({ terminal }) => terminal.agent?.status === status);
  homeMode = status;
  homeScroll = 0;
  homeFocusPane = firstBlocked && terminalCount(firstBlocked.workspace) === 1
    ? firstBlocked.terminal.pane_id
    : undefined;
  homeFocusWorkspace = firstBlocked && terminalCount(firstBlocked.workspace) > 1
    ? firstBlocked.workspace.id
    : undefined;
  restoreHomePlace = true;
  renderHome();
}

function showAllTerminals(): void {
  restoreAllTerminalsPlace({
    focusPane: allTerminalsFocusPane,
    focusWorkspace: allTerminalsFocusWorkspace,
    scroll: allTerminalsScroll,
  });
  renderHome();
}

function restoreAllTerminalsPlace(place: HomePlace): void {
  homeMode = "all";
  homeScroll = place.scroll;
  homeFocusPane = place.focusPane;
  homeFocusWorkspace = place.focusWorkspace;
  restoreHomePlace = true;
}

function openTerminal(entry: TerminalEntry): void {
  if (shownHerdr === undefined || !liveActionsAvailable()) return;
  homeScroll = window.scrollY;
  homeFocusPane = entry.terminal.pane_id;
  homeFocusWorkspace = undefined;
  homeAnchorTop = homeView.row(entry.terminal.pane_id)?.getBoundingClientRect().top;
  homeHadListFocus = currentFocusedPane() === entry.terminal.pane_id;
  selectedTerminal = entry;
  selectedTerminalHerdr = shownHerdr;
  window.location.hash = terminalHash(shownHerdr, entry.terminal.pane_id);
}

function openCreatedTerminal(target: TerminalActionTarget, herdr: Herdr): void {
  homeScroll = window.scrollY;
  homeFocusPane = target.pane_id;
  homeFocusWorkspace = undefined;
  homeAnchorTop = homeView.row(target.pane_id)?.getBoundingClientRect().top;
  homeHadListFocus = false;
  selectedTerminal = undefined;
  window.location.hash = terminalHash(herdr, target.pane_id, target.terminal_id);
}

function renderTerminal(route: TerminalRoute): void {
  document.body.classList.add("terminal-active");
  window.scrollTo(0, 0);
  if (route.herdr === "unknown") {
    renderTerminalUnavailable();
    return;
  }
  const herdr = route.herdr;
  // Shepherdr says in every Home message whether it can open this Herdr's
  // terminals; until the first one arrives, the page waits as usual.
  if (publishedStateSignature !== "" && !sides[herdr].can_open_terminals) {
    renderTerminalUnavailable(false);
    return;
  }
  const { paneID, terminalID: expectedTerminalID } = route;
  const current = findTerminal(sides[herdr].home, paneID);
  const selected = selectedTerminal?.terminal.pane_id === paneID && selectedTerminalHerdr === herdr ? selectedTerminal : undefined;
  const outcome = terminalRouteOutcome({
    currentStateAvailable: liveActionsAvailable(herdr),
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
  selectedTerminalHerdr = herdr;
  if (terminalPage?.herdr === herdr && terminalPage.paneID === paneID && terminalPage.terminalID === entry.terminal.terminal_id) {
    terminalPage.updateTarget(entry.terminal.title, entry.terminal.agent?.status);
    return;
  }
  terminalPage?.destroy();
  terminalPage = new TerminalPage(app, {
    agentStatus: entry.terminal.agent?.status,
    herdr,
    paneID,
    terminalID: entry.terminal.terminal_id,
    title: entry.terminal.title,
    workspaceID: entry.workspace.id,
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

function renderTerminalUnavailable(gone = true): void {
  terminalPage?.destroy();
  terminalPage = undefined;
  app.className = "terminal-state-screen";
  const panel = element("section", "state-panel terminal-unavailable");
  panel.append(
    element("strong", undefined, "Terminal unavailable"),
    ...(gone ? [element("p", undefined, "This terminal is no longer here.")] : []),
    settingsAction(document, () => notifications.openSettings(), "terminal-notifications"),
    button("Home", leaveTerminal),
  );
  app.replaceChildren(panel);
}

function leaveTerminal(): void {
  terminalPage?.destroy();
  terminalPage = undefined;
  selectedTerminal = undefined;
  selectedTerminalHerdr = undefined;
  restoreHomePlace = true;
  window.location.hash = "";
}

window.addEventListener("hashchange", () => {
  if (!enterInvitationFromHash()) render();
});
const resumeHome = () => {
  const replacement = homeResumeTracker.resumeReplacement();
  resumeHomeConnection(
    true,
    homeInterfaceActive(),
    renderHome,
    () => connectHome(replacement),
  );
};
window.addEventListener("offline", () => homeResumeTracker.markNonForeground());
window.addEventListener("online", resumeHome);
window.addEventListener("pagehide", () => homeResumeTracker.markNonForeground());
window.addEventListener("pageshow", resumeHome);
document.addEventListener("visibilitychange", () => {
  if (document.visibilityState !== "visible") {
    homeResumeTracker.markNonForeground();
    return;
  }
  resumeHome();
});

render();
if (!trustRoute) probeAccess();

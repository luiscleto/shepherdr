import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import "./style.css";
import { HomeView } from "./home-view";
import {
  homeReachability as deriveHomeReachability,
  nextHomeCheckDelay,
  type HomeReachability,
} from "./home-connection";
import {
  allTerminals,
  findTerminal,
  type HomeState,
  type TerminalEntry,
} from "./home-model";
import { updateTerminalIdentity } from "./terminal-presentation";
import { resolveTerminalRoute, returningToHome, terminalWaitingLabel } from "./terminal-route";
import { installTerminalTouchSelection } from "./terminal-touch-selection";
import {
  hasRecentTraffic,
  type InteractionMode,
  type ProtocolOrderingEvidence,
  protocolOrderingEvidence,
  TerminalRecoveryPolicy,
  TerminalSelection,
  RecoveryWindow,
  TERMINAL_GEOMETRY_SETTLE_MS,
  TERMINAL_RETRY_DELAY_MS,
  TERMINAL_STALE_AFTER_MS,
  terminalControlLabels,
  terminalStateActionLabels,
  type TerminalMode,
  terminalStateCopy,
} from "./terminal-state";

interface SelectedTarget {
  entry: TerminalEntry;
  expectedGap: number;
  expectedTerminalID: string;
}

interface TerminalMessage {
  bytes?: string;
  detail?: string;
  epoch?: unknown;
  full?: boolean;
  gap?: unknown;
  height?: number;
  mode?: TerminalMode;
  seq?: number;
  terminal_id?: string;
  type: "terminal.frame" | "terminal.heartbeat" | "terminal.status";
  width?: number;
}

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
let selected: SelectedTarget | undefined;
let homeScroll = 0;
let homeFocusPane: string | undefined;
let homeAnchorTop: number | undefined;
let homeHadListFocus = false;
let lastFocusedHomePane: string | undefined;
let allTerminalsScroll = 0;
let allTerminalsFocusPane: string | undefined;
let homeMode: "all" | "blocked" = "all";
let restoreHomePlace = false;
let terminalView: ReturnType<typeof createTerminalView> | undefined;
let hasConnectedHome = false;
let publishedStateSignature = "";
let renderedTerminalPane: string | undefined;
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

function button(text: string, action: () => void, className?: string): HTMLButtonElement {
  const node = element("button", className, text);
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
      terminalView?.refreshTargetTruth(next.connection, protocolOrderingEvidence(next));
      const paneID = terminalPaneFromHash();
      if (paneID) {
        const entry = findTerminal(state.home, paneID);
        if (entry) {
          if (!selected || selected.entry.terminal.pane_id !== paneID) selected = selectTarget(entry);
          else selected.entry = entry;
          terminalView?.updateEntry(entry);
        }
      }
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

function findCurrentTarget(paneID: string): SelectedTarget | undefined {
  const entry = findTerminal(state.home, paneID);
  return entry ? selectTarget(entry) : undefined;
}

function selectTarget(entry: TerminalEntry): SelectedTarget {
  return {
    entry,
    expectedGap: state.gap,
    expectedTerminalID: entry.terminal.terminal_id,
  };
}

function render(): void {
  const paneID = terminalPaneFromHash();
  if (returningToHome(renderedTerminalPane, paneID)) restoreHomePlace = true;
  renderedTerminalPane = paneID;
  if (paneID) {
    if (!selected) selected = findCurrentTarget(paneID);
    renderTerminal(paneID);
  } else {
    renderHome();
  }
}

function renderHome(): void {
  terminalView?.destroy();
  terminalView = undefined;
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
  selected = selectTarget(entry);
  window.location.hash = `terminal=${encodeURIComponent(entry.terminal.pane_id)}`;
}

function renderTerminal(paneID: string): void {
  const route = resolveTerminalRoute(state, paneID, {
    homeCurrent: homeReachability() === "current",
    offlineHint: !navigator.onLine,
    receivedHome: hasConnectedHome,
  });
  if (route.kind === "missing") {
    terminalView?.destroy();
    terminalView = undefined;
    renderTerminalUnavailable();
    return;
  }
  if (terminalView?.paneID === paneID) {
    if (route.kind === "target") {
      if (!selected) selected = selectTarget(route.entry);
      else selected.entry = route.entry;
      terminalView.updateEntry(route.entry);
      terminalView.refreshTargetTruth(state.connection, protocolOrderingEvidence(state));
    }
    return;
  }

  terminalView?.destroy();
  terminalView = undefined;
  document.body.classList.add("terminal-active");
  window.scrollTo(0, 0);
  app.className = "terminal-screen";

  if (route.kind === "waiting") {
    const known = selected?.entry.terminal.pane_id === paneID ? selected.entry : undefined;
    renderWaitingTerminal(known, terminalWaitingLabel(route.state));
    return;
  }

  let target = selected?.entry.terminal.pane_id === paneID ? selected : undefined;
  if (!target) target = selectTarget(route.entry);
  target.entry = route.entry;
  selected = target;
  terminalView = createTerminalView(target);
  window.scrollTo(0, 0);
}

function renderTerminalUnavailable(): void {
  document.body.classList.add("terminal-active");
  window.scrollTo(0, 0);
  app.className = "terminal-state-screen";
  const panel = element("section", "state-panel terminal-unavailable");
  panel.append(
    element("strong", undefined, "Terminal unavailable"),
    element("p", undefined, "This terminal is no longer here."),
    button("Back to Home", leaveTerminal),
  );
  app.replaceChildren(panel);
}

function terminalChrome(titleText: string, statusText: string) {
  const header = element("header", "terminal-header");
  const back = button("Home", leaveTerminal);
  const title = element("div", "terminal-title");
  const heading = element("h1", undefined, titleText);
  title.append(heading);
  header.append(back, title);

  const stateBand = element("section", "terminal-state-band");
  stateBand.setAttribute("role", "status");
  stateBand.setAttribute("aria-live", "polite");
  stateBand.setAttribute("aria-atomic", "true");
  const status = element("strong", "terminal-status", statusText);
  const statusBody = element("span", "terminal-status-body");
  const stateActions = element("div", "terminal-state-actions");
  stateBand.append(status, statusBody, stateActions);
  const host = element("div", "terminal-host");
  host.setAttribute("aria-label", "Terminal");
  const actions = element("nav", "terminal-actions");
  actions.setAttribute("aria-label", "Terminal controls");
  return { actions, header, heading, host, stateActions, stateBand, status, statusBody };
}

function renderWaitingTerminal(entry: TerminalEntry | undefined, statusText: string): void {
  const chrome = terminalChrome(entry?.terminal.title ?? "Terminal", statusText);
  if (entry) updateTerminalIdentity(chrome, entry);
  chrome.host.classList.add("terminal-host-waiting");
  app.replaceChildren(chrome.header, chrome.stateBand, chrome.host, chrome.actions);
}

function createTerminalView(target: SelectedTarget) {
  const chrome = terminalChrome(target.entry.terminal.title, "Connecting to terminal");
  updateTerminalIdentity(chrome, target.entry);
  const { actions, host, stateActions, stateBand, status, statusBody } = chrome;
  app.replaceChildren(chrome.header, stateBand, host, actions);

  const terminal = new Terminal({
    allowProposedApi: false,
    convertEol: false,
    cursorBlink: true,
    fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
    fontSize: 13,
    minimumContrastRatio: 4.5,
    screenReaderMode: true,
    scrollback: 0,
    theme: {
      background: "#18201e",
      black: "#2b3532",
      blue: "#82afe3",
      brightBlack: "#89938e",
      brightBlue: "#a1c7f2",
      brightCyan: "#96ddd7",
      brightGreen: "#afe0aa",
      brightMagenta: "#dfb3e3",
      brightRed: "#ff9a9a",
      brightWhite: "#fff8ec",
      brightYellow: "#efd781",
      cursor: "#b85c32",
      cyan: "#77c5bf",
      foreground: "#f3eadb",
      green: "#91c98f",
      magenta: "#c999d0",
      red: "#ef7f80",
      selectionBackground: "#355b63",
      white: "#e6ded1",
      yellow: "#d8bd6a",
    },
  });
  const fit = new FitAddon();
  terminal.loadAddon(fit);
  terminal.open(host);
  host.querySelector<HTMLElement>(".live-region")?.setAttribute("aria-live", "off");
  fit.fit();

  let socket: WebSocket | undefined;
  let retry: number | undefined;
  let recoveryTimer: number | undefined;
  let watchdog: number | undefined;
  let mode: TerminalMode = "connecting";
  let interaction: InteractionMode = "input";
  let resizeTimer: number | undefined;
  let generation = 0;
  let lastTrafficAt: number | undefined;
  let hasFrame = false;
  let awaitingFullFrame = true;
  let lastResize = "";
  const recovery = new RecoveryWindow();
  const recoveryPolicy = new TerminalRecoveryPolicy();
  recoveryPolicy.home(state.connection, protocolOrderingEvidence(state));
  const selection = new TerminalSelection();

  const instrument = (event: string): void => {
    console.debug("Shepherdr terminal transport", {
      browserOnline: navigator.onLine,
      event,
      generation,
      time: Math.round(performance.now()),
    });
  };

  const trafficCurrent = (): boolean => hasRecentTraffic(lastTrafficAt, performance.now());
  const shepherdrCurrent = (): boolean => trafficCurrent() || homeEvidenceCurrent();

  const send = (message: object): void => {
    if (socket?.readyState === WebSocket.OPEN && trafficCurrent() && hasFrame) socket.send(JSON.stringify(message));
  };

  const controls = new Map<string, HTMLButtonElement>();
  const addControl = (label: string, action: () => void, className?: string): HTMLButtonElement => {
    const control = button(label, action, className);
    controls.set(label, control);
    actions.append(control);
    return control;
  };

  const release = addControl("Release control", () => send({ type: "terminal.release" }), "session-action");
  const takeover = addControl("Take over", () => {
    if (window.confirm("Take control? The current controller will lose input.")) send({ type: "terminal.takeover", confirmed: true });
  }, "primary session-action");
  const acquire = addControl("Control", () => send({ type: "terminal.control" }), "session-action");
  addControl("Ctrl C", () => send({ type: "terminal.input", text: "\u0003" }));
  addControl("Enter", () => send({ type: "terminal.input", text: "\r" }));
  addControl("Select text", () => {
    interaction = "select";
    terminal.blur();
    updatePresentation();
  });
  addControl("Copy", () => {
    if (selection.copyAvailable) void navigator.clipboard?.writeText(selection.text).catch(() => undefined);
  });
  addControl("Done", () => {
    clearVisibleSelection();
    interaction = "input";
    updatePresentation();
    scheduleGeometry();
    terminal.focus();
  });
  addControl("Up", () => send({ type: "terminal.input", text: "\u001b[A" }));
  addControl("Down", () => send({ type: "terminal.input", text: "\u001b[B" }));
  addControl("Left", () => send({ type: "terminal.input", text: "\u001b[D" }));
  addControl("Right", () => send({ type: "terminal.input", text: "\u001b[C" }));
  addControl("Esc", () => send({ type: "terminal.input", text: "\u001b" }));
  addControl("Tab", () => send({ type: "terminal.input", text: "\t" }));
  addControl("Page up", () => send({ type: "terminal.scroll", direction: "up", lines: Math.max(1, terminal.rows - 2) }));
  addControl("Page down", () => send({ type: "terminal.scroll", direction: "down", lines: Math.max(1, terminal.rows - 2) }));

  const retryNow = button("Try again", () => {
    recovery.manualRetry(performance.now());
    armRecoveryDeadline();
    recoveryPolicy.retry();
    mode = "reconnecting";
    instrument("manual retry");
    connect();
  }, "primary");
  const retryHome = button("Home", leaveTerminal);
  stateActions.append(retryNow, retryHome);

  const currentSelection = (): string => {
    const native = window.getSelection();
    if (native?.anchorNode && native.focusNode && host.contains(native.anchorNode) && host.contains(native.focusNode)) {
      const value = native.toString();
      if (value !== "") return value;
    }
    return terminal.getSelection();
  };

  const updateSelection = (): void => {
    const cleared = selection.update(currentSelection());
    if (cleared) scheduleGeometry();
    updatePresentation();
  };

  const clearDisabledSelectionScratch = (): void => {
    const input = terminal.textarea;
    if (!input?.disabled) return;
    // xterm uses its disabled helper as Linux selection scratch space. An
    // enabled helper may hold intentional input or composition and is not ours
    // to clear.
    input.value = "";
    input.setSelectionRange(0, 0);
  };

  const clearVisibleSelection = (): void => {
    const native = window.getSelection();
    if (native?.anchorNode && host.contains(native.anchorNode)) native.removeAllRanges();
    terminal.clearSelection();
    clearDisabledSelectionScratch();
    const cleared = selection.invalidate();
    if (cleared) scheduleGeometry();
    updatePresentation();
  };

  function updatePresentation(): void {
    const offline = !shepherdrCurrent() && !navigator.onLine;
    const copyText = terminalStateCopy(mode, interaction, hasFrame, offline);
    if (status.textContent !== copyText.heading) status.textContent = copyText.heading;
    if (statusBody.textContent !== (copyText.body ?? "")) statusBody.textContent = copyText.body ?? "";
    if (statusBody.hidden !== !copyText.body) statusBody.hidden = !copyText.body;
    const stateClass = `terminal-state-band terminal-state-${mode}`;
    if (stateBand.className !== stateClass) stateBand.className = stateClass;
    const visible = new Set(terminalControlLabels(mode, interaction, selection.copyAvailable));
    for (const [label, control] of controls) {
      const hidden = !visible.has(label);
      if (control.hidden !== hidden) control.hidden = hidden;
    }
    const visibleStateActions = new Set(terminalStateActionLabels(mode));
    const showStateActions = visibleStateActions.size > 0;
    if (stateActions.hidden !== !showStateActions) stateActions.hidden = !showStateActions;
    retryNow.hidden = !visibleStateActions.has("Try again");
    retryHome.hidden = !visibleStateActions.has("Home");
    const selecting = mode !== "controlled" || interaction === "select";
    host.classList.toggle("terminal-selecting", selecting);
    const disableStdin = !(mode === "controlled" && interaction === "input" && trafficCurrent() && hasFrame);
    if (terminal.options.disableStdin !== disableStdin) terminal.options.disableStdin = disableStdin;
    if (terminal.textarea) {
      if (disableStdin && document.activeElement === terminal.textarea) terminal.blur();
      if (!disableStdin) clearDisabledSelectionScratch();
      terminal.textarea.disabled = disableStdin;
      terminal.textarea.tabIndex = disableStdin ? -1 : 0;
      if (disableStdin) terminal.textarea.setAttribute("aria-hidden", "true");
      else terminal.textarea.removeAttribute("aria-hidden");
    }
    const controlsLive = trafficCurrent() && hasFrame;
    if (release.disabled !== !controlsLive) release.disabled = !controlsLive;
    if (takeover.disabled !== !controlsLive) takeover.disabled = !controlsLive;
    if (acquire.disabled !== !controlsLive) acquire.disabled = !controlsLive;
  };

  const stopAutomaticRecovery = (): void => {
    recoveryPolicy.stop();
    window.clearTimeout(retry);
    window.clearTimeout(recoveryTimer);
    socket?.close();
    socket = undefined;
    mode = "retry_exhausted";
    instrument("recovery exhausted");
    updatePresentation();
  };

  const scheduleReconnect = (): void => {
    if (!recoveryPolicy.automatic) return;
    const now = performance.now();
    if (recovery.exhausted(now)) {
      stopAutomaticRecovery();
      return;
    }
    window.clearTimeout(retry);
    retry = window.setTimeout(connect, Math.min(TERMINAL_RETRY_DELAY_MS, recovery.remaining(now)));
  };

  function armRecoveryDeadline(): void {
    window.clearTimeout(recoveryTimer);
    const remaining = recovery.remaining(performance.now());
    recoveryTimer = window.setTimeout(() => {
      if (recoveryPolicy.automatic && recovery.exhausted(performance.now())) stopAutomaticRecovery();
      else if (recoveryPolicy.automatic) armRecoveryDeadline();
    }, remaining);
  }

  function connect(): void {
    window.clearTimeout(retry);
    if (!recoveryPolicy.automatic) return;
    if (recovery.exhausted(performance.now())) {
      stopAutomaticRecovery();
      return;
    }
    const previous = socket;
    socket = undefined;
    previous?.close();
    if (interaction !== "select" && !selection.copyAvailable) fit.fit();
    const url = webSocketURL("/api/terminal");
    url.searchParams.set("pane", target.entry.terminal.pane_id);
    url.searchParams.set("terminal", target.expectedTerminalID);
    url.searchParams.set("gap", String(target.expectedGap));
    url.searchParams.set("cols", String(Math.max(1, terminal.cols)));
    url.searchParams.set("rows", String(Math.max(1, terminal.rows)));
    const nextSocket = new WebSocket(url);
    socket = nextSocket;
    const nextGeneration = ++generation;
    lastTrafficAt = undefined;
    awaitingFullFrame = true;
    if (mode !== "herdr_not_running" && mode !== "incompatible") mode = "connecting";
    instrument("attachment started");
    updatePresentation();
    nextSocket.addEventListener("open", () => {
      if (socket !== nextSocket || generation !== nextGeneration) return;
      lastTrafficAt = performance.now();
      instrument("handshake");
      updatePresentation();
    });
    nextSocket.addEventListener("message", (event) => {
      if (socket !== nextSocket || generation !== nextGeneration) return;
      let message: TerminalMessage;
      try {
        message = JSON.parse(String(event.data)) as TerminalMessage;
      } catch {
        nextSocket.close();
        return;
      }
      lastTrafficAt = performance.now();
      if (message.type === "terminal.heartbeat") {
        updatePresentation();
        return;
      }
      if (message.type === "terminal.status" && message.mode) {
        const orderingEvidence = protocolOrderingEvidence(message);
        if (message.terminal_id && orderingEvidence) {
          target.expectedTerminalID = message.terminal_id;
          target.expectedGap = orderingEvidence.gap;
        }
        const reportedMode = message.mode;
        const disposition = recoveryPolicy.status(reportedMode, orderingEvidence);
        mode = disposition === "recover" ? "reconnecting" : reportedMode;
        if (disposition === "recover") {
          recovery.markLost(performance.now());
          armRecoveryDeadline();
        }
        instrument(`status ${reportedMode}`);
        updatePresentation();
        if (disposition !== "keep") {
          socket = undefined;
          lastTrafficAt = undefined;
          generation++;
          nextSocket.close();
        }
        if (disposition === "recover") scheduleReconnect();
        return;
      }
      if (message.type === "terminal.frame" && message.bytes) {
        if (awaitingFullFrame && !message.full) return;
        try {
          const decoded = window.atob(message.bytes);
          const bytes = Uint8Array.from(decoded, (character) => character.charCodeAt(0));
          if (message.full) {
            terminal.reset();
            awaitingFullFrame = false;
            hasFrame = true;
            recovery.markFullFrame();
            window.clearTimeout(recoveryTimer);
            instrument("fresh full frame");
          }
          // A terminal frame can overwrite any selected cell. Revoke Copy
          // before applying both full and delta mutations so cached text can
          // never outlive the visible terminal content that authorized it.
          clearVisibleSelection();
          terminal.write(bytes);
          updatePresentation();
        } catch {
          nextSocket.close();
        }
      }
    });
    nextSocket.addEventListener("close", () => {
      if (socket !== nextSocket || generation !== nextGeneration) return;
      socket = undefined;
      lastTrafficAt = undefined;
      if (recoveryPolicy.automatic) {
        recovery.markLost(performance.now());
        armRecoveryDeadline();
        if (mode !== "herdr_not_running" && mode !== "incompatible") mode = "reconnecting";
        instrument("attachment lost");
        updatePresentation();
        scheduleReconnect();
      }
    });
  }

  terminal.onData((data) => {
    if (mode === "controlled" && interaction === "input") send({ type: "terminal.input", text: data });
  });
  terminal.onSelectionChange(updateSelection);
  document.addEventListener("selectionchange", updateSelection);
  const removeTouchSelection = installTerminalTouchSelection({
    enabled: () => mode !== "controlled" || interaction === "select",
    host,
    selectionCleared: clearVisibleSelection,
    terminal,
  });
  host.addEventListener("wheel", (event) => {
    if (mode !== "controlled" || interaction !== "input" || event.deltaY === 0) return;
    event.preventDefault();
    send({ type: "terminal.scroll", direction: event.deltaY < 0 ? "up" : "down", lines: Math.max(1, Math.min(12, Math.round(Math.abs(event.deltaY) / 12))) });
  }, { passive: false });

  const applyViewport = (): void => {
    const viewport = window.visualViewport;
    app.style.setProperty("--terminal-viewport-height", `${Math.max(1, viewport?.height ?? window.innerHeight)}px`);
    app.style.setProperty("--terminal-viewport-top", `${Math.max(0, viewport?.offsetTop ?? 0)}px`);
  };

  function scheduleGeometry(): void {
    applyViewport();
    window.clearTimeout(resizeTimer);
    resizeTimer = window.setTimeout(() => {
      if (interaction === "select" || (mode !== "controlled" && selection.copyAvailable)) return;
      const oldCols = terminal.cols;
      const oldRows = terminal.rows;
      fit.fit();
      if (terminal.cols === oldCols && terminal.rows === oldRows) return;
      const size = `${terminal.cols}x${terminal.rows}`;
      if (size === lastResize) return;
      lastResize = size;
      if (mode === "controlled" && interaction === "input") {
        send({ type: "terminal.resize", cols: terminal.cols, rows: terminal.rows });
      }
    }, TERMINAL_GEOMETRY_SETTLE_MS);
  }

  const resize = new ResizeObserver(scheduleGeometry);
  resize.observe(host);
  window.visualViewport?.addEventListener("resize", scheduleGeometry);
  window.visualViewport?.addEventListener("scroll", scheduleGeometry);
  window.addEventListener("resize", scheduleGeometry);
  watchdog = window.setInterval(() => {
    const now = performance.now();
    if (socket?.readyState === WebSocket.OPEN && lastTrafficAt !== undefined && !hasRecentTraffic(lastTrafficAt, now)) {
      instrument("stale traffic suspected");
      recovery.markLost(now);
      armRecoveryDeadline();
      mode = "reconnecting";
      const stale = socket;
      socket = undefined;
      lastTrafficAt = undefined;
      stale.close();
      updatePresentation();
      scheduleReconnect();
      return;
    }
    if (recovery.exhausted(now) && recoveryPolicy.automatic) stopAutomaticRecovery();
    else updatePresentation();
  }, Math.min(500, TERMINAL_STALE_AFTER_MS / 4));
  updatePresentation();
  scheduleGeometry();
  connect();

  return {
    paneID: target.entry.terminal.pane_id,
    updateEntry(entry: TerminalEntry): void {
      updateTerminalIdentity(chrome, entry);
    },
    refreshTargetTruth(connection: HomeState["connection"], evidence: ProtocolOrderingEvidence | undefined): void {
      if (!recoveryPolicy.home(connection, evidence)) return;
      recovery.manualRetry(performance.now());
      armRecoveryDeadline();
      mode = "reconnecting";
      connect();
    },
    refreshOnlineState(): void {
      instrument(navigator.onLine ? "browser online hint" : "browser offline hint");
      updatePresentation();
      if (!socket && recoveryPolicy.automatic) connect();
    },
    destroy(): void {
      recoveryPolicy.stop();
      window.clearTimeout(retry);
      window.clearTimeout(recoveryTimer);
      window.clearTimeout(resizeTimer);
      window.clearInterval(watchdog);
      resize.disconnect();
      window.visualViewport?.removeEventListener("resize", scheduleGeometry);
      window.visualViewport?.removeEventListener("scroll", scheduleGeometry);
      window.removeEventListener("resize", scheduleGeometry);
      document.removeEventListener("selectionchange", updateSelection);
      removeTouchSelection();
      if (mode === "controlled" && trafficCurrent()) send({ type: "terminal.release" });
      socket?.close();
      terminal.dispose();
      app.style.removeProperty("--terminal-viewport-height");
      app.style.removeProperty("--terminal-viewport-top");
    },
  };
}

function leaveTerminal(): void {
  terminalView?.destroy();
  terminalView = undefined;
  selected = undefined;
  restoreHomePlace = true;
  window.location.hash = "";
}

window.addEventListener("hashchange", render);
window.addEventListener("online", () => {
  connectHome();
  terminalView?.refreshOnlineState();
});
window.addEventListener("offline", () => {
  terminalView?.refreshOnlineState();
});
window.addEventListener("pagehide", () => terminalView?.destroy());

connectHome();
render();

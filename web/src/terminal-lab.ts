import type { RendererKind, TerminalAdapter, TerminalDimensions } from "./terminal-lab/adapter";
import { rendererOptions } from "./terminal-lab/adapter";
import { ReaderInputQueue } from "./terminal-lab/reader-input";
import { ReaderView } from "./terminal-lab/reader-view";
import { LabSession, type SessionMode } from "./terminal-lab/session";
import { WTermAdapter } from "./terminal-lab/wterm-adapter";
import { XTermAdapter } from "./terminal-lab/xterm-adapter";

interface LabTarget {
  label: string;
  pane: string;
}

const keySequences: Record<string, string> = {
  escape: "\x1b",
  "ctrl-c": "\x03",
  "ctrl-d": "\x04",
  "ctrl-z": "\x1a",
  tab: "\t",
  left: "\x1b[D",
  up: "\x1b[A",
  down: "\x1b[B",
  right: "\x1b[C",
  enter: "\r",
  backspace: "\x7f",
};

function required<T extends Element>(selector: string): T {
  const node = document.querySelector<T>(selector);
  if (!node) throw new Error(`Terminal lab element is missing: ${selector}`);
  return node;
}

function requiredClosest<T extends Element>(node: Element, selector: string): T {
  const match = node.closest<T>(selector);
  if (!match) throw new Error(`Terminal lab ancestor is missing: ${selector}`);
  return match;
}

const rendererSelect = required<HTMLSelectElement>("#renderer");
const targetSelect = required<HTMLSelectElement>("#target");
const modeSelect = required<HTMLSelectElement>("#mode");
const modeField = requiredClosest<HTMLLabelElement>(modeSelect, "label");
const connectButton = required<HTMLButtonElement>("#connect");
const disconnectButton = required<HTMLButtonElement>("#disconnect");
const statusNode = required<HTMLElement>("#status");
const localSizeNode = required<HTMLElement>("#local-size");
const remoteSizeNode = required<HTMLElement>("#remote-size");
const viewportSizeNode = required<HTMLElement>("#viewport-size");
const surface = required<HTMLElement>("#surface");
const logNode = required<HTMLElement>("#log");

let adapter: TerminalAdapter | undefined;
let adapterGeneration = 0;
let frameCount = 0;
let logLines: string[] = [];
let reader: ReaderView | undefined;
let readerInput: ReaderInputQueue | undefined;
let session: LabSession | undefined;

for (const option of rendererOptions) {
  const node = document.createElement("option");
  node.value = option.kind;
  node.textContent = option.label;
  rendererSelect.append(node);
}
const labParameters = new URLSearchParams(location.search);
const requestedRenderer = labParameters.get("renderer");
const requestedPane = labParameters.get("pane");
const readerPreferred = (matchMedia("(pointer: coarse)").matches && !matchMedia("(any-pointer: fine)").matches) || innerWidth <= 700;
rendererSelect.value = rendererOptions.some((option) => option.kind === requestedRenderer)
  ? requestedRenderer as RendererKind
  : sessionStorage.getItem("terminal-lab.renderer.v2") ?? (readerPreferred ? "reader" : "xterm");
modeSelect.value = sessionStorage.getItem("terminal-lab.mode") ?? "observe";

function short(value: string, length = 100): string {
  const visible = value
    .replaceAll("\x1b", "\\e")
    .replaceAll("\r", "\\r")
    .replaceAll("\n", "\\n")
    .replaceAll("\t", "\\t");
  return visible.length > length ? `${visible.slice(0, length)}…` : visible;
}

function terminalSubmission(text: string): string[] {
  const safePaste = text.replaceAll("\x1b", "").replace(/\r\n|\r|\n/g, "\r");
  return [`\x1b[200~${safePaste}\x1b[201~`, "\r"];
}

function log(event: string, detail?: unknown): void {
  let suffix = "";
  if (detail !== undefined) {
    try {
      suffix = ` ${JSON.stringify(detail)}`;
    } catch {
      suffix = ` ${String(detail)}`;
    }
  }
  const timestamp = (performance.now() / 1000).toFixed(3).padStart(8, " ");
  logLines.push(`${timestamp} ${event}${suffix}`);
  if (logLines.length > 160) logLines = logLines.slice(-160);
  logNode.textContent = logLines.join("\n");
}

function setStatus(message: string): void {
  statusNode.textContent = message;
}

function createAdapter(kind: RendererKind): TerminalAdapter {
  if (kind === "xterm") return new XTermAdapter();
  if (kind === "reader") throw new Error("Reader does not use a terminal adapter");
  return new WTermAdapter(kind === "wterm-ghostty");
}

async function activateRenderer(kind: RendererKind): Promise<void> {
  session?.disconnect();
  session = undefined;
  readerInput?.clearTarget();
  readerInput = undefined;
  reader?.destroy();
  reader = undefined;
  adapter?.destroy();
  adapter = undefined;
  const generation = ++adapterGeneration;
  const mount = document.createElement("div");
  mount.className = "terminal-mount";
  surface.replaceChildren(mount);
  rendererSelect.disabled = true;
  connectButton.disabled = true;
  setStatus(`Loading ${kind}`);
  if (kind === "reader") {
    readerInput = new ReaderInputQueue({
      onBlocked(message) {
        reader?.inputBlocked(
          message,
          () => readerInput?.retry(false),
          () => readerInput?.retry(true),
        );
      },
      onLog: log,
      onSending: (chunks) => reader?.inputSending(chunks),
      onSent: () => reader?.inputSent(),
    });
    reader = new ReaderView(surface, {
      onLog: log,
      onStatus: setStatus,
      onSubmit: (text) => readerInput?.enqueueBatch(terminalSubmission(text)) ?? false,
    });
    modeField.hidden = true;
    sessionStorage.setItem("terminal-lab.renderer.v2", kind);
    const url = new URL(location.href);
    url.searchParams.set("renderer", kind);
    history.replaceState(null, "", url);
    localSizeNode.textContent = "Local Reader";
    setStatus("Ready to connect");
    log("reader.ready");
    rendererSelect.disabled = false;
    connectButton.disabled = false;
    return;
  }
  modeField.hidden = false;
  const next = createAdapter(kind);
  adapter = next;
  try {
    await next.mount(mount, {
      onData(data) {
        log("renderer.input", { characters: data.length, text: short(data) });
        session?.input(data);
      },
      onResize(dimensions) {
        localSizeNode.textContent = `Local ${dimensions.cols}×${dimensions.rows}`;
        session?.resize(dimensions);
      },
    });
    if (generation !== adapterGeneration) {
      next.destroy();
      return;
    }
    sessionStorage.setItem("terminal-lab.renderer.v2", kind);
    const url = new URL(location.href);
    url.searchParams.set("renderer", kind);
    history.replaceState(null, "", url);
    setStatus("Ready to connect");
    log("renderer.ready", { kind, ...next.dimensions() });
  } catch (error) {
    if (generation !== adapterGeneration) return;
    next.destroy();
    adapter = undefined;
    setStatus("Renderer failed");
    log("renderer.failed", { kind, error: String(error) });
  } finally {
    if (generation === adapterGeneration) {
      rendererSelect.disabled = false;
      connectButton.disabled = adapter === undefined;
    }
  }
}

async function connect(): Promise<void> {
  if (!adapter && !reader) return;
  const pane = targetSelect.value;
  if (!pane) {
    setStatus("Choose a terminal");
    return;
  }
  session?.disconnect();
  readerInput?.clearTarget();
  reader?.setInteractive(false);
  frameCount = 0;
  remoteSizeNode.textContent = "Remote —";
  const mode: SessionMode = reader ? "observe" : modeSelect.value as SessionMode;
  if (!reader) sessionStorage.setItem("terminal-lab.mode", mode);
  sessionStorage.setItem("terminal-lab.pane", pane);
  let dimensions = adapter?.dimensions() ?? reader?.dimensions() ?? { cols: 80, rows: 24 };
  if (reader) {
    connectButton.disabled = true;
    setStatus("Loading output");
    dimensions = await reader.open(pane);
    readerInput?.setTarget(pane, dimensions);
    localSizeNode.textContent = `PTY ${dimensions.cols}×${dimensions.rows} · Reader reflows locally`;
    connectButton.disabled = false;
  }
  session = new LabSession(mode, {
    onFrame(frame, bytes) {
      frameCount += 1;
      remoteSizeNode.textContent = `Remote ${frame.width}×${frame.height} · seq ${frame.seq} · ${frameCount} frames`;
      log("terminal.frame", {
        bytes: bytes.byteLength,
        full: frame.full,
        height: frame.height,
        seq: frame.seq,
        width: frame.width,
      });
      if (reader) reader.refreshSoon();
      else adapter?.write(bytes);
    },
    onLog: log,
    onStatus: setStatus,
  });
  session.connect(pane, dimensions);
  reader?.setInteractive(true);
  if (mode !== "observe") adapter?.focus();
}

function targetsFromHome(message: unknown): LabTarget[] {
  if (!message || typeof message !== "object") return [];
  const root = message as Record<string, unknown>;
  if (!root.home || typeof root.home !== "object") return [];
  const workspaces = (root.home as Record<string, unknown>).workspaces;
  if (!Array.isArray(workspaces)) return [];
  const targets: LabTarget[] = [];
  for (const workspaceValue of workspaces) {
    if (!workspaceValue || typeof workspaceValue !== "object") continue;
    const workspace = workspaceValue as Record<string, unknown>;
    if (!Array.isArray(workspace.tabs)) continue;
    for (const tabValue of workspace.tabs) {
      if (!tabValue || typeof tabValue !== "object") continue;
      const tab = tabValue as Record<string, unknown>;
      if (!Array.isArray(tab.terminals)) continue;
      for (const terminalValue of tab.terminals) {
        if (!terminalValue || typeof terminalValue !== "object") continue;
        const terminal = terminalValue as Record<string, unknown>;
        if (typeof terminal.pane_id !== "string") continue;
        const parts = [workspace.label, tab.label, terminal.title].filter((part): part is string => typeof part === "string" && part.length > 0);
        targets.push({ pane: terminal.pane_id, label: parts.join(" · ") || "Terminal" });
      }
    }
  }
  return targets;
}

function publishTargets(targets: LabTarget[]): void {
  const previous = targetSelect.value || requestedPane || sessionStorage.getItem("terminal-lab.pane") || "";
  targetSelect.replaceChildren();
  for (const target of targets) {
    const option = document.createElement("option");
    option.value = target.pane;
    option.textContent = target.label;
    targetSelect.append(option);
  }
  if (targets.some((target) => target.pane === previous)) targetSelect.value = previous;
  if (targets.length === 0) {
    const option = document.createElement("option");
    option.value = "";
    option.textContent = "No terminals available";
    targetSelect.append(option);
  }
  log("home.targets", { count: targets.length });
}

function connectHome(): void {
  const protocol = location.protocol === "https:" ? "wss:" : "ws:";
  const socket = new WebSocket(`${protocol}//${location.host}/api/home`);
  socket.addEventListener("message", (event) => {
    try {
      const parsed: unknown = JSON.parse(String(event.data));
      const targets = targetsFromHome(parsed);
      if (targets.length > 0 || (parsed && typeof parsed === "object" && "home" in parsed)) publishTargets(targets);
    } catch (error) {
      log("home.invalid", String(error));
    }
  });
  socket.addEventListener("close", () => log("home.closed"));
  socket.addEventListener("error", () => log("home.error"));
}

function updateVisualViewport(): void {
  const viewport = window.visualViewport;
  const dimensions = viewport
    ? { width: Math.round(viewport.width), height: Math.round(viewport.height), offsetTop: Math.round(viewport.offsetTop), scale: viewport.scale }
    : { width: window.innerWidth, height: window.innerHeight, offsetTop: 0, scale: 1 };
  viewportSizeNode.textContent = `Viewport ${dimensions.width}×${dimensions.height} · y ${dimensions.offsetTop} · ${dimensions.scale.toFixed(2)}×`;
  log("visualViewport", dimensions);
}

function instrumentInput(): void {
  surface.addEventListener("beforeinput", (event) => {
    const input = event as InputEvent;
    log("browser.beforeinput", { data: short(input.data ?? ""), inputType: input.inputType, composing: input.isComposing });
  }, true);
  surface.addEventListener("input", (event) => {
    const input = event as InputEvent;
    log("browser.input", { data: short(input.data ?? ""), inputType: input.inputType, composing: input.isComposing });
  }, true);
  surface.addEventListener("compositionstart", (event) => log("browser.compositionstart", short((event as CompositionEvent).data)), true);
  surface.addEventListener("compositionupdate", (event) => log("browser.compositionupdate", short((event as CompositionEvent).data)), true);
  surface.addEventListener("compositionend", (event) => log("browser.compositionend", short((event as CompositionEvent).data)), true);
  surface.addEventListener("keydown", (event) => {
    log("browser.keydown", { key: event.key, code: event.code, ctrl: event.ctrlKey, alt: event.altKey, meta: event.metaKey, repeat: event.repeat });
  }, true);
  surface.addEventListener("paste", (event) => {
    const text = event.clipboardData?.getData("text") ?? "";
    log("browser.paste", { characters: text.length, text: short(text) });
  }, true);
}

rendererSelect.addEventListener("change", () => void activateRenderer(rendererSelect.value as RendererKind));
connectButton.addEventListener("click", () => void connect());
disconnectButton.addEventListener("click", () => {
  session?.disconnect();
  session = undefined;
  readerInput?.clearTarget();
  reader?.setInteractive(false);
});
modeSelect.addEventListener("change", () => sessionStorage.setItem("terminal-lab.mode", modeSelect.value));

document.querySelectorAll<HTMLButtonElement>("[data-key]").forEach((button) => {
  button.addEventListener("click", () => {
    const sequence = keySequences[button.dataset.key ?? ""];
    if (!sequence) return;
    log("accessory.input", { key: button.dataset.key, text: short(sequence) });
    if (reader) readerInput?.enqueue(sequence);
    else {
      session?.input(sequence);
      if (modeSelect.value !== "observe") adapter?.focus();
    }
  });
});

required<HTMLButtonElement>("#page-up").addEventListener("click", () => {
  if (reader) reader.scrollPage(-1);
  else adapter?.scrollLines(-Math.max(1, adapter.dimensions().rows - 1));
});
required<HTMLButtonElement>("#page-down").addEventListener("click", () => {
  if (reader) reader.scrollPage(1);
  else adapter?.scrollLines(Math.max(1, adapter.dimensions().rows - 1));
});
required<HTMLButtonElement>("#focus").addEventListener("click", () => {
  if (reader) reader.focus();
  else adapter?.focus();
});
required<HTMLButtonElement>("#paste").addEventListener("click", async () => {
  try {
    const text = await navigator.clipboard.readText();
    const pasted = reader?.paste(text) ?? adapter?.paste(text) ?? false;
    if (!pasted) {
      setStatus("This renderer has no public Paste API; use Android's paste action");
      log("clipboard.paste-unavailable", { renderer: adapter?.kind, characters: text.length });
    }
  } catch (error) {
    setStatus("Clipboard read was denied");
    log("clipboard.read-failed", String(error));
  }
});
required<HTMLButtonElement>("#copy").addEventListener("click", async () => {
  const text = reader?.selection() ?? adapter?.selection() ?? "";
  if (!text) {
    setStatus("Nothing is selected");
    return;
  }
  try {
    await navigator.clipboard.writeText(text);
    setStatus(`Copied ${text.length} characters`);
    log("clipboard.copy", { characters: text.length });
  } catch (error) {
    setStatus("Clipboard write was denied");
    log("clipboard.write-failed", String(error));
  }
});
required<HTMLButtonElement>("#clear-log").addEventListener("click", () => {
  logLines = [];
  logNode.textContent = "";
});
required<HTMLButtonElement>("#copy-log").addEventListener("click", async () => {
  try {
    await navigator.clipboard.writeText(logLines.join("\n"));
    setStatus("Diagnostics copied");
  } catch (error) {
    setStatus("Clipboard write was denied");
    log("clipboard.write-failed", String(error));
  }
});

window.visualViewport?.addEventListener("resize", updateVisualViewport);
window.visualViewport?.addEventListener("scroll", updateVisualViewport);
window.addEventListener("resize", updateVisualViewport);
window.addEventListener("pagehide", () => {
  session?.disconnect();
  readerInput?.clearTarget();
});

instrumentInput();
updateVisualViewport();
connectHome();
void activateRenderer(rendererSelect.value as RendererKind);

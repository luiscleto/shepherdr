import { terminalKeySequences, terminalSubmission } from "./terminal-input";
import type { TerminalDimensions } from "./terminal-lab/adapter";
import { ReaderInputQueue } from "./terminal-lab/reader-input";
import { ReaderView } from "./terminal-lab/reader-view";
import { TerminalSession } from "./terminal-lab/session";
import { XTermAdapter } from "./terminal-lab/xterm-adapter";

export interface TerminalPageTarget {
  agentStatus?: string;
  paneID: string;
  terminalID: string;
  title: string;
}

interface TerminalPageOptions {
  onHome(): void;
}

const reconnectDelayMilliseconds = 1_000;

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

function action(text: string, run: () => void, className?: string): HTMLButtonElement {
  const node = element("button", className, text);
  node.type = "button";
  node.addEventListener("click", run);
  return node;
}

function readerRequested(): boolean {
  const override = new URLSearchParams(location.search).get("terminal");
  if (override === "desktop") return false;
  if (override === "reader") return true;
  return matchMedia("(pointer: coarse)").matches && !matchMedia("(any-pointer: fine)").matches;
}

export class TerminalPage {
  readonly paneID: string;
  readonly terminalID: string;

  #controlAction: HTMLButtonElement;
  #agentStatus: string | undefined;
  #connectionStatus = "Connecting";
  #controller: TerminalSession | undefined;
  #controllerAcquired = false;
  #controlAttempted = false;
  #destroyed = false;
  #host: HTMLElement;
  #observer: TerminalSession | undefined;
  #observerRetry: number | undefined;
  #reader: ReaderView | undefined;
  #readerInput: ReaderInputQueue | undefined;
  #readerMode: boolean;
  #status: HTMLSpanElement;
  #surface: HTMLDivElement;
  #title: HTMLHeadingElement;
  #xterm: XTermAdapter | undefined;

  constructor(host: HTMLElement, target: TerminalPageTarget, options: TerminalPageOptions) {
    this.#host = host;
    this.#agentStatus = target.agentStatus;
    this.paneID = target.paneID;
    this.terminalID = target.terminalID;
    this.#readerMode = readerRequested();

    const header = element("header", "terminal-header");
    const home = action("Home", options.onHome, "terminal-home");
    const title = element("div", "terminal-title");
    this.#title = element("h1", undefined, target.title);
    this.#status = element("span", "terminal-connection");
    this.#renderStatus();
    title.append(this.#title, this.#status);
    this.#controlAction = element("button", "terminal-control-action", "Control");
    this.#controlAction.type = "button";
    this.#controlAction.hidden = true;
    header.append(home, title, this.#controlAction);

    this.#surface = element("div", "terminal-production-surface");
    this.#surface.setAttribute("aria-label", target.title);
    host.className = `terminal-screen ${this.#readerMode ? "terminal-reader-page" : "terminal-desktop-page"}`;
    host.replaceChildren(header, this.#surface);

    if (this.#readerMode) void this.#startReader();
    else void this.#startDesktop();
  }

  updateTarget(title: string, agentStatus?: string): void {
    this.#title.textContent = title;
    this.#surface.setAttribute("aria-label", title);
    this.#agentStatus = agentStatus;
    this.#renderStatus();
  }

  destroy(): void {
    this.#destroyed = true;
    if (this.#observerRetry !== undefined) window.clearTimeout(this.#observerRetry);
    this.#observerRetry = undefined;
    const observer = this.#observer;
    this.#observer = undefined;
    observer?.disconnect();
    const controller = this.#controller;
    this.#controller = undefined;
    controller?.disconnect();
    this.#readerInput?.clearTarget();
    this.#readerInput = undefined;
    this.#reader?.destroy();
    this.#reader = undefined;
    this.#xterm?.destroy();
    this.#xterm = undefined;
    this.#host.replaceChildren();
  }

  async #startReader(): Promise<void> {
    const reader = new ReaderView(this.#surface, {
      onLog: () => undefined,
      onStatus: (message) => this.#setStatus(message),
      onSubmit: (text) => this.#readerInput?.enqueueBatch(terminalSubmission(text)) ?? false,
    }, { collapsibleComposer: true, endpoint: "/api/terminal/read" });
    this.#reader = reader;
    const input = new ReaderInputQueue({
      onBlocked: (message) => {
        reader.inputBlocked(
          message,
          () => input.retry(false),
          () => {
            if (window.confirm("Take control? The current controller will lose input.")) input.retry(true);
          },
        );
        reader.showComposer();
      },
      onLog: () => undefined,
      onSending: (count) => reader.inputSending(count),
      onSent: () => {
        reader.inputSent();
        reader.hideComposer();
      },
    }, { endpoint: "/api/terminal" });
    this.#readerInput = input;
    const dimensions = await reader.open(this.paneID, this.terminalID);
    if (this.#destroyed) return;
    input.setTarget(this.paneID, dimensions, this.terminalID);
    reader.setInteractive(true);
    const controls = this.#readerControls(reader, input);
    this.#host.append(controls);
    this.#connectObserver(dimensions);
  }

  #readerControls(reader: ReaderView, input: ReaderInputQueue): HTMLElement {
    const controls = element("nav", "terminal-command-bar");
    controls.setAttribute("aria-label", "Terminal commands");
    controls.append(action("Write text", () => reader.showComposer(), "terminal-write-text"));
    const keys: Array<[string, string, string]> = [
      ["escape", "Esc", "Escape"],
      ["ctrl-c", "Ctrl C", "Control C"],
      ["ctrl-d", "Ctrl D", "Control D"],
      ["ctrl-z", "Ctrl Z", "Control Z"],
      ["tab", "Tab", "Tab"],
      ["left", "←", "Left arrow"],
      ["up", "↑", "Up arrow"],
      ["down", "↓", "Down arrow"],
      ["right", "→", "Right arrow"],
      ["enter", "Enter", "Enter"],
      ["backspace", "⌫", "Backspace"],
    ];
    for (const [key, label, accessibleName] of keys) {
      const button = action(label, () => input.enqueue(terminalKeySequences[key]));
      button.setAttribute("aria-label", accessibleName);
      controls.append(button);
    }
    return controls;
  }

  async #startDesktop(): Promise<void> {
    const terminal = new XTermAdapter();
    this.#xterm = terminal;
    await terminal.mount(this.#surface, {
      onData: (data) => {
        if (this.#controllerAcquired) this.#controller?.input(data);
      },
      onResize: (dimensions) => {
        if (this.#controllerAcquired) this.#controller?.resize(dimensions);
      },
    });
    if (this.#destroyed) {
      terminal.destroy();
      return;
    }
    this.#connectObserver(terminal.dimensions());
  }

  #connectObserver(dimensions: TerminalDimensions): void {
    if (this.#destroyed) return;
    const previous = this.#observer;
    this.#observer = undefined;
    previous?.disconnect();
    let receivedFrame = false;
    const session = new TerminalSession("observe", {
      onFrame: (_frame, bytes) => {
        if (this.#observer !== session) return;
        if (this.#reader) this.#reader.refreshSoon();
        else this.#xterm?.write(bytes);
        if (receivedFrame) return;
        receivedFrame = true;
        this.#setStatus("Observing");
        if (!this.#readerMode && !this.#controlAttempted) this.#connectController(false);
      },
      onLog: (event) => {
        if (event !== "session.close" || this.#observer !== session) return;
        this.#observer = undefined;
        this.#releaseController();
        this.#scheduleObserverReconnect();
      },
      onStatus: (message) => {
        if (message === "Connecting") this.#setStatus("Connecting");
      },
    }, { endpoint: "/api/terminal", terminalID: this.terminalID });
    this.#observer = session;
    session.connect(this.paneID, dimensions);
  }

  #scheduleObserverReconnect(): void {
    if (this.#destroyed || this.#observerRetry !== undefined) return;
    this.#setStatus("Reconnecting");
    this.#observerRetry = window.setTimeout(() => {
      this.#observerRetry = undefined;
      const dimensions = this.#reader?.dimensions() ?? this.#xterm?.dimensions() ?? { cols: 80, rows: 24 };
      this.#controlAttempted = false;
      this.#connectObserver(dimensions);
    }, reconnectDelayMilliseconds);
  }

  #connectController(takeover: boolean): void {
    if (this.#destroyed || this.#readerMode || !this.#observer || this.#controller) return;
    this.#controlAttempted = true;
    this.#controlAction.hidden = true;
    this.#setStatus(takeover ? "Taking control" : "Requesting control");
    let lastStatus = "";
    const session = new TerminalSession(takeover ? "takeover" : "control", {
      onFrame: () => {
        if (this.#controller !== session || this.#controllerAcquired) return;
        this.#controllerAcquired = true;
        this.#setStatus("Controlling");
        this.#xterm?.focus();
      },
      onLog: (event) => {
        if (event !== "session.close" || this.#controller !== session) return;
        this.#controller = undefined;
        const wasControlling = this.#controllerAcquired;
        this.#controllerAcquired = false;
        if (this.#destroyed) return;
        if (lastStatus.includes("already has an attached client")) {
          this.#setStatus("Controlled elsewhere · observing");
          this.#showControlAction("Take over", true);
        } else {
          this.#setStatus(wasControlling ? "Control lost · observing" : "Observing");
          this.#showControlAction("Control", false);
        }
      },
      onStatus: (message) => {
        lastStatus = message;
      },
    }, { endpoint: "/api/terminal", terminalID: this.terminalID });
    this.#controller = session;
    session.connect(this.paneID, this.#xterm?.dimensions() ?? { cols: 80, rows: 24 });
  }

  #showControlAction(label: string, takeover: boolean): void {
    this.#controlAction.textContent = label;
    this.#controlAction.hidden = false;
    this.#controlAction.onclick = () => {
      if (takeover && !window.confirm("Take control? The current controller will lose input.")) return;
      this.#connectController(takeover);
    };
  }

  #releaseController(): void {
    const controller = this.#controller;
    this.#controller = undefined;
    this.#controllerAcquired = false;
    controller?.disconnect();
  }

  #setStatus(message: string): void {
    this.#connectionStatus = message;
    this.#renderStatus();
  }

  #renderStatus(): void {
    if (this.#destroyed) return;
    this.#status.textContent = [this.#agentStatus, this.#connectionStatus].filter(Boolean).join(" · ");
  }
}

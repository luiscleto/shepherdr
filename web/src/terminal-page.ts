import { terminalKeySequences, terminalSubmission } from "./terminal-input";
import { settingsAction } from "./settings-action";
import type { TerminalDimensions } from "./terminal/adapter";
import { terminalReaderForDevice } from "./terminal/device";
import { sendTerminalFiles, uploadStateAfterLastFileRemoved } from "./terminal/file-uploads";
import { nextTerminalOwnership, terminalOwnershipAction, type TerminalOwnership } from "./terminal/ownership";
import { readerActionAvailability, type ReaderInputState } from "./terminal/reader-availability";
import { ReaderInputQueue } from "./terminal/reader-input";
import { ReaderView, readerMessageAction } from "./terminal/reader-view";
import { TerminalSession } from "./terminal/session";
import { XTermAdapter } from "./terminal/xterm-adapter";

export interface TerminalPageTarget {
  agentStatus?: string;
  paneID: string;
  terminalID: string;
  title: string;
  workspaceID: string;
}

interface TerminalPageOptions {
  onHome(): void;
  onNotifications?(): void;
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

export class TerminalPage {
  readonly paneID: string;
  readonly terminalID: string;

  #agentStatus: string | undefined;
  #commandButtons: HTMLButtonElement[] = [];
  #connectionStatus = "Connecting";
  #controlAction: HTMLButtonElement;
  #controller: TerminalSession | undefined;
  #desktopAutoAcquire = true;
  #destroyed = false;
  #hasObserved = false;
  #host: HTMLElement;
  #newOutput = false;
  #observer: TerminalSession | undefined;
  #observerReady = false;
  #observerRetry: number | undefined;
  #ownership: TerminalOwnership = "waiting";
  #reader: ReaderView | undefined;
  #readerInput: ReaderInputQueue | undefined;
  #readerMode: boolean;
  #readerResizePending = false;
  #readerSizer: TerminalSession | undefined;
  #readerTextQueued = false;
  #uploadAbort: AbortController | undefined;
  #uploadAttempt = 0;
  #uploadState: ReaderInputState = "ready";
  #status: HTMLSpanElement;
  #surface: HTMLDivElement;
  #title: HTMLHeadingElement;
  #xterm: XTermAdapter | undefined;
  #workspaceID: string;

  constructor(host: HTMLElement, target: TerminalPageTarget, options: TerminalPageOptions) {
    this.#host = host;
    this.#agentStatus = target.agentStatus;
    this.paneID = target.paneID;
    this.terminalID = target.terminalID;
    this.#workspaceID = target.workspaceID;
    this.#readerMode = terminalReaderForDevice();

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
    const notifications = settingsAction(document, () => options.onNotifications?.(), "terminal-notifications");
    header.append(home, title, notifications, this.#controlAction);

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
    this.#syncReaderActions();
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
    const readerSizer = this.#readerSizer;
    this.#readerSizer = undefined;
    readerSizer?.disconnect();
    this.#uploadAttempt++;
    this.#uploadAbort?.abort();
    this.#uploadAbort = undefined;
    this.#reader?.destroy();
    this.#reader = undefined;
    this.#xterm?.destroy();
    this.#xterm = undefined;
    this.#host.replaceChildren();
  }

  async #startReader(): Promise<void> {
    const reader = new ReaderView(this.#surface, {
      onLog: () => undefined,
      onNewOutput: (available) => {
        this.#newOutput = available;
        this.#renderStatus();
      },
      onPendingFilesEmpty: () => {
        const nextState = uploadStateAfterLastFileRemoved(this.#uploadState);
        if (nextState === this.#uploadState) return;
        this.#uploadState = nextState;
        this.#reader?.clearInputRecovery();
        this.#syncReaderActions();
        this.#renderReaderStatus();
        this.#startReaderResize();
      },
      onResize: (dimensions) => {
        this.#readerInput?.resize(dimensions);
        this.#readerResizePending = true;
        this.#startReaderResize();
      },
      onStatus: (message) => this.#setStatus(message),
      onSubmit: (text, files) => {
        if (files.length > 0) {
          void this.#sendFiles(false);
          return true;
        }
        const queued = this.#readerInput?.enqueueBatch(terminalSubmission(text)) ?? false;
        if (queued) this.#readerTextQueued = true;
        return queued;
      },
    }, { collapsibleComposer: true, endpoint: "/api/terminal/read" });
    this.#reader = reader;
    const input = new ReaderInputQueue({
      onFailed: (message) => reader.inputFailed(message, () => input.retry(false)),
      onForwarded: () => {
        reader.inputForwarded(this.#readerTextQueued);
        this.#readerTextQueued = false;
        reader.hideComposer();
      },
      onLog: () => undefined,
      onOccupied: (message) => reader.inputOccupied(message, () => {
        if (window.confirm("Take control? The current controller will lose input.")) input.retry(true);
      }),
      onSending: (count) => reader.inputSending(count),
      onState: () => {
        this.#syncReaderActions();
        this.#renderReaderStatus();
        this.#startReaderResize();
      },
      onUncertain: (message) => {
        this.#readerTextQueued = false;
        reader.inputUncertain(message, () => input.dismissUncertain());
      },
    }, { endpoint: "/api/terminal" });
    this.#readerInput = input;
    const dimensions = await reader.open(this.paneID, this.terminalID);
    if (this.#destroyed) return;
    input.setTarget(this.paneID, dimensions, this.terminalID);
    const controls = this.#readerControls(reader, input);
    this.#host.append(controls);
    this.#syncReaderActions();
    this.#connectObserver(dimensions);
  }

  #readerControls(reader: ReaderView, input: ReaderInputQueue): HTMLElement {
    const controls = element("nav", "terminal-command-bar");
    controls.setAttribute("aria-label", "Terminal commands");
    this.#commandButtons.push(readerMessageAction(() => reader.showComposer()));
    const keys: Array<[string, string, string]> = [
      ["escape", "Esc", "Escape"],
      ["enter", "Enter", "Enter"],
      ["left", "←", "Left arrow"],
      ["up", "↑", "Up arrow"],
      ["down", "↓", "Down arrow"],
      ["right", "→", "Right arrow"],
      ["ctrl-c", "Ctrl C", "Control C"],
      ["ctrl-d", "Ctrl D", "Control D"],
      ["ctrl-z", "Ctrl Z", "Control Z"],
      ["tab", "Tab", "Tab"],
      ["backspace", "⌫", "Backspace"],
    ];
    for (const [key, label, accessibleName] of keys) {
      const button = action(label, () => input.enqueue(terminalKeySequences[key]));
      button.setAttribute("aria-label", accessibleName);
      this.#commandButtons.push(button);
    }
    controls.append(...this.#commandButtons);
    return controls;
  }

  #syncReaderActions(): void {
    if (!this.#readerMode) return;
    const state = this.#uploadState !== "ready" ? this.#uploadState : this.#readerInput?.state() ?? "ready";
    const fileRecovery = this.#uploadState !== "ready" && this.#uploadState !== "uncertain";
    const availability = readerActionAvailability(this.#observerReady && !this.#readerSizer, state);
    if (fileRecovery && this.#agentStatus === undefined) availability.recover = false;
    this.#reader?.setActionAvailability(availability);
    this.#reader?.setFileSelectionAvailable(this.#observerReady && !this.#readerSizer && this.#agentStatus !== undefined && state === "ready");
    for (const button of this.#commandButtons) button.disabled = !availability.send;
  }

  #renderReaderStatus(): void {
    if (!this.#observerReady) {
      this.#setStatus(this.#hasObserved ? "Reconnecting" : "Connecting");
      return;
    }
    const inputState = this.#uploadState !== "ready" ? this.#uploadState : this.#readerInput?.state() ?? "ready";
    const status = ({
      failed: "Could not send · observing",
      forwarding: "Forwarding input",
      occupied: "Controlled elsewhere · observing",
      ready: "Observing",
      requesting: "Requesting control",
      uncertain: "Delivery uncertain · observing",
    } satisfies Record<ReaderInputState, string>)[inputState];
    this.#setStatus(this.#uploadState === "requesting" ? "Sending files" : status);
  }

  async #sendFiles(takeover: boolean): Promise<void> {
    const reader = this.#reader;
    if (!reader || this.#uploadState !== "ready" && this.#uploadState !== "failed" && this.#uploadState !== "occupied" ||
      !this.#observerReady || this.#agentStatus === undefined || !reader.hasPendingFiles()) return;
    if (reader.viewportDimensions()) this.#readerResizePending = true;
    this.#uploadState = "requesting";
    const attempt = ++this.#uploadAttempt;
    const abort = new AbortController();
    this.#uploadAbort?.abort();
    this.#uploadAbort = abort;
    reader.filesSending();
    this.#syncReaderActions();
    this.#renderReaderStatus();
    try {
      const outcome = await sendTerminalFiles({
        paneID: this.paneID,
        terminalID: this.terminalID,
        workspaceID: this.#workspaceID,
      }, reader.draftText(), reader.pendingFiles(), takeover, abort.signal);
      if (this.#destroyed || attempt !== this.#uploadAttempt) return;
      this.#uploadAbort = undefined;
      switch (outcome.result) {
      case "forwarded":
        this.#uploadState = "ready";
        reader.inputForwarded(true, true);
        reader.hideComposer();
        break;
      case "occupied":
        this.#uploadState = "occupied";
        reader.inputOccupied(outcome.message, () => {
          if (window.confirm("Take control? The current controller will lose input.")) void this.#sendFiles(true);
        });
        break;
      case "unknown":
        this.#uploadState = "uncertain";
        reader.inputUncertain(outcome.message, () => {
          this.#uploadState = "ready";
          this.#syncReaderActions();
          this.#renderReaderStatus();
          this.#startReaderResize();
        });
        break;
      default:
        this.#uploadState = "failed";
        reader.inputFailed(outcome.message, () => void this.#sendFiles(false));
      }
    } catch {
      if (this.#destroyed || attempt !== this.#uploadAttempt) return;
      this.#uploadAbort = undefined;
      this.#uploadState = "uncertain";
      reader.inputUncertain("The result could not be confirmed. Check the terminal before sending the files again.", () => {
        this.#uploadState = "ready";
        this.#syncReaderActions();
        this.#renderReaderStatus();
        this.#startReaderResize();
      });
    }
    this.#syncReaderActions();
    this.#renderReaderStatus();
    this.#startReaderResize();
  }

  async #startDesktop(): Promise<void> {
    const terminal = new XTermAdapter();
    this.#xterm = terminal;
    await terminal.mount(this.#surface, {
      onData: (data) => {
        if (this.#ownership === "controlling") this.#controller?.input(data);
      },
      onResize: (dimensions) => {
        if (this.#ownership === "controlling") this.#controller?.resize(dimensions);
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
    this.#observerReady = false;
    let receivedFrame = false;
    const session = new TerminalSession("observe", {
      onFrame: (frame, bytes) => {
        if (this.#observer !== session) return;
        if (this.#reader) {
          if (!receivedFrame && this.#hasObserved) this.#reader.connectionReset();
          this.#reader.refreshSoon();
        } else {
          if (frame.full) this.#xterm?.replace(bytes);
          else this.#xterm?.write(bytes);
        }
        if (receivedFrame) return;
        receivedFrame = true;
        this.#observerReady = true;
        this.#ownership = nextTerminalOwnership(this.#ownership, "observer-ready");
        this.#hasObserved = true;
        if (this.#readerMode) {
          this.#syncReaderActions();
          this.#renderReaderStatus();
          if (this.#reader?.viewportDimensions()) this.#readerResizePending = true;
          this.#startReaderResize();
        } else {
          this.#renderDesktopOwnership();
          if (this.#desktopAutoAcquire) {
            this.#desktopAutoAcquire = false;
            this.#connectController(false);
          }
        }
      },
      onLog: (event) => {
        if (event !== "session.close" || this.#observer !== session) return;
        this.#observer = undefined;
        this.#observerReady = false;
        const readerSizer = this.#readerSizer;
        this.#readerSizer = undefined;
        readerSizer?.disconnect();
        this.#reader?.pauseLiveRefresh();
        this.#syncReaderActions();
        if (this.#controller || this.#ownership === "controlling" || this.#ownership === "requesting") this.#desktopAutoAcquire = true;
        this.#releaseController(false);
        this.#ownership = nextTerminalOwnership(this.#ownership, "observer-lost");
        this.#scheduleObserverReconnect();
      },
      onStatus: (message) => {
        if (message === "Connecting") this.#setStatus("Connecting");
      },
    }, { endpoint: "/api/terminal", terminalID: this.terminalID });
    this.#observer = session;
    session.connect(this.paneID, dimensions);
  }

  #startReaderResize(): void {
    const dimensions = this.#reader?.viewportDimensions();
    if (!this.#readerMode || !dimensions || !this.#readerResizePending || this.#readerSizer || !this.#observerReady ||
      this.#readerInput?.state() !== "ready" || this.#uploadState !== "ready") return;
    this.#readerResizePending = false;
    let acquired = false;
    let session: TerminalSession;
    const finish = () => {
      if (this.#readerSizer !== session) return;
      this.#readerSizer = undefined;
      session.disconnect();
      this.#reader?.refreshSoon();
      this.#syncReaderActions();
      this.#renderReaderStatus();
      this.#startReaderResize();
    };
    session = new TerminalSession("control", {
      onFrame: () => {
        if (acquired) return;
        acquired = true;
        finish();
      },
      onLog: (event) => {
        if (event === "session.close") finish();
      },
      onStatus: () => undefined,
    }, { endpoint: "/api/terminal", terminalID: this.terminalID });
    this.#readerSizer = session;
    this.#syncReaderActions();
    session.connect(this.paneID, dimensions);
  }

  #scheduleObserverReconnect(): void {
    if (this.#destroyed || this.#observerRetry !== undefined) return;
    this.#setStatus("Reconnecting");
    this.#observerRetry = window.setTimeout(() => {
      this.#observerRetry = undefined;
      const dimensions = this.#reader?.dimensions() ?? this.#xterm?.dimensions() ?? { cols: 80, rows: 24 };
      this.#connectObserver(dimensions);
    }, reconnectDelayMilliseconds);
  }

  #connectController(takeover: boolean): void {
    if (this.#destroyed || this.#readerMode || !this.#observerReady || !this.#observer || this.#controller) return;
    this.#ownership = nextTerminalOwnership(this.#ownership, "request");
    this.#renderDesktopOwnership(takeover ? "Taking control" : "Requesting control");
    let lastStatus = "";
    const session = new TerminalSession(takeover ? "takeover" : "control", {
      onFrame: () => {
        if (this.#controller !== session || this.#ownership !== "requesting") return;
        this.#ownership = nextTerminalOwnership(this.#ownership, "acquired");
        this.#renderDesktopOwnership();
        this.#xterm?.focus();
      },
      onLog: (event) => {
        if (event !== "session.close" || this.#controller !== session) return;
        this.#controller = undefined;
        if (this.#destroyed || !this.#observerReady) return;
        this.#ownership = nextTerminalOwnership(
          this.#ownership,
          lastStatus.includes("already has an attached client") ? "occupied" : "failed",
        );
        this.#renderDesktopOwnership();
      },
      onStatus: (message) => {
        lastStatus = message;
      },
    }, { endpoint: "/api/terminal", terminalID: this.terminalID });
    this.#controller = session;
    session.connect(this.paneID, this.#xterm?.dimensions() ?? { cols: 80, rows: 24 });
  }

  #releaseController(explicit: boolean): void {
    const controller = this.#controller;
    this.#controller = undefined;
    controller?.disconnect();
    if (explicit) this.#desktopAutoAcquire = false;
    if (!this.#observerReady) return;
    this.#ownership = nextTerminalOwnership(this.#ownership, "release");
    this.#renderDesktopOwnership();
  }

  #renderDesktopOwnership(temporaryStatus?: string): void {
    if (this.#readerMode) return;
    const control = terminalOwnershipAction(this.#ownership);
    this.#controlAction.hidden = control === undefined;
    this.#controlAction.onclick = null;
    if (control === "release") {
      this.#controlAction.textContent = "Release";
      this.#controlAction.onclick = () => this.#releaseController(true);
    } else if (control === "takeover") {
      this.#controlAction.textContent = "Take over";
      this.#controlAction.onclick = () => {
        if (window.confirm("Take control? The current controller will lose input.")) this.#connectController(true);
      };
    } else if (control === "control") {
      this.#controlAction.textContent = "Control";
      this.#controlAction.onclick = () => this.#connectController(false);
    }
    const status = temporaryStatus ?? ({
      controlling: "Controlling",
      observing: "Observing",
      occupied: "Controlled elsewhere · observing",
      requesting: "Requesting control",
      waiting: "Connecting",
    } satisfies Record<TerminalOwnership, string>)[this.#ownership];
    this.#setStatus(status);
  }

  #setStatus(message: string): void {
    this.#connectionStatus = message;
    this.#renderStatus();
  }

  #renderStatus(): void {
    if (this.#destroyed) return;
    this.#status.textContent = [this.#agentStatus, this.#connectionStatus, this.#newOutput ? "New output" : undefined].filter(Boolean).join(" · ");
  }
}

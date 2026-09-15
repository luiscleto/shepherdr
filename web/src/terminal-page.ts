import { terminalKeySequences, terminalSubmission } from "./terminal-input";
import { settingsAction } from "./settings-action";
import type { TerminalDimensions } from "./terminal/adapter";
import { terminalReaderForDevice } from "./terminal/device";
import { sendTerminalFiles, uploadStateAfterLastFileRemoved } from "./terminal/file-uploads";
import { terminalOwnershipAction, type TerminalOwnership } from "./terminal/ownership";
import { readerActionAvailability, type ReaderInputState } from "./terminal/reader-availability";
import { ReaderInputQueue } from "./terminal/reader-input";
import { ReaderView, readerMessageAction, setIconButton } from "./terminal/reader-view";
import { TerminalSession, type SessionMode } from "./terminal/session";
import { readTerminalView, saveTerminalView } from "./terminal/view-preference";
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

function element<K extends keyof HTMLElementTagNameMap>(tag: K, className?: string, text?: string): HTMLElementTagNameMap[K] {
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
  #workspaceID: string;
  #host: HTMLElement;
  #surface: HTMLDivElement;
  #title: HTMLHeadingElement;
  #status: HTMLSpanElement;
  #controlAction: HTMLButtonElement;
  #toggle: HTMLButtonElement | undefined;
  #message: HTMLButtonElement | undefined;
  #shortcuts: HTMLButtonElement[] = [];
  #mobile: boolean;
  #readerVisible: boolean;
  #reader: ReaderView | undefined;
  #readerInput: ReaderInputQueue | undefined;
  #readerTextQueued = false;
  #xterm: XTermAdapter;
  #terminalMount: HTMLDivElement;
  #layoutObserver: ResizeObserver | undefined;
  #session: TerminalSession | undefined;
  #typedRequests = new Set<number>();
  #typingTimer: number | undefined;
  #ownership: TerminalOwnership = "waiting";
  #desiredControl = true;
  #ready = false;
  #destroyed = false;
  #generation = 0;
  #retry: number | undefined;
  #lastDimensions: TerminalDimensions | undefined;
  #hasConnected = false;
  #newOutput = false;
  #uploadAbort: AbortController | undefined;
  #uploadAttempt = 0;
  #uploadState: ReaderInputState = "ready";
  #uploadIntent: "insert" | "submit" = "submit";
  #viewportChanged = (): void => {
    if (this.#mobile && window.visualViewport) {
      this.#host.style.height = `${window.visualViewport.height}px`;
      this.#host.style.top = `${window.visualViewport.offsetTop}px`;
    }
    this.#layout();
  };

  constructor(host: HTMLElement, target: TerminalPageTarget, options: TerminalPageOptions) {
    this.#host = host;
    this.#agentStatus = target.agentStatus;
    this.paneID = target.paneID;
    this.terminalID = target.terminalID;
    this.#workspaceID = target.workspaceID;
    this.#mobile = terminalReaderForDevice();
    // Mobile scroll gestures navigate Herdr history. Local frame scrollback is
    // reset on full frames; disabling it also removes FitAddon's unused gutter.
    this.#xterm = new XTermAdapter(this.#mobile ? 0 : 10_000);
    this.#readerVisible = this.#mobile && (readTerminalView() ?? "reader") === "reader";
    const header = element("header", "terminal-header");
    const title = element("div", "terminal-title");
    this.#title = element("h1", undefined, target.title);
    this.#status = element("span", "terminal-connection");
    title.append(this.#title, this.#status);
    this.#controlAction = action("Control", () => this.#ownershipAction(), "terminal-control-action");
    this.#controlAction.hidden = true;
    const settings = settingsAction(document, () => options.onNotifications?.(), "terminal-notifications");
    header.append(action("Home", options.onHome, "terminal-home"), title);
    if (this.#mobile) {
      this.#toggle = action("", () => this.#switchView(), "terminal-view-toggle");
      header.append(this.#toggle);
    }
    header.append(settings);
    if (!this.#mobile) header.append(this.#controlAction);
    this.#surface = element("div", "terminal-production-surface");
    this.#surface.setAttribute("aria-label", target.title);
    this.#terminalMount = element("div", "terminal-full-mount");
    this.#surface.append(this.#terminalMount);
    host.className = `terminal-screen ${this.#mobile ? "terminal-reader-page terminal-shared-page" : "terminal-desktop-page"}`;
    host.replaceChildren(header, this.#surface);
    if (this.#mobile) this.#mountReader();
    this.#render();
    void this.#mountTerminal();
  }

  updateTarget(title: string, agentStatus?: string): void {
    this.#title.textContent = title;
    this.#surface.setAttribute("aria-label", title);
    this.#agentStatus = agentStatus;
    this.#render();
  }

  destroy(): void {
    this.#destroyed = true;
    this.#generation++;
    if (this.#retry !== undefined) window.clearTimeout(this.#retry);
    this.#retry = undefined;
    void this.#session?.disconnect();
    this.#session = undefined;
    this.#readerInput?.clearTarget();
    if (this.#typingTimer !== undefined) window.clearTimeout(this.#typingTimer);
    this.#typedRequests.clear();
    this.#uploadAttempt++;
    this.#uploadAbort?.abort();
    this.#layoutObserver?.disconnect();
    window.visualViewport?.removeEventListener("resize", this.#viewportChanged);
    window.visualViewport?.removeEventListener("scroll", this.#viewportChanged);
    this.#reader?.destroy();
    this.#xterm.destroy();
    this.#host.replaceChildren();
    this.#host.style.height = "";
    this.#host.style.top = "";
  }

  #mountReader(): void {
    const mount = element("div", "terminal-reader-mount");
    this.#surface.append(mount);
    const reader = new ReaderView(mount, {
      onLog: () => undefined,
      onStatus: () => undefined,
      onNewOutput: (available) => { this.#newOutput = available; this.#render(); },
      onPendingFilesEmpty: () => {
        const previous = this.#uploadState;
        this.#uploadState = uploadStateAfterLastFileRemoved(this.#uploadState);
        if (previous !== this.#uploadState) reader.clearInputRecovery();
        this.#render();
      },
      onLayout: () => this.#layout(),
      onOpenTerminal: () => { if (this.#readerVisible) this.#switchView(); },
      onInsertFiles: () => { void this.#sendFiles("insert", false); return true; },
      onSubmit: (text, files) => {
        if (files.length) { void this.#sendFiles("submit", false); return true; }
        this.#readerTextQueued = true;
        const accepted = this.#readerInput?.enqueueBatch(terminalSubmission(text)) ?? false;
        if (!accepted) this.#readerTextQueued = false;
        return accepted;
      },
    }, { collapsibleComposer: true, endpoint: "/api/terminal/read" });
    this.#reader = reader;
    const input = new ReaderInputQueue({
      onFailed: (message) => reader.inputFailed(message, () => { void input.retry(false); }),
      onForwarded: () => {
        reader.inputForwarded(this.#readerTextQueued);
        if (this.#readerTextQueued) reader.hideComposer();
        this.#readerTextQueued = false;
        this.#render();
      },
      onLog: () => undefined,
      onOccupied: (message) => reader.inputOccupied(message, () => {
        if (window.confirm("Take control? The current controller will lose input.")) void input.retry(true);
      }),
      onSending: (count) => reader.inputSending(count),
      onState: () => this.#render(),
      onUncertain: (message) => {
        this.#readerTextQueued = false;
        reader.inputUncertain(message, () => input.dismissUncertain());
      },
    }, {
      session: () => this.#ownership === "controlling" ? this.#session : undefined,
      occupied: () => this.#ownership === "occupied",
      acquire: (takeover) => this.#acquire(takeover),
    });
    this.#readerInput = input;
    const controls = element("nav", "terminal-command-bar");
    controls.setAttribute("aria-label", "Terminal commands");
    this.#message = readerMessageAction(() => {
      if (this.#readerVisible) reader.showComposer();
      else reader.showFileInsertion();
    });
    controls.append(this.#message, this.#controlAction);
    const keys: Array<[string, string, string]> = [
      ["escape", "Esc", "Escape"], ["enter", "Enter", "Enter"],
      ["left", "←", "Left arrow"], ["up", "↑", "Up arrow"], ["down", "↓", "Down arrow"], ["right", "→", "Right arrow"],
      ["ctrl-c", "Ctrl C", "Control C"], ["ctrl-d", "Ctrl D", "Control D"], ["ctrl-z", "Ctrl Z", "Control Z"],
      ["tab", "Tab", "Tab"], ["backspace", "⌫", "Backspace"],
    ];
    for (const [key, label, name] of keys) {
      const button = action(label, () => input.enqueue(terminalKeySequences[key]));
      button.setAttribute("aria-label", name);
      this.#shortcuts.push(button);
      controls.append(button);
    }
    this.#host.append(controls);
    reader.setPresentation(this.#readerVisible);
    void reader.open(this.paneID, this.terminalID);
  }

  async #mountTerminal(): Promise<void> {
    await this.#xterm.mount(this.#terminalMount, {
      onData: (data) => {
        if (!this.#canType()) return;
        const requestID = this.#session?.input(data);
        if (this.#mobile && requestID !== undefined) {
          this.#typedRequests.add(requestID);
          if (this.#typingTimer === undefined) this.#typingTimer = window.setTimeout(() => this.#unconfirmedTyping(), 2_500);
          this.#render();
        }
      },
      onResize: (dimensions) => {
        this.#lastDimensions = dimensions;
        if (this.#ownership === "controlling") this.#session?.resize(dimensions);
        if (!this.#hasConnected && this.#ready) void this.#connect("control");
      },
      onScroll: (scroll) => this.#canType() && this.#session?.scroll(scroll) === true,
    });
    if (this.#destroyed) { this.#xterm.destroy(); return; }
    this.#ready = true;
    if (this.#mobile) {
      this.#layoutObserver = new ResizeObserver(() => this.#layout());
      this.#layoutObserver.observe(this.#surface);
      const scroll = this.#surface.querySelector(".reader-scroll");
      if (scroll) this.#layoutObserver.observe(scroll);
      window.visualViewport?.addEventListener("resize", this.#viewportChanged);
      window.visualViewport?.addEventListener("scroll", this.#viewportChanged);
    }
    this.#viewportChanged();
    this.#present();
    if (this.#lastDimensions && !this.#hasConnected) void this.#connect("control");
  }

  #layout(): void {
    if (!this.#ready || this.#destroyed) return;
    const height = this.#reader?.contentHeight();
    if (height !== undefined && Number.isFinite(height) && height > 0) this.#terminalMount.style.height = `${height}px`;
    this.#xterm.fit();
  }

  #switchView(): void {
    if (this.#uploadState === "requesting") return;
    this.#readerVisible = !this.#readerVisible;
    saveTerminalView(this.#readerVisible ? "reader" : "terminal");
    this.#reader?.setPresentation(this.#readerVisible);
    this.#present();
    this.#render();
  }

  #present(): void {
    this.#terminalMount.style.visibility = this.#readerVisible ? "hidden" : "visible";
    this.#terminalMount.inert = this.#readerVisible;
    this.#terminalMount.style.pointerEvents = this.#readerVisible ? "none" : "auto";
    if (this.#toggle) setIconButton(this.#toggle, this.#readerVisible ? "Full terminal" : "Reader", this.#readerVisible ? "terminal" : "phone");
    if (this.#message) {
      this.#message.textContent = this.#readerVisible ? "Message" : "📎";
      this.#message.setAttribute("aria-label", this.#readerVisible ? "Message" : "Attach files");
    }
    this.#xterm.setInputEnabled(this.#canType());
  }

  #canType(): boolean {
    return !this.#readerVisible && this.#ownership === "controlling" && this.#uploadState !== "requesting" &&
      (!this.#mobile || this.#readerInput?.state() === "ready");
  }

  async #connect(mode: SessionMode): Promise<boolean> {
    if (this.#destroyed || !this.#lastDimensions) return false;
    const generation = ++this.#generation;
    this.#hasConnected = true;
    if (this.#retry !== undefined) window.clearTimeout(this.#retry);
    this.#retry = undefined;
    const previous = this.#session;
    this.#session = undefined;
    if (previous) this.#readerInput?.disconnected(previous);
    this.#unconfirmedTyping();
    this.#ownership = mode === "observe" ? (this.#ownership === "occupied" ? "occupied" : "waiting") : "requesting";
    this.#render();
    await previous?.disconnect();
    if (this.#destroyed || generation !== this.#generation) return false;
    return new Promise<boolean>((resolve) => {
      let received = false;
      let lastStatus = "";
      const session = new TerminalSession(mode, {
        onFrame: (frame, bytes) => {
          if (this.#session !== session) return;
          this.#xterm.receiveDimensions({ cols: frame.width, rows: frame.height }, mode !== "observe");
          if (frame.full) this.#xterm.replace(bytes);
          else this.#xterm.write(bytes);
          this.#reader?.refreshSoon();
          if (received) return;
          received = true;
          if (mode !== "observe") {
            this.#ownership = "controlling";
            if (this.#lastDimensions) session.resize(this.#lastDimensions);
          }
          else if (this.#ownership !== "occupied") this.#ownership = "observing";
          this.#render();
          if (!this.#mobile && mode !== "observe") this.#xterm.focus();
          resolve(mode !== "observe");
        },
        onInputForwarded: (requestID) => {
          this.#typedRequests.delete(requestID);
          if (this.#typedRequests.size === 0 && this.#typingTimer !== undefined) {
            window.clearTimeout(this.#typingTimer);
            this.#typingTimer = undefined;
          }
          this.#readerInput?.forwarded(session, requestID);
          this.#render();
        },
        onStatus: (message) => { lastStatus = message; },
        onLog: (event) => {
          if (event !== "session.close" || this.#session !== session) return;
          this.#session = undefined;
          this.#readerInput?.disconnected(session);
          this.#unconfirmedTyping();
          this.#reader?.pauseLiveRefresh();
          const occupied = lastStatus.includes("already has an attached client") || lastStatus.includes("taken over");
          if (occupied) {
            this.#desiredControl = false;
            this.#ownership = "occupied";
            void this.#connect("observe");
          } else if (!this.#mobile && mode !== "observe") {
            this.#desiredControl = false;
            this.#ownership = "waiting";
            void this.#connect("observe");
          } else {
            this.#ownership = "waiting";
            this.#scheduleReconnect();
          }
          this.#render();
          resolve(false);
        },
      }, { endpoint: "/api/terminal", terminalID: this.terminalID });
      this.#session = session;
      session.connect(this.paneID, this.#lastDimensions!);
    });
  }

  #scheduleReconnect(): void {
    if (this.#destroyed || this.#retry !== undefined) return;
    this.#retry = window.setTimeout(() => {
      this.#retry = undefined;
      this.#reader?.connectionReset();
      void this.#connect(this.#desiredControl ? "control" : "observe");
    }, 1_000);
  }

  #unconfirmedTyping(): void {
    if (this.#typingTimer !== undefined) window.clearTimeout(this.#typingTimer);
    this.#typingTimer = undefined;
    if (this.#typedRequests.size === 0) return;
    this.#typedRequests.clear();
    this.#readerInput?.inputUnconfirmed();
  }

  #acquire(takeover: boolean): Promise<boolean> {
    this.#desiredControl = true;
    return this.#connect(takeover ? "takeover" : "control");
  }

  #ownershipAction(): void {
    if (this.#uploadState === "requesting") return;
    if (this.#ownership === "controlling") {
      this.#desiredControl = false;
      void this.#connect("observe");
    } else if (this.#ownership === "occupied") {
      if (window.confirm("Take control? The current controller will lose input.")) void this.#acquire(true);
    } else if (this.#ownership === "observing") void this.#acquire(false);
  }

  async #sendFiles(intent: "insert" | "submit", takeover: boolean): Promise<void> {
    const reader = this.#reader;
    if (!reader || !reader.hasPendingFiles() || this.#agentStatus === undefined ||
      this.#typedRequests.size > 0 || !["ready", "occupied", "failed"].includes(this.#uploadState)) return;
    this.#uploadIntent = intent;
    if (takeover && !await this.#acquire(true)) return;
    const controller = this.#ownership === "controlling" ? this.#session?.controllerHandle() : undefined;
    if (!controller) {
      if (this.#ownership === "occupied") {
        this.#uploadState = "occupied";
        reader.inputOccupied("Someone else is controlling this terminal, so the files were not sent.", () => {
          if (window.confirm("Take control? The current controller will lose input.")) void this.#sendFiles(this.#uploadIntent, true);
        });
        this.#render();
      }
      return;
    }
    this.#uploadState = "requesting";
    const attempt = ++this.#uploadAttempt;
    const abort = new AbortController();
    this.#uploadAbort = abort;
    reader.filesSending();
    this.#render();
    try {
      const outcome = await sendTerminalFiles({
        paneID: this.paneID, terminalID: this.terminalID, workspaceID: this.#workspaceID, controller, intent,
      }, intent === "insert" ? "" : reader.draftText(), reader.pendingFiles(), false, abort.signal);
      if (this.#destroyed || attempt !== this.#uploadAttempt) return;
      if (outcome.result === "forwarded") {
        this.#uploadState = "ready";
        reader.inputForwarded(intent === "submit", true);
        reader.hideComposer();
      } else if (outcome.result === "unknown") this.#uploadUncertain(outcome.message);
      else {
        this.#uploadState = "failed";
        reader.inputFailed(outcome.message, () => { void this.#sendFiles(intent, false); });
      }
    } catch {
      if (this.#destroyed || attempt !== this.#uploadAttempt) return;
      this.#uploadUncertain("The result could not be confirmed. Check the terminal before sending the files again.");
    }
    this.#uploadAbort = undefined;
    this.#render();
  }

  #uploadUncertain(message: string): void {
    this.#uploadState = "uncertain";
    this.#reader?.inputUncertain(message, () => { this.#uploadState = "ready"; this.#render(); });
  }

  #render(): void {
    if (this.#destroyed) return;
    const state = this.#uploadState !== "ready" ? this.#uploadState : this.#readerInput?.state() ?? "ready";
    const live = this.#ownership === "controlling" || this.#ownership === "observing" || this.#ownership === "occupied";
    const availability = readerActionAvailability(live, state);
    availability.send = availability.send && this.#typedRequests.size === 0 && (this.#ownership === "controlling" || this.#ownership === "occupied");
    this.#reader?.setActionAvailability(availability);
    this.#reader?.setFileSelectionAvailable(live && this.#agentStatus !== undefined && state === "ready");
    for (const button of this.#shortcuts) button.disabled = !availability.send;
    if (this.#message) {
      this.#message.disabled = this.#readerVisible ? !availability.observerReady || state !== "ready" : !availability.send;
      this.#message.hidden = !this.#readerVisible && this.#agentStatus === undefined;
    }
    if (this.#toggle) this.#toggle.disabled = this.#uploadState === "requesting";
    const control = terminalOwnershipAction(this.#ownership);
    this.#controlAction.hidden = control === undefined;
    this.#controlAction.disabled = state === "forwarding" || state === "requesting";
    this.#controlAction.textContent = control === "release" ? "Release" : control === "takeover" ? "Take over" : "Control";
    const status = state === "forwarding" ? "Forwarding input" : this.#uploadState === "requesting" ? "Sending files" : ({
      controlling: "Controlling", observing: "Observing", occupied: "Controlled elsewhere · observing",
      requesting: "Requesting control", waiting: this.#hasConnected ? "Reconnecting" : "Connecting",
    } satisfies Record<TerminalOwnership, string>)[this.#ownership];
    this.#status.textContent = [this.#agentStatus, status, this.#newOutput ? "New output" : undefined].filter(Boolean).join(" · ");
    this.#xterm.setInputEnabled(this.#canType());
  }
}

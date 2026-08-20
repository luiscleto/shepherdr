import { renderANSI } from "./ansi-dom";
import type { TerminalDimensions } from "./adapter";
import type { ReaderActionAvailability } from "./reader-availability";

interface ReaderSnapshot extends TerminalDimensions {
  ansi: string;
  generation?: number;
  terminal_id?: string;
}

interface ReaderEvents {
  onLog(event: string, detail?: unknown): void;
  onNewOutput?(available: boolean): void;
  onStatus(message: string): void;
  onSubmit(text: string): boolean;
}

interface ReaderOptions {
  collapsibleComposer?: boolean;
  endpoint: string;
}

const historyPageLines = 500;
const maximumHistoryLines = 20_000;
const liveRefreshDelayMilliseconds = 150;

export function readerAtLatest(scrollHeight: number, scrollTop: number, clientHeight: number): boolean {
  return scrollHeight - scrollTop - clientHeight < 32;
}

export function pauseReaderLiveRefresh(atLatest: boolean, hasSelection: boolean): boolean {
  return !atLatest || hasSelection;
}

export class ReaderView {
  #abort: AbortController | undefined;
  #collapsibleComposer: boolean;
  #composer: HTMLDivElement;
  #dimensions: TerminalDimensions = { cols: 80, rows: 24 };
  #events: ReaderEvents;
  #historyLines = historyPageLines;
  #historyRefreshPending = false;
  #host: HTMLElement;
  #input: HTMLTextAreaElement;
  #intersectionObserver: IntersectionObserver;
  #lastANSI = "";
  #generation: number | undefined;
  #loading = false;
  #newOutput = false;
  #output: HTMLDivElement;
  #pane = "";
  #refreshTimer: number | undefined;
  #refreshQueued: { force: boolean; preserveTop: boolean } | undefined;
  #send: HTMLButtonElement;
  #sendFeedback: HTMLDivElement;
  #sendFeedbackMessage: HTMLSpanElement;
  #retrySend: HTMLButtonElement;
  #takeoverSend: HTMLButtonElement;
  #retryAction: (() => void) | undefined;
  #takeoverAction: (() => void) | undefined;
  #scroll: HTMLDivElement;
  #readEndpoint: string;
  #topSentinel: HTMLDivElement;
  #terminalID: string | undefined;
  #allHistoryLoaded = false;
  #selectionChange: () => void;

  constructor(host: HTMLElement, events: ReaderEvents, options: ReaderOptions) {
    this.#host = host;
    this.#events = events;
    this.#collapsibleComposer = options.collapsibleComposer ?? false;
    this.#readEndpoint = options.endpoint;
    host.classList.add("reader-surface");

    this.#topSentinel = document.createElement("div");
    this.#topSentinel.className = "reader-top-sentinel";
    this.#topSentinel.setAttribute("aria-hidden", "true");

    this.#output = document.createElement("div");
    this.#output.className = "reader-output";
    this.#output.setAttribute("aria-label", "Terminal output");
    this.#scroll = document.createElement("div");
    this.#scroll.className = "reader-scroll";
    this.#scroll.append(this.#topSentinel, this.#output);
    this.#scroll.addEventListener("scroll", () => {
      if (this.#newOutput && this.#atLatest() && !this.#hasSelection()) {
        this.#setNewOutput(false);
        this.refreshSoon();
      }
    }, { passive: true });

    this.#composer = document.createElement("div");
    this.#composer.className = "reader-composer";
    this.#input = document.createElement("textarea");
    this.#input.rows = this.#collapsibleComposer ? 2 : 3;
    this.#input.placeholder = "Type text to send";
    this.#input.autocapitalize = "off";
    this.#input.autocomplete = "off";
    this.#input.spellcheck = false;
    this.#send = document.createElement("button");
    this.#send.type = "button";
    this.#send.textContent = "Send text";
    this.#sendFeedback = document.createElement("div");
    this.#sendFeedback.className = "reader-send-feedback";
    this.#sendFeedback.hidden = true;
    this.#sendFeedbackMessage = document.createElement("span");
    this.#retrySend = document.createElement("button");
    this.#retrySend.type = "button";
    this.#retrySend.textContent = "Try again";
    this.#takeoverSend = document.createElement("button");
    this.#takeoverSend.type = "button";
    this.#takeoverSend.textContent = "Take over and send";
    this.#sendFeedback.append(this.#sendFeedbackMessage, this.#retrySend, this.#takeoverSend);
    this.#composer.append(this.#input, this.#send);
    if (this.#collapsibleComposer) {
      const close = document.createElement("button");
      close.type = "button";
      close.className = "reader-composer-close";
      close.textContent = "Close";
      close.addEventListener("click", () => this.hideComposer());
      this.#composer.append(close);
      this.#composer.hidden = true;
    }
    host.replaceChildren(this.#scroll, this.#composer, this.#sendFeedback);

    this.#intersectionObserver = new IntersectionObserver((entries) => {
      if (!entries.some((entry) => entry.isIntersecting) || !this.#pane || !this.#lastANSI || this.#allHistoryLoaded || this.#hasSelection()) return;
      if (this.#historyLines >= maximumHistoryLines) {
        this.#allHistoryLoaded = true;
        return;
      }
      this.#historyLines = Math.min(maximumHistoryLines, this.#historyLines + historyPageLines);
      void this.refresh(true, true);
    }, { root: this.#scroll, rootMargin: "160px 0px 0px" });
    this.#intersectionObserver.observe(this.#topSentinel);
    this.#selectionChange = () => {
      if (this.#hasSelection()) return;
      if (this.#historyRefreshPending) {
        this.#historyRefreshPending = false;
        void this.refresh(true, true);
        return;
      }
      if (this.#newOutput && this.#atLatest()) {
        this.#setNewOutput(false);
        this.refreshSoon();
      }
    };
    document.addEventListener("selectionchange", this.#selectionChange);
    this.#retrySend.addEventListener("click", () => this.#retryAction?.());
    this.#takeoverSend.addEventListener("click", () => this.#takeoverAction?.());
    this.#send.addEventListener("click", () => {
      if (!this.#input.value || !this.#events.onSubmit(this.#input.value)) return;
      this.#events.onLog("reader.input", { characters: this.#input.value.length });
    });
    this.setActionAvailability({ observerReady: false, recover: false, send: false });
  }

  dimensions(): TerminalDimensions {
    return this.#dimensions;
  }

  async open(pane: string, terminalID?: string): Promise<TerminalDimensions> {
    this.#abort?.abort();
    this.#sendFeedback.hidden = true;
    this.#retryAction = undefined;
    this.#takeoverAction = undefined;
    this.#pane = pane;
    this.#terminalID = terminalID;
    this.#historyLines = historyPageLines;
    this.#historyRefreshPending = false;
    this.#lastANSI = "";
    this.#generation = undefined;
    this.#setNewOutput(false);
    this.#allHistoryLoaded = false;
    await this.refresh(true, false);
    return this.#dimensions;
  }

  refreshSoon(): void {
    if (this.#refreshTimer !== undefined || !this.#pane) return;
    if (pauseReaderLiveRefresh(this.#atLatest(), this.#hasSelection())) {
      this.#setNewOutput(true);
      return;
    }
    this.#refreshTimer = window.setTimeout(() => {
      this.#refreshTimer = undefined;
      void this.refresh(false, false);
    }, liveRefreshDelayMilliseconds);
  }

  async refresh(force: boolean, preserveTop: boolean): Promise<void> {
    if (!this.#pane) return;
    if (this.#hasSelection()) {
      if (preserveTop) this.#historyRefreshPending = true;
      else this.#setNewOutput(true);
      return;
    }
    if (this.#loading) {
      const queued = this.#refreshQueued;
      this.#refreshQueued = { force: force || queued?.force === true, preserveTop: preserveTop || queued?.preserveTop === true };
      return;
    }
    this.#loading = true;
    const abort = new AbortController();
    this.#abort = abort;
    const pane = this.#pane;
    const query = new URLSearchParams({ pane, lines: String(this.#historyLines), source: "recent-unwrapped" });
    if (this.#terminalID) query.set("terminal", this.#terminalID);
    try {
      const response = await fetch(`${this.#readEndpoint}?${query}`, { cache: "no-store", signal: abort.signal });
      if (!response.ok) throw new Error(`${response.status} ${await response.text()}`);
      const snapshot = await response.json() as ReaderSnapshot;
      if (this.#abort !== abort || this.#pane !== pane || !this.#validSnapshot(snapshot)) throw new Error("invalid terminal history response");
      this.#dimensions = { cols: snapshot.cols, rows: snapshot.rows };
      if (preserveTop && snapshot.ansi === this.#lastANSI) {
        this.#allHistoryLoaded = true;
        this.#events.onStatus("All retained output is loaded");
        return;
      }
      if (this.#hasSelection()) {
        if (preserveTop) this.#historyRefreshPending = true;
        else this.#setNewOutput(true);
        return;
      }
      if (!force && pauseReaderLiveRefresh(this.#atLatest(), this.#hasSelection())) {
        this.#setNewOutput(true);
        return;
      }
      this.#render(snapshot, preserveTop);
      this.#events.onLog("reader.snapshot", { characters: snapshot.ansi.length, lines: this.#historyLines, ...this.#dimensions });
    } catch (error) {
      if (abort.signal.aborted) return;
      this.#events.onStatus("Could not read terminal output");
      this.#events.onLog("reader.failed", String(error));
    } finally {
      if (this.#abort === abort) {
        this.#loading = false;
        const queued = this.#refreshQueued;
        this.#refreshQueued = undefined;
        if (queued) void this.refresh(queued.force, queued.preserveTop);
      }
    }
  }

  setActionAvailability(availability: ReaderActionAvailability): void {
    this.#input.disabled = !availability.send;
    this.#send.disabled = !availability.send;
    this.#retrySend.disabled = !availability.recover;
    this.#takeoverSend.disabled = !availability.recover;
    this.#input.placeholder = availability.observerReady ? "Type text to send" : "Connect to send text";
    if (!availability.observerReady && this.#collapsibleComposer) this.hideComposer();
  }

  showComposer(): void {
    if (!this.#collapsibleComposer || this.#input.disabled) return;
    this.#composer.hidden = false;
    this.#input.focus();
  }

  hideComposer(): void {
    if (!this.#collapsibleComposer) return;
    this.#composer.hidden = true;
    this.#input.blur();
  }

  inputSending(chunks: number): void {
    this.#sendFeedback.hidden = false;
    this.#sendFeedbackMessage.textContent = chunks === 1 ? "Sending queued input…" : `Sending ${chunks} queued inputs…`;
    this.#retrySend.hidden = true;
    this.#takeoverSend.hidden = true;
  }

  inputForwarded(clearText: boolean): void {
    if (clearText) this.#input.value = "";
    this.#sendFeedback.hidden = true;
    this.#retryAction = undefined;
    this.#takeoverAction = undefined;
  }

  inputFailed(message: string, retry: () => void): void {
    this.#showRecovery(message, { label: "Try again", run: retry }, undefined);
  }

  inputOccupied(message: string, takeover: () => void): void {
    this.#showRecovery(message, undefined, takeover);
  }

  inputUncertain(message: string, dismiss: () => void): void {
    this.#showRecovery(message, {
      label: "Dismiss",
      run: () => {
        this.#sendFeedback.hidden = true;
        dismiss();
      },
    }, undefined);
  }

  connectionReset(): void {
    this.pauseLiveRefresh();
    this.#generation = undefined;
  }

  pauseLiveRefresh(): void {
    this.#abort?.abort();
    this.#refreshQueued = undefined;
    if (this.#refreshTimer !== undefined) window.clearTimeout(this.#refreshTimer);
    this.#refreshTimer = undefined;
  }

  focus(): void {
    if (!this.#input.disabled) this.#input.focus();
  }

  paste(text: string): boolean {
    if (this.#input.disabled) return false;
    this.#input.setRangeText(text, this.#input.selectionStart, this.#input.selectionEnd, "end");
    this.#input.focus();
    return true;
  }

  selection(): string {
    const selection = window.getSelection();
    if (!selection || !selection.anchorNode || !selection.focusNode) return "";
    if (!this.#output.contains(selection.anchorNode) || !this.#output.contains(selection.focusNode)) return "";
    return selection.toString();
  }

  scrollPage(direction: -1 | 1): void {
    this.#scroll.scrollBy({ behavior: "smooth", top: direction * this.#scroll.clientHeight * 0.8 });
  }

  destroy(): void {
    this.#pane = "";
    this.#terminalID = undefined;
    this.#generation = undefined;
    this.#historyRefreshPending = false;
    this.#refreshQueued = undefined;
    this.#abort?.abort();
    this.#intersectionObserver.disconnect();
    document.removeEventListener("selectionchange", this.#selectionChange);
    if (this.#refreshTimer !== undefined) window.clearTimeout(this.#refreshTimer);
    this.#host.classList.remove("reader-surface");
    this.#host.replaceChildren();
  }

  #hasSelection(): boolean {
    const selection = window.getSelection();
    return Boolean(selection && !selection.isCollapsed && selection.anchorNode && this.#output.contains(selection.anchorNode));
  }

  #atLatest(): boolean {
    return readerAtLatest(this.#scroll.scrollHeight, this.#scroll.scrollTop, this.#scroll.clientHeight);
  }

  #setNewOutput(available: boolean): void {
    if (this.#newOutput === available) return;
    this.#newOutput = available;
    this.#events.onNewOutput?.(available);
  }

  #showRecovery(
    message: string,
    primary: { label: string; run: () => void } | undefined,
    takeover: (() => void) | undefined,
  ): void {
    this.#sendFeedback.hidden = false;
    this.#sendFeedbackMessage.textContent = message;
    this.#retrySend.textContent = primary?.label ?? "Try again";
    this.#retrySend.hidden = primary === undefined;
    this.#takeoverSend.hidden = takeover === undefined;
    this.#retryAction = primary?.run;
    this.#takeoverAction = takeover;
  }

  #validSnapshot(snapshot: ReaderSnapshot): boolean {
    if (typeof snapshot.ansi !== "string" || !Number.isSafeInteger(snapshot.cols) || snapshot.cols < 2 || snapshot.cols > 1000 ||
      !Number.isSafeInteger(snapshot.rows) || snapshot.rows < 1 || snapshot.rows > 1000) return false;
    if (this.#terminalID) {
      if (snapshot.terminal_id !== this.#terminalID || !Number.isSafeInteger(snapshot.generation) || Number(snapshot.generation) < 0) return false;
      if (this.#generation !== undefined && snapshot.generation !== this.#generation) return false;
      this.#generation = snapshot.generation;
    }
    return true;
  }

  #render(snapshot: ReaderSnapshot, preserveTop: boolean): void {
    const oldHeight = this.#scroll.scrollHeight;
    const oldTop = this.#scroll.scrollTop;
    const followBottom = oldHeight - oldTop - this.#scroll.clientHeight < 32;
    renderANSI(this.#output, snapshot.ansi);
    this.#lastANSI = snapshot.ansi;
    this.#setNewOutput(false);
    if (preserveTop) this.#scroll.scrollTop = oldTop + this.#scroll.scrollHeight - oldHeight;
    else if (followBottom) this.#scroll.scrollTop = this.#scroll.scrollHeight;
    else this.#scroll.scrollTop = oldTop;
  }
}

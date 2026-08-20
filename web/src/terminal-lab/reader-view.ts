import { renderANSI } from "./ansi-dom";
import type { TerminalDimensions } from "./adapter";

interface ReaderSnapshot extends TerminalDimensions {
  ansi: string;
}

interface ReaderEvents {
  onLog(event: string, detail?: unknown): void;
  onStatus(message: string): void;
  onSubmit(text: string): boolean;
}

const historyPageLines = 500;
const maximumHistoryLines = 20_000;
const liveRefreshDelayMilliseconds = 150;

export class ReaderView {
  #abort: AbortController | undefined;
  #dimensions: TerminalDimensions = { cols: 80, rows: 24 };
  #events: ReaderEvents;
  #historyLines = historyPageLines;
  #host: HTMLElement;
  #input: HTMLTextAreaElement;
  #intersectionObserver: IntersectionObserver;
  #lastANSI = "";
  #loading = false;
  #output: HTMLDivElement;
  #pane = "";
  #pending: ReaderSnapshot | undefined;
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
  #topSentinel: HTMLDivElement;
  #allHistoryLoaded = false;
  #selectionChange: () => void;

  constructor(host: HTMLElement, events: ReaderEvents) {
    this.#host = host;
    this.#events = events;
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

    const composer = document.createElement("div");
    composer.className = "reader-composer";
    this.#input = document.createElement("textarea");
    this.#input.rows = 3;
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
    composer.append(this.#input, this.#send, this.#sendFeedback);
    host.replaceChildren(this.#scroll, composer);

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
      if (!this.#pending || this.#hasSelection()) return;
      const pending = this.#pending;
      this.#pending = undefined;
      this.#render(pending, false);
    };
    document.addEventListener("selectionchange", this.#selectionChange);
    this.#retrySend.addEventListener("click", () => this.#retryAction?.());
    this.#takeoverSend.addEventListener("click", () => this.#takeoverAction?.());
    this.#send.addEventListener("click", () => {
      if (!this.#input.value || !this.#events.onSubmit(this.#input.value)) return;
      this.#events.onLog("reader.input", { characters: this.#input.value.length });
      this.#input.value = "";
    });
    this.setInteractive(false);
  }

  dimensions(): TerminalDimensions {
    return this.#dimensions;
  }

  async open(pane: string): Promise<TerminalDimensions> {
    this.#abort?.abort();
    this.#pane = pane;
    this.#historyLines = historyPageLines;
    this.#lastANSI = "";
    this.#allHistoryLoaded = false;
    await this.refresh(true, false);
    return this.#dimensions;
  }

  refreshSoon(): void {
    if (this.#refreshTimer !== undefined || !this.#pane) return;
    this.#refreshTimer = window.setTimeout(() => {
      this.#refreshTimer = undefined;
      void this.refresh(false, false);
    }, liveRefreshDelayMilliseconds);
  }

  async refresh(force: boolean, preserveTop: boolean): Promise<void> {
    if (!this.#pane) return;
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
    try {
      const response = await fetch(`/api/terminal-lab/read?${query}`, { cache: "no-store", signal: abort.signal });
      if (!response.ok) throw new Error(`${response.status} ${await response.text()}`);
      const snapshot = await response.json() as ReaderSnapshot;
      if (this.#abort !== abort || this.#pane !== pane) return;
      this.#dimensions = { cols: snapshot.cols, rows: snapshot.rows };
      if (preserveTop && snapshot.ansi === this.#lastANSI) {
        this.#allHistoryLoaded = true;
        this.#events.onStatus("All retained output is loaded");
        return;
      }
      if (!force && this.#hasSelection()) {
        this.#pending = snapshot;
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

  setInteractive(interactive: boolean): void {
    this.#input.disabled = !interactive;
    this.#send.disabled = !interactive;
    this.#input.placeholder = interactive ? "Type text to send" : "Connect to send text";
  }

  inputSending(chunks: number): void {
    this.#sendFeedback.hidden = false;
    this.#sendFeedbackMessage.textContent = chunks === 1 ? "Sending queued input…" : `Sending ${chunks} queued inputs…`;
    this.#retrySend.hidden = true;
    this.#takeoverSend.hidden = true;
  }

  inputSent(): void {
    this.#sendFeedback.hidden = true;
    this.#retryAction = undefined;
    this.#takeoverAction = undefined;
  }

  inputBlocked(message: string, retry: () => void, takeover: () => void): void {
    this.#sendFeedback.hidden = false;
    this.#sendFeedbackMessage.textContent = message;
    this.#retrySend.hidden = false;
    this.#takeoverSend.hidden = false;
    this.#retryAction = retry;
    this.#takeoverAction = takeover;
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

  #render(snapshot: ReaderSnapshot, preserveTop: boolean): void {
    const oldHeight = this.#scroll.scrollHeight;
    const oldTop = this.#scroll.scrollTop;
    const followBottom = oldHeight - oldTop - this.#scroll.clientHeight < 32;
    renderANSI(this.#output, snapshot.ansi);
    this.#lastANSI = snapshot.ansi;
    this.#pending = undefined;
    if (preserveTop) this.#scroll.scrollTop = oldTop + this.#scroll.scrollHeight - oldHeight;
    else if (followBottom) this.#scroll.scrollTop = this.#scroll.scrollHeight;
    else this.#scroll.scrollTop = oldTop;
  }
}

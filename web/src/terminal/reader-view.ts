import { renderANSI } from "./ansi-dom";
import type { TerminalDimensions } from "./adapter";
import { prepareTerminalFiles } from "./file-uploads";
import type { ReaderActionAvailability } from "./reader-availability";

interface ReaderSnapshot extends TerminalDimensions {
  ansi: string;
  generation?: number;
  terminal_id?: string;
}

interface ReaderEvents {
  onLog(event: string, detail?: unknown): void;
  onNewOutput?(available: boolean): void;
  onPendingFilesEmpty?(): void;
  onStatus(message: string): void;
  onSubmit(text: string, files: readonly File[]): boolean;
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
  #addFiles: HTMLButtonElement;
  #abort: AbortController | undefined;
  #closeComposer: HTMLButtonElement | undefined;
  #collapsibleComposer: boolean;
  #composer: HTMLDivElement;
  #dimensions: TerminalDimensions = { cols: 80, rows: 24 };
  #events: ReaderEvents;
  #fileChoice: HTMLButtonElement;
  #fileInput: HTMLInputElement;
  #fileList: HTMLDivElement;
  #fileMenu: HTMLDivElement;
  #fileMenuDismiss: (event: Event) => void;
  #filePreparation = 0;
  #filePreparationStatus: HTMLSpanElement;
  #fileSelectionAvailable = false;
  #filesPreparing = false;
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
  #pendingFiles: Array<{ file: File; preview?: string }> = [];
  #photoChoice: HTMLButtonElement;
  #photoInput: HTMLInputElement;
  #submissionActive = false;
  #refreshTimer: number | undefined;
  #refreshQueued: { force: boolean; preserveTop: boolean } | undefined;
  #send: HTMLButtonElement;
  #sendAvailable = false;
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
    this.#fileInput = document.createElement("input");
    this.#fileInput.type = "file";
    this.#fileInput.multiple = true;
    this.#fileInput.className = "reader-file-input";
    this.#fileInput.hidden = true;
    this.#photoInput = document.createElement("input");
    this.#photoInput.type = "file";
    this.#photoInput.accept = "image/*";
    this.#photoInput.multiple = true;
    this.#photoInput.className = "reader-photo-input";
    this.#photoInput.hidden = true;
    this.#addFiles = document.createElement("button");
    this.#addFiles.type = "button";
    this.#addFiles.className = "reader-add-files reader-icon-button";
    setIconButton(this.#addFiles, "Add files", "paperclip");
    this.#addFiles.setAttribute("aria-expanded", "false");
    this.#addFiles.hidden = true;
    this.#fileMenu = document.createElement("div");
    this.#fileMenu.className = "reader-file-menu";
    this.#fileMenu.hidden = true;
    this.#fileMenu.setAttribute("role", "group");
    this.#fileMenu.setAttribute("aria-label", "Add files from");
    this.#photoChoice = document.createElement("button");
    this.#photoChoice.type = "button";
    this.#photoChoice.textContent = "Photos";
    setControlLabel(this.#photoChoice, "Choose photos");
    this.#fileChoice = document.createElement("button");
    this.#fileChoice.type = "button";
    this.#fileChoice.textContent = "Files";
    setControlLabel(this.#fileChoice, "Choose files");
    this.#fileMenu.append(this.#photoChoice, this.#fileChoice);
    this.#fileList = document.createElement("div");
    this.#fileList.className = "reader-file-list";
    this.#fileList.setAttribute("aria-label", "Pending files");
    this.#fileList.hidden = true;
    this.#send = document.createElement("button");
    this.#send.type = "button";
    this.#send.className = "reader-send reader-icon-button";
    setIconButton(this.#send, "Send text", "send");
    const actionRail = document.createElement("div");
    actionRail.className = "reader-action-rail";
    actionRail.append(this.#addFiles, this.#send);
    this.#filePreparationStatus = document.createElement("span");
    this.#filePreparationStatus.className = "reader-file-preparing";
    this.#filePreparationStatus.hidden = true;
    this.#filePreparationStatus.setAttribute("role", "status");
    this.#filePreparationStatus.setAttribute("aria-live", "polite");
    this.#filePreparationStatus.textContent = "Preparing files…";
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
    this.#composer.append(this.#input, actionRail);
    if (this.#collapsibleComposer) {
      const close = document.createElement("button");
      close.type = "button";
      close.className = "reader-composer-close reader-icon-button";
      setIconButton(close, "Close composer", "close");
      close.addEventListener("click", () => this.hideComposer());
      this.#closeComposer = close;
      this.#composer.append(close);
      this.#composer.hidden = true;
    }
    this.#composer.append(this.#fileMenu, this.#fileInput, this.#photoInput, this.#filePreparationStatus, this.#fileList);
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
    this.#fileMenuDismiss = (event) => {
      const target = event.target as Node | null;
      if (target && (this.#fileMenu.contains(target) || this.#addFiles.contains(target))) return;
      this.#closeFileMenu(true);
    };
    this.#addFiles.addEventListener("click", () => {
      if (this.#fileMenu.hidden) this.#openFileMenu();
      else this.#closeFileMenu(true);
    });
    this.#fileMenu.addEventListener("keydown", (event) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      this.#closeFileMenu(true);
    });
    this.#fileMenu.addEventListener("focusout", (event) => {
      if (this.#fileMenu.hidden) return;
      const next = event.relatedTarget as Node | null;
      if (next && (this.#fileMenu.contains(next) || this.#addFiles.contains(next))) return;
      this.#closeFileMenu(next === null);
    });
    this.#photoChoice.addEventListener("click", () => this.#openFileInput(this.#photoInput));
    this.#fileChoice.addEventListener("click", () => this.#openFileInput(this.#fileInput));
    this.#bindFileInput(this.#photoInput);
    this.#bindFileInput(this.#fileInput);
    this.#input.addEventListener("input", () => this.#syncSubmit());
    this.#send.addEventListener("click", () => {
      if (!this.#canSubmit() || !this.#events.onSubmit(this.#input.value, this.pendingFiles())) return;
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
    this.#sendAvailable = availability.send;
    const keepOpenForEditing = this.#collapsibleComposer && !this.#composer.hidden && !availability.observerReady;
    this.#input.disabled = this.#submissionActive || !availability.send && !keepOpenForEditing;
    this.#addFiles.disabled = this.#submissionActive || this.#filesPreparing || !availability.send;
    if (this.#closeComposer) this.#closeComposer.disabled = this.#submissionActive;
    this.#retrySend.disabled = !availability.recover;
    this.#takeoverSend.disabled = !availability.recover;
    this.#input.placeholder = availability.observerReady ? "Type text to send" : "Connect to send text";
    this.#syncSubmit();
  }

  setFileSelectionAvailable(available: boolean): void {
    const active = this.#host.ownerDocument.activeElement;
    const attachmentFocus = active !== null && (this.#addFiles.contains(active) || this.#fileMenu.contains(active));
    if (!available) this.#closeFileMenu(false);
    this.#fileSelectionAvailable = available;
    this.#addFiles.hidden = !available;
    if (!available && attachmentFocus) this.#restoreComposerFocus();
    this.#syncSubmit();
  }

  pendingFiles(): File[] {
    return this.#pendingFiles.map(({ file }) => file);
  }

  draftText(): string {
    return this.#input.value;
  }

  hasPendingFiles(): boolean {
    return this.#pendingFiles.length > 0;
  }

  showComposer(): void {
    if (!this.#collapsibleComposer || !this.#sendAvailable) return;
    this.#composer.hidden = false;
    this.#input.focus();
  }

  hideComposer(): void {
    if (!this.#collapsibleComposer || this.#submissionActive) return;
    const active = this.#host.ownerDocument.activeElement as HTMLElement | null;
    this.#closeFileMenu(false);
    if (active && this.#composer.contains(active)) active.blur();
    this.#composer.hidden = true;
    this.#input.blur();
    if (!this.#sendAvailable) this.#input.disabled = true;
  }

  inputSending(chunks: number): void {
    this.#sendFeedback.hidden = false;
    this.#sendFeedbackMessage.textContent = chunks === 1 ? "Sending queued input…" : `Sending ${chunks} queued inputs…`;
    this.#retrySend.hidden = true;
    this.#takeoverSend.hidden = true;
  }

  filesSending(): void {
    this.#submissionActive = true;
    this.#input.disabled = true;
    this.#addFiles.disabled = true;
    if (this.#closeComposer) this.#closeComposer.disabled = true;
    this.#renderPendingFiles();
    this.#sendFeedback.hidden = false;
    this.#sendFeedbackMessage.textContent = "Sending files…";
    this.#retrySend.hidden = true;
    this.#takeoverSend.hidden = true;
  }

  inputForwarded(clearText: boolean, clearFiles = false): void {
    this.#submissionFinished();
    if (clearText) this.#input.value = "";
    if (clearFiles) this.#clearPendingFiles();
    this.#sendFeedback.hidden = true;
    this.#retryAction = undefined;
    this.#takeoverAction = undefined;
    this.#syncSubmit();
  }

  inputFailed(message: string, retry: () => void): void {
    this.#submissionFinished();
    this.#showRecovery(message, { label: "Try again", run: retry }, undefined);
  }

  inputOccupied(message: string, takeover: () => void): void {
    this.#submissionFinished();
    this.#showRecovery(message, undefined, takeover);
  }

  inputUncertain(message: string, dismiss: () => void): void {
    this.#submissionFinished();
    this.#showRecovery(message, {
      label: "Dismiss",
      run: () => {
        this.#sendFeedback.hidden = true;
        dismiss();
      },
    }, undefined);
  }

  clearInputRecovery(): void {
    this.#sendFeedback.hidden = true;
    this.#retryAction = undefined;
    this.#takeoverAction = undefined;
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
    this.#filePreparation++;
    this.#closeFileMenu(false);
    this.#pane = "";
    this.#terminalID = undefined;
    this.#generation = undefined;
    this.#historyRefreshPending = false;
    this.#refreshQueued = undefined;
    this.#abort?.abort();
    this.#intersectionObserver.disconnect();
    document.removeEventListener("selectionchange", this.#selectionChange);
    if (this.#refreshTimer !== undefined) window.clearTimeout(this.#refreshTimer);
    this.#clearPendingFiles();
    this.#host.classList.remove("reader-surface");
    this.#host.replaceChildren();
  }

  #addPendingFile(file: File): void {
    let preview: string | undefined;
    const url = this.#host.ownerDocument.defaultView?.URL;
    if (file.type.startsWith("image/") && typeof url?.createObjectURL === "function") {
      preview = url.createObjectURL(file);
    }
    this.#pendingFiles.push({ file, preview });
  }

  #bindFileInput(input: HTMLInputElement): void {
    input.addEventListener("change", () => {
      const selected = Array.from(input.files ?? []);
      input.value = "";
      if (selected.length === 0) {
        this.#restoreFileActionFocus();
        return;
      }
      void this.#prepareFiles(selected);
    });
    input.addEventListener("cancel", () => {
      input.value = "";
      this.#restoreFileActionFocus();
    });
  }

  #openFileInput(input: HTMLInputElement): void {
    this.#closeFileMenu(true);
    input.click();
  }

  #openFileMenu(): void {
    if (this.#addFiles.disabled || this.#addFiles.hidden || this.#filesPreparing) return;
    this.#fileMenu.hidden = false;
    this.#addFiles.setAttribute("aria-expanded", "true");
    this.#host.ownerDocument.addEventListener("pointerdown", this.#fileMenuDismiss, true);
    this.#photoChoice.focus();
  }

  #closeFileMenu(restoreFocus: boolean): void {
    if (this.#fileMenu.hidden) return;
    this.#fileMenu.hidden = true;
    this.#addFiles.setAttribute("aria-expanded", "false");
    this.#host.ownerDocument.removeEventListener("pointerdown", this.#fileMenuDismiss, true);
    if (restoreFocus) this.#restoreFileActionFocus();
  }

  async #prepareFiles(files: readonly File[]): Promise<void> {
    const preparation = ++this.#filePreparation;
    this.#filesPreparing = true;
    this.#filePreparationStatus.hidden = false;
    this.#addFiles.disabled = true;
    this.#syncSubmit();
    try {
      await prepareTerminalFiles(files);
      if (preparation !== this.#filePreparation) return;
      for (const file of files) this.#addPendingFile(file);
    } finally {
      if (preparation !== this.#filePreparation) return;
      this.#filesPreparing = false;
      this.#filePreparationStatus.hidden = true;
      this.#addFiles.disabled = this.#submissionActive || !this.#sendAvailable;
      this.#renderPendingFiles();
      this.#restoreFileActionFocus();
    }
  }

  #restoreFileActionFocus(): void {
    if (!this.#composer.hidden && !this.#addFiles.hidden && !this.#addFiles.disabled) this.#addFiles.focus();
  }

  #restoreComposerFocus(): void {
    if (this.#composer.hidden) return;
    if (!this.#input.disabled) {
      this.#input.focus();
      return;
    }
    if (this.#closeComposer && !this.#closeComposer.disabled) this.#closeComposer.focus();
  }

  #clearPendingFiles(): void {
    for (const pending of this.#pendingFiles) this.#revokePreview(pending.preview);
    this.#pendingFiles = [];
    this.#renderPendingFiles();
  }

  #removePendingFile(index: number): void {
    const [removed] = this.#pendingFiles.splice(index, 1);
    this.#revokePreview(removed?.preview);
    this.#renderPendingFiles();
    if (this.#pendingFiles.length === 0) this.#events.onPendingFilesEmpty?.();
  }

  #revokePreview(preview?: string): void {
    const url = this.#host.ownerDocument.defaultView?.URL;
    if (preview && typeof url?.revokeObjectURL === "function") url.revokeObjectURL(preview);
  }

  #renderPendingFiles(): void {
    this.#fileList.replaceChildren();
    this.#fileList.hidden = this.#pendingFiles.length === 0;
    this.#pendingFiles.forEach((pending, index) => {
      const chip = document.createElement("div");
      chip.className = "reader-file-chip";
      if (pending.preview) {
        const thumbnail = document.createElement("img");
        thumbnail.alt = "";
        thumbnail.src = pending.preview;
        thumbnail.addEventListener("error", () => {
          this.#revokePreview(pending.preview);
          pending.preview = undefined;
          thumbnail.remove();
        }, { once: true });
        chip.append(thumbnail);
      }
      const detail = document.createElement("span");
      detail.textContent = `${pending.file.name || "file"} · ${formatFileSize(pending.file.size)}`;
      const remove = document.createElement("button");
      remove.type = "button";
      remove.className = "reader-file-remove reader-icon-button";
      remove.disabled = this.#submissionActive;
      setIconButton(remove, `Remove ${pending.file.name || "file"}`, "close");
      remove.addEventListener("click", () => this.#removePendingFile(index));
      chip.append(detail, remove);
      this.#fileList.append(chip);
    });
    this.#syncSubmit();
  }

  #canSubmit(): boolean {
    return !this.#submissionActive && !this.#filesPreparing && this.#sendAvailable && (this.#input.value.length > 0 || this.#pendingFiles.length > 0) &&
      (this.#pendingFiles.length === 0 || this.#fileSelectionAvailable);
  }

  #submissionFinished(): void {
    if (!this.#submissionActive) return;
    this.#submissionActive = false;
    if (this.#closeComposer) this.#closeComposer.disabled = false;
    this.#renderPendingFiles();
  }

  #syncSubmit(): void {
    this.#send.disabled = !this.#canSubmit();
    const label = this.#pendingFiles.length === 0 ? "Send text" : this.#input.value.length === 0 ? "Send files" : "Send text and files";
    setControlLabel(this.#send, label);
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

type ReaderIcon = "close" | "paperclip" | "send";

function setControlLabel(button: HTMLButtonElement, label: string): void {
  button.setAttribute("aria-label", label);
  button.title = label;
}

function setIconButton(button: HTMLButtonElement, label: string, iconName: ReaderIcon): void {
  setControlLabel(button, label);
  const icon = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  icon.setAttribute("viewBox", "0 0 24 24");
  icon.setAttribute("aria-hidden", "true");
  icon.setAttribute("focusable", "false");
  icon.setAttribute("fill", "none");
  icon.setAttribute("stroke", "currentColor");
  icon.setAttribute("stroke-linecap", "round");
  icon.setAttribute("stroke-linejoin", "round");
  icon.setAttribute("stroke-width", "2");
  const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
  path.setAttribute("d", ({
    close: "M18 6 6 18M6 6l12 12",
    paperclip: "m21.4 11.1-9.2 9.2a6 6 0 0 1-8.5-8.5l9.2-9.2a4 4 0 0 1 5.7 5.7l-9.2 9.2a2 2 0 0 1-2.8-2.8l8.5-8.5",
    send: "M22 2 11 13M22 2l-7 20-4-9-9-4 20-7Z",
  } satisfies Record<ReaderIcon, string>)[iconName]);
  icon.append(path);
  button.replaceChildren(icon);
}

function formatFileSize(size: number): string {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(size < 10 * 1024 ? 1 : 0)} KiB`;
  return `${(size / (1024 * 1024)).toFixed(size < 10 * 1024 * 1024 ? 1 : 0)} MiB`;
}

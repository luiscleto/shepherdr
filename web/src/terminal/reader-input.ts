import type { TerminalDimensions } from "./adapter";
import type { ReaderInputState } from "./reader-availability";
import {
  TerminalSession,
  type SessionMode,
  type TerminalSessionEvents,
  type TerminalSessionLike,
} from "./session";

interface ReaderInputEvents {
  onFailed(message: string): void;
  onForwarded(count: number): void;
  onLog(event: string, detail?: unknown): void;
  onOccupied(message: string): void;
  onSending(count: number): void;
  onState?(state: ReaderInputState): void;
  onUncertain(message: string): void;
}

interface ReaderInputOptions {
  createSession?: (mode: SessionMode, events: TerminalSessionEvents, endpoint: string, terminalID?: string) => TerminalSessionLike;
  endpoint: string;
}

const acquireTimeoutMilliseconds = 2_500;

export class ReaderInputQueue {
  #acquired = false;
  #activeBatch: string[] | undefined;
  #attemptTimer: number | undefined;
  #currentRequestID: number | undefined;
  #createSession: NonNullable<ReaderInputOptions["createSession"]>;
  #dimensions: TerminalDimensions = { cols: 80, rows: 24 };
  #events: ReaderInputEvents;
  #endpoint: string;
  #lastStatus = "";
  #pane = "";
  #pendingBatch: string[] | undefined;
  #session: TerminalSessionLike | undefined;
  #state: ReaderInputState = "ready";
  #takeover = false;
  #terminalID: string | undefined;

  constructor(events: ReaderInputEvents, options: ReaderInputOptions) {
    this.#events = events;
    this.#endpoint = options.endpoint;
    this.#createSession = options.createSession ?? ((mode, sessionEvents, endpoint, terminalID) =>
      new TerminalSession(mode, sessionEvents, { endpoint, terminalID }));
  }

  setTarget(pane: string, dimensions: TerminalDimensions, terminalID?: string): void {
    this.clearTarget();
    this.#pane = pane;
    this.#dimensions = dimensions;
    this.#terminalID = terminalID;
  }

  resize(dimensions: TerminalDimensions): void {
    this.#dimensions = dimensions;
    this.#session?.resize(dimensions);
  }

  clearTarget(): void {
    this.#clearTimer();
    this.#session?.disconnect();
    this.#session = undefined;
    this.#pane = "";
    this.#pendingBatch = undefined;
    this.#activeBatch = undefined;
    this.#currentRequestID = undefined;
    this.#acquired = false;
    this.#lastStatus = "";
    this.#state = "ready";
    this.#terminalID = undefined;
  }

  enqueue(text: string): boolean {
    return this.enqueueBatch([text]);
  }

  state(): ReaderInputState {
    return this.#state;
  }

  enqueueBatch(chunks: string[]): boolean {
    const batch = chunks.filter(Boolean);
    if (!this.#pane || this.#state !== "ready" || batch.length === 0 || this.#session || this.#pendingBatch || this.#activeBatch) return false;
    this.#pendingBatch = batch;
    this.#events.onLog("reader.input-queued", {
      chunks: batch.length,
      characters: batch.reduce((total, text) => total + text.length, 0),
    });
    this.#start(false);
    return true;
  }

  retry(takeover: boolean): void {
    const permitted = takeover ? this.#state === "occupied" : this.#state === "failed";
    if (!permitted || this.#session || !this.#pendingBatch) return;
    this.#start(takeover);
  }

  dismissUncertain(): void {
    if (this.#state !== "uncertain") return;
    this.#setState("ready");
  }

  #start(takeover: boolean): void {
    if (this.#session || !this.#pane || !this.#pendingBatch) return;
    this.#lastStatus = "";
    this.#takeover = takeover;
    this.#acquired = false;
    const mode = takeover ? "takeover" : "control";
    let session: TerminalSessionLike;
    session = this.#createSession(mode, {
      onFrame: () => this.#acquire(session),
      onInputForwarded: (requestID) => this.#inputForwarded(session, requestID),
      onLog: (event, detail) => {
        this.#events.onLog(`reader.control.${event}`, detail);
        if (event === "session.close") this.#failed(session);
      },
      onStatus: (message) => {
        this.#lastStatus = message;
      },
    }, this.#endpoint, this.#terminalID);
    this.#session = session;
    this.#events.onSending(1);
    this.#setState("requesting");
    this.#events.onLog("reader.control.acquire", { mode, ...this.#dimensions });
    session.connect(this.#pane, this.#dimensions);
    this.#armTimer(session, "Timed out while waiting for terminal control");
  }

  #acquire(session: TerminalSessionLike): void {
    if (this.#session !== session || this.#acquired) return;
    this.#acquired = true;
    this.#clearTimer();
    this.#setState("forwarding");
    this.#sendNext(session);
  }

  #inputForwarded(session: TerminalSessionLike, requestID: number): void {
    if (this.#session !== session || requestID !== this.#currentRequestID) return;
    this.#clearTimer();
    this.#currentRequestID = undefined;
    this.#activeBatch = undefined;
    this.#finished(session);
  }

  #sendNext(session: TerminalSessionLike): void {
    if (this.#session !== session || this.#currentRequestID !== undefined) return;
    const next = this.#pendingBatch;
    if (!next) return;
    this.#pendingBatch = undefined;
    this.#activeBatch = next;
    const requestID = session.inputBatch(next);
    if (requestID === undefined) {
      this.#pendingBatch = next;
      this.#activeBatch = undefined;
      this.#lastStatus = "The terminal input stream was not available";
      this.#failed(session);
      return;
    }
    this.#currentRequestID = requestID;
    this.#armTimer(session, "Timed out while forwarding terminal input");
  }

  #finished(session: TerminalSessionLike): void {
    if (this.#session !== session) return;
    this.#session = undefined;
    session.disconnect();
    this.#events.onLog("reader.control.forwarded", {
      takeover: this.#takeover,
    });
    this.#acquired = false;
    this.#setState("ready");
    this.#events.onForwarded(1);
  }

  #armTimer(session: TerminalSessionLike, message: string): void {
    this.#clearTimer();
    this.#attemptTimer = window.setTimeout(() => {
      if (this.#session !== session) return;
      this.#lastStatus = message;
      this.#failed(session);
    }, acquireTimeoutMilliseconds);
  }

  #failed(session: TerminalSessionLike): void {
    if (this.#session !== session) return;
    this.#clearTimer();
    this.#session = undefined;
    session.disconnect();
    const ambiguous = this.#currentRequestID !== undefined;
    if (!ambiguous && this.#activeBatch) this.#pendingBatch = this.#activeBatch;
    this.#activeBatch = undefined;
    this.#currentRequestID = undefined;
    this.#acquired = false;
    if (ambiguous) {
      this.#pendingBatch = undefined;
      this.#setState("uncertain");
      this.#events.onLog("reader.control.uncertain", { message: this.#lastStatus });
      this.#events.onUncertain("The input was handed to the connection, but delivery could not be confirmed. Check the terminal before sending it again.");
      return;
    }
    const occupied = this.#lastStatus.includes("already has an attached client");
    if (occupied) {
      this.#setState("occupied");
      this.#events.onLog("reader.control.occupied", { message: this.#lastStatus });
      this.#events.onOccupied("Someone else is controlling this terminal, so your input was not sent.");
      return;
    }
    this.#setState("failed");
    this.#events.onLog("reader.control.failed", { message: this.#lastStatus });
    this.#events.onFailed("Could not acquire terminal control, so your input was not sent.");
  }

  #clearTimer(): void {
    if (this.#attemptTimer === undefined) return;
    window.clearTimeout(this.#attemptTimer);
    this.#attemptTimer = undefined;
  }

  #setState(state: ReaderInputState): void {
    this.#state = state;
    this.#events.onState?.(state);
  }
}

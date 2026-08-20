import type { TerminalDimensions } from "./adapter";
import {
  TerminalSession,
  type SessionMode,
  type TerminalSessionEvents,
  type TerminalSessionLike,
} from "./session";

interface ReaderInputEvents {
  onBlocked(message: string): void;
  onForwarded(count: number): void;
  onLog(event: string, detail?: unknown): void;
  onSending(count: number): void;
  onState?(state: "forwarding" | "observing" | "occupied" | "requesting"): void;
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
  #forwardedBatches = 0;
  #lastStatus = "";
  #pane = "";
  #pending: string[][] = [];
  #session: TerminalSessionLike | undefined;
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

  clearTarget(): void {
    this.#clearTimer();
    this.#session?.disconnect();
    this.#session = undefined;
    this.#pane = "";
    this.#pending = [];
    this.#activeBatch = undefined;
    this.#currentRequestID = undefined;
    this.#acquired = false;
    this.#forwardedBatches = 0;
    this.#lastStatus = "";
    this.#terminalID = undefined;
  }

  enqueue(text: string): boolean {
    return this.enqueueBatch([text]);
  }

  enqueueBatch(chunks: string[]): boolean {
    const batch = chunks.filter(Boolean);
    if (!this.#pane || batch.length === 0 || (!this.#session && this.#pending.length > 0)) return false;
    this.#pending.push(batch);
    this.#events.onLog("reader.input-queued", {
      batches: this.#pending.length,
      chunks: batch.length,
      characters: batch.reduce((total, text) => total + text.length, 0),
    });
    this.#start(false);
    return true;
  }

  retry(takeover: boolean): void {
    if (this.#session || this.#pending.length === 0) return;
    this.#start(takeover);
  }

  #start(takeover: boolean): void {
    if (this.#session || !this.#pane || this.#pending.length === 0) return;
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
    this.#events.onSending(this.#pending.length);
    this.#events.onState?.("requesting");
    this.#events.onLog("reader.control.acquire", { mode, batches: this.#pending.length, ...this.#dimensions });
    session.connect(this.#pane, this.#dimensions);
    this.#armTimer(session, "Timed out while waiting for terminal control");
  }

  #acquire(session: TerminalSessionLike): void {
    if (this.#session !== session || this.#acquired) return;
    this.#acquired = true;
    this.#clearTimer();
    this.#events.onState?.("forwarding");
    this.#sendNext(session);
  }

  #inputForwarded(session: TerminalSessionLike, requestID: number): void {
    if (this.#session !== session || requestID !== this.#currentRequestID) return;
    this.#clearTimer();
    this.#currentRequestID = undefined;
    this.#activeBatch = undefined;
    this.#forwardedBatches += 1;
    this.#sendNext(session);
  }

  #sendNext(session: TerminalSessionLike): void {
    if (this.#session !== session || this.#currentRequestID !== undefined) return;
    const next = this.#pending.shift();
    if (!next) {
      this.#finished(session);
      return;
    }
    this.#activeBatch = next;
    const requestID = session.inputBatch(next);
    if (requestID === undefined) {
      this.#pending.unshift(next);
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
    const forwardedBatches = this.#forwardedBatches;
    this.#session = undefined;
    session.disconnect();
    this.#events.onLog("reader.control.forwarded", {
      takeover: this.#takeover,
      batches: forwardedBatches,
    });
    this.#forwardedBatches = 0;
    this.#acquired = false;
    this.#events.onState?.("observing");
    this.#events.onForwarded(forwardedBatches);
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
    if (!ambiguous && this.#activeBatch) this.#pending.unshift(this.#activeBatch);
    this.#activeBatch = undefined;
    this.#currentRequestID = undefined;
    this.#acquired = false;
    this.#forwardedBatches = 0;
    const occupied = this.#lastStatus.includes("already has an attached client");
    if (occupied) {
      this.#events.onState?.("occupied");
      this.#events.onLog("reader.control.blocked", { message: this.#lastStatus, queuedBatches: this.#pending.length });
      this.#events.onBlocked("Someone else is controlling this terminal, so your input was not sent.");
      return;
    }
    this.#events.onState?.("observing");
    if (ambiguous) {
      this.#pending = [];
      this.#events.onLog("reader.control.uncertain", { message: this.#lastStatus, queuedBatches: this.#pending.length });
      this.#events.onUncertain("The input was handed to the connection, but delivery could not be confirmed. Check the terminal before sending it again.");
      return;
    }
    this.#events.onLog("reader.control.blocked", { message: this.#lastStatus, queuedBatches: this.#pending.length });
    this.#events.onBlocked("Could not acquire terminal control, so your input was not sent.");
  }

  #clearTimer(): void {
    if (this.#attemptTimer === undefined) return;
    window.clearTimeout(this.#attemptTimer);
    this.#attemptTimer = undefined;
  }
}

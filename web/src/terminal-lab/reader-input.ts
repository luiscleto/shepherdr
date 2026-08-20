import type { TerminalDimensions } from "./adapter";
import { TerminalSession } from "./session";

interface ReaderInputEvents {
  onBlocked(message: string): void;
  onLog(event: string, detail?: unknown): void;
  onSending(count: number): void;
  onSent(count: number): void;
}

interface ReaderInputOptions {
  endpoint?: string;
}

const acquireTimeoutMilliseconds = 2_500;

export class ReaderInputQueue {
  #acquireTimer: number | undefined;
  #current: string | undefined;
  #dimensions: TerminalDimensions = { cols: 80, rows: 24 };
  #events: ReaderInputEvents;
  #endpoint: string;
  #inFlight: string[][] = [];
  #lastStatus = "";
  #outbound: string[] = [];
  #pane = "";
  #pending: string[][] = [];
  #sentBatches = 0;
  #session: TerminalSession | undefined;
  #takeover = false;
  #terminalID: string | undefined;

  constructor(events: ReaderInputEvents, options: ReaderInputOptions = {}) {
    this.#events = events;
    this.#endpoint = options.endpoint ?? "/api/terminal-lab";
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
    this.#inFlight = [];
    this.#outbound = [];
    this.#current = undefined;
    this.#sentBatches = 0;
    this.#lastStatus = "";
    this.#terminalID = undefined;
  }

  enqueue(text: string): boolean {
    return this.enqueueBatch([text]);
  }

  enqueueBatch(chunks: string[]): boolean {
    const batch = chunks.filter(Boolean);
    if (!this.#pane || batch.length === 0) return false;
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
    this.#inFlight = this.#pending.splice(0);
    this.#lastStatus = "";
    this.#takeover = takeover;
    const mode = takeover ? "takeover" : "control";
    const session = new TerminalSession(mode, {
      onFrame: () => this.#acquired(session),
      onInputAcknowledged: () => this.#inputAcknowledged(session),
      onLog: (event, detail) => {
        this.#events.onLog(`reader.control.${event}`, detail);
        if (event === "session.close") this.#failed(session);
      },
      onStatus: (message) => {
        this.#lastStatus = message;
      },
    }, { endpoint: this.#endpoint, terminalID: this.#terminalID });
    this.#session = session;
    this.#events.onSending(this.#inFlight.length);
    this.#events.onLog("reader.control.acquire", { mode, batches: this.#inFlight.length, ...this.#dimensions });
    session.connect(this.#pane, this.#dimensions);
    this.#armTimer(session, "Timed out while waiting for terminal control");
  }

  #acquired(session: TerminalSession): void {
    if (this.#session !== session || this.#current || this.#outbound.length > 0) return;
    this.#clearTimer();
    const batches = [...this.#inFlight, ...this.#pending];
    this.#inFlight = [];
    this.#pending = [];
    this.#sentBatches = batches.length;
    this.#outbound = batches.flat();
    this.#sendNext(session);
  }

  #inputAcknowledged(session: TerminalSession): void {
    if (this.#session !== session || !this.#current) return;
    this.#clearTimer();
    this.#current = undefined;
    this.#sendNext(session);
  }

  #sendNext(session: TerminalSession): void {
    if (this.#session !== session || this.#current) return;
    if (this.#outbound.length === 0 && this.#pending.length > 0) {
      this.#sentBatches += this.#pending.length;
      this.#outbound = this.#pending.splice(0).flat();
    }
    const next = this.#outbound.shift();
    if (next === undefined) {
      this.#finished(session);
      return;
    }
    this.#current = next;
    if (!session.input(next)) {
      this.#lastStatus = "The terminal input stream was not available";
      this.#failed(session);
      return;
    }
    this.#armTimer(session, "Timed out while sending terminal input");
  }

  #finished(session: TerminalSession): void {
    if (this.#session !== session) return;
    const sentBatches = this.#sentBatches;
    this.#session = undefined;
    session.disconnect();
    this.#events.onLog("reader.control.sent", {
      takeover: this.#takeover,
      batches: sentBatches,
    });
    this.#sentBatches = 0;
    this.#events.onSent(sentBatches);
  }

  #armTimer(session: TerminalSession, message: string): void {
    this.#clearTimer();
    this.#acquireTimer = window.setTimeout(() => {
      if (this.#session !== session) return;
      this.#lastStatus = message;
      session.disconnect();
      this.#failed(session);
    }, acquireTimeoutMilliseconds);
  }

  #failed(session: TerminalSession): void {
    if (this.#session !== session) return;
    this.#clearTimer();
    this.#session = undefined;
    session.disconnect();
    const uncertain = [this.#current, ...this.#outbound].filter((text): text is string => text !== undefined);
    this.#pending = [...this.#inFlight, ...(uncertain.length > 0 ? [uncertain] : []), ...this.#pending];
    this.#inFlight = [];
    this.#outbound = [];
    this.#current = undefined;
    this.#sentBatches = 0;
    const occupied = this.#lastStatus.includes("already has an attached client");
    const message = occupied
      ? "Someone else is controlling this terminal, so your input was not sent."
      : this.#lastStatus.includes("sending terminal input")
        ? "The terminal did not confirm your input, so it was kept for retry."
        : "Could not acquire terminal control, so your input was not sent.";
    this.#events.onLog("reader.control.blocked", { message: this.#lastStatus, queuedBatches: this.#pending.length });
    this.#events.onBlocked(message);
  }

  #clearTimer(): void {
    if (this.#acquireTimer === undefined) return;
    window.clearTimeout(this.#acquireTimer);
    this.#acquireTimer = undefined;
  }
}

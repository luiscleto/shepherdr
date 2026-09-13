import type { ReaderInputState } from "./reader-availability";
import type { TerminalSessionLike } from "./session";

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
  session(): TerminalSessionLike | undefined;
  occupied(): boolean;
  acquire(takeover: boolean): Promise<boolean>;
}

// Tracks one deliberate action's acknowledgement; the page owns the connection.
export class ReaderInputQueue {
  #events: ReaderInputEvents;
  #options: ReaderInputOptions;
  #state: ReaderInputState = "ready";
  #pending: string[] | undefined;
  #active: { session: TerminalSessionLike; requestID: number } | undefined;
  #timer: number | undefined;
  #generation = 0;

  constructor(events: ReaderInputEvents, options: ReaderInputOptions) {
    this.#events = events;
    this.#options = options;
  }

  state(): ReaderInputState { return this.#state; }
  enqueue(text: string): boolean { return this.enqueueBatch([text]); }

  enqueueBatch(chunks: string[]): boolean {
    const batch = chunks.filter(Boolean);
    if (this.#state !== "ready" || batch.length === 0) return false;
    if (!this.#options.session() && !this.#options.occupied()) return false;
    this.#pending = batch;
    this.#send();
    return true;
  }

  async retry(takeover: boolean): Promise<void> {
    if (!this.#pending || (takeover ? this.#state !== "occupied" : this.#state !== "failed")) return;
    if (takeover) {
      const generation = this.#generation;
      this.#setState("requesting");
      const acquired = await this.#options.acquire(true);
      if (generation !== this.#generation) return;
      if (!acquired) { this.#failed(); return; }
    }
    this.#send();
  }

  forwarded(session: TerminalSessionLike, requestID: number): void {
    if (this.#active?.session !== session || this.#active.requestID !== requestID) return;
    this.#clearTimer();
    this.#active = undefined;
    this.#setState("ready");
    this.#events.onForwarded(1);
  }

  disconnected(session: TerminalSessionLike): void {
    if (this.#active?.session === session) this.#uncertain();
  }

  inputUnconfirmed(): void {
    this.#clearTimer();
    this.#active = undefined;
    this.#pending = undefined;
    this.#setState("uncertain");
    this.#events.onUncertain("The input was handed to the connection, but delivery could not be confirmed. Check the terminal before sending it again.");
  }

  dismissUncertain(): void {
    if (this.#state === "uncertain") this.#setState("ready");
  }

  clearTarget(): void {
    this.#generation++;
    this.#clearTimer();
    this.#pending = undefined;
    this.#active = undefined;
    this.#state = "ready";
  }

  #send(): void {
    const session = this.#options.session();
    if (!session) { this.#failed(); return; }
    const chunks = this.#pending;
    if (!chunks) return;
    const requestID = session.inputBatch(chunks);
    if (requestID === undefined) { this.#failed(); return; }
    this.#pending = undefined;
    this.#active = { session, requestID };
    this.#setState("forwarding");
    this.#events.onSending(1);
    this.#timer = window.setTimeout(() => this.#uncertain(), 2_500);
  }

  #failed(): void {
    if (this.#options.occupied()) {
      this.#setState("occupied");
      this.#events.onOccupied("Someone else is controlling this terminal, so your input was not sent.");
    } else {
      this.#setState("failed");
      this.#events.onFailed("The terminal input stream was not available, so your input was not sent.");
    }
  }

  #uncertain(): void {
    if (!this.#active) return;
    this.inputUnconfirmed();
  }

  #clearTimer(): void {
    if (this.#timer !== undefined) window.clearTimeout(this.#timer);
    this.#timer = undefined;
  }

  #setState(state: ReaderInputState): void {
    this.#state = state;
    this.#events.onState?.(state);
  }
}

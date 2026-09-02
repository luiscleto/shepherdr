import type { TerminalDimensions } from "./adapter";
import {
  TerminalSession,
  type SessionMode,
  type TerminalSessionEvents,
  type TerminalSessionLike,
} from "./session";

interface ReaderSizerEvents {
  onActive(active: boolean): void;
  onSettled(): void;
}

interface ReaderSizerOptions {
  createSession?: (mode: SessionMode, events: TerminalSessionEvents, endpoint: string, terminalID: string) => TerminalSessionLike;
  endpoint: string;
}

export class ReaderTerminalSizer {
  #active: TerminalSessionLike | undefined;
  #available = false;
  #cols: number | undefined;
  #createSession: NonNullable<ReaderSizerOptions["createSession"]>;
  #destroyed = false;
  #endpoint: string;
  #events: ReaderSizerEvents;
  #observerReady = false;
  #pane: string;
  #pending = false;
  #rows: number | undefined;
  #terminalID: string;

  constructor(pane: string, terminalID: string, events: ReaderSizerEvents, options: ReaderSizerOptions) {
    this.#pane = pane;
    this.#terminalID = terminalID;
    this.#events = events;
    this.#endpoint = options.endpoint;
    this.#createSession = options.createSession ?? ((mode, sessionEvents, endpoint, currentTerminalID) =>
      new TerminalSession(mode, sessionEvents, { endpoint, terminalID: currentTerminalID }));
  }

  active(): boolean {
    return this.#active !== undefined;
  }

  observerFrame(dimensions: TerminalDimensions): void {
    if (this.#destroyed) return;
    const needsResize = !this.#observerReady || dimensions.rows !== this.#rows;
    this.#observerReady = true;
    this.#rows = dimensions.rows;
    if (needsResize) this.#pending = true;
    this.#start();
  }

  observerLost(): void {
    this.#observerReady = false;
    this.#rows = undefined;
    this.#pending = false;
    this.#stop(false);
  }

  request(): void {
    if (this.#destroyed || this.#cols === undefined) return;
    this.#pending = true;
    this.#start();
  }

  setAvailable(available: boolean): void {
    if (this.#destroyed || this.#available === available) return;
    this.#available = available;
    if (!available) this.#stop(true);
    else this.#start();
  }

  viewportChanged(dimensions: TerminalDimensions): void {
    if (this.#destroyed || dimensions.cols === this.#cols) return;
    this.#cols = dimensions.cols;
    this.#pending = true;
    this.#start();
  }

  destroy(): void {
    this.#destroyed = true;
    this.#observerReady = false;
    this.#pending = false;
    const active = this.#active;
    this.#active = undefined;
    active?.disconnect();
  }

  #start(): void {
    if (this.#destroyed || !this.#available || !this.#observerReady || !this.#pending || this.#active ||
      this.#cols === undefined || this.#rows === undefined) return;
    this.#pending = false;
    const dimensions = { cols: this.#cols, rows: this.#rows };
    let session: TerminalSessionLike;
    const finish = () => {
      if (this.#active !== session) return;
      this.#active = undefined;
      session.disconnect();
      this.#events.onActive(false);
      this.#events.onSettled();
      this.#start();
    };
    session = this.#createSession("control", {
      onFrame: finish,
      onLog: (event) => {
        if (event === "session.close") finish();
      },
      onStatus: () => undefined,
    }, this.#endpoint, this.#terminalID);
    this.#active = session;
    this.#events.onActive(true);
    session.connect(this.#pane, dimensions);
  }

  #stop(retry: boolean): void {
    const active = this.#active;
    if (!active) return;
    this.#active = undefined;
    if (retry && this.#observerReady) this.#pending = true;
    active.disconnect();
    this.#events.onActive(false);
  }
}

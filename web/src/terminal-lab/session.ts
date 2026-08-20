import type { TerminalDimensions } from "./adapter";

export type SessionMode = "observe" | "control" | "takeover";

interface TerminalFrame {
  bytes: string;
  encoding: "ansi";
  full: boolean;
  height: number;
  seq: number;
  type: "terminal.frame";
  width: number;
}

interface TerminalStatus {
  message: string;
  type: "terminal.status";
}

interface TerminalInputAccepted {
  request_id: number;
  type: "terminal.input-accepted";
}

export interface TerminalSessionEvents {
  onFrame(frame: TerminalFrame, bytes: Uint8Array): void;
  onInputAcknowledged?(requestID: number): void;
  onLog(message: string, detail?: unknown): void;
  onStatus(message: string): void;
}

interface TerminalSessionOptions {
  endpoint?: string;
  terminalID?: string;
}

function decodeBase64(value: string): Uint8Array {
  const decoded = atob(value);
  return Uint8Array.from(decoded, (character) => character.charCodeAt(0));
}

export class TerminalSession {
  #endpoint: string;
  #events: TerminalSessionEvents;
  #lastSize: string | undefined;
  #mode: SessionMode;
  #nextInputRequestID = 0;
  #socket: WebSocket | undefined;
  #terminalID: string | undefined;

  constructor(mode: SessionMode, events: TerminalSessionEvents, options: TerminalSessionOptions = {}) {
    this.#mode = mode;
    this.#events = events;
    this.#endpoint = options.endpoint ?? "/api/terminal-lab";
    this.#terminalID = options.terminalID;
  }

  connect(pane: string, dimensions: TerminalDimensions): void {
    this.disconnect();
    const query = new URLSearchParams({
      pane,
      mode: this.#mode,
      cols: String(dimensions.cols),
      rows: String(dimensions.rows),
    });
    if (this.#terminalID) query.set("terminal", this.#terminalID);
    const protocol = location.protocol === "https:" ? "wss:" : "ws:";
    const socket = new WebSocket(`${protocol}//${location.host}${this.#endpoint}?${query}`);
    this.#socket = socket;
    this.#lastSize = `${dimensions.cols}x${dimensions.rows}`;
    this.#events.onStatus("Connecting");
    socket.addEventListener("open", () => {
      this.#events.onStatus(this.#mode === "observe" ? "Observing" : "Controlling");
      this.#events.onLog("session.open", { pane, mode: this.#mode, ...dimensions });
    });
    socket.addEventListener("message", (event) => {
      if (typeof event.data !== "string") return;
      let message: unknown;
      try {
        message = JSON.parse(event.data);
      } catch (error) {
        this.#events.onLog("session.invalid-json", String(error));
        return;
      }
      if (!message || typeof message !== "object" || !("type" in message)) return;
      if (message.type === "terminal.frame") {
        const frame = message as TerminalFrame;
        this.#events.onFrame(frame, decodeBase64(frame.bytes));
      } else if (message.type === "terminal.status") {
        const status = message as TerminalStatus;
        this.#events.onStatus(status.message);
        this.#events.onLog("session.status", status.message);
      } else if (message.type === "terminal.input-accepted") {
        const accepted = message as TerminalInputAccepted;
        this.#events.onLog("input.accepted", { requestID: accepted.request_id });
        this.#events.onInputAcknowledged?.(accepted.request_id);
      } else {
        this.#events.onLog("session.message", message);
      }
    });
    socket.addEventListener("close", (event) => {
      if (this.#socket !== socket) return;
      this.#socket = undefined;
      this.#events.onStatus("Disconnected");
      this.#events.onLog("session.close", { code: event.code, reason: event.reason });
    });
    socket.addEventListener("error", () => this.#events.onLog("session.error"));
  }

  input(text: string): boolean {
    if (this.#mode === "observe") {
      this.#events.onLog("input.blocked", { reason: "observer", characters: text.length });
      return false;
    }
    this.#nextInputRequestID += 1;
    return this.#send({ type: "terminal.input", text, request_id: this.#nextInputRequestID });
  }

  resize(dimensions: TerminalDimensions): void {
    const size = `${dimensions.cols}x${dimensions.rows}`;
    if (size === this.#lastSize) return;
    this.#lastSize = size;
    this.#events.onLog("viewport.resize", dimensions);
    if (this.#mode === "observe") return;
    this.#send({ type: "terminal.resize", ...dimensions });
  }

  disconnect(): void {
    const socket = this.#socket;
    this.#socket = undefined;
    if (!socket) return;
    if (this.#mode !== "observe" && socket.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: "terminal.release" }));
    }
    socket.close(1000, "lab disconnect");
    this.#events.onStatus("Disconnected");
  }

  #send(value: unknown): boolean {
    if (this.#socket?.readyState !== WebSocket.OPEN) {
      this.#events.onLog("send.blocked", { reason: "not connected" });
      return false;
    }
    this.#socket.send(JSON.stringify(value));
    return true;
  }
}

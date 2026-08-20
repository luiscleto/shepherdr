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

interface LabStatus {
  message: string;
  type: "lab.status";
}

interface LabInputAccepted {
  request_id: number;
  type: "lab.input-accepted";
}

export interface LabSessionEvents {
  onFrame(frame: TerminalFrame, bytes: Uint8Array): void;
  onInputAcknowledged?(requestID: number): void;
  onLog(message: string, detail?: unknown): void;
  onStatus(message: string): void;
}

function decodeBase64(value: string): Uint8Array {
  const decoded = atob(value);
  return Uint8Array.from(decoded, (character) => character.charCodeAt(0));
}

export class LabSession {
  #events: LabSessionEvents;
  #lastSize: string | undefined;
  #mode: SessionMode;
  #nextInputRequestID = 0;
  #socket: WebSocket | undefined;

  constructor(mode: SessionMode, events: LabSessionEvents) {
    this.#mode = mode;
    this.#events = events;
  }

  connect(pane: string, dimensions: TerminalDimensions): void {
    this.disconnect();
    const query = new URLSearchParams({
      pane,
      mode: this.#mode,
      cols: String(dimensions.cols),
      rows: String(dimensions.rows),
    });
    const protocol = location.protocol === "https:" ? "wss:" : "ws:";
    const socket = new WebSocket(`${protocol}//${location.host}/api/terminal-lab?${query}`);
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
      } else if (message.type === "lab.status") {
        const status = message as LabStatus;
        this.#events.onStatus(status.message);
        this.#events.onLog("session.status", status.message);
      } else if (message.type === "lab.input-accepted") {
        const accepted = message as LabInputAccepted;
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

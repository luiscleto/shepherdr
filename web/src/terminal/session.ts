import type { TerminalDimensions, TerminalScroll } from "./adapter";

export type SessionMode = "observe" | "control" | "takeover";

export interface TerminalFrame {
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

interface TerminalInputForwarded {
  request_id: number;
  type: "terminal.input-forwarded";
}

interface TerminalHeartbeat {
  type: "terminal.heartbeat";
}

interface TerminalController {
  type: "terminal.controller";
  handle: string;
}

type TerminalServerMessage = TerminalFrame | TerminalStatus | TerminalInputForwarded | TerminalHeartbeat | TerminalController;

export interface TerminalSessionEvents {
  onFrame(frame: TerminalFrame, bytes: Uint8Array): void;
  onInputForwarded?(requestID: number): void;
  onLog(message: string, detail?: unknown): void;
  onStatus(message: string): void;
}

export interface TerminalSessionLike {
  connect(pane: string, dimensions: TerminalDimensions): void;
  disconnect(): void;
  input(text: string): number | undefined;
  inputBatch(chunks: string[]): number | undefined;
  resize(dimensions: TerminalDimensions): void;
  scroll(scroll: TerminalScroll): boolean;
}

interface TerminalSessionOptions {
  endpoint: string;
  terminalID?: string;
}

const terminalMessageTimeoutMilliseconds = 7_000;
const base64Pattern = /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/;

function exactKeys(value: Record<string, unknown>, keys: string[]): boolean {
  const actual = Object.keys(value).sort();
  return actual.length === keys.length && actual.every((key, index) => key === [...keys].sort()[index]);
}

function positiveSafeInteger(value: unknown): value is number {
  return Number.isSafeInteger(value) && Number(value) > 0;
}

function boundedDimension(value: unknown, minimum: number): value is number {
  return Number.isSafeInteger(value) && Number(value) >= minimum && Number(value) <= 1_000;
}

function decodeBase64(value: string): Uint8Array {
  if (value.length % 4 !== 0 || !base64Pattern.test(value)) throw new Error("invalid terminal frame bytes");
  const decoded = atob(value);
  return Uint8Array.from(decoded, (character) => character.charCodeAt(0));
}

export class TerminalFrameSequence {
  #lastSequence = 0;

  accept(value: unknown): { frame: TerminalFrame; bytes: Uint8Array } {
    if (!value || typeof value !== "object") throw new Error("terminal frame is not an object");
    const frame = value as Record<string, unknown>;
    if (!exactKeys(frame, ["bytes", "encoding", "full", "height", "seq", "type", "width"]) ||
      frame.type !== "terminal.frame" || frame.encoding !== "ansi" || typeof frame.full !== "boolean" ||
      typeof frame.bytes !== "string" || !positiveSafeInteger(frame.seq) ||
      !boundedDimension(frame.width, 1) || !boundedDimension(frame.height, 1)) {
      throw new Error("terminal frame fields are invalid");
    }
    if (this.#lastSequence === 0 && frame.full !== true) throw new Error("first terminal frame is not full");
    if (this.#lastSequence > 0 && frame.seq <= this.#lastSequence) throw new Error("terminal frame sequence is not monotonic");
    if (this.#lastSequence > 0 && frame.full !== true && frame.seq !== this.#lastSequence + 1) {
      throw new Error("terminal delta frame has a sequence gap");
    }
    const bytes = decodeBase64(frame.bytes);
    this.#lastSequence = frame.seq;
    return { frame: frame as unknown as TerminalFrame, bytes };
  }
}

function parseNonFrameMessage(value: unknown): TerminalServerMessage {
  if (!value || typeof value !== "object") throw new Error("terminal message is not an object");
  const message = value as Record<string, unknown>;
  if (message.type === "terminal.controller" && exactKeys(message, ["type", "handle"]) &&
    typeof message.handle === "string" && /^[a-f0-9]{64}$/.test(message.handle)) return message as unknown as TerminalController;
  if (message.type === "terminal.heartbeat" && exactKeys(message, ["type"])) {
    return message as unknown as TerminalHeartbeat;
  }
  if (message.type === "terminal.status" && exactKeys(message, ["message", "type"]) && typeof message.message === "string" && message.message.length <= 8_192) {
    return message as unknown as TerminalStatus;
  }
  if (message.type === "terminal.input-forwarded" && exactKeys(message, ["request_id", "type"]) && positiveSafeInteger(message.request_id)) {
    return message as unknown as TerminalInputForwarded;
  }
  throw new Error("terminal message fields are invalid");
}

export class TerminalSession implements TerminalSessionLike {
  #controllerHandle: string | undefined;
  #endpoint: string;
  #events: TerminalSessionEvents;
  #lastSize: string | undefined;
  #livenessTimer: number | undefined;
  #mode: SessionMode;
  #nextInputRequestID = 0;
  #socket: WebSocket | undefined;
  #terminalID: string | undefined;

  constructor(mode: SessionMode, events: TerminalSessionEvents, options: TerminalSessionOptions) {
    this.#mode = mode;
    this.#events = events;
    this.#endpoint = options.endpoint;
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
    const frames = new TerminalFrameSequence();
    this.#socket = socket;
    this.#lastSize = `${dimensions.cols}x${dimensions.rows}`;
    this.#events.onStatus("Connecting");
    this.#armLiveness(socket);
    socket.addEventListener("open", () => {
      if (this.#socket !== socket) return;
      this.#events.onLog("session.open", { pane, mode: this.#mode, ...dimensions });
      this.#armLiveness(socket);
    });
    socket.addEventListener("message", (event) => {
      if (this.#socket !== socket || typeof event.data !== "string") return;
      try {
        const parsed: unknown = JSON.parse(event.data);
        if (typeof parsed === "object" && parsed !== null && "type" in parsed && parsed.type === "terminal.frame") {
          const accepted = frames.accept(parsed);
          this.#armLiveness(socket);
          this.#events.onFrame(accepted.frame, accepted.bytes);
          return;
        }
        const message = parseNonFrameMessage(parsed);
        this.#armLiveness(socket);
        if (message.type === "terminal.status") {
          this.#events.onStatus(message.message);
          this.#events.onLog("session.status", message.message);
        } else if (message.type === "terminal.input-forwarded") {
          this.#events.onLog("input.forwarded", { requestID: message.request_id });
          this.#events.onInputForwarded?.(message.request_id);
        } else if (message.type === "terminal.controller" && this.#mode !== "observe") {
          this.#controllerHandle = message.handle;
        }
      } catch (error) {
        this.#events.onLog("session.invalid-message", String(error));
        socket.close(1002, "Invalid terminal protocol");
      }
    });
    socket.addEventListener("close", (event) => {
      if (this.#socket !== socket) return;
      this.#socket = undefined;
      this.#controllerHandle = undefined;
      this.#clearLiveness();
      this.#events.onLog("session.close", { code: event.code, reason: event.reason });
      this.#events.onStatus("Disconnected");
    });
    socket.addEventListener("error", () => {
      if (this.#socket === socket) this.#events.onLog("session.error");
    });
  }

  input(text: string): number | undefined {
    if (!text) return undefined;
    return this.#sendInput("terminal.input", { text });
  }

  inputBatch(chunks: string[]): number | undefined {
    const batch = chunks.filter(Boolean);
    if (batch.length === 0) return undefined;
    return this.#sendInput("terminal.input-batch", { chunks: batch });
  }

  resize(dimensions: TerminalDimensions): void {
    if (!boundedDimension(dimensions.cols, 2) || !boundedDimension(dimensions.rows, 1)) return;
    const size = `${dimensions.cols}x${dimensions.rows}`;
    if (size === this.#lastSize) return;
    this.#lastSize = size;
    this.#events.onLog("viewport.resize", dimensions);
    if (this.#mode === "observe") return;
    this.#send({ type: "terminal.resize", ...dimensions });
  }

  scroll(scroll: TerminalScroll): boolean {
    if (this.#mode === "observe") return false;
    return this.#send({ type: "terminal.scroll", source: "wheel", ...scroll });
  }

  disconnect(): Promise<void> {
    this.#controllerHandle = undefined;
    const socket = this.#socket;
    this.#socket = undefined;
    this.#clearLiveness();
    if (!socket || socket.readyState === WebSocket.CLOSED) return Promise.resolve();
    const closed = new Promise<void>((resolve) => socket.addEventListener("close", () => resolve(), { once: true }));
    if (this.#mode !== "observe" && socket.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: "terminal.release" }));
    }
    socket.close(1000, "Client disconnect");
    this.#events.onStatus("Disconnected");
    return closed;
  }

  controllerHandle(): string | undefined { return this.#controllerHandle; }

  #sendInput(type: "terminal.input" | "terminal.input-batch", fields: Record<string, unknown>): number | undefined {
    if (this.#mode === "observe") {
      this.#events.onLog("input.blocked", { reason: "observer" });
      return undefined;
    }
    this.#nextInputRequestID += 1;
    const requestID = this.#nextInputRequestID;
    if (!this.#send({ type, ...fields, request_id: requestID })) return undefined;
    return requestID;
  }

  #send(value: unknown): boolean {
    if (this.#socket?.readyState !== WebSocket.OPEN) {
      this.#events.onLog("send.blocked", { reason: "not connected" });
      return false;
    }
    this.#socket.send(JSON.stringify(value));
    return true;
  }

  #armLiveness(socket: WebSocket): void {
    this.#clearLiveness();
    this.#livenessTimer = window.setTimeout(() => {
      if (this.#socket !== socket) return;
      this.#events.onLog("session.timeout");
      socket.close(4000, "Terminal stream timed out");
    }, terminalMessageTimeoutMilliseconds);
  }

  #clearLiveness(): void {
    if (this.#livenessTimer === undefined) return;
    window.clearTimeout(this.#livenessTimer);
    this.#livenessTimer = undefined;
  }
}

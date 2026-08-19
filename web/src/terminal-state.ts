export const TERMINAL_STALE_AFTER_MS = 8_000;
export const TERMINAL_RECOVERY_LIMIT_MS = 30_000;
export const TERMINAL_RETRY_DELAY_MS = 1_000;
export const TERMINAL_GEOMETRY_SETTLE_MS = 140;

export type TerminalMode =
  | "connecting"
  | "observing"
  | "controlled"
  | "controlled_elsewhere"
  | "taking_control"
  | "taking_over"
  | "releasing"
  | "reconnecting"
  | "retry_exhausted"
  | "connection_failed"
  | "herdr_not_running"
  | "incompatible"
  | "terminal_unavailable";

export type InteractionMode = "input" | "select";

export interface TerminalStateCopy {
  body?: string;
  heading: string;
}

export class RecoveryWindow {
  #startedAt: number | undefined;

  markLost(now: number): void {
    this.#startedAt ??= now;
  }

  markFullFrame(): void {
    this.#startedAt = undefined;
  }

  manualRetry(now: number): void {
    this.#startedAt = now;
  }

  exhausted(now: number): boolean {
    return this.#startedAt !== undefined && now - this.#startedAt >= TERMINAL_RECOVERY_LIMIT_MS;
  }

  remaining(now: number): number {
    if (this.#startedAt === undefined) return TERMINAL_RECOVERY_LIMIT_MS;
    return Math.max(0, TERMINAL_RECOVERY_LIMIT_MS - (now - this.#startedAt));
  }
}

type RecoveryPolicy = "automatic" | "stopped" | "waiting_for_home";
export type TerminalStatusDisposition = "detach" | "keep" | "recover";
type HomeConnection = "live" | "reconnecting" | "not_running" | "incompatible";
export interface ProtocolOrderingEvidence {
  epoch: string;
  gap: number;
}
type WaitingEvidence =
  | { evidence?: ProtocolOrderingEvidence; kind: "target" }
  | { epoch: string; kind: "server" };

const serverEpochPattern = /^[0-9a-f]{32}$/;

export function protocolOrderingEvidence(value: { epoch?: unknown; gap?: unknown }): ProtocolOrderingEvidence | undefined {
  if (
    typeof value.epoch !== "string" ||
    !serverEpochPattern.test(value.epoch) ||
    typeof value.gap !== "number" ||
    !Number.isSafeInteger(value.gap) ||
    value.gap < 0
  ) {
    return undefined;
  }
  return { epoch: value.epoch, gap: value.gap };
}

export class TerminalRecoveryPolicy {
  #home: { connection: HomeConnection; evidence?: ProtocolOrderingEvidence } | undefined;
  #policy: RecoveryPolicy = "automatic";
  #waiting: WaitingEvidence | undefined;

  get automatic(): boolean {
    return this.#policy === "automatic";
  }

  home(connection: HomeConnection, evidence: ProtocolOrderingEvidence | undefined): boolean {
    const previous = this.#home;
    this.#home = { connection, evidence };
    if (this.#policy !== "waiting_for_home" || !this.#waiting) return false;

    if (
      this.#waiting.kind === "target" &&
      evidence &&
      this.#waiting.evidence &&
      evidence.epoch !== this.#waiting.evidence.epoch &&
      previous?.evidence?.epoch === this.#waiting.evidence.epoch
    ) {
      this.#waiting = { epoch: evidence.epoch, kind: "server" };
    }

    const currentTarget =
      this.#waiting.kind === "target" &&
      connection === "live" &&
      evidence !== undefined &&
      this.#waiting.evidence !== undefined &&
      evidence.epoch === this.#waiting.evidence.epoch &&
      evidence.gap >= this.#waiting.evidence.gap;
    const currentServer =
      this.#waiting.kind === "server" && connection === "live" && evidence?.epoch === this.#waiting.epoch;
    if (currentTarget || currentServer) {
      this.#policy = "automatic";
      this.#waiting = undefined;
      return true;
    }
    return false;
  }

  status(mode: TerminalMode, evidence: ProtocolOrderingEvidence | undefined): TerminalStatusDisposition {
    if (mode === "herdr_not_running" || mode === "incompatible") {
      if (
        evidence &&
        evidence.epoch === this.#home?.evidence?.epoch &&
        this.#home.connection === "live" &&
        this.#home.evidence.gap >= evidence.gap
      ) {
        this.#policy = "automatic";
        this.#waiting = undefined;
        return "recover";
      }
      this.#policy = "waiting_for_home";
      this.#waiting = { evidence, kind: "target" };
      return "detach";
    }
    if (mode === "terminal_unavailable" || mode === "connection_failed") {
      this.#policy = "stopped";
      this.#waiting = undefined;
      return "detach";
    }
    return "keep";
  }

  retry(): void {
    this.#policy = "automatic";
    this.#waiting = undefined;
  }

  stop(): void {
    this.#policy = "stopped";
    this.#waiting = undefined;
  }
}

export class TerminalSelection {
  #text = "";

  get copyAvailable(): boolean {
    return this.#text !== "";
  }

  get text(): string {
    return this.#text;
  }

  update(text: string): boolean {
    const cleared = this.#text !== "" && text === "";
    this.#text = text;
    return cleared;
  }

  invalidate(): boolean {
    return this.update("");
  }
}

export function hasRecentTraffic(lastTrafficAt: number | undefined, now: number): boolean {
  return lastTrafficAt !== undefined && now - lastTrafficAt < TERMINAL_STALE_AFTER_MS;
}

export function hasCurrentHomeEvidence(
  socketOpen: boolean,
  receivedHome: boolean,
  lastTrafficAt: number | undefined,
  now: number,
): boolean {
  return socketOpen && receivedHome && hasRecentTraffic(lastTrafficAt, now);
}

export function terminalStateCopy(
  mode: TerminalMode,
  interaction: InteractionMode,
  hasFrame: boolean,
  offline: boolean,
): TerminalStateCopy {
  if (interaction === "select" && mode === "controlled") {
    return { heading: "Select text", body: "Input is paused." };
  }
  if (offline && (mode === "connecting" || mode === "reconnecting")) {
    return { heading: "Offline · terminal is not live", body: hasFrame ? "State below may be stale." : undefined };
  }
  const labels: Record<TerminalMode, string> = {
    connecting: "Connecting to terminal",
    observing: "Observing · input is unavailable",
    controlled: "You have control",
    controlled_elsewhere: "Controlled elsewhere · observing",
    taking_control: "Taking control…",
    taking_over: "Taking control…",
    releasing: "Releasing control…",
    reconnecting: "Reconnecting to terminal",
    retry_exhausted: "Couldn't reconnect",
    connection_failed: "Connection failed",
    herdr_not_running: "Herdr is not running",
    incompatible: "Cannot use this Herdr",
    terminal_unavailable: "Terminal unavailable",
  };
  const stale = hasFrame && (mode === "reconnecting" || mode === "retry_exhausted" || mode === "connection_failed");
  if (mode === "terminal_unavailable") {
    return { heading: labels[mode], body: "This terminal is no longer here." };
  }
  return { heading: labels[mode], body: stale ? "State below may be stale." : undefined };
}

export function terminalControlLabels(mode: TerminalMode, interaction: InteractionMode, copyAvailable = false): string[] {
  if (mode === "controlled" && interaction === "select") {
    return ["Release control", ...(copyAvailable ? ["Copy"] : []), "Done"];
  }
  if (mode === "controlled") {
    return [
      "Release control",
      "Ctrl C",
      "Enter",
      "Select text",
      "Up",
      "Down",
      "Left",
      "Right",
      "Esc",
      "Tab",
      "Page up",
      "Page down",
    ];
  }
  if (mode === "controlled_elsewhere") return ["Take over", ...(copyAvailable ? ["Copy"] : [])];
  if (mode === "observing") return ["Control", ...(copyAvailable ? ["Copy"] : [])];
  return [];
}

export function terminalStateActionLabels(mode: TerminalMode): string[] {
  if (mode === "retry_exhausted") return ["Try again", "Home"];
  if (mode === "terminal_unavailable") return ["Home"];
  return [];
}

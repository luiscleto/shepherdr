export const HOME_STALE_AFTER_MS = 20_000;
export const HOME_RECOVERY_LIMIT_MS = 45_000;
export const HOME_RETRY_DELAY_MS = 1_200;
export const HOME_FIRST_VALID_FRAME_WINDOW_MS = 10_000;

export type HomeReachability = "current" | "offline" | "reconnecting";
export type HomeAccessMode = "active" | "checking" | "sign-in-off" | "signed-out" | "trust";

export interface HomeConnectionHandle {
  close(): void;
}

export class HomeConnectionOwner<T extends HomeConnectionHandle> {
  #current: T | undefined;
  #attemptStartedAt: number | undefined;
  #firstValidFrameReceived = false;

  get current(): T | undefined {
    return this.#current;
  }

  get attemptStartedAt(): number | undefined {
    return this.#attemptStartedAt;
  }

  get awaitingFirstValidFrame(): boolean {
    return this.#current !== undefined && !this.#firstValidFrameReceived;
  }

  active(isActive: (connection: T) => boolean): boolean {
    return this.#current !== undefined && isActive(this.#current);
  }

  owns(connection: T): boolean {
    return this.#current === connection;
  }

  release(connection: T): boolean {
    if (!this.owns(connection)) return false;
    this.#current = undefined;
    this.#attemptStartedAt = undefined;
    this.#firstValidFrameReceived = false;
    return true;
  }

  recordValidFrame(connection: T): boolean {
    if (!this.owns(connection)) return false;
    this.#firstValidFrameReceived = true;
    return true;
  }

  clear(): void {
    const stale = this.#current;
    this.#current = undefined;
    this.#attemptStartedAt = undefined;
    this.#firstValidFrameReceived = false;
    stale?.close();
  }

  replace(create: () => T, activate: (connection: T) => void, startedAt: number): T {
    this.clear();

    const connection = create();
    this.#current = connection;
    this.#attemptStartedAt = startedAt;
    this.#firstValidFrameReceived = false;
    activate(connection);
    return connection;
  }
}

export type HomeConnectionMaintenance = "connected" | "replaced" | "stopped" | "waiting";

export function homeConnectionAllowed(accessMode: HomeAccessMode, authorityReady: boolean): boolean {
  return accessMode === "sign-in-off" || (accessMode === "active" && authorityReady);
}

export function homeReachability(lastValidHomeFrameAt: number, now: number): HomeReachability {
  const absence = Math.max(0, now - lastValidHomeFrameAt);
  if (absence >= HOME_RECOVERY_LIMIT_MS) return "offline";
  if (absence >= HOME_STALE_AFTER_MS) return "reconnecting";
  return "current";
}

export function nextHomeCheckDelay(
  lastValidHomeFrameAt: number,
  now: number,
  socketActive: boolean,
  firstValidFrameWaitStartedAt?: number,
): number {
  const reachability = homeReachability(lastValidHomeFrameAt, now);
  const evidenceDeadline = lastValidHomeFrameAt + (
    reachability === "current" ? HOME_STALE_AFTER_MS : HOME_RECOVERY_LIMIT_MS
  );
  const untilEvidenceDeadline = reachability === "offline"
    ? HOME_RETRY_DELAY_MS
    : Math.max(1, evidenceDeadline - now);
  if (socketActive && firstValidFrameWaitStartedAt !== undefined) {
    const untilFirstFrameDeadline = firstValidFrameWaitStartedAt + HOME_FIRST_VALID_FRAME_WINDOW_MS - now;
    if (untilFirstFrameDeadline > 0) {
      return reachability === "offline"
        ? untilFirstFrameDeadline
        : Math.min(untilEvidenceDeadline, untilFirstFrameDeadline);
    }
  }
  return socketActive
    ? untilEvidenceDeadline
    : Math.min(HOME_RETRY_DELAY_MS, untilEvidenceDeadline);
}

export function maintainHomeConnection<T extends HomeConnectionHandle>(
  reachability: HomeReachability,
  owner: HomeConnectionOwner<T>,
  isActive: (connection: T) => boolean,
  create: () => T,
  activate: (connection: T) => void,
  now: number,
  replaceActive = false,
  allowed = true,
): HomeConnectionMaintenance {
  if (!allowed) {
    owner.clear();
    return "stopped";
  }
  const active = owner.active(isActive);
  if (active && owner.attemptStartedAt !== undefined) {
    const attemptAge = Math.max(0, now - owner.attemptStartedAt);
    if (attemptAge < HOME_FIRST_VALID_FRAME_WINDOW_MS) return "waiting";
    if (!owner.awaitingFirstValidFrame && reachability === "current" && !replaceActive) {
      return "waiting";
    }
  }
  owner.replace(create, activate, now);
  return active ? "replaced" : "connected";
}

export function refreshHomeConnectionView(homeActive: boolean, renderHome: () => void): void {
  if (homeActive) renderHome();
}

export function resumeHomeConnection(
  shouldResume: boolean,
  homeActive: boolean,
  renderHome: () => void,
  reconnect: () => void,
): boolean {
  if (!shouldResume) return false;
  refreshHomeConnectionView(homeActive, renderHome);
  reconnect();
  return true;
}

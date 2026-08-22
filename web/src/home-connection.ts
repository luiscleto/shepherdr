export const HOME_STALE_AFTER_MS = 20_000;
export const HOME_RECOVERY_LIMIT_MS = 45_000;
export const HOME_RETRY_DELAY_MS = 1_200;

export type HomeReachability = "current" | "offline" | "reconnecting";
export type HomeAccessMode = "active" | "checking" | "sign-in-off" | "signed-out" | "trust";

export interface HomeConnectionHandle {
  close(): void;
}

export class HomeConnectionOwner<T extends HomeConnectionHandle> {
  #current: T | undefined;

  get current(): T | undefined {
    return this.#current;
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
    return true;
  }

  clear(): void {
    const stale = this.#current;
    this.#current = undefined;
    stale?.close();
  }

  replace(create: () => T, activate: (connection: T) => void): T {
    this.clear();

    const connection = create();
    this.#current = connection;
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
): number {
  const reachability = homeReachability(lastValidHomeFrameAt, now);
  if (reachability === "offline") return HOME_RETRY_DELAY_MS;
  const deadline = lastValidHomeFrameAt + (
    reachability === "current" ? HOME_STALE_AFTER_MS : HOME_RECOVERY_LIMIT_MS
  );
  const untilDeadline = Math.max(1, deadline - now);
  return socketActive ? untilDeadline : Math.min(HOME_RETRY_DELAY_MS, untilDeadline);
}

export function maintainHomeConnection<T extends HomeConnectionHandle>(
  reachability: HomeReachability,
  owner: HomeConnectionOwner<T>,
  isActive: (connection: T) => boolean,
  create: () => T,
  activate: (connection: T) => void,
  replaceActive = false,
  allowed = true,
): HomeConnectionMaintenance {
  if (!allowed) {
    owner.clear();
    return "stopped";
  }
  const active = owner.active(isActive);
  if (active && reachability === "current" && !replaceActive) return "waiting";
  owner.replace(create, activate);
  return active ? "replaced" : "connected";
}

export function refreshHomeConnectionView(homeActive: boolean, renderHome: () => void): void {
  if (homeActive) renderHome();
}

export function resumeHomeConnection(
  visibilityState: DocumentVisibilityState,
  homeActive: boolean,
  renderHome: () => void,
  reconnect: () => void,
): boolean {
  if (visibilityState !== "visible") return false;
  refreshHomeConnectionView(homeActive, renderHome);
  reconnect();
  return true;
}

export const HOME_STALE_AFTER_MS = 20_000;
export const HOME_RECOVERY_LIMIT_MS = 45_000;
export const HOME_RETRY_DELAY_MS = 1_200;

export type HomeReachability = "current" | "offline" | "reconnecting";

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

export function resumeHomeConnection(
  visibilityState: DocumentVisibilityState,
  reconnect: () => void,
): void {
  if (visibilityState === "visible") reconnect();
}

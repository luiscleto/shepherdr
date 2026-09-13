export type TerminalOwnership = "controlling" | "observing" | "occupied" | "requesting" | "waiting";

export function terminalOwnershipAction(state: TerminalOwnership): "control" | "release" | "takeover" | undefined {
  if (state === "observing") return "control";
  if (state === "occupied") return "takeover";
  if (state === "controlling") return "release";
  return undefined;
}

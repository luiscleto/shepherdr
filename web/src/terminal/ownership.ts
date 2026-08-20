export type TerminalOwnership = "controlling" | "observing" | "occupied" | "requesting" | "waiting";

export type TerminalOwnershipEvent =
  | "acquired"
  | "failed"
  | "observer-lost"
  | "observer-ready"
  | "occupied"
  | "release"
  | "request";

export function nextTerminalOwnership(state: TerminalOwnership, event: TerminalOwnershipEvent): TerminalOwnership {
  if (event === "observer-lost") return "waiting";
  if (event === "observer-ready" && state === "waiting") return "observing";
  if (event === "request" && (state === "observing" || state === "occupied")) return "requesting";
  if (event === "acquired" && state === "requesting") return "controlling";
  if (event === "occupied" && state === "requesting") return "occupied";
  if ((event === "failed" || event === "release") && state !== "waiting") return "observing";
  return state;
}

export function terminalOwnershipAction(state: TerminalOwnership): "control" | "release" | "takeover" | undefined {
  if (state === "observing") return "control";
  if (state === "occupied") return "takeover";
  if (state === "controlling") return "release";
  return undefined;
}

import { findTerminal, type HomeState, type TerminalEntry } from "./home-model";

export type TerminalWaitingState = "offline" | "reconnecting" | "not_running" | "incompatible";

export type TerminalRouteResolution =
  | { entry: TerminalEntry; kind: "target" }
  | { kind: "missing" }
  | { kind: "waiting"; state: TerminalWaitingState };

export interface TerminalRouteContext {
  homeCurrent: boolean;
  offlineHint: boolean;
  receivedHome: boolean;
}

export function resolveTerminalRoute(
  state: HomeState,
  paneID: string,
  context: TerminalRouteContext,
): TerminalRouteResolution {
  const entry = findTerminal(state.home, paneID);
  if (entry) return { entry, kind: "target" };

  const coherentCurrentHome =
    context.receivedHome && context.homeCurrent && state.connection === "live" && !state.last_known;
  if (coherentCurrentHome) return { kind: "missing" };

  if (state.connection === "not_running") return { kind: "waiting", state: "not_running" };
  if (state.connection === "incompatible") return { kind: "waiting", state: "incompatible" };
  if (context.offlineHint && !context.homeCurrent) return { kind: "waiting", state: "offline" };
  return { kind: "waiting", state: "reconnecting" };
}

export function terminalWaitingLabel(state: TerminalWaitingState): string {
  switch (state) {
    case "offline":
      return "Offline · terminal is not live";
    case "not_running":
      return "Herdr is not running";
    case "incompatible":
      return "Cannot use this Herdr";
    case "reconnecting":
      return "Reconnecting to terminal";
  }
}

export function returningToHome(previousPane: string | undefined, nextPane: string | undefined): boolean {
  return previousPane !== undefined && nextPane === undefined;
}

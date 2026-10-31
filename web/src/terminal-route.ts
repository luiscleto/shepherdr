import type { Herdr } from "./home-model";

export function returningToHome(previousPane: string | undefined, nextPane: string | undefined): boolean {
  return previousPane !== undefined && nextPane === undefined;
}

export interface TerminalRoute {
  // herdr names the Herdr that holds the terminal. A link without it is from
  // the Herdr TUI; any other value names no Herdr.
  herdr: Herdr | "unknown";
  paneID: string;
  terminalID?: string;
}

export function parseTerminalRoute(hash: string): TerminalRoute | undefined {
  if (!hash.startsWith("#terminal=")) return undefined;
  const parameters = new URLSearchParams(hash.slice(1));
  const paneID = parameters.get("terminal");
  const terminalID = parameters.get("terminal_id");
  if (!paneID) return undefined;
  const named = parameters.get("herdr") ?? "tui";
  const herdr = named === "tui" || named === "core" ? named : "unknown";
  return { herdr, paneID, ...(terminalID ? { terminalID } : {}) };
}

export function terminalHash(herdr: Herdr, paneID: string, terminalID?: string): string {
  const terminal = terminalID === undefined ? "" : `&terminal_id=${encodeURIComponent(terminalID)}`;
  return `terminal=${encodeURIComponent(paneID)}${terminal}&herdr=${herdr}`;
}

export type TerminalRouteOutcome = "current" | "selected" | "unavailable" | "waiting";

export function terminalRouteOutcome(state: {
  currentStateAvailable: boolean;
  currentTerminalID?: string;
  expectedTerminalID?: string;
  selectedTerminalID?: string;
}): TerminalRouteOutcome {
  if (state.expectedTerminalID !== undefined) {
    if (!state.currentStateAvailable) return "waiting";
    return state.currentTerminalID === state.expectedTerminalID ? "current" : "unavailable";
  }
  if (state.currentTerminalID !== undefined) {
    if (state.selectedTerminalID !== undefined && state.currentTerminalID !== state.selectedTerminalID) {
      return "unavailable";
    }
    return "current";
  }
  if (state.currentStateAvailable) return "unavailable";
  return state.selectedTerminalID === undefined ? "waiting" : "selected";
}

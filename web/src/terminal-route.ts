export function returningToHome(previousPane: string | undefined, nextPane: string | undefined): boolean {
  return previousPane !== undefined && nextPane === undefined;
}

export interface TerminalRoute {
  paneID: string;
  terminalID?: string;
}

export function parseTerminalRoute(hash: string): TerminalRoute | undefined {
  if (!hash.startsWith("#terminal=")) return undefined;
  const parameters = new URLSearchParams(hash.slice(1));
  const paneID = parameters.get("terminal");
  const terminalID = parameters.get("terminal_id");
  if (!paneID) return undefined;
  return { paneID, ...(terminalID ? { terminalID } : {}) };
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

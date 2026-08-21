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

export function exactTerminalMatches(currentTerminalID: string, expectedTerminalID?: string): boolean {
  return expectedTerminalID === undefined || currentTerminalID === expectedTerminalID;
}

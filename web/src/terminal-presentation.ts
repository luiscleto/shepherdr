import type { TerminalEntry } from "./home-model";

export interface TerminalIdentityNodes {
  heading: HTMLElement;
}

export function updateTerminalIdentity(nodes: TerminalIdentityNodes, entry: TerminalEntry): void {
  if (nodes.heading.textContent !== entry.terminal.title) nodes.heading.textContent = entry.terminal.title;
}

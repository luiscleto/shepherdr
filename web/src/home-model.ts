export type AgentStatus = "working" | "blocked" | "idle" | "done" | "unknown";
export type Connection = "reconnecting" | "live" | "not_running" | "incompatible";

export interface Agent {
  kind: string;
  name: string;
  status: AgentStatus;
}

export interface Terminal {
  agent?: Agent;
  pane_id: string;
  terminal_id: string;
  title: string;
}

export interface Tab {
  current: boolean;
  id: string;
  label: string;
  number: number;
  terminals: Terminal[];
}

export interface Workspace {
  agent_counts?: AgentCounts;
  id: string;
  label: string;
  number: number;
  tabs: Tab[];
  worktrees?: Workspace[];
}

export interface AgentCounts {
  working?: number;
  blocked?: number;
  idle?: number;
  done?: number;
  unknown?: number;
}

export interface Home {
  blocked_count: number;
  working_count: number;
  workspaces: Workspace[];
}

export interface HomeState {
  connection: Connection;
  detail?: string;
  epoch?: unknown;
  gap: number;
  has_home: boolean;
  home: Home;
  last_known: boolean;
}

export interface TerminalEntry {
  tab: Tab;
  terminal: Terminal;
  workspace: Workspace;
}

export function allTerminals(home: Home): TerminalEntry[] {
  return allWorkspaces(home).flatMap((workspace) =>
    workspace.tabs.flatMap((tab) => tab.terminals.map((terminal) => ({ tab, terminal, workspace }))),
  );
}

export function allWorkspaces(home: Home): Workspace[] {
  return home.workspaces.flatMap((workspace) => [workspace, ...(workspace.worktrees ?? [])]);
}

export function workspaceSets(home: Home): Workspace[] {
  return home.workspaces.filter((workspace) => (workspace.worktrees?.length ?? 0) > 0);
}

export function findTerminal(home: Home, paneID: string): TerminalEntry | undefined {
  return allTerminals(home).find(({ terminal }) => terminal.pane_id === paneID);
}

export function terminalCount(workspace: Workspace): number {
  return workspace.tabs.reduce((total, tab) => total + tab.terminals.length, 0);
}

export function showTabHeadings(workspace: Workspace): boolean {
  return workspace.tabs.filter((tab) => tab.terminals.length > 0).length > 1;
}

export function visibleTabs(workspace: Workspace, blockedOnly: boolean): Array<{ tab: Tab; terminals: Terminal[] }> {
  return workspace.tabs
    .map((tab) => ({
      tab,
      terminals: blockedOnly ? tab.terminals.filter((terminal) => terminal.agent?.status === "blocked") : tab.terminals,
    }))
    .filter(({ terminals }) => terminals.length > 0);
}

export function accessibleTerminalName(entry: TerminalEntry, tabsShown: boolean): string {
  const parts = [`Open ${entry.terminal.title}`, `workspace ${entry.workspace.label}`];
  if (tabsShown) parts.push(`tab ${entry.tab.label}`);
  if (entry.terminal.agent) {
    if (entry.terminal.agent.name !== entry.terminal.title) parts.push(entry.terminal.agent.name);
    parts.push(entry.terminal.agent.status);
  }
  return parts.join(", ");
}

import type { WorkspaceAction } from "./workspace-actions";

export type AgentStatus = "working" | "blocked" | "error" | "idle" | "done" | "unknown";
export type Herdr = "tui" | "core";
export type HomeMode = "all" | "blocked" | "error";
export type Connection = "reconnecting" | "live" | "not_running" | "incompatible";
export type TerminalAction = "split_terminal" | "rename_terminal" | "close_terminal";

export interface Agent {
  kind: string;
  name: string;
  status: AgentStatus;
}

export interface Terminal {
  actions: TerminalAction[];
  agent?: Agent;
  manual_name?: string;
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
  actions: WorkspaceAction[];
  agent_counts?: AgentCounts;
  checkout_path?: string;
  group_agent_counts?: AgentCounts;
  id: string;
  label: string;
  number: number;
  tabs: Tab[];
  worktrees?: Workspace[];
}

export interface AgentCounts {
  working?: number;
  blocked?: number;
  error?: number;
  idle?: number;
  done?: number;
  unknown?: number;
}

export interface Home {
  blocked_count: number;
  error_count: number;
  working_count: number;
  workspaces: Workspace[];
}

export interface HomeState {
  connection: Connection;
  detail?: string;
  epoch?: string;
  gap: number;
  has_home: boolean;
  herdr: Herdr;
  herdr_version: string;
  home: Home;
  last_known: boolean;
}

export interface TerminalEntry {
  tab: Tab;
  terminal: Terminal;
  workspace: Workspace;
}

export interface HomePlace {
  focusPane?: string;
  focusWorkspace?: string;
  scroll: number;
}

export function automaticAllTerminalsPlace(
  mode: HomeMode,
  viewCount: number,
  allTerminalsPlace: HomePlace,
): HomePlace | undefined {
  return mode !== "all" && viewCount === 0 ? allTerminalsPlace : undefined;
}

export function viewCount(mode: HomeMode, home: Home): number {
  if (mode === "blocked") return home.blocked_count;
  if (mode === "error") return home.error_count;
  return 0;
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

export function visibleTabs(workspace: Workspace, only?: AgentStatus): Array<{ tab: Tab; terminals: Terminal[] }> {
  return workspace.tabs
    .map((tab) => ({
      tab,
      terminals: only ? tab.terminals.filter((terminal) => terminal.agent?.status === only) : tab.terminals,
    }))
    .filter(({ terminals }) => terminals.length > 0);
}

export function spaceWord(herdr: Herdr): string {
  return herdr === "core" ? "space" : "workspace";
}

export function accessibleTerminalName(entry: TerminalEntry, tabsShown: boolean, herdr: Herdr = "tui"): string {
  const parts = [`Open ${entry.terminal.title}`, `${spaceWord(herdr)} ${entry.workspace.label}`];
  if (tabsShown) parts.push(`tab ${entry.tab.label}`);
  if (entry.terminal.agent) {
    if (entry.terminal.agent.name !== entry.terminal.title) parts.push(entry.terminal.agent.name);
    parts.push(entry.terminal.agent.status);
  }
  return parts.join(", ");
}

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

// HomeState is one Herdr's state. Each Home message carries both.
export interface HomeState {
  connection: Connection;
  detail?: string;
  gap: number;
  has_home: boolean;
  herdr_version: string;
  home: Home;
  last_known: boolean;
}

export interface HomeSides {
  core: HomeState;
  tui: HomeState;
}

export interface HomeFrame extends HomeSides {
  epoch: string;
}

export const herdrNames: Readonly<Record<Herdr, string>> = { tui: "Herdr TUI", core: "Herdr Core" };

export function emptyHomeState(): HomeState {
  return {
    connection: "reconnecting",
    gap: 0,
    has_home: false,
    herdr_version: "",
    home: { blocked_count: 0, error_count: 0, working_count: 0, workspaces: [] },
    last_known: false,
  };
}

// A Herdr is settled once Shepherdr has finished its first attempt to reach it.
export function herdrSettled(state: HomeState): boolean {
  return state.connection !== "reconnecting" || state.has_home;
}

// A Herdr is present once Shepherdr has had a Home from it, or while it
// answers but can't be used.
export function herdrPresent(state: HomeState): boolean {
  return state.has_home || state.connection === "incompatible";
}

export function herdrSwitchShown(sides: HomeSides): boolean {
  return herdrPresent(sides.tui) && herdrPresent(sides.core);
}

// chooseHerdr picks the Herdr Home shows first: undefined until both are
// settled, then the remembered one if present, else the only present one,
// else Herdr Core when both are present, else the Herdr TUI.
export function chooseHerdr(sides: HomeSides, remembered: Herdr | undefined): Herdr | undefined {
  if (!herdrSettled(sides.tui) || !herdrSettled(sides.core)) return undefined;
  if (remembered && herdrPresent(sides[remembered])) return remembered;
  if (herdrPresent(sides.core)) return "core";
  return "tui";
}

const rememberedHerdrKey = "shepherdr.home.herdr";

export function readRememberedHerdr(storage: () => Storage): Herdr | undefined {
  try {
    const value = storage().getItem(rememberedHerdrKey);
    return value === "tui" || value === "core" ? value : undefined;
  } catch {
    return undefined;
  }
}

export function rememberHerdr(storage: () => Storage, herdr: Herdr): void {
  try {
    storage().setItem(rememberedHerdrKey, herdr);
  } catch {
    // Without storage the choice lasts for this visit.
  }
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
    if (entry.terminal.agent.name !== "" && entry.terminal.agent.name !== entry.terminal.title) {
      parts.push(entry.terminal.agent.name);
    }
    parts.push(entry.terminal.agent.status);
  }
  return parts.join(", ");
}

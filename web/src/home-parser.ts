import type {
  Agent,
  AgentCounts,
  AgentStatus,
  Home,
  HomeAction,
  HomeFrame,
  HomeState,
  Tab,
  Terminal,
  TerminalAction,
  Workspace,
} from "./home-model";
import type { WorkspaceAction } from "./workspace-actions";

const connections = new Set(["reconnecting", "live", "not_running", "incompatible"] as const);
const agentStatuses = new Set<AgentStatus>(["working", "blocked", "error", "idle", "done", "unknown"]);
const workspaceActions = new Set<WorkspaceAction>([
  "create_worktree",
  "close_workspace",
  "close_group",
  "delete_checkout",
  "rename_space",
  "rename_tab",
  "new_tab",
]);
const terminalActions = new Set<TerminalAction>(["split_terminal", "rename_terminal", "close_terminal", "pin_agent", "unpin_agent"]);

function record(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : undefined;
}

function exactKeys(value: Record<string, unknown>, required: readonly string[], optional: readonly string[] = []): boolean {
  const allowed = new Set([...required, ...optional]);
  return required.every((key) => Object.hasOwn(value, key)) && Object.keys(value).every((key) => allowed.has(key));
}

function nonnegativeInteger(value: unknown): value is number {
  return Number.isSafeInteger(value) && (value as number) >= 0;
}

function parseAgent(value: unknown): Agent | undefined {
  const agent = record(value);
  if (!agent || !exactKeys(agent, ["kind", "name", "status"], ["pinned"]) ||
    typeof agent.kind !== "string" || typeof agent.name !== "string" ||
    typeof agent.status !== "string" || !agentStatuses.has(agent.status as AgentStatus) ||
    (Object.hasOwn(agent, "pinned") && agent.pinned !== true)) return undefined;
  return { kind: agent.kind, name: agent.name, status: agent.status as AgentStatus, ...(agent.pinned === true ? { pinned: true as const } : {}) };
}

function parseTerminal(value: unknown): Terminal | undefined {
  const terminal = record(value);
  if (!terminal || !exactKeys(terminal, ["actions", "pane_id", "terminal_id", "title"], ["agent", "manual_name"]) ||
    !Array.isArray(terminal.actions) ||
    typeof terminal.pane_id !== "string" || typeof terminal.terminal_id !== "string" ||
    typeof terminal.title !== "string" ||
    (Object.hasOwn(terminal, "manual_name") && typeof terminal.manual_name !== "string")) return undefined;
  const actions: TerminalAction[] = [];
  for (const action of terminal.actions) {
    if (typeof action !== "string" || !terminalActions.has(action as TerminalAction) || actions.includes(action as TerminalAction)) {
      return undefined;
    }
    actions.push(action as TerminalAction);
  }
  const agent = Object.hasOwn(terminal, "agent") ? parseAgent(terminal.agent) : undefined;
  if (Object.hasOwn(terminal, "agent") && !agent) return undefined;
  return {
    actions,
    pane_id: terminal.pane_id,
    terminal_id: terminal.terminal_id,
    title: terminal.title,
    ...(agent ? { agent } : {}),
    ...(typeof terminal.manual_name === "string" ? { manual_name: terminal.manual_name } : {}),
  };
}

function parseTab(value: unknown): Tab | undefined {
  const tab = record(value);
  if (!tab || !exactKeys(tab, ["current", "id", "label", "number", "terminals"], ["manual_name"]) ||
    typeof tab.current !== "boolean" || typeof tab.id !== "string" || typeof tab.label !== "string" ||
    (Object.hasOwn(tab, "manual_name") && typeof tab.manual_name !== "string") ||
    !nonnegativeInteger(tab.number) || !Array.isArray(tab.terminals)) return undefined;
  const terminals: Terminal[] = [];
  for (const value of tab.terminals) {
    const terminal = parseTerminal(value);
    if (!terminal) return undefined;
    terminals.push(terminal);
  }
  return {
    current: tab.current,
    id: tab.id,
    label: tab.label,
    ...(typeof tab.manual_name === "string" ? { manual_name: tab.manual_name } : {}),
    number: tab.number,
    terminals,
  };
}

function parseAgentCounts(value: unknown): AgentCounts | undefined {
  const counts = record(value);
  const keys = ["working", "blocked", "error", "idle", "done", "unknown"] as const;
  if (!counts || !exactKeys(counts, [], keys)) return undefined;
  const result: AgentCounts = {};
  for (const key of keys) {
    if (!Object.hasOwn(counts, key)) continue;
    if (!nonnegativeInteger(counts[key])) return undefined;
    result[key] = counts[key];
  }
  return result;
}

function parseWorkspace(value: unknown, topLevel: boolean): Workspace | undefined {
  const workspace = record(value);
  if (!workspace || !exactKeys(
    workspace,
    ["actions", "id", "label", "number", "tabs"],
    ["agent_counts", "checkout_path", "group_agent_counts", "manual_name", "worktrees"],
  ) || typeof workspace.id !== "string" || typeof workspace.label !== "string" ||
    (Object.hasOwn(workspace, "manual_name") && typeof workspace.manual_name !== "string") ||
    !nonnegativeInteger(workspace.number) || !Array.isArray(workspace.tabs) || !Array.isArray(workspace.actions)) {
    return undefined;
  }
  const actions: WorkspaceAction[] = [];
  for (const action of workspace.actions) {
    if (typeof action !== "string" || !workspaceActions.has(action as WorkspaceAction) || actions.includes(action as WorkspaceAction)) {
      return undefined;
    }
    actions.push(action as WorkspaceAction);
  }
  if (Object.hasOwn(workspace, "checkout_path") && (!topLevel || typeof workspace.checkout_path !== "string")) {
    return undefined;
  }
  const tabs: Tab[] = [];
  for (const value of workspace.tabs) {
    const tab = parseTab(value);
    if (!tab) return undefined;
    tabs.push(tab);
  }
  let agentCounts: AgentCounts | undefined;
  if (Object.hasOwn(workspace, "agent_counts")) {
    agentCounts = parseAgentCounts(workspace.agent_counts);
    if (!agentCounts) return undefined;
  }
  let groupAgentCounts: AgentCounts | undefined;
  if (Object.hasOwn(workspace, "group_agent_counts")) {
    if (!topLevel) return undefined;
    groupAgentCounts = parseAgentCounts(workspace.group_agent_counts);
    if (!groupAgentCounts) return undefined;
  }
  let worktrees: Workspace[] | undefined;
  if (Object.hasOwn(workspace, "worktrees")) {
    if (!topLevel || !Array.isArray(workspace.worktrees)) return undefined;
    worktrees = [];
    for (const value of workspace.worktrees) {
      const worktree = parseWorkspace(value, false);
      if (!worktree) return undefined;
      worktrees.push(worktree);
    }
  }
  return {
    actions,
    id: workspace.id,
    label: workspace.label,
    number: workspace.number,
    tabs,
    ...(agentCounts ? { agent_counts: agentCounts } : {}),
    ...(groupAgentCounts ? { group_agent_counts: groupAgentCounts } : {}),
    ...(typeof workspace.checkout_path === "string" ? { checkout_path: workspace.checkout_path } : {}),
    ...(typeof workspace.manual_name === "string" ? { manual_name: workspace.manual_name } : {}),
    ...(worktrees ? { worktrees } : {}),
  };
}

function parseHome(value: unknown): Home | undefined {
  const home = record(value);
  if (!home || !exactKeys(home, ["blocked_count", "error_count", "working_count", "workspaces"], ["actions", "app_window"]) ||
    !nonnegativeInteger(home.blocked_count) || !nonnegativeInteger(home.error_count) || !nonnegativeInteger(home.working_count) ||
    !Array.isArray(home.workspaces)) return undefined;
  if (Object.hasOwn(home, "actions") && (!Array.isArray(home.actions) || home.actions.length !== 1 || home.actions[0] !== "create_space")) {
    return undefined;
  }
  if (Object.hasOwn(home, "app_window") && home.app_window !== "closed") return undefined;
  const workspaces: Workspace[] = [];
  for (const value of home.workspaces) {
    const workspace = parseWorkspace(value, true);
    if (!workspace) return undefined;
    workspaces.push(workspace);
  }
  return {
    ...(Object.hasOwn(home, "actions") ? { actions: ["create_space"] as HomeAction[] } : {}),
    ...(Object.hasOwn(home, "app_window") ? { app_window: "closed" as const } : {}),
    blocked_count: home.blocked_count,
    error_count: home.error_count,
    working_count: home.working_count,
    workspaces,
  };
}

const serverEpochPattern = /^[0-9a-f]{32}$/;

function parseHomeState(value: unknown): HomeState | undefined {
  const state = record(value);
  if (!state || !exactKeys(
    state,
    ["can_open_terminals", "connection", "gap", "has_home", "herdr_version", "home", "last_known"],
    ["detail"],
  ) || typeof state.can_open_terminals !== "boolean" || typeof state.connection !== "string" || !connections.has(state.connection as HomeState["connection"]) ||
    !nonnegativeInteger(state.gap) || typeof state.has_home !== "boolean" ||
    typeof state.herdr_version !== "string" || typeof state.last_known !== "boolean" ||
    (Object.hasOwn(state, "detail") && typeof state.detail !== "string")) return undefined;
  const home = parseHome(state.home);
  if (!home) return undefined;
  return {
    can_open_terminals: state.can_open_terminals,
    connection: state.connection as HomeState["connection"],
    gap: state.gap,
    has_home: state.has_home,
    herdr_version: state.herdr_version,
    home,
    last_known: state.last_known,
    ...(typeof state.detail === "string" ? { detail: state.detail } : {}),
  };
}

// parseCompleteHomeState reads one Home message: both Herdrs' complete states.
export function parseCompleteHomeState(value: unknown): HomeFrame | undefined {
  const frame = record(value);
  if (!frame || !exactKeys(frame, ["core", "epoch", "tui"]) ||
    typeof frame.epoch !== "string" || !serverEpochPattern.test(frame.epoch)) return undefined;
  const tui = parseHomeState(frame.tui);
  const core = parseHomeState(frame.core);
  if (!tui || !core) return undefined;
  return { core, epoch: frame.epoch, tui };
}

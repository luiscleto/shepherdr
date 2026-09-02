import type { Agent, AgentStatus } from "./home-model";
import type { ConfirmationFacts, RefusalReason } from "./workspace-actions";

export interface TerminalActionTarget {
  workspace_id: string;
  tab_id: string;
  pane_id: string;
  terminal_id: string;
}

export type SplitDirection = "right" | "down";

export interface PrepareTerminalCloseRequest extends TerminalActionTarget {
  action: "close_terminal";
}

export interface SplitTerminalRequest extends TerminalActionTarget {
  action: "split_terminal";
  direction: SplitDirection;
}

export interface RenameTerminalRequest extends TerminalActionTarget {
  action: "rename_terminal";
  name: string;
}

export interface TerminalCloseFacts {
  target: TerminalActionTarget;
  workspace_label: string;
  tab_label: string;
  terminal_title: string;
  agent?: Agent;
  closes_tab: boolean;
  workspace_action?: "close_workspace" | "close_group";
  workspace_expected?: ConfirmationFacts;
}

export interface CloseTerminalRequest {
  action: "close_terminal";
  expected: TerminalCloseFacts;
}

export type RunTerminalActionRequest = SplitTerminalRequest | RenameTerminalRequest | CloseTerminalRequest;

export interface PreparedTerminalClose {
  outcome: "prepared";
  action: "close_terminal";
  expected: TerminalCloseFacts;
}

export interface RefusedTerminalAction {
  outcome: "refused";
  reason: RefusalReason;
  detail?: string;
}

export interface UnknownTerminalAction {
  outcome: "unknown";
}

export interface SucceededTerminalAction {
  outcome: "succeeded";
  terminal?: TerminalActionTarget;
}

export type PreparedTerminalActionResponse = PreparedTerminalClose | RefusedTerminalAction;
export type RunTerminalActionResponse = SucceededTerminalAction | RefusedTerminalAction | UnknownTerminalAction;

type Fetcher = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

const statuses = new Set<AgentStatus>(["working", "blocked", "idle", "done", "unknown"]);
const refusalReasons = new Set<RefusalReason>([
  "invalid_request", "not_found", "not_applicable", "busy", "stale", "herdr_unavailable", "herdr_refused",
  "checkout_has_changes",
]);

function record(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : undefined;
}

function exactKeys(value: Record<string, unknown>, required: readonly string[], optional: readonly string[] = []): boolean {
  const allowed = new Set([...required, ...optional]);
  return required.every((key) => Object.hasOwn(value, key)) && Object.keys(value).every((key) => allowed.has(key));
}

function parseTarget(value: unknown): TerminalActionTarget | undefined {
  const target = record(value);
  if (!target || !exactKeys(target, ["workspace_id", "tab_id", "pane_id", "terminal_id"]) ||
    typeof target.workspace_id !== "string" || target.workspace_id === "" ||
    typeof target.tab_id !== "string" || target.tab_id === "" ||
    typeof target.pane_id !== "string" || target.pane_id === "" ||
    typeof target.terminal_id !== "string" || target.terminal_id === "") return undefined;
  return {
    workspace_id: target.workspace_id,
    tab_id: target.tab_id,
    pane_id: target.pane_id,
    terminal_id: target.terminal_id,
  };
}

function parseAgent(value: unknown): Agent | undefined {
  const agent = record(value);
  if (!agent || !exactKeys(agent, ["kind", "name", "status"]) ||
    typeof agent.kind !== "string" || typeof agent.name !== "string" ||
    typeof agent.status !== "string" || !statuses.has(agent.status as AgentStatus)) return undefined;
  return { kind: agent.kind, name: agent.name, status: agent.status as AgentStatus };
}

function nonnegativeInteger(value: unknown): value is number {
  return Number.isSafeInteger(value) && (value as number) >= 0;
}

function parseConfirmationFacts(value: unknown): ConfirmationFacts | undefined {
  const facts = record(value);
  if (!facts || !exactKeys(
    facts,
    ["workspace_label", "scope_workspace_ids", "agent_total", "interruption_counts"],
  ) || typeof facts.workspace_label !== "string" || !Array.isArray(facts.scope_workspace_ids) ||
    facts.scope_workspace_ids.length === 0 || facts.scope_workspace_ids.some((id) => typeof id !== "string" || id === "") ||
    !nonnegativeInteger(facts.agent_total)) return undefined;
  if (new Set(facts.scope_workspace_ids).size !== facts.scope_workspace_ids.length) return undefined;
  for (let index = 1; index < facts.scope_workspace_ids.length; index++) {
    if ((facts.scope_workspace_ids[index - 1] as string) > (facts.scope_workspace_ids[index] as string)) return undefined;
  }
  const counts = record(facts.interruption_counts);
  if (!counts || !exactKeys(counts, ["working", "blocked", "unknown"]) ||
    !nonnegativeInteger(counts.working) || !nonnegativeInteger(counts.blocked) || !nonnegativeInteger(counts.unknown)) return undefined;
  if (counts.working + counts.blocked + counts.unknown > facts.agent_total) return undefined;
  return {
    workspace_label: facts.workspace_label,
    scope_workspace_ids: facts.scope_workspace_ids as string[],
    agent_total: facts.agent_total,
    interruption_counts: { working: counts.working, blocked: counts.blocked, unknown: counts.unknown },
  };
}

function parseCloseFacts(value: unknown): TerminalCloseFacts | undefined {
  const facts = record(value);
  if (!facts || !exactKeys(
    facts,
    ["target", "workspace_label", "tab_label", "terminal_title", "closes_tab"],
    ["agent", "workspace_action", "workspace_expected"],
  ) || typeof facts.workspace_label !== "string" || typeof facts.tab_label !== "string" ||
    typeof facts.terminal_title !== "string" || typeof facts.closes_tab !== "boolean") return undefined;
  const target = parseTarget(facts.target);
  if (!target) return undefined;
  const agent = Object.hasOwn(facts, "agent") ? parseAgent(facts.agent) : undefined;
  if (Object.hasOwn(facts, "agent") && !agent) return undefined;
  const hasWorkspaceAction = Object.hasOwn(facts, "workspace_action");
  const hasWorkspaceExpected = Object.hasOwn(facts, "workspace_expected");
  if (hasWorkspaceAction !== hasWorkspaceExpected || (facts.closes_tab && hasWorkspaceAction)) return undefined;
  let workspaceAction: "close_workspace" | "close_group" | undefined;
  let workspaceExpected: ConfirmationFacts | undefined;
  if (hasWorkspaceAction) {
    if (facts.workspace_action !== "close_workspace" && facts.workspace_action !== "close_group") return undefined;
    workspaceAction = facts.workspace_action;
    workspaceExpected = parseConfirmationFacts(facts.workspace_expected);
    if (!workspaceExpected) return undefined;
  }
  return {
    target,
    workspace_label: facts.workspace_label,
    tab_label: facts.tab_label,
    terminal_title: facts.terminal_title,
    closes_tab: facts.closes_tab,
    ...(agent ? { agent } : {}),
    ...(workspaceAction && workspaceExpected
      ? { workspace_action: workspaceAction, workspace_expected: workspaceExpected }
      : {}),
  };
}

export function parseTerminalActionResponse(value: unknown): PreparedTerminalActionResponse | RunTerminalActionResponse | undefined {
  const response = record(value);
  if (!response || typeof response.outcome !== "string") return undefined;
  if (response.outcome === "unknown") return exactKeys(response, ["outcome"]) ? { outcome: "unknown" } : undefined;
  if (response.outcome === "refused") {
    if (!exactKeys(response, ["outcome", "reason"], ["detail"]) ||
      typeof response.reason !== "string" || !refusalReasons.has(response.reason as RefusalReason) ||
      (Object.hasOwn(response, "detail") && typeof response.detail !== "string")) return undefined;
    return {
      outcome: "refused",
      reason: response.reason as RefusalReason,
      ...(typeof response.detail === "string" ? { detail: response.detail } : {}),
    };
  }
  if (response.outcome === "prepared") {
    if (!exactKeys(response, ["outcome", "action", "expected"]) || response.action !== "close_terminal") return undefined;
    const expected = parseCloseFacts(response.expected);
    return expected ? { outcome: "prepared", action: "close_terminal", expected } : undefined;
  }
  if (response.outcome === "succeeded") {
    if (!exactKeys(response, ["outcome"], ["terminal"])) return undefined;
    const terminal = Object.hasOwn(response, "terminal") ? parseTarget(response.terminal) : undefined;
    if (Object.hasOwn(response, "terminal") && !terminal) return undefined;
    return { outcome: "succeeded", ...(terminal ? { terminal } : {}) };
  }
  return undefined;
}

function validStatus(status: number, response: PreparedTerminalActionResponse | RunTerminalActionResponse): boolean {
  if (response.outcome === "prepared" || response.outcome === "succeeded") return status === 200;
  if (response.outcome === "unknown") return status === 502 || status === 504;
  const statuses: Record<RefusalReason, number[]> = {
    invalid_request: [400],
    not_found: [404],
    not_applicable: [409],
    busy: [409],
    stale: [409],
    checkout_has_changes: [422],
    herdr_refused: [422],
    herdr_unavailable: [503],
  };
  return statuses[response.reason].includes(status);
}

export class TerminalActionRequestError extends Error {
  constructor() {
    super("Terminal action response was unavailable or invalid");
    this.name = "TerminalActionRequestError";
  }
}

export class TerminalActionsClient {
  readonly #fetcher: Fetcher;

  constructor(fetcher: Fetcher = globalThis.fetch.bind(globalThis)) {
    this.#fetcher = fetcher;
  }

  async prepare(request: PrepareTerminalCloseRequest): Promise<PreparedTerminalActionResponse> {
    const response = await this.#post("/api/terminal-actions/prepare", request);
    if (response.outcome !== "prepared" && response.outcome !== "refused") throw new TerminalActionRequestError();
    return response;
  }

  async run(request: RunTerminalActionRequest): Promise<RunTerminalActionResponse> {
    const response = await this.#post("/api/terminal-actions", request);
    if (response.outcome === "prepared") throw new TerminalActionRequestError();
    return response;
  }

  async #post(
    endpoint: string,
    request: object,
  ): Promise<PreparedTerminalActionResponse | RunTerminalActionResponse> {
    const httpResponse = await this.#fetcher(endpoint, {
      method: "POST",
      mode: "same-origin",
      credentials: "same-origin",
      cache: "no-store",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(request),
    });
    const contentType = httpResponse.headers.get("Content-Type") ?? "";
    if (!contentType.toLocaleLowerCase().includes("application/json")) throw new TerminalActionRequestError();
    let value: unknown;
    try {
      value = await httpResponse.json();
    } catch {
      throw new TerminalActionRequestError();
    }
    const response = parseTerminalActionResponse(value);
    if (!response || !validStatus(httpResponse.status, response)) throw new TerminalActionRequestError();
    return response;
  }
}

export type WorkspaceAction = "create_worktree" | "close_workspace" | "close_group" | "delete_checkout";
export type DestructiveWorkspaceAction = Exclude<WorkspaceAction, "create_worktree">;

export interface InterruptionCounts {
  working: number;
  blocked: number;
  unknown: number;
}

export interface ConfirmationFacts {
  workspace_label: string;
  scope_workspace_ids: string[];
  agent_total: number;
  interruption_counts: InterruptionCounts;
  checkout_path?: string;
}

export interface PreparedWorkspaceAction {
  outcome: "prepared";
  action: DestructiveWorkspaceAction;
  workspace_id: string;
  expected: ConfirmationFacts;
}

export type RefusalReason =
  | "invalid_request"
  | "not_found"
  | "not_applicable"
  | "busy"
  | "stale"
  | "herdr_unavailable"
  | "checkout_has_changes"
  | "herdr_refused";

export interface RefusedWorkspaceAction {
  outcome: "refused";
  reason: RefusalReason;
  detail?: string;
}

export interface UnknownWorkspaceAction {
  outcome: "unknown";
}

export interface SucceededWorkspaceAction {
  outcome: "succeeded";
}

export type WorkspaceActionResponse =
  | PreparedWorkspaceAction
  | RefusedWorkspaceAction
  | UnknownWorkspaceAction
  | SucceededWorkspaceAction;

export interface PrepareWorkspaceActionRequest {
  action: DestructiveWorkspaceAction;
  workspace_id: string;
}

export interface CreateSpaceRequest {
  action: "create_space";
  working_directory: string;
  label?: string;
}

export interface CreateWorktreeRequest {
  action: "create_worktree";
  workspace_id: string;
  branch?: string;
}

export interface ConfirmWorkspaceActionRequest {
  action: DestructiveWorkspaceAction;
  workspace_id: string;
  expected: ConfirmationFacts;
}

export type RunWorkspaceActionRequest = CreateSpaceRequest | CreateWorktreeRequest | ConfirmWorkspaceActionRequest;
export type PreparedWorkspaceActionResponse = PreparedWorkspaceAction | RefusedWorkspaceAction;
export type RunWorkspaceActionResponse = SucceededWorkspaceAction | RefusedWorkspaceAction | UnknownWorkspaceAction;

type Fetcher = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

const destructiveActions = new Set<DestructiveWorkspaceAction>([
  "close_workspace",
  "close_group",
  "delete_checkout",
]);
const refusalReasons = new Set<RefusalReason>([
  "invalid_request",
  "not_found",
  "not_applicable",
  "busy",
  "stale",
  "herdr_unavailable",
  "checkout_has_changes",
  "herdr_refused",
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

function nonnegativeInteger(value: unknown): value is number {
  return Number.isSafeInteger(value) && (value as number) >= 0;
}

function parseConfirmationFacts(value: unknown, action: DestructiveWorkspaceAction): ConfirmationFacts | undefined {
  const facts = record(value);
  if (!facts || !exactKeys(
    facts,
    ["workspace_label", "scope_workspace_ids", "agent_total", "interruption_counts"],
    ["checkout_path"],
  )) return undefined;
  if (typeof facts.workspace_label !== "string" || !nonnegativeInteger(facts.agent_total)) return undefined;
  if (!Array.isArray(facts.scope_workspace_ids) || facts.scope_workspace_ids.length === 0 ||
    !facts.scope_workspace_ids.every((id) => typeof id === "string") ||
    new Set(facts.scope_workspace_ids).size !== facts.scope_workspace_ids.length) {
    return undefined;
  }
  for (let index = 1; index < facts.scope_workspace_ids.length; index++) {
    if (facts.scope_workspace_ids[index - 1] > facts.scope_workspace_ids[index]) return undefined;
  }
  const counts = record(facts.interruption_counts);
  if (!counts || !exactKeys(counts, ["working", "blocked", "unknown"])) return undefined;
  if (!nonnegativeInteger(counts.working) || !nonnegativeInteger(counts.blocked) || !nonnegativeInteger(counts.unknown)) {
    return undefined;
  }
  if (counts.working + counts.blocked + counts.unknown > facts.agent_total) return undefined;
  if (action === "delete_checkout") {
    if (typeof facts.checkout_path !== "string") return undefined;
  } else if (Object.hasOwn(facts, "checkout_path")) {
    return undefined;
  }
  return {
    workspace_label: facts.workspace_label,
    scope_workspace_ids: [...facts.scope_workspace_ids],
    agent_total: facts.agent_total,
    interruption_counts: {
      working: counts.working,
      blocked: counts.blocked,
      unknown: counts.unknown,
    },
    ...(typeof facts.checkout_path === "string" ? { checkout_path: facts.checkout_path } : {}),
  };
}

export function parseWorkspaceActionResponse(value: unknown): WorkspaceActionResponse | undefined {
  const response = record(value);
  if (!response || typeof response.outcome !== "string") return undefined;
  if (response.outcome === "succeeded" || response.outcome === "unknown") {
    return exactKeys(response, ["outcome"]) ? { outcome: response.outcome } : undefined;
  }
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
    if (!exactKeys(response, ["outcome", "action", "workspace_id", "expected"]) ||
      typeof response.action !== "string" ||
      !destructiveActions.has(response.action as DestructiveWorkspaceAction) ||
      typeof response.workspace_id !== "string") return undefined;
    const action = response.action as DestructiveWorkspaceAction;
    const expected = parseConfirmationFacts(response.expected, action);
    if (!expected) return undefined;
    return { outcome: "prepared", action, workspace_id: response.workspace_id, expected };
  }
  return undefined;
}

function validStatus(status: number, response: WorkspaceActionResponse): boolean {
  if (response.outcome === "prepared" || response.outcome === "succeeded") return status === 200;
  if (response.outcome === "unknown") return status === 502 || status === 504;
  const expectedStatus: Record<RefusalReason, number> = {
    invalid_request: 400,
    not_found: 404,
    not_applicable: 409,
    busy: 409,
    stale: 409,
    herdr_unavailable: 503,
    checkout_has_changes: 422,
    herdr_refused: 422,
  };
  return status === expectedStatus[response.reason];
}

export class WorkspaceActionRequestError extends Error {
  constructor() {
    super("The workspace action response could not be confirmed.");
    this.name = "WorkspaceActionRequestError";
  }
}

export class WorkspaceActionsClient {
  readonly #fetch: Fetcher;

  constructor(fetcher: Fetcher = globalThis.fetch.bind(globalThis)) {
    this.#fetch = fetcher;
  }

  async prepare(request: PrepareWorkspaceActionRequest): Promise<PreparedWorkspaceActionResponse> {
    const response = await this.#post("/api/workspace-actions/prepare", request);
    if (response.outcome === "refused") return response;
    if (response.outcome !== "prepared" || response.action !== request.action || response.workspace_id !== request.workspace_id) {
      throw new WorkspaceActionRequestError();
    }
    return response;
  }

  async run(request: RunWorkspaceActionRequest): Promise<RunWorkspaceActionResponse> {
    try {
      const response = await this.#post("/api/workspace-actions", request);
      if (response.outcome === "succeeded" || response.outcome === "refused" || response.outcome === "unknown") {
        return response;
      }
    } catch {
      // Once a mutation request is submitted, a transport or parsing failure cannot
      // prove that Herdr did not act. The browser must not retry it.
    }
    return { outcome: "unknown" };
  }

  async #post(endpoint: string, request: object): Promise<WorkspaceActionResponse> {
    const httpResponse = await this.#fetch(endpoint, {
      method: "POST",
      mode: "same-origin",
      credentials: "same-origin",
      cache: "no-store",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(request),
    });
    const contentType = httpResponse.headers.get("Content-Type") ?? "";
    if (!contentType.toLocaleLowerCase().includes("application/json")) throw new WorkspaceActionRequestError();
    let value: unknown;
    try {
      value = await httpResponse.json();
    } catch {
      throw new WorkspaceActionRequestError();
    }
    const response = parseWorkspaceActionResponse(value);
    if (!response || !validStatus(httpResponse.status, response)) throw new WorkspaceActionRequestError();
    return response;
  }
}

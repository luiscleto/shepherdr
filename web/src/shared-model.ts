import type { Herdr } from "./home-model";

// A file an agent shared with `shepherdr share`, as the Home connection
// lists it. Names and labels are untrusted text.
export type SharedKind = "image" | "video" | "text" | "pdf" | "other";

export interface SharedTarget {
  pane_id: string;
  terminal_id: string;
  workspace_id: string;
}

export interface SharedFrom {
  agent?: string;
  // current is the terminal while it is still the one that shared.
  current?: SharedTarget;
  herdr: Herdr;
  place: string;
  // source names the exact terminal that shared, opaquely.
  source: string;
  tab?: string;
}

export interface SharedFile {
  from?: SharedFrom;
  id: string;
  kind: SharedKind;
  name: string;
  seen: boolean;
  shared_at: number;
  size: number;
  tag: string;
}

const kinds = new Set<SharedKind>(["image", "video", "text", "pdf", "other"]);
const idPattern = /^[0-9a-f]{32}$/;
const sourcePattern = /^[0-9a-f]{24}$/;
const serverEpochPattern = /^[0-9a-f]{32}$/;

function record(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : undefined;
}

function exactKeys(value: Record<string, unknown>, required: readonly string[], optional: readonly string[] = []): boolean {
  const allowed = new Set([...required, ...optional]);
  return required.every((key) => Object.hasOwn(value, key)) && Object.keys(value).every((key) => allowed.has(key));
}

function boundedString(value: unknown, max = 512): value is string {
  return typeof value === "string" && value.length <= max;
}

function parseTarget(value: unknown): SharedTarget | undefined {
  const target = record(value);
  if (!target || !exactKeys(target, ["pane_id", "terminal_id", "workspace_id"]) ||
    !boundedString(target.pane_id) || !boundedString(target.terminal_id) || !boundedString(target.workspace_id) ||
    target.pane_id === "" || target.terminal_id === "") return undefined;
  return { pane_id: target.pane_id, terminal_id: target.terminal_id, workspace_id: target.workspace_id };
}

function parseFrom(value: unknown): SharedFrom | undefined {
  const from = record(value);
  if (!from || !exactKeys(from, ["herdr", "place", "source"], ["agent", "current", "tab"]) ||
    (from.herdr !== "tui" && from.herdr !== "core") || !boundedString(from.place, 2048) ||
    typeof from.source !== "string" || !sourcePattern.test(from.source) ||
    (Object.hasOwn(from, "agent") && !boundedString(from.agent, 2048)) ||
    (Object.hasOwn(from, "tab") && !boundedString(from.tab, 2048))) return undefined;
  const current = Object.hasOwn(from, "current") ? parseTarget(from.current) : undefined;
  if (Object.hasOwn(from, "current") && !current) return undefined;
  return {
    herdr: from.herdr,
    place: from.place,
    source: from.source,
    ...(typeof from.agent === "string" && from.agent ? { agent: from.agent } : {}),
    ...(typeof from.tab === "string" && from.tab ? { tab: from.tab } : {}),
    ...(current ? { current } : {}),
  };
}

function parseFile(value: unknown): SharedFile | undefined {
  const file = record(value);
  if (!file || !exactKeys(file, ["id", "kind", "name", "seen", "shared_at", "size", "tag"], ["from"]) ||
    typeof file.id !== "string" || !idPattern.test(file.id) || typeof file.kind !== "string" || !kinds.has(file.kind as SharedKind) ||
    !boundedString(file.name, 1024) || file.name === "" || typeof file.seen !== "boolean" ||
    !Number.isSafeInteger(file.shared_at) || !Number.isSafeInteger(file.size) || (file.size as number) < 0 ||
    !boundedString(file.tag, 8)) return undefined;
  const from = Object.hasOwn(file, "from") ? parseFrom(file.from) : undefined;
  if (Object.hasOwn(file, "from") && !from) return undefined;
  return {
    id: file.id,
    kind: file.kind as SharedKind,
    name: file.name,
    seen: file.seen,
    shared_at: file.shared_at as number,
    size: file.size as number,
    tag: file.tag,
    ...(from ? { from } : {}),
  };
}

// parseSharedMessage reads the complete shared files list the Home
// connection sends, or undefined for any other or malformed message.
export function parseSharedMessage(value: unknown): SharedFile[] | undefined {
  const message = record(value);
  if (!message || message.type !== "shared" || !exactKeys(message, ["epoch", "files", "type"]) ||
    typeof message.epoch !== "string" || !serverEpochPattern.test(message.epoch) || !Array.isArray(message.files)) return undefined;
  const files: SharedFile[] = [];
  for (const candidate of message.files) {
    const file = parseFile(candidate);
    if (!file) return undefined;
    files.push(file);
  }
  return files;
}

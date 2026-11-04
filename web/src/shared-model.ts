import type { Herdr, TerminalEntry } from "./home-model";

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

export type SharedSort = "newest" | "oldest" | "largest" | "smallest" | "agent";

export const sharedSorts: readonly SharedSort[] = ["newest", "oldest", "largest", "smallest", "agent"];

export const sharedSortLabels: Record<SharedSort, string> = {
  agent: "By agent",
  largest: "Largest first",
  newest: "Newest first",
  oldest: "Oldest first",
  smallest: "Smallest first",
};

const sortKey = "shepherdr.shared.sort";

// Each browser remembers its sort.
export function readSharedSort(storage: () => Storage): SharedSort {
  try {
    const value = storage().getItem(sortKey);
    return sharedSorts.includes(value as SharedSort) ? value as SharedSort : "newest";
  } catch {
    return "newest";
  }
}

export function rememberSharedSort(storage: () => Storage, sort: SharedSort): void {
  try {
    storage().setItem(sortKey, sort);
  } catch {
    // The choice lasts for this visit.
  }
}

// SharedTerminal is a Terminal page's exact terminal.
export interface SharedTerminal {
  herdr: Herdr;
  paneID: string;
  terminalID: string;
}

// SharedFilter limits the list to one terminal's files: by its opaque
// source once known, else by its current IDs.
export interface SharedFilter {
  herdr: Herdr;
  source?: string;
  terminal?: SharedTerminal;
}

export function currentlyFrom(file: SharedFile, terminal: SharedTerminal): boolean {
  const current = file.from?.current;
  return file.from?.herdr === terminal.herdr && current?.pane_id === terminal.paneID && current.terminal_id === terminal.terminalID;
}

// filterSource is the filter's exact terminal: its source, or the source of
// a file its terminal shared while it is still current.
export function filterSource(files: readonly SharedFile[], filter: SharedFilter): string | undefined {
  if (filter.source) return filter.source;
  const terminal = filter.terminal;
  if (!terminal) return undefined;
  return files.find((file) => currentlyFrom(file, terminal))?.from?.source;
}

export function filesFor(files: readonly SharedFile[], filter: SharedFilter | undefined): SharedFile[] {
  if (!filter) return [...files];
  const source = filterSource(files, filter);
  return source === undefined ? [] : files.filter((file) => file.from?.source === source);
}

export function unseenCount(files: readonly SharedFile[], filter?: SharedFilter): number {
  return filesFor(files, filter).filter((file) => !file.seen).length;
}

export function sortFiles(files: readonly SharedFile[], sort: SharedSort): SharedFile[] {
  const newest = (a: SharedFile, b: SharedFile) => b.shared_at - a.shared_at || a.id.localeCompare(b.id);
  const sorted = [...files];
  switch (sort) {
    case "oldest": return sorted.sort((a, b) => -newest(a, b));
    case "largest": return sorted.sort((a, b) => b.size - a.size || newest(a, b));
    case "smallest": return sorted.sort((a, b) => a.size - b.size || newest(a, b));
    default: return sorted.sort(newest);
  }
}

export interface SharedGroup {
  files: SharedFile[];
  from?: SharedFrom;
  key: string;
}

// groupByAgent groups files under the terminal that shared them, newest
// file first in each group and the group with the newest file first.
export function groupByAgent(files: readonly SharedFile[]): SharedGroup[] {
  const groups = new Map<string, SharedGroup>();
  for (const file of sortFiles(files, "newest")) {
    const key = file.from?.source ?? "outside";
    const group = groups.get(key) ?? { files: [], key, ...(file.from ? { from: file.from } : {}) };
    group.files.push(file);
    groups.set(key, group);
  }
  return [...groups.values()];
}

export const herdrNames: Record<Herdr, string> = { core: "Herdr Core", tui: "Herdr TUI" };

// otherTerminalsID is the server's id for Home's last group, the Herdr Core
// terminals no space places.
const otherTerminalsID = "other";

// terminalFrom names a terminal from Home the way a file it shares will be:
// its space, plus the tab when a Herdr Core space has several, or its own
// title when it is outside every space.
export function terminalFrom(herdr: Herdr, entry: TerminalEntry): SharedFrom {
  const place = herdr === "core" && entry.workspace.id === otherTerminalsID ? entry.terminal.title : entry.workspace.label;
  const tab = herdr === "core" && entry.workspace.tabs.length > 1 ? entry.tab.label : undefined;
  return {
    herdr, place, source: "",
    ...(tab ? { tab } : {}),
    ...(entry.terminal.agent ? { agent: entry.terminal.agent.name } : {}),
  };
}

// withLiveNames names each file whose terminal is still current as that
// terminal is named on Home now. A file keeps the name it was shared with
// once its terminal is gone.
export function withLiveNames(files: readonly SharedFile[], live: (herdr: Herdr, target: SharedTarget) => SharedFrom | undefined): SharedFile[] {
  return files.map((file) => {
    const from = file.from;
    const name = from?.current ? live(from.herdr, from.current) : undefined;
    if (!from?.current || !name) return file;
    return {
      ...file,
      from: {
        current: from.current, herdr: from.herdr, place: name.place, source: from.source,
        ...(name.tab ? { tab: name.tab } : {}), ...(name.agent ? { agent: name.agent } : {}),
      },
    };
  });
}

// placeText is where a file came from: the space and tab, or the
// workspace.
export function placeText(from: SharedFrom): string {
  return from.tab ? `${from.place} / ${from.tab}` : from.place;
}

// agentText is the agent beside the place, unless the place already says it,
// as an agent terminal outside every space does.
export function agentText(from: SharedFrom): string | undefined {
  return from.agent && from.agent !== placeText(from) ? from.agent : undefined;
}

// fromText is a file's From line. markHerdr names its Herdr when both are
// present and Home shows the other one.
export function fromText(from: SharedFrom | undefined, markHerdr: boolean): string {
  if (!from) return "Outside Herdr";
  const parts = [placeText(from)];
  const agent = agentText(from);
  if (agent) parts.push(agent);
  if (markHerdr) parts.push(herdrNames[from.herdr]);
  return parts.join(" · ");
}

function trimSize(value: number): string {
  return value >= 10 ? value.toFixed(0) : value.toFixed(1).replace(/\.0$/, "");
}

// formatSize matches what `shepherdr share` prints, such as 96 KB.
export function formatSize(size: number): string {
  if (size < 1000) return `${size} B`;
  if (size < 1000 ** 2) return `${trimSize(size / 1000)} KB`;
  if (size < 1000 ** 3) return `${trimSize(size / 1000 ** 2)} MB`;
  return `${trimSize(size / 1000 ** 3)} GB`;
}

const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

function clock(date: Date): string {
  return `${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
}

function sameDay(a: Date, b: Date): boolean {
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
}

// formatShared says when a file was shared: minutes or hours ago today,
// then Yesterday and a date.
export function formatShared(at: number, now: Date): string {
  const shared = new Date(at);
  const minutes = Math.floor((now.getTime() - at) / 60_000);
  if (minutes < 1) return "Just now";
  if (minutes < 60) return `${minutes} min ago`;
  if (sameDay(shared, now)) return `${Math.floor(minutes / 60)} h ago`;
  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  if (sameDay(shared, yesterday)) return `Yesterday ${clock(shared)}`;
  const date = `${months[shared.getMonth()]} ${shared.getDate()}`;
  return shared.getFullYear() === now.getFullYear() ? `${date} ${clock(shared)}` : `${date}, ${shared.getFullYear()}`;
}

// SharedRoute is the Shared files page's place in the address: an open
// file, a filter, and the terminal Back returns to. A terminal's list shown
// whole keeps its terminal for Back, marked "all".
export interface SharedRoute {
  back?: SharedTerminal;
  file?: string;
  filter?: SharedFilter;
}

export function parseSharedRoute(hash: string): SharedRoute | undefined {
  if (hash !== "#shared" && !hash.startsWith("#shared=") && !hash.startsWith("#shared&")) return undefined;
  const parameters = new URLSearchParams(hash.slice(1));
  const file = parameters.get("shared") ?? "";
  const named = parameters.get("herdr");
  const herdr: Herdr | undefined = named === "tui" || named === "core" ? named : undefined;
  const paneID = parameters.get("terminal");
  const terminalID = parameters.get("terminal_id");
  const source = parameters.get("source");
  const all = parameters.get("all") === "1";
  const route: SharedRoute = {};
  if (idPattern.test(file)) route.file = file;
  if (herdr) {
    const terminal: SharedTerminal | undefined = paneID && terminalID ? { herdr, paneID, terminalID } : undefined;
    const knownSource = source && sourcePattern.test(source) ? source : undefined;
    if (terminal) route.back = terminal;
    if (!all && (terminal || knownSource)) {
      route.filter = { herdr, ...(knownSource ? { source: knownSource } : {}), ...(terminal ? { terminal } : {}) };
    }
  }
  return route;
}

export function sharedHash(route: SharedRoute): string {
  const parameters = new URLSearchParams();
  parameters.set("shared", route.file ?? "");
  const filter = route.filter;
  if (filter) {
    parameters.set("herdr", filter.herdr);
    if (filter.terminal) {
      parameters.set("terminal", filter.terminal.paneID);
      parameters.set("terminal_id", filter.terminal.terminalID);
    }
    if (filter.source) parameters.set("source", filter.source);
  } else if (route.back) {
    parameters.set("herdr", route.back.herdr);
    parameters.set("terminal", route.back.paneID);
    parameters.set("terminal_id", route.back.terminalID);
    parameters.set("all", "1");
  }
  return `#${parameters.toString().replace(/^shared=&/, "shared&").replace(/^shared=$/, "shared")}`;
}

export function sharedFileURL(id: string, download = false): string {
  return `/api/shared/${id}${download ? "?download=1" : ""}`;
}

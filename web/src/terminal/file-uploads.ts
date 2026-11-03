import type { ReaderInputState } from "./reader-availability";

export interface TerminalFileTarget {
  controller?: string;
  herdr: "tui" | "core";
  intent?: "insert" | "submit";
  paneID: string;
  terminalID: string;
  workspaceID: string;
}

export type TerminalFileResult = "forwarded" | "not_sent" | "occupied" | "unknown";

export interface TerminalFileOutcome {
  message: string;
  result: TerminalFileResult;
}

type Fetcher = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

const results = new Set<TerminalFileResult>(["forwarded", "not_sent", "occupied", "unknown"]);

export function uploadStateAfterLastFileRemoved(state: ReaderInputState): ReaderInputState {
  return state === "failed" || state === "occupied" || state === "uncertain" ? "ready" : state;
}

export async function sendTerminalFiles(
  target: TerminalFileTarget,
  text: string,
  files: readonly File[],
  takeover: boolean,
  signal: AbortSignal,
  fetcher: Fetcher = globalThis.fetch.bind(globalThis),
): Promise<TerminalFileOutcome> {
  try {
    for (const file of files) {
      if (signal.aborted) throw new DOMException("Aborted", "AbortError");
      await file.slice(0, 1).arrayBuffer();
    }
  } catch (error) {
    if (signal.aborted) throw error;
    return { message: "The selected files could not be read, so nothing was sent.", result: "not_sent" };
  }
  // The request line lists each file's name and size; the files' own bytes
  // follow it, so the browser streams them from disk without copying them.
  const request = JSON.stringify({
    controller: target.controller,
    intent: target.intent,
    files: files.map((file) => ({ name: file.name, size: file.size })),
    pane_id: target.paneID,
    takeover,
    terminal_id: target.terminalID,
    text,
    workspace_id: target.workspaceID,
  });
  let response: Response;
  try {
    response = await fetcher(`/api/terminal/files?herdr=${target.herdr}`, {
      body: new Blob([request, "\n", ...files]),
      cache: "no-store",
      credentials: "same-origin",
      headers: { Accept: "application/json", "Content-Type": "application/octet-stream" },
      method: "POST",
      signal,
    });
  } catch (error) {
    if (signal.aborted) throw error;
    return {
      message: "The input may have reached the terminal, but the result could not be confirmed. Check the terminal before sending it again.",
      result: "unknown",
    };
  }

  try {
    const value: unknown = await response.json();
    if (!value || typeof value !== "object") throw new Error("invalid response");
    const record = value as Record<string, unknown>;
    if (typeof record.message !== "string" || typeof record.result !== "string" || !results.has(record.result as TerminalFileResult)) {
      throw new Error("invalid response");
    }
    return { message: record.message, result: record.result as TerminalFileResult };
  } catch {
    return {
      message: "The input may have reached the terminal, but the result could not be confirmed. Check the terminal before sending it again.",
      result: "unknown",
    };
  }
}

import type { ReaderInputState } from "./reader-availability";

export interface TerminalFileTarget {
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
const preparedFileData = new WeakMap<File, Promise<string>>();

export function uploadStateAfterLastFileRemoved(state: ReaderInputState): ReaderInputState {
  return state === "failed" || state === "occupied" || state === "uncertain" ? "ready" : state;
}

export async function prepareTerminalFiles(files: readonly File[]): Promise<void> {
  await Promise.all(files.map(async (file) => {
    try {
      await terminalFileData(file);
    } catch {
      // Preserve selection; the existing send path will retry the read and report a definite failure if needed.
    }
  }));
}

export async function sendTerminalFiles(
  target: TerminalFileTarget,
  text: string,
  files: readonly File[],
  takeover: boolean,
  signal: AbortSignal,
  fetcher: Fetcher = globalThis.fetch.bind(globalThis),
): Promise<TerminalFileOutcome> {
  let encoded: Array<{ data: string; name: string }>;
  try {
    encoded = [];
    for (const file of files) {
      if (signal.aborted) throw new DOMException("Aborted", "AbortError");
      encoded.push({ data: await terminalFileData(file), name: file.name });
    }
  } catch (error) {
    if (signal.aborted) throw error;
    return { message: "The selected files could not be read, so nothing was sent.", result: "not_sent" };
  }

  let response: Response;
  try {
    response = await fetcher("/api/terminal/files", {
      body: JSON.stringify({
        files: encoded,
        pane_id: target.paneID,
        takeover,
        terminal_id: target.terminalID,
        text,
        workspace_id: target.workspaceID,
      }),
      cache: "no-store",
      credentials: "same-origin",
      headers: { Accept: "application/json", "Content-Type": "application/json" },
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

function terminalFileData(file: File): Promise<string> {
  const prepared = preparedFileData.get(file);
  if (prepared) return prepared;
  const preparation = file.arrayBuffer().then((value) => bytesToBase64(new Uint8Array(value)));
  preparedFileData.set(file, preparation);
  void preparation.catch(() => {
    if (preparedFileData.get(file) === preparation) preparedFileData.delete(file);
  });
  return preparation;
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  const chunkSize = 32_768;
  for (let offset = 0; offset < bytes.length; offset += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(offset, Math.min(bytes.length, offset + chunkSize)));
  }
  return btoa(binary);
}

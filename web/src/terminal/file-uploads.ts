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
      encoded.push({ data: bytesToBase64(new Uint8Array(await file.arrayBuffer())), name: file.name });
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
    if (!response.ok && response.status !== 401) return { message: "Files were not sent. Try again.", result: "not_sent" };
    return {
      message: "The input may have reached the terminal, but the result could not be confirmed. Check the terminal before sending it again.",
      result: "unknown",
    };
  }
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  const chunkSize = 32_768;
  for (let offset = 0; offset < bytes.length; offset += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(offset, Math.min(bytes.length, offset + chunkSize)));
  }
  return btoa(binary);
}

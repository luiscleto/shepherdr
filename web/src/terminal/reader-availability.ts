export type ReaderInputState = "failed" | "forwarding" | "occupied" | "ready" | "requesting" | "uncertain";

export interface ReaderActionAvailability {
  edit: boolean;
  observerReady: boolean;
  recover: boolean;
  send: boolean;
}

export function readerActionAvailability(observerReady: boolean, inputState: ReaderInputState): ReaderActionAvailability {
  return {
    edit: inputState === "ready" || inputState === "uncertain",
    observerReady,
    recover: inputState === "uncertain" || (observerReady && (inputState === "failed" || inputState === "occupied")),
    send: observerReady && inputState === "ready",
  };
}

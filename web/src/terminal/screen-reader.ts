// In screen reader mode xterm drops text a phone keyboard inserts without a
// key press (emoji picks, predictive-text taps, dictation). On a touch device
// the full terminal leaves it off unless this browser turned it on in
// Settings. Desktop keeps it on.
const preferenceKey = "shepherdr.terminal.screen-reader";

const listeners = new Set<() => void>();

export function readTerminalScreenReader(storage: () => Storage = () => window.localStorage): boolean {
  try {
    return storage().getItem(preferenceKey) === "on";
  } catch {
    return false;
  }
}

export function saveTerminalScreenReader(on: boolean, storage: () => Storage = () => window.localStorage): void {
  try {
    if (on) storage().setItem(preferenceKey, "on");
    else storage().removeItem(preferenceKey);
  } catch {
    // Without storage the full terminal keeps its default.
  }
  for (const listener of listeners) listener();
}

export function fullTerminalScreenReader(touch: boolean, storage: () => Storage = () => window.localStorage): boolean {
  return !touch || readTerminalScreenReader(storage);
}

// An open full terminal follows a change made in Settings right away.
export function onTerminalScreenReaderChange(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

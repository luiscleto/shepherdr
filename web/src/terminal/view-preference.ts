type TerminalView = "reader" | "terminal";

const preferenceKey = "shepherdr.terminal.view";

export function readTerminalView(): TerminalView | undefined {
  try {
    const value = window.localStorage.getItem(preferenceKey);
    return value === "reader" || value === "terminal" ? value : undefined;
  } catch {
    return undefined;
  }
}

export function saveTerminalView(view: TerminalView): void {
  try {
    window.localStorage.setItem(preferenceKey, view);
  } catch {
    // The chosen view still applies for this visit.
  }
}

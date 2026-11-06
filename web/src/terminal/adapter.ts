export interface TerminalDimensions {
  cols: number;
  rows: number;
}

export interface TerminalScroll {
  column: number;
  direction: "down" | "up";
  lines: number;
  modifiers: number;
  row: number;
}

const maximumScrollLines = 1_000;
const defaultFastScrollSensitivity = 5;
const likelyTrackpadScale = 0.3;

export interface TerminalWheelState {
  applicationRemainder: number;
  historyRemainder: number;
  pendingHistoryLines: number;
}

export function initialTerminalWheelState(): TerminalWheelState {
  return { applicationRemainder: 0, historyRemainder: 0, pendingHistoryLines: 0 };
}

export function terminalWheelRows(
  delta: number,
  deltaMode: number,
  rowHeight: number,
  viewportRows: number,
  state: TerminalWheelState,
  altKey = false,
): { lines: number; state: TerminalWheelState } {
  if (!Number.isFinite(delta) || !Number.isFinite(rowHeight) || rowHeight <= 0 ||
    !Number.isFinite(state.applicationRemainder) || !Number.isFinite(state.historyRemainder) ||
    !Number.isFinite(state.pendingHistoryLines)) {
    return { lines: 0, state: initialTerminalWheelState() };
  }
  const rows = deltaMode === 1 ? delta : deltaMode === 2 ? delta * viewportRows : delta / rowHeight;
  const historyTotal = rows + state.historyRemainder;
  const wholeHistoryRows = Math.trunc(historyTotal);
  const pendingHistoryLines = state.pendingHistoryLines + wholeHistoryRows;
  let applicationRemainder = state.applicationRemainder;
  let emit = rows !== 0;
  if (deltaMode === 0) {
    // Herdr uses lines for host history, but one command becomes one application wheel event.
    // Gate commands at xterm's application rate while retaining the full host-history distance.
    let applicationRows = rows;
    if (Math.abs(delta) < 50) {
      if (altKey) applicationRows *= defaultFastScrollSensitivity;
      applicationRows *= likelyTrackpadScale;
    }
    const applicationTotal = applicationRows + applicationRemainder;
    emit = Math.trunc(applicationTotal) !== 0;
    applicationRemainder = applicationTotal - Math.trunc(applicationTotal);
  }
  const nextState = {
    applicationRemainder,
    historyRemainder: historyTotal - wholeHistoryRows,
    pendingHistoryLines: emit ? 0 : pendingHistoryLines,
  };
  if (!emit) return { lines: 0, state: nextState };
  const lines = pendingHistoryLines === 0 ? (delta < 0 ? -1 : 1) : pendingHistoryLines;
  return {
    lines: Math.max(-maximumScrollLines, Math.min(maximumScrollLines, lines)),
    state: nextState,
  };
}

// Herdr Core has no scroll command, so a core terminal's wheel scrolls the
// page's own scrollback, which the attach fills with core's history. Whole
// rows scroll at once; the part of a row left over carries to the next event.
export function localScrollRows(
  delta: number,
  deltaMode: number,
  rowHeight: number,
  viewportRows: number,
  remainder: number,
): { lines: number; remainder: number } {
  if (!Number.isFinite(delta) || !Number.isFinite(rowHeight) || rowHeight <= 0 || !Number.isFinite(remainder)) {
    return { lines: 0, remainder: 0 };
  }
  const rows = (deltaMode === 1 ? delta : deltaMode === 2 ? delta * viewportRows : delta / rowHeight) + remainder;
  const lines = Math.trunc(rows);
  return { lines: Math.max(-maximumScrollLines, Math.min(maximumScrollLines, lines)), remainder: rows - lines };
}

// A core terminal's area, when the terminal is bigger than it, and its
// scrollback read as one page: moving up scrolls the area to its top, then
// the scrollback; moving down scrolls the scrollback back to the latest
// output, then the area. An area that doesn't overflow never moves.
export function localScrollMovesArea(up: boolean, areaTop: number, viewportY: number, baseY: number, overflows: boolean): boolean {
  return overflows && (up ? areaTop > 0 : viewportY >= baseY);
}

// A terminal bigger than its area, such as one another viewer sized, keeps
// its overflow inside the area. A swipe along an axis that overflows scrolls
// the area, so all of the terminal stays reachable; any other swipe stays a
// terminal gesture.
export function touchScrollsArea(dx: number, dy: number, overflowsX: boolean, overflowsY: boolean): boolean {
  return Math.abs(dx) > Math.abs(dy) ? overflowsX : overflowsY;
}

// One person's input can raise several DOM events: a keydown, then the
// beforeinput or paste it causes. The filter answers true once per input. An
// IME may send beforeinput with no keydown, which counts on its own.
export function personInputFilter(): (type: "keydown" | "keyup" | "beforeinput" | "paste") => boolean {
  let keyDown = false;
  return (type) => {
    if (type === "keyup") {
      keyDown = false;
      return false;
    }
    const caused = type !== "keydown" && keyDown;
    keyDown = type === "keydown";
    return !caused;
  };
}

export interface TerminalAdapterEvents {
  onData(data: string): void;
  onResize(dimensions: TerminalDimensions): void;
  onScroll?(scroll: TerminalScroll): boolean;
  onLink?(uri: string): void;
}

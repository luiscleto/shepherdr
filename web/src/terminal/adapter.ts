export type RendererKind = "reader" | "wterm" | "wterm-ghostty" | "xterm";

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
    const applicationTotal = rows * (Math.abs(delta) < 50 ? likelyTrackpadScale : 1) + applicationRemainder;
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

export interface TerminalAdapterEvents {
  onData(data: string): void;
  onResize(dimensions: TerminalDimensions): void;
  onScroll?(scroll: TerminalScroll): boolean;
}

export interface TerminalAdapter {
  readonly kind: RendererKind;
  mount(host: HTMLElement, events: TerminalAdapterEvents): Promise<void>;
  dimensions(): TerminalDimensions;
  focus(): void;
  paste(text: string): boolean;
  replace?(data: Uint8Array): void;
  scrollLines(lines: number): void;
  selection(): string;
  write(data: Uint8Array): void;
  destroy(): void;
}

export const rendererOptions: ReadonlyArray<{ kind: RendererKind; label: string }> = [
  { kind: "reader", label: "Reader · mobile default" },
  { kind: "xterm", label: "xterm.js · full terminal" },
  { kind: "wterm", label: "WTerm · built-in core" },
  { kind: "wterm-ghostty", label: "WTerm · Ghostty core" },
];

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

export function terminalWheelRows(
  delta: number,
  deltaMode: number,
  rowHeight: number,
  viewportRows: number,
  remainder: number,
): { lines: number; remainder: number } {
  if (!Number.isFinite(delta) || !Number.isFinite(rowHeight) || rowHeight <= 0 || !Number.isFinite(remainder)) {
    return { lines: 0, remainder: 0 };
  }
  const rows = deltaMode === 1 ? delta : deltaMode === 2 ? delta * viewportRows : delta / rowHeight;
  const total = rows + remainder;
  const wholeRows = Math.trunc(total);
  return {
    lines: wholeRows === 0 ? 0 : Math.max(-maximumScrollLines, Math.min(maximumScrollLines, wholeRows)),
    remainder: total - wholeRows,
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

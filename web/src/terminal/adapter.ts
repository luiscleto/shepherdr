export type RendererKind = "reader" | "wterm" | "wterm-ghostty" | "xterm";

export interface TerminalDimensions {
  cols: number;
  rows: number;
}

export interface TerminalAdapterEvents {
  onData(data: string): void;
  onResize(dimensions: TerminalDimensions): void;
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

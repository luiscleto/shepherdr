import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import type { TerminalAdapter, TerminalAdapterEvents, TerminalDimensions } from "./adapter";

export class XTermAdapter implements TerminalAdapter {
  readonly kind = "xterm" as const;

  #fit: FitAddon | undefined;
  #resizeObserver: ResizeObserver | undefined;
  #terminal: Terminal | undefined;

  async mount(host: HTMLElement, events: TerminalAdapterEvents): Promise<void> {
    const terminal = new Terminal({
      allowProposedApi: false,
      cursorBlink: true,
      fontFamily: '"IBM Plex Mono", "Cascadia Mono", monospace',
      fontSize: 14,
      minimumContrastRatio: 4.5,
      screenReaderMode: true,
      scrollback: 10_000,
      theme: {
        background: "#101715",
        foreground: "#e5e7df",
        cursor: "#e89b73",
        selectionBackground: "#47645b",
        black: "#1a211f",
        red: "#df8271",
        green: "#94b48b",
        yellow: "#d9b56c",
        blue: "#83a6c9",
        magenta: "#bd92ba",
        cyan: "#78b7ad",
        white: "#d9ded8",
        brightBlack: "#66726d",
        brightRed: "#f29a87",
        brightGreen: "#aed0a4",
        brightYellow: "#efcb82",
        brightBlue: "#9bbfe2",
        brightMagenta: "#d7acd1",
        brightCyan: "#92d1c6",
        brightWhite: "#f4f5ef",
      },
    });
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(host);
    fit.fit();
    terminal.onData(events.onData);
    terminal.onResize(({ cols, rows }) => events.onResize({ cols, rows }));
    this.#terminal = terminal;
    this.#fit = fit;
    this.#resizeObserver = new ResizeObserver(() => fit.fit());
    this.#resizeObserver.observe(host);
    events.onResize(this.dimensions());
  }

  dimensions(): TerminalDimensions {
    return { cols: this.#terminal?.cols ?? 80, rows: this.#terminal?.rows ?? 24 };
  }

  focus(): void {
    this.#terminal?.focus();
  }

  paste(text: string): boolean {
    this.#terminal?.paste(text);
    return this.#terminal !== undefined;
  }

  scrollLines(lines: number): void {
    this.#terminal?.scrollLines(lines);
  }

  selection(): string {
    return this.#terminal?.getSelection() ?? "";
  }

  write(data: Uint8Array): void {
    this.#terminal?.write(data);
  }

  destroy(): void {
    this.#resizeObserver?.disconnect();
    this.#terminal?.dispose();
    this.#resizeObserver = undefined;
    this.#fit = undefined;
    this.#terminal = undefined;
  }
}

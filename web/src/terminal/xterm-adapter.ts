import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import {
  initialTerminalWheelState,
  terminalWheelRows,
  type TerminalAdapter,
  type TerminalAdapterEvents,
  type TerminalDimensions,
  type TerminalWheelState,
} from "./adapter";

export class XTermAdapter implements TerminalAdapter {
  readonly kind = "xterm" as const;

  #fit: FitAddon | undefined;
  #outputQueue: Array<{ data: Uint8Array; full: boolean }> = [];
  #resizeObserver: ResizeObserver | undefined;
  #terminal: Terminal | undefined;
  #wheelState: TerminalWheelState = initialTerminalWheelState();
  #writing = false;

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
    terminal.attachCustomWheelEventHandler((event) => {
      if (!events.onScroll || event.ctrlKey || event.deltaY === 0) return true;
      const screen = terminal.element?.querySelector<HTMLElement>(".xterm-screen");
      const bounds = screen?.getBoundingClientRect();
      if (!bounds || bounds.width <= 0 || bounds.height <= 0) return true;
      const result = terminalWheelRows(event.deltaY, event.deltaMode, bounds.height / terminal.rows, terminal.rows, this.#wheelState, event.altKey);
      this.#wheelState = result.state;
      if (result.lines === 0) {
        event.preventDefault();
        return false;
      }
      const column = Math.max(0, Math.min(terminal.cols - 1, Math.floor((event.clientX - bounds.left) * terminal.cols / bounds.width)));
      const row = Math.max(0, Math.min(terminal.rows - 1, Math.floor((event.clientY - bounds.top) * terminal.rows / bounds.height)));
      const accepted = events.onScroll({
        column,
        direction: result.lines < 0 ? "up" : "down",
        lines: Math.abs(result.lines),
        modifiers: (event.shiftKey ? 1 : 0) | (event.altKey ? 4 : 0) | (event.metaKey ? 8 : 0),
        row,
      });
      if (accepted) event.preventDefault();
      return !accepted;
    });
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
    this.#outputQueue.push({ data, full: false });
    this.#drainOutput();
  }

  replace(data: Uint8Array): void {
    this.#outputQueue = [{ data, full: true }];
    this.#drainOutput();
  }

  destroy(): void {
    this.#resizeObserver?.disconnect();
    this.#outputQueue = [];
    this.#terminal?.dispose();
    this.#resizeObserver = undefined;
    this.#fit = undefined;
    this.#terminal = undefined;
    this.#wheelState = initialTerminalWheelState();
  }

  #drainOutput(): void {
    const terminal = this.#terminal;
    if (!terminal || this.#writing) return;
    const next = this.#outputQueue.shift();
    if (!next) return;
    this.#writing = true;
    if (next.full) {
      terminal.reset();
      terminal.clear();
    }
    terminal.write(next.data, () => {
      this.#writing = false;
      this.#drainOutput();
    });
  }
}

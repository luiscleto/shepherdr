import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import { terminalPaste } from "../terminal-input";
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
  #events: TerminalAdapterEvents | undefined;
  #host: HTMLElement | undefined;
  #measured: TerminalDimensions | undefined;
  #controlling: boolean | undefined;
  #inputEnabled = true;
  #touchCleanup: (() => void) | undefined;
  #pasteCleanup: (() => void) | undefined;

  constructor(private readonly scrollback = 10_000) {}

  async mount(host: HTMLElement, events: TerminalAdapterEvents): Promise<void> {
    const terminal = new Terminal({
      allowProposedApi: false,
      cursorBlink: true,
      fontFamily: '"IBM Plex Mono", "Cascadia Mono", monospace',
      fontSize: 14,
      minimumContrastRatio: 4.5,
      screenReaderMode: true,
      scrollback: this.scrollback,
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
    this.#terminal = terminal;
    this.#fit = fit;
    this.#host = host;
    this.#events = events;
    // Rendered frames do not carry application input modes. Herdr 0.9 unwraps
    // a complete paste and applies the real application's current paste mode.
    const paste = (event: ClipboardEvent): void => {
      event.preventDefault();
      event.stopImmediatePropagation();
      if (!this.#inputEnabled) return;
      const text = event.clipboardData?.getData("text/plain");
      if (text) events.onData(terminalPaste(text));
    };
    host.addEventListener("paste", paste, true);
    this.#pasteCleanup = () => host.removeEventListener("paste", paste, true);
    terminal.onData(events.onData);
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
    let touch: { x: number; y: number } | undefined;
    const start = (event: TouchEvent): void => {
      if (event.touches.length !== 1 || !this.#inputEnabled) return;
      touch = { x: event.touches[0].clientX, y: event.touches[0].clientY };
      event.stopImmediatePropagation();
    };
    const move = (event: TouchEvent): void => {
      if (!touch || event.touches.length !== 1 || !this.#inputEnabled) return;
      const bounds = terminal.element?.querySelector(".xterm-screen")?.getBoundingClientRect();
      if (!bounds || bounds.height <= 0 || bounds.width <= 0) return;
      const point = event.touches[0];
      const lines = Math.trunc((touch.y - point.clientY) / (bounds.height / terminal.rows));
      event.preventDefault();
      event.stopImmediatePropagation();
      if (!lines) return;
      events.onScroll?.({
        direction: lines < 0 ? "up" : "down", lines: Math.min(1000, Math.abs(lines)), modifiers: 0,
        column: Math.max(0, Math.min(terminal.cols - 1, Math.floor((touch.x - bounds.left) * terminal.cols / bounds.width))),
        row: Math.max(0, Math.min(terminal.rows - 1, Math.floor((point.clientY - bounds.top) * terminal.rows / bounds.height))),
      });
      touch = { x: point.clientX, y: point.clientY };
    };
    const end = (): void => { touch = undefined; };
    host.addEventListener("touchstart", start, { capture: true, passive: true });
    host.addEventListener("touchmove", move, { capture: true, passive: false });
    host.addEventListener("touchend", end, true);
    host.addEventListener("touchcancel", end, true);
    this.#touchCleanup = () => {
      host.removeEventListener("touchstart", start, true);
      host.removeEventListener("touchmove", move, true);
      host.removeEventListener("touchend", end, true);
      host.removeEventListener("touchcancel", end, true);
    };
    this.#resizeObserver = new ResizeObserver(() => this.fit());
    this.#resizeObserver.observe(host);
    this.fit();
  }

  fit(): void {
    const bounds = this.#host?.getBoundingClientRect();
    if (!bounds || !Number.isFinite(bounds.width) || !Number.isFinite(bounds.height) || bounds.width <= 0 || bounds.height <= 0) return;
    const proposed = this.#fit?.proposeDimensions();
    if (!proposed || !Number.isFinite(proposed.cols) || !Number.isFinite(proposed.rows) || proposed.cols < 2 || proposed.rows < 1) return;
    const dimensions = { cols: Math.min(1000, proposed.cols), rows: Math.min(1000, proposed.rows) };
    if (this.#controlling !== false) this.#terminal?.resize(dimensions.cols, dimensions.rows);
    if (this.#measured?.cols === dimensions.cols && this.#measured.rows === dimensions.rows) return;
    this.#measured = dimensions;
    this.#events?.onResize(dimensions);
  }

  receiveDimensions(dimensions: TerminalDimensions, controlling: boolean): void {
    this.#controlling = controlling;
    this.#terminal?.resize(dimensions.cols, dimensions.rows);
  }

  setInputEnabled(enabled: boolean): void {
    this.#inputEnabled = enabled;
    if (this.#terminal) this.#terminal.options.disableStdin = !enabled;
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
    this.#pasteCleanup?.();
    this.#touchCleanup?.();
    this.#resizeObserver?.disconnect();
    this.#outputQueue = [];
    this.#terminal?.dispose();
    this.#resizeObserver = undefined;
    this.#fit = undefined;
    this.#terminal = undefined;
    this.#host = undefined;
    this.#events = undefined;
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

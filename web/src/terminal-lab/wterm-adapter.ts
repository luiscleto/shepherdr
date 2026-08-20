import { WTerm } from "@wterm/dom";
import { GhosttyCore } from "@wterm/ghostty";
import type { RendererKind, TerminalAdapter, TerminalAdapterEvents, TerminalDimensions } from "../terminal/adapter";

export class WTermAdapter implements TerminalAdapter {
  readonly kind: RendererKind;

  #ghostty: boolean;
  #host: HTMLElement | undefined;
  #terminal: WTerm | undefined;

  constructor(ghostty: boolean) {
    this.#ghostty = ghostty;
    this.kind = ghostty ? "wterm-ghostty" : "wterm";
  }

  async mount(host: HTMLElement, events: TerminalAdapterEvents): Promise<void> {
    this.#host = host;
    const core = this.#ghostty
      ? await GhosttyCore.load({
          wasmPath: "/ghostty-vt.wasm",
          foregroundColor: "#e5e7df",
          backgroundColor: "#101715",
          scrollbackLimit: 4 * 1024 * 1024,
        })
      : undefined;
    const terminal = new WTerm(host, {
      autoResize: true,
      core,
      cursorBlink: true,
      onData: events.onData,
      onResize: (cols, rows) => events.onResize({ cols, rows }),
    });
    this.#terminal = terminal;
    await terminal.init();
    events.onResize(this.dimensions());
  }

  dimensions(): TerminalDimensions {
    return { cols: this.#terminal?.cols ?? 80, rows: this.#terminal?.rows ?? 24 };
  }

  focus(): void {
    this.#terminal?.focus();
  }

  paste(_text: string): boolean {
    // WTerm intentionally exposes native browser paste, but no public
    // programmatic paste API. The lab does not reach into its InputHandler.
    return false;
  }

  scrollLines(lines: number): void {
    if (!this.#host) return;
    const rowHeight = this.#host.clientHeight / Math.max(1, this.dimensions().rows);
    this.#host.scrollBy({ top: lines * rowHeight });
  }

  selection(): string {
    const selection = window.getSelection();
    if (!selection || !this.#host || !selection.anchorNode || !selection.focusNode) return "";
    if (!this.#host.contains(selection.anchorNode) || !this.#host.contains(selection.focusNode)) return "";
    return selection.toString();
  }

  write(data: Uint8Array): void {
    this.#terminal?.write(data);
  }

  destroy(): void {
    this.#terminal?.destroy();
    this.#terminal = undefined;
    this.#host = undefined;
  }
}

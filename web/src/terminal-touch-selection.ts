import type { Terminal } from "@xterm/xterm";

type SelectionTerminal = Pick<Terminal, "blur" | "element" | "modes">;

export interface TouchSelectionOptions {
  enabled: () => boolean;
  host: HTMLElement;
  selectionCleared: () => void;
  terminal: SelectionTerminal;
}

type SelectionMouseEvent = "mousedown" | "mousemove" | "mouseup";

function dispatchSelectionMouse(terminal: SelectionTerminal, type: SelectionMouseEvent, pointer: PointerEvent): boolean {
  const element = terminal.element;
  const view = element?.ownerDocument.defaultView;
  if (!element || !view) return false;
  const dragging = type !== "mouseup";
  const event = new view.MouseEvent(type, {
    bubbles: true,
    button: 0,
    buttons: dragging ? 1 : 0,
    cancelable: true,
    clientX: pointer.clientX,
    clientY: pointer.clientY,
    detail: type === "mousedown" ? 1 : 0,
    // xterm uses Shift to force its own selection while an application has
    // enabled terminal mouse reporting. In the ordinary mode, Shift instead
    // extends an existing selection, so only set it when forcing is needed.
    shiftKey: terminal.modes.mouseTrackingMode !== "none",
  });
  element.dispatchEvent(event);
  terminal.blur();
  return true;
}

export function installTerminalTouchSelection(options: TouchSelectionOptions): () => void {
  const { enabled, host, selectionCleared, terminal } = options;
  let pointerID: number | undefined;
  let lastPointer: PointerEvent | undefined;

  const blockFocus = (event: Event): void => {
    if (!enabled()) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    terminal.blur();
  };

  const pointerDown = (event: PointerEvent): void => {
    if (event.pointerType !== "touch" || !enabled()) return;
    blockFocus(event);
    selectionCleared();
    if (!dispatchSelectionMouse(terminal, "mousedown", event)) return;
    pointerID = event.pointerId;
    lastPointer = event;
    host.setPointerCapture(event.pointerId);
  };

  const pointerMove = (event: PointerEvent): void => {
    if (event.pointerId !== pointerID) return;
    blockFocus(event);
    lastPointer = event;
    dispatchSelectionMouse(terminal, "mousemove", event);
  };

  const pointerEnd = (event: PointerEvent): void => {
    if (event.pointerId !== pointerID) return;
    blockFocus(event);
    dispatchSelectionMouse(terminal, "mouseup", event);
    pointerID = undefined;
    lastPointer = undefined;
    if (host.hasPointerCapture(event.pointerId)) host.releasePointerCapture(event.pointerId);
  };

  host.addEventListener("click", blockFocus, true);
  host.addEventListener("pointerdown", pointerDown, true);
  host.addEventListener("pointermove", pointerMove, true);
  host.addEventListener("pointerup", pointerEnd, true);
  host.addEventListener("pointercancel", pointerEnd, true);
  return () => {
    if (pointerID !== undefined && lastPointer) {
      dispatchSelectionMouse(terminal, "mouseup", lastPointer);
      if (host.hasPointerCapture(pointerID)) host.releasePointerCapture(pointerID);
      pointerID = undefined;
      lastPointer = undefined;
    }
    host.removeEventListener("click", blockFocus, true);
    host.removeEventListener("pointerdown", pointerDown, true);
    host.removeEventListener("pointermove", pointerMove, true);
    host.removeEventListener("pointerup", pointerEnd, true);
    host.removeEventListener("pointercancel", pointerEnd, true);
  };
}

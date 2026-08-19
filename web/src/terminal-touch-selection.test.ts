import assert from "node:assert/strict";
import test from "node:test";

import { Window } from "happy-dom";

import { installTerminalTouchSelection, type TouchSelectionOptions } from "./terminal-touch-selection";

function pointer(window: Window, type: string, x: number, y: number, pointerId = 7): PointerEvent {
  return new window.PointerEvent(type, {
    bubbles: true,
    cancelable: true,
    clientX: x,
    clientY: y,
    pointerId,
    pointerType: "touch",
  }) as unknown as PointerEvent;
}

test("observer touch delegates unchanged rendered coordinates to xterm selection without focusing input", () => {
  const window = new Window();
  const host = window.document.createElement("div") as unknown as HTMLElement;
  const element = window.document.createElement("div") as unknown as HTMLElement;
  const textarea = window.document.createElement("textarea") as unknown as HTMLTextAreaElement;
  element.append(textarea);
  host.append(element);
  window.document.body.append(host);

  const mouseEvents: Array<[string, number, number, number, boolean]> = [];
  element.addEventListener("mousedown", (event) => {
    const mouse = event as MouseEvent;
    mouseEvents.push([mouse.type, mouse.clientX, mouse.clientY, mouse.buttons, mouse.shiftKey]);
  });
  window.document.addEventListener("mousemove", (event) => {
    const mouse = event as MouseEvent;
    mouseEvents.push([mouse.type, mouse.clientX, mouse.clientY, mouse.buttons, mouse.shiftKey]);
  });
  window.document.addEventListener("mouseup", (event) => {
    const mouse = event as MouseEvent;
    mouseEvents.push([mouse.type, mouse.clientX, mouse.clientY, mouse.buttons, mouse.shiftKey]);
  });

  let clears = 0;
  const terminal: TouchSelectionOptions["terminal"] = {
    blur: () => textarea.blur(),
    element,
    modes: { mouseTrackingMode: "none" } as TouchSelectionOptions["terminal"]["modes"],
  };
  const remove = installTerminalTouchSelection({
    enabled: () => true,
    host,
    selectionCleared: () => { clears++; },
    terminal,
  });

  textarea.focus();
  // These coordinates model the reviewed production mismatch: the xterm box
  // and its rendered cell grid have different widths. The adapter must not
  // reinterpret either endpoint using the outer box.
  const down = pointer(window, "pointerdown", 297.4, 18);
  host.dispatchEvent(down);
  host.dispatchEvent(pointer(window, "pointermove", 328.6, 18));
  host.dispatchEvent(pointer(window, "pointerup", 328.6, 18));

  assert.equal(down.defaultPrevented, true);
  assert.notEqual(window.document.activeElement, textarea, "observer touch must blur the terminal input textarea");
  assert.equal(clears, 1);
  assert.deepEqual(mouseEvents, [
    ["mousedown", 297.4, 18, 1, false],
    ["mousemove", 328.6, 18, 1, false],
    ["mouseup", 328.6, 18, 0, false],
  ], "xterm must receive the exact visible-grid coordinates and own cell, wrap, width, and viewport mapping");

  remove();
  window.close();
});

test("selection is forced through xterm when terminal mouse reporting is active", () => {
  const window = new Window();
  const host = window.document.createElement("div") as unknown as HTMLElement;
  const element = window.document.createElement("div") as unknown as HTMLElement;
  host.append(element);
  window.document.body.append(host);
  let forced = false;
  element.addEventListener("mousedown", (event) => { forced = (event as MouseEvent).shiftKey; });
  const remove = installTerminalTouchSelection({
    enabled: () => true,
    host,
    selectionCleared: () => undefined,
    terminal: {
      blur: () => undefined,
      element,
      modes: { mouseTrackingMode: "any" } as TouchSelectionOptions["terminal"]["modes"],
    },
  });
  host.dispatchEvent(pointer(window, "pointerdown", 10, 10));
  host.dispatchEvent(pointer(window, "pointerup", 10, 10));
  assert.equal(forced, true, "Shift must force xterm selection instead of sending the gesture to the terminal application");
  remove();
  window.close();
});

test("controller input touch is left to xterm and cannot invoke selection behavior", () => {
  const window = new Window();
  const host = window.document.createElement("div") as unknown as HTMLElement;
  const element = window.document.createElement("div") as unknown as HTMLElement;
  host.append(element);
  window.document.body.append(host);
  let clears = 0;
  let mouseDowns = 0;
  element.addEventListener("mousedown", () => { mouseDowns++; });
  const remove = installTerminalTouchSelection({
    enabled: () => false,
    host,
    selectionCleared: () => { clears++; },
    terminal: {
      blur: () => undefined,
      element,
      modes: { mouseTrackingMode: "none" } as TouchSelectionOptions["terminal"]["modes"],
    },
  });
  const down = pointer(window, "pointerdown", 5, 5, 8);
  host.dispatchEvent(down);
  assert.equal(down.defaultPrevented, false);
  assert.equal(clears, 0);
  assert.equal(mouseDowns, 0);
  remove();
  window.close();
});

import assert from "node:assert/strict";
import test from "node:test";
import { Window } from "happy-dom";
import { KeysSheet } from "./terminal/keys-sheet";
import { coreCanSend, validateKey, readShortcuts, saveShortcuts, type KeySelection } from "./terminal/keys";

for (const stored of [null, '[{"base":"f1"}]']) {
  test(`shortcut edits survive failed writes with ${stored === null ? "missing" : "stale"} readable storage`, t => {
    const browser = new Window({ url: "http://localhost" });
    const originalWindow = globalThis.window;
    globalThis.window = browser as unknown as Window & typeof globalThis;
    t.after(() => { globalThis.window = originalWindow; browser.close(); });
    const storage = browser.localStorage;
    const storageKey = "shepherdr.terminal.shortcuts";
    // Begin with working storage, including when another test used the fallback.
    saveShortcuts([{ base: "f1" }]);
    if (stored === null) storage.removeItem(storageKey);
    assert.equal(JSON.stringify(readShortcuts()), stored ?? '[{"base":"esc"},{"base":"up"},{"base":"down"},{"base":"backspace"},{"base":"c","ctrl":true}]');
    const write = t.mock.method(storage, "setItem", () => { throw new Error("quota exceeded"); });
    saveShortcuts([{ base: "k", ctrl: true, shift: true }]);
    assert.equal(storage.getItem(storageKey), stored);
    assert.equal(JSON.stringify(readShortcuts()), '[{"base":"k","ctrl":true,"shift":true}]');
    // Another terminal reads the same visit, including a deliberate empty list.
    assert.equal(JSON.stringify(readShortcuts()), '[{"base":"k","ctrl":true,"shift":true}]');
    saveShortcuts([]);
    assert.equal(JSON.stringify(readShortcuts()), '[]');
    write.mock.restore();
    saveShortcuts([{ base: "tab" }]);
    assert.equal(storage.getItem(storageKey), '[{"base":"tab"}]');
    assert.equal(JSON.stringify(readShortcuts()), '[{"base":"tab"}]');
  });
}

test("key preferences validate combinations and editing never sends", t => {
  const browser = new Window({ url: "http://localhost" });
  const originalWindow = globalThis.window, originalDocument = globalThis.document;
  globalThis.window = browser as unknown as Window & typeof globalThis;
  globalThis.document = browser.document as unknown as Document;
  let sends = 0;
  let saved: KeySelection[] = [];
  const sheet = new KeysSheet(document.body, { send: () => { sends++; return true; }, changed: keys => { saved = keys; }, dismiss() {} });
  t.after(() => { sheet.destroy(); globalThis.window = originalWindow; globalThis.document = originalDocument; browser.close(); });
  const click = (text: string) => Array.from(document.querySelectorAll("button")).find(button => button.textContent === text)!.click();
  const labeled = (label: string) => document.querySelector<HTMLButtonElement>(`[aria-label="${label}"]`)!.click();
  const expectedDefaults = '[{"base":"esc"},{"base":"up"},{"base":"down"},{"base":"backspace"},{"base":"c","ctrl":true}]';
  assert.equal(JSON.stringify(saved), expectedDefaults);
  // Existing choices, including the earlier defaults and an empty list, stay put.
  saveShortcuts([{ base: "esc" }, { base: "tab" }]);
  assert.equal(JSON.stringify(readShortcuts()), '[{"base":"esc"},{"base":"tab"}]');
  saveShortcuts([{ base: "esc" }, { base: "c", ctrl: true }, { base: "up" }, { base: "down" }]);
  assert.equal(JSON.stringify(readShortcuts()), '[{"base":"esc"},{"base":"c","ctrl":true},{"base":"up"},{"base":"down"}]');
  saveShortcuts([]);
  assert.equal(JSON.stringify(readShortcuts()), '[]');
  const superNote = () => document.querySelector<HTMLElement>(".keys-super-note")!.hidden;
  assert.equal(Array.from(document.querySelectorAll("button")).some(button => button.textContent === "Hyper"), false);
  click("Character"); assert.equal(superNote(), true);
  click("Ctrl"); click("Super / Command"); assert.equal(superNote(), false);
  const input = document.querySelector("input")!;
  input.value = "😀"; input.dispatchEvent(new browser.Event("input") as unknown as Event);
  click("Pin shortcut");
  click("Edit shortcuts"); labeled("Move Ctrl + Super + 😀 earlier"); labeled("Edit Ctrl + Super + 😀");
  input.value = "ignored detached input";
  const current = document.querySelector("input")!;
  current.value = "+"; current.dispatchEvent(new browser.Event("input") as unknown as Event);
  click("Save shortcut"); labeled("Remove Esc");
  assert.equal(JSON.stringify(readShortcuts()), JSON.stringify([{ base: "up" }, { base: "down" }, { base: "backspace" }, { base: "+", ctrl: true, super: true }, { base: "c", ctrl: true }]));
  assert.equal(sends, 0);
  click("Restore defaults");
  assert.equal(JSON.stringify(readShortcuts()), expectedDefaults);
  for (const base of ["F13", "home", "a+b", "", "e\u0301", "\ud800", "\n"]) assert.equal(validateKey({ base }) === undefined, true, base);
  for (const base of ["f1", "f12", "A", "é", " ", "+", "backspace"]) assert.equal(validateKey({ base, ctrl: true, alt: true, shift: true, super: true }) !== undefined, true);
  assert.equal(validateKey({ base: "a", destination: "other" }) === undefined, true);
  // Hyper is no longer offered; older saved shortcuts that use it are dropped.
  assert.equal(validateKey({ base: "a", hyper: true }) === undefined, true);
  browser.localStorage.setItem("shepherdr.terminal.shortcuts", '[{"base":"esc"},{"base":"a","ctrl":true,"hyper":true},{"base":"up","super":true}]');
  assert.equal(JSON.stringify(readShortcuts()), '[{"base":"esc"},{"base":"up","super":true}]');
  const many = Array.from({ length: 40 }, (_, index) => ({ base: String.fromCharCode(33 + index) }));
  saveShortcuts(many); assert.equal(readShortcuts().length, 40);
  Object.defineProperty(browser, "localStorage", { get() { throw new Error("storage blocked"); }, configurable: true });
  saveShortcuts([{ base: "界", alt: true }]);
  assert.equal(JSON.stringify(readShortcuts()), '[{"base":"界","alt":true}]');
});

test("Herdr Core offers only keys it can send, and says so for any other character", t => {
  const browser = new Window({ url: "http://localhost" });
  const originalWindow = globalThis.window, originalDocument = globalThis.document;
  globalThis.window = browser as unknown as Window & typeof globalThis;
  globalThis.document = browser.document as unknown as Document;
  t.after(() => { globalThis.window = originalWindow; globalThis.document = originalDocument; browser.close(); });
  const sendable = ["esc", "tab", "enter", "backspace", "up", "f5", "f12", "a", "A", "7", " ", "`", "\\", "[", "]", ",", "=", "-", ".", "'", ";", "/"];
  const unsendable = ["+", "%", "&", ":", '"', "!", "@", "~", "é", "界"];
  assert.equal(sendable.filter(base => !coreCanSend({ base })).join(" "), "");
  assert.equal(unsendable.filter(base => coreCanSend({ base })).join(" "), "");
  for (const core of [true, false]) {
    const sheet = new KeysSheet(document.body, { send: () => true, changed() {}, dismiss() {}, core });
    sheet.available(true, "");
    Array.from(document.querySelectorAll("button")).find(button => button.textContent === "Character")!.click();
    const type = (value: string) => {
      const input = document.querySelector("input")!;
      input.value = value;
      input.dispatchEvent(new browser.Event("input") as unknown as Event);
      const note = document.querySelector<HTMLElement>(".keys-core-note")!;
      const send = document.querySelector<HTMLButtonElement>(".keys-send")!;
      return `${value}:${note.hidden ? "-" : note.textContent}:${send.disabled ? "off" : "on"}`;
    };
    assert.equal(document.querySelector(".keys-super-note")!.textContent, core
      ? "Super / Command reaches only apps that turn on enhanced keyboard input. An ordinary shell may receive just the key."
      : "Super / Command reaches only apps that turn on enhanced keyboard input. An ordinary shell may receive nothing.");
    const results = ["+", "A", "%"].map(type).join(" ");
    assert.equal(results, core
      ? "+:Herdr can't send this key.:off A:-:on %:Herdr can't send this key.:off"
      : "+:-:on A:-:on %:-:on");
    sheet.destroy();
  }
});

import assert from "node:assert/strict";
import test from "node:test";
import { Window } from "happy-dom";
import { KeysSheet } from "./terminal/keys-sheet";
import { validateKey, readShortcuts, saveShortcuts, type KeySelection } from "./terminal/keys";

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
    assert.equal(JSON.stringify(readShortcuts()), stored ?? '[{"base":"esc"},{"base":"c","ctrl":true},{"base":"up"},{"base":"down"}]');
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
  const expectedDefaults = '[{"base":"esc"},{"base":"c","ctrl":true},{"base":"up"},{"base":"down"}]';
  assert.equal(JSON.stringify(saved), expectedDefaults);
  // Existing choices, including the earlier defaults and an empty list, stay put.
  saveShortcuts([{ base: "esc" }, { base: "tab" }]);
  assert.equal(JSON.stringify(readShortcuts()), '[{"base":"esc"},{"base":"tab"}]');
  saveShortcuts([]);
  assert.equal(JSON.stringify(readShortcuts()), '[]');
  click("Character"); click("Ctrl"); click("Super / Command"); click("Hyper");
  const input = document.querySelector("input")!;
  input.value = "😀"; input.dispatchEvent(new browser.Event("input") as unknown as Event);
  click("Pin shortcut");
  click("Edit shortcuts"); labeled("Move Ctrl + Super + Hyper + 😀 earlier"); labeled("Edit Ctrl + Super + Hyper + 😀");
  input.value = "ignored detached input";
  const current = document.querySelector("input")!;
  current.value = "+"; current.dispatchEvent(new browser.Event("input") as unknown as Event);
  click("Save shortcut"); labeled("Remove Esc");
  assert.equal(JSON.stringify(readShortcuts()), JSON.stringify([{ base: "c", ctrl: true }, { base: "up" }, { base: "+", ctrl: true, super: true, hyper: true }, { base: "down" }]));
  assert.equal(sends, 0);
  click("Restore defaults");
  assert.equal(JSON.stringify(readShortcuts()), expectedDefaults);
  for (const base of ["F13", "home", "a+b", "", "e\u0301", "\ud800", "\n"]) assert.equal(validateKey({ base }) === undefined, true, base);
  for (const base of ["f1", "f12", "A", "é", " ", "+", "backspace"]) assert.equal(validateKey({ base, ctrl: true, alt: true, shift: true, super: true, hyper: true }) !== undefined, true);
  assert.equal(validateKey({ base: "a", destination: "other" }) === undefined, true);
  const many = Array.from({ length: 40 }, (_, index) => ({ base: String.fromCharCode(33 + index) }));
  saveShortcuts(many); assert.equal(readShortcuts().length, 40);
  Object.defineProperty(browser, "localStorage", { get() { throw new Error("storage blocked"); }, configurable: true });
  saveShortcuts([{ base: "界", alt: true }]);
  assert.equal(JSON.stringify(readShortcuts()), '[{"base":"界","alt":true}]');
});

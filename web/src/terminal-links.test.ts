import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import { Window } from "happy-dom";
import { LinkSheet, openableLink } from "./terminal/link-sheet";

test("only web links can open", () => {
  assert.equal(openableLink("https://example.com/a b"), "https://example.com/a%20b");
  assert.equal(openableLink("HTTP://Example.com"), "http://example.com/");
  for (const uri of ["javascript:alert(1)", "file:///etc/passwd", "data:text/html,x", "vbscript:x", "#terminal=w1:p1", "/settings", "not a url", ""]) {
    assert.equal(openableLink(uri), undefined, uri);
  }
});

test("a long address scrolls inside its own bounded box", () => {
  const styles = readFileSync(new URL("./terminal-keys.css", import.meta.url), "utf8");
  const rule = styles.slice(styles.indexOf(".link-sheet-address {"), styles.indexOf("}", styles.indexOf(".link-sheet-address {")));
  assert.equal(rule.includes("max-height: 6.5rem;"), true);
  assert.equal(rule.includes("overflow-y: auto;"), true);
});

test("a terminal link opens only after confirmation, in a separate tab", t => {
  const browser = new Window({ url: "http://localhost/#terminal=w1:p1" });
  const originalWindow = globalThis.window, originalDocument = globalThis.document;
  globalThis.window = browser as unknown as Window & typeof globalThis;
  globalThis.document = browser.document as unknown as Document;
  const opened: string[] = [];
  browser.open = ((url: string, target: string, features: string) => { opened.push(`${url} ${target} ${features}`); return null; }) as typeof browser.open;
  const sheet = new LinkSheet(document.body);
  t.after(() => { sheet.destroy(); globalThis.window = originalWindow; globalThis.document = originalDocument; browser.close(); });
  const dialog = () => document.querySelector("dialog.link-sheet") as HTMLDialogElement;
  const click = (text: string) => Array.from(dialog().querySelectorAll("button")).find(button => button.textContent === text)!.click();

  sheet.open("javascript:alert(1)");
  assert.equal(dialog().open, false);
  sheet.open("https://evil.example/<b>x</b>");
  assert.equal(dialog().open, true);
  assert.equal(dialog().querySelector(".link-sheet-address")!.textContent, "https://evil.example/%3Cb%3Ex%3C/b%3E");
  assert.equal(dialog().querySelector(".link-sheet-address")!.children.length, 0);
  assert.equal(opened.length, 0);
  click("Cancel");
  assert.equal(dialog().open, false);
  assert.equal(opened.length, 0);
  // Each link gets its own address box, so an earlier scroll cannot carry over.
  const first = dialog().querySelector(".link-sheet-address");
  sheet.open("http://127.0.0.1:9/x");
  assert.equal(dialog().querySelectorAll(".link-sheet-address").length, 1);
  assert.equal(dialog().querySelector(".link-sheet-address") === first, false);
  click("Open");
  assert.equal(JSON.stringify(opened), JSON.stringify(["http://127.0.0.1:9/x _blank noopener,noreferrer"]));
  assert.equal(dialog().open, false);
  assert.equal(browser.location.href, "http://localhost/#terminal=w1:p1");
});

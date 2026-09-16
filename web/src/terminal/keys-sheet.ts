import { commonKeys, defaultShortcuts, keyLabel, keyModifiers, modifierLabels, readShortcuts, saveShortcuts, validateKey, validCharacter, type KeySelection, type KeyResult } from "./keys";

function node<K extends keyof HTMLElementTagNameMap>(tag: K, text?: string, className?: string): HTMLElementTagNameMap[K] {
  const element = document.createElement(tag);
  if (text !== undefined) element.textContent = text;
  if (className) element.className = className;
  return element;
}
function button(label: string, run: () => void): HTMLButtonElement {
  const element = node("button", label);
  element.type = "button";
  element.addEventListener("click", run);
  return element;
}

export class KeysSheet {
  #dialog = node("dialog", undefined, "keys-sheet");
  #body = node("div", undefined, "keys-sheet-body");
  #footer = node("div", undefined, "keys-sheet-footer");
  #heading = node("h2", "Send keys");
  #editAction: HTMLButtonElement;
  #preview = node("output", undefined, "keys-preview");
  #feedback = node("p", undefined, "keys-feedback");
  #dismiss: HTMLButtonElement;
  #send: HTMLButtonElement;
  #pin: HTMLButtonElement;
  #saved = readShortcuts();
  #selection: KeySelection = { base: "esc" };
  #category = "Common";
  #editing = false;
  #editIndex: number | undefined;
  #available = false;
  #composing = false;
  #uncertain = false;
  #reason = "";
  #options: { send(key: KeySelection): boolean; changed(keys: KeySelection[]): void; dismiss(): void };
  #viewport = (): void => {
    const viewport = window.visualViewport;
    this.#dialog.style.top = `${(viewport?.offsetTop ?? 0) + (viewport?.height ?? window.innerHeight)}px`;
    this.#dialog.style.maxHeight = `${Math.max(0, (viewport?.height ?? window.innerHeight) - 12)}px`;
  };

  constructor(host: HTMLElement, options: { send(key: KeySelection): boolean; changed(keys: KeySelection[]): void; dismiss(): void }) {
    this.#options = options;
    this.#heading.id = "send-keys-heading";
    this.#heading.tabIndex = -1;
    this.#dialog.setAttribute("aria-labelledby", this.#heading.id);
    const header = node("header", undefined, "keys-sheet-header");
    this.#editAction = button("Edit shortcuts", () => { this.#editing = true; this.#render(); });
    const close = button("×", () => this.#dialog.close());
    close.setAttribute("aria-label", "Close Send keys");
    header.append(this.#heading, this.#editAction, close);
    this.#send = button("Send", () => {
      const key = this.#chosen();
      if (key && this.#available) options.send(key);
    });
    this.#send.className = "keys-send";
    this.#pin = button("Pin shortcut", () => {
      const key = this.#chosen();
      if (!key) return;
      if (this.#editIndex === undefined) this.#saved.push(key);
      else this.#saved[this.#editIndex] = key;
      this.#save();
      this.#feedback.textContent = this.#editIndex === undefined ? "Shortcut pinned." : "Shortcut saved.";
      if (this.#editIndex !== undefined) { this.#editing = true; this.#editIndex = undefined; this.#render(); }
    });
    this.#dismiss = button("Dismiss", () => {
      this.#uncertain = false;
      this.#feedback.textContent = "";
      options.dismiss();
      this.#sync();
    });
    this.#dismiss.hidden = true;
    this.#feedback.setAttribute("role", "status");
    this.#footer.append(this.#preview, this.#pin, this.#send, this.#feedback, this.#dismiss);
    this.#dialog.append(header, this.#body, this.#footer);
    this.#dialog.addEventListener("click", event => { if (event.target === this.#dialog) this.#dialog.close(); });
    host.append(this.#dialog);
    window.visualViewport?.addEventListener("resize", this.#viewport);
    window.visualViewport?.addEventListener("scroll", this.#viewport);
    window.addEventListener("resize", this.#viewport);
    options.changed(this.#saved);
    this.#render();
  }

  open(): void {
    this.#viewport();
    if (!this.#dialog.open) { this.#dialog.showModal(); this.#heading.focus({ preventScroll: true }); }
  }

  available(available: boolean, reason: string): void { this.#available = available; this.#reason = reason; this.#sync(); }

  result(result: KeyResult): void {
    this.#uncertain = result === "unknown";
    this.#feedback.textContent = result === "accepted" ? "" : result === "not_sent" ? "Key not sent. Check control and try again." : "Could not confirm the key. Check the terminal before sending again.";
    if (result !== "accepted") {
      if (this.#editing) { this.#editing = false; this.#render(); }
      this.open();
    } else if (this.#dialog.open) {
      this.#dialog.close();
    }
    this.#sync();
  }

  destroy(): void {
    window.visualViewport?.removeEventListener("resize", this.#viewport);
    window.visualViewport?.removeEventListener("scroll", this.#viewport);
    window.removeEventListener("resize", this.#viewport);
    this.#dialog.close();
    this.#dialog.remove();
  }

  #chosen(): KeySelection | undefined {
    return this.#composing || this.#category === "Character" && !validCharacter(this.#selection.base) ? undefined : validateKey(this.#selection);
  }
  #save(): void { saveShortcuts(this.#saved); this.#options.changed(this.#saved); }

  #sync(): void {
    const key = this.#chosen();
    this.#preview.textContent = key ? keyLabel(key) : "Choose one character";
    this.#send.textContent = key ? `Send ${keyLabel(key)}` : "Send";
    this.#send.disabled = !key || !this.#available;
    this.#send.title = this.#available ? "" : this.#reason;
    this.#pin.disabled = !key;
    this.#pin.textContent = this.#editIndex === undefined ? "Pin shortcut" : "Save shortcut";
    this.#dismiss.hidden = !this.#uncertain;
    const reason = this.#body.querySelector<HTMLElement>(".keys-availability");
    if (reason) { reason.textContent = this.#reason; reason.hidden = this.#available || !this.#reason; }
  }

  #render(): void {
    this.#body.replaceChildren();
    this.#heading.textContent = this.#editing ? "Edit shortcuts" : this.#editIndex === undefined ? "Send keys" : "Edit shortcut";
    this.#footer.hidden = this.#editing;
    this.#editAction.hidden = this.#editing;
    if (this.#editing) { this.#renderSaved(); return; }
    const modifiers = node("div", undefined, "keys-modifiers");
    modifiers.setAttribute("aria-label", "Modifiers");
    for (const modifier of keyModifiers) {
      const toggle = button(modifierLabels[modifier], () => {
        this.#selection[modifier] = !this.#selection[modifier];
        toggle.setAttribute("aria-pressed", String(Boolean(this.#selection[modifier])));
        this.#sync();
      });
      toggle.setAttribute("aria-pressed", String(Boolean(this.#selection[modifier])));
      modifiers.append(toggle);
    }
    const categories = node("div", undefined, "keys-categories");
    for (const category of ["Common", "F keys", "Character"]) {
      const tab = button(category, () => { this.#category = category; this.#render(); });
      tab.setAttribute("aria-pressed", String(this.#category === category));
      categories.append(tab);
    }
    this.#body.append(modifiers, categories);
    if (this.#category === "Character") {
      const label = node("label", "Character", "keys-character");
      const input = node("input");
      input.type = "text";
      input.autocomplete = "off";
      input.autocapitalize = "off";
      input.spellcheck = false;
      input.setAttribute("autocorrect", "off");
      input.value = validCharacter(this.#selection.base) ? this.#selection.base : "";
      this.#selection.base = input.value;
      input.addEventListener("compositionstart", () => { this.#composing = true; this.#sync(); });
      input.addEventListener("compositionend", () => { this.#composing = false; this.#selection.base = input.value; this.#sync(); });
      input.addEventListener("input", () => { this.#selection.base = input.value; this.#sync(); });
      label.append(input);
      this.#body.append(label, node("p", "Type one character, including a space or +.", "keys-help"));
    } else {
      const grid = node("div", undefined, "keys-grid");
      const keys: ReadonlyArray<readonly [string, string]> = this.#category === "Common" ? commonKeys : Array.from({ length: 12 }, (_, i) => [`f${i + 1}`, `F${i + 1}`]);
      for (const [base, label] of keys) {
        const key = button(label, () => {
          this.#selection.base = base;
          for (const child of Array.from(grid.children)) child.setAttribute("aria-pressed", String(child === key));
          this.#sync();
        });
        const name = ({ up: "Up arrow", down: "Down arrow", left: "Left arrow", right: "Right arrow" } as Record<string, string>)[base];
        if (name) key.setAttribute("aria-label", name);
        key.setAttribute("aria-pressed", String(this.#selection.base === base));
        grid.append(key);
      }
      this.#body.append(grid);
    }
    this.#body.append(node("p", undefined, "keys-availability"));
    this.#sync();
  }

  #renderSaved(): void {
    this.#body.append(node("p", "Message, files, Enter, Send keys and control stay available.", "keys-help"));
    this.#saved.forEach((key, index) => {
      const row = node("div", undefined, "keys-saved-row");
      const edit = button(keyLabel(key), () => {
        this.#selection = { ...key }; this.#editIndex = index; this.#editing = false;
        this.#category = commonKeys.some(([base]) => base === key.base) ? "Common" : /^f\d+$/.test(key.base) ? "F keys" : "Character";
        this.#render();
      });
      edit.setAttribute("aria-label", `Edit ${keyLabel(key)}`);
      const up = button("↑", () => this.#move(index, -1));
      up.setAttribute("aria-label", `Move ${keyLabel(key)} earlier`); up.disabled = index === 0;
      const down = button("↓", () => this.#move(index, 1));
      down.setAttribute("aria-label", `Move ${keyLabel(key)} later`); down.disabled = index === this.#saved.length - 1;
      const remove = button("Remove", () => { this.#saved.splice(index, 1); this.#save(); this.#render(); });
      remove.setAttribute("aria-label", `Remove ${keyLabel(key)}`);
      row.append(edit, up, down, remove);
      this.#body.append(row);
    });
    const actions = node("div", undefined, "keys-edit-actions");
    actions.append(
      button("Add shortcut", () => { this.#editing = false; this.#editIndex = undefined; this.#render(); }),
      button("Restore defaults", () => { this.#saved = defaultShortcuts(); this.#save(); this.#render(); }),
      button("Done", () => { this.#editing = false; this.#editIndex = undefined; this.#render(); }),
    );
    this.#body.append(actions);
  }

  #move(index: number, delta: number): void {
    const next = index + delta;
    [this.#saved[index], this.#saved[next]] = [this.#saved[next], this.#saved[index]];
    this.#save(); this.#render();
  }
}

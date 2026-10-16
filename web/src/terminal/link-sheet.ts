// Terminal output chooses link addresses, so only web addresses can open, and
// only in a separate tab after the person has seen the exact address.
export function openableLink(uri: string): string | undefined {
  try {
    const url = new URL(uri);
    return url.protocol === "http:" || url.protocol === "https:" ? url.href : undefined;
  } catch {
    return undefined;
  }
}

export class LinkSheet {
  #dialog = document.createElement("dialog");
  #address = document.createElement("p");
  #cancel = document.createElement("button");
  #href: string | undefined;

  constructor(host: HTMLElement) {
    this.#dialog.className = "link-sheet";
    const heading = document.createElement("h2");
    heading.id = "link-sheet-heading";
    heading.textContent = "Open link?";
    this.#dialog.setAttribute("aria-labelledby", heading.id);
    const help = document.createElement("p");
    help.textContent = "This address comes from the terminal. Check it before opening.";
    this.#address.className = "link-sheet-address";
    const actions = document.createElement("div");
    actions.className = "link-sheet-actions";
    this.#cancel.type = "button";
    this.#cancel.textContent = "Cancel";
    this.#cancel.addEventListener("click", () => this.#dialog.close());
    const open = document.createElement("button");
    open.type = "button";
    open.className = "link-sheet-open";
    open.textContent = "Open";
    open.addEventListener("click", () => {
      if (this.#href) window.open(this.#href, "_blank", "noopener,noreferrer");
      this.#dialog.close();
    });
    actions.append(this.#cancel, open);
    this.#dialog.append(heading, help, this.#address, actions);
    this.#dialog.addEventListener("close", () => { this.#href = undefined; this.#address.textContent = ""; });
    this.#dialog.addEventListener("click", event => { if (event.target === this.#dialog) this.#dialog.close(); });
    host.append(this.#dialog);
  }

  open(uri: string): void {
    const href = openableLink(uri);
    if (!href) return;
    this.#href = href;
    // A fresh box always starts at the host; a reused one keeps the scroll
    // position from an earlier link, however that sheet was closed.
    const address = document.createElement("p");
    address.className = "link-sheet-address";
    address.textContent = href;
    this.#address.replaceWith(address);
    this.#address = address;
    if (!this.#dialog.open) this.#dialog.showModal();
    this.#cancel.focus({ preventScroll: true });
  }

  destroy(): void {
    this.#dialog.close();
    this.#dialog.remove();
  }
}

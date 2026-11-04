import type { Herdr } from "./home-model";
import {
  agentText,
  filesFor,
  formatShared,
  formatSize,
  fromText,
  groupByAgent,
  placeText,
  sharedFileURL,
  sharedSortLabels,
  sharedSorts,
  sortFiles,
  type SharedFile,
  type SharedFrom,
  type SharedRoute,
  type SharedSort,
  type SharedTarget,
} from "./shared-model";

const svgNamespace = "http://www.w3.org/2000/svg";

function element<K extends keyof HTMLElementTagNameMap>(document: Document, tag: K, className?: string, text?: string): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function button(document: Document, text: string, run: () => void, className?: string): HTMLButtonElement {
  const node = element(document, "button", className, text);
  node.type = "button";
  node.addEventListener("click", run);
  return node;
}

function icon(document: Document, paths: readonly string[]): SVGSVGElement {
  const svg = document.createElementNS(svgNamespace, "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("aria-hidden", "true");
  svg.setAttribute("focusable", "false");
  for (const value of paths) {
    const path = document.createElementNS(svgNamespace, "path");
    path.setAttribute("d", value);
    svg.append(path);
  }
  return svg;
}

const trayPaths = ["M12 3v9m-3.5-3.5L12 12l3.5-3.5", "M4 13.5h4.2l1.6 2.5h4.4l1.6-2.5H20", "M4 13.5V19h16v-5.5L17.6 6H16M8 6H6.4L4 13.5"];
const downloadPaths = ["M12 4v11m-4.5-4.5L12 15l4.5-4.5M5 20h14"];

// SharedFilesAction is the tray button that opens Shared files, with the
// number of files nobody has listed yet. Zero shows no number.
export class SharedFilesAction {
  readonly element: HTMLButtonElement;
  readonly #badge: HTMLSpanElement;
  readonly #label: string;

  constructor(document: Document, run: () => void, className: string, label = "Shared files") {
    this.#label = label;
    this.element = button(document, "", run, `shared-files-action ${className}`);
    this.element.title = "Shared files";
    this.#badge = element(document, "span", "shared-count-badge");
    this.#badge.setAttribute("aria-hidden", "true");
    this.element.append(icon(document, trayPaths), this.#badge);
    this.setCount(0);
  }

  setCount(count: number): void {
    this.#badge.hidden = count === 0;
    this.#badge.textContent = count > 99 ? "99+" : String(count);
    this.element.setAttribute("aria-label", count === 0 ? this.#label : `${this.#label}, ${count} new`);
  }
}

export interface SharedPageModel {
  // files is undefined until the first list arrives.
  files: readonly SharedFile[] | undefined;
  // markHerdr says whether a file from this Herdr names it: both are present
  // and Home shows the other one.
  markHerdr(herdr: Herdr): boolean;
  // terminalLabel names a Terminal page's terminal before it has shared.
  terminalLabel?: SharedFrom;
  // canAsk offers Ask for a file: the filter's terminal is current.
  canAsk: boolean;
  route: SharedRoute;
  sort: SharedSort;
}

export interface SharedPageActions {
  askForFile(): void;
  back(): void;
  navigate(route: SharedRoute): void;
  openTerminal(herdr: Herdr, target: SharedTarget): void;
  remove(ids: string[]): Promise<boolean>;
  sort(sort: SharedSort): void;
}

// ListedModel is a model whose list has arrived.
interface ListedModel extends SharedPageModel {
  files: readonly SharedFile[];
}

interface RowNodes {
  download: HTMLAnchorElement;
  from: HTMLSpanElement;
  fromColumn: HTMLSpanElement;
  item: HTMLLIElement;
  menu: HTMLDivElement;
  menuTrigger: HTMLButtonElement;
  name: HTMLSpanElement;
  open: HTMLButtonElement;
  size: HTMLSpanElement;
  when: HTMLSpanElement;
  whenColumn: HTMLSpanElement;
}

// SharedPage is the Shared files page: the list with sort, filter, preview,
// download and delete. It updates in place as the list changes, keeping
// each row and its thumbnail.
export class SharedPage {
  readonly #actions: SharedPageActions;
  readonly #document: Document;
  readonly #host: HTMLElement;
  readonly #page: HTMLElement;
  readonly #backButton: HTMLButtonElement;
  readonly #subtitle: HTMLSpanElement;
  readonly #pageMenu: HTMLDivElement;
  readonly #pageMenuTrigger: HTMLButtonElement;
  readonly #pageMenuPopover: HTMLDivElement;
  readonly #filterBar: HTMLDivElement;
  readonly #tools: HTMLElement;
  readonly #sortButton: HTMLButtonElement;
  readonly #columns: HTMLDivElement;
  readonly #lists: HTMLDivElement;
  readonly #empty: HTMLElement;
  readonly #layer: HTMLDivElement;
  readonly #rows = new Map<string, RowNodes>();
  #model: ListedModel | undefined;
  #openMenu: string | undefined;
  #sheet: (() => void) | undefined;
  #sheetFiles: string[] = [];
  #viewerFile: string | undefined;
  #viewerReturn: string | undefined;
  #textAbort: AbortController | undefined;
  #clock: number | undefined;
  #keydown = (event: KeyboardEvent): void => {
    if (event.key !== "Escape") return;
    if (this.#sheet) this.#closeSheet();
    else if (this.#openMenu !== undefined) this.#closeMenus(true);
    else if (this.#viewerFile) this.#closeViewer();
  };
  #documentClick = (event: MouseEvent): void => {
    if (this.#openMenu === undefined) return;
    const target = event.target as Node | null;
    const menu = this.#openMenu === "" ? this.#pageMenu : this.#rows.get(this.#openMenu)?.menu;
    if (menu && target && !menu.contains(target)) this.#closeMenus(false);
  };

  constructor(host: HTMLElement, actions: SharedPageActions) {
    this.#host = host;
    this.#actions = actions;
    this.#document = host.ownerDocument;
    const document = this.#document;
    this.#page = element(document, "main", "shared-page");
    const header = element(document, "header", "terminal-header shared-header");
    this.#backButton = button(document, "Home", () => this.#actions.back(), "terminal-home");
    const title = element(document, "div", "terminal-title");
    const heading = element(document, "h1", "shared-title", "Shared files");
    this.#subtitle = element(document, "span", "terminal-connection");
    title.append(heading, this.#subtitle);
    this.#pageMenu = element(document, "div", "workspace-menu shared-page-menu");
    this.#pageMenuTrigger = button(document, "⋮", () => this.#toggleMenu(""), "workspace-menu-trigger");
    this.#pageMenuTrigger.setAttribute("aria-haspopup", "menu");
    this.#pageMenuTrigger.setAttribute("aria-expanded", "false");
    this.#pageMenuTrigger.setAttribute("aria-label", "More for shared files");
    this.#pageMenuPopover = element(document, "div", "workspace-menu-popover");
    this.#pageMenuPopover.setAttribute("role", "menu");
    this.#pageMenuPopover.hidden = true;
    this.#pageMenu.append(this.#pageMenuTrigger, this.#pageMenuPopover);
    header.append(this.#backButton, title, this.#pageMenu);

    this.#filterBar = element(document, "div", "shared-filter");
    this.#tools = element(document, "section", "shared-tools");
    this.#tools.setAttribute("aria-label", "Shared files controls");
    this.#sortButton = button(document, "", () => this.#openSort(), "shared-sort");
    this.#sortButton.setAttribute("aria-haspopup", "dialog");
    this.#tools.append(this.#sortButton);
    this.#columns = element(document, "div", "shared-head");
    this.#columns.setAttribute("aria-hidden", "true");
    for (const label of ["", "Name", "From", "Shared", "Size"]) this.#columns.append(element(document, "span", undefined, label));
    this.#lists = element(document, "div", "shared-lists");
    this.#empty = element(document, "section", "shared-empty");
    this.#layer = element(document, "div", "shared-layer");
    this.#page.append(header, this.#filterBar, this.#tools, this.#columns, this.#lists, this.#empty);
    host.className = "shared-screen";
    host.replaceChildren(this.#page, this.#layer);
    document.addEventListener("keydown", this.#keydown);
    document.addEventListener("click", this.#documentClick, true);
    // Times like "2 min ago" move on while the page is open.
    this.#clock = document.defaultView?.setInterval(() => { if (this.#model) this.#renderRows(this.#model); }, 30_000);
  }

  destroy(): void {
    this.#textAbort?.abort();
    if (this.#clock !== undefined) this.#document.defaultView?.clearInterval(this.#clock);
    this.#document.removeEventListener("keydown", this.#keydown);
    this.#document.removeEventListener("click", this.#documentClick, true);
    this.#host.replaceChildren();
    this.#host.className = "";
  }

  update(update: SharedPageModel): void {
    const route = update.route;
    this.#backButton.textContent = route.back ? "Back" : "Home";
    this.#backButton.setAttribute("aria-label", route.back ? "Back to the terminal" : "Home");
    // Until the list arrives nothing is known about it: the page says
    // nothing and keeps the file in its address for when it does.
    if (update.files === undefined) {
      for (const node of [this.#filterBar, this.#tools, this.#columns, this.#empty, this.#pageMenu]) node.hidden = true;
      this.#subtitle.textContent = "";
      return;
    }
    const firstRender = this.#model === undefined;
    const model: ListedModel = { ...update, files: update.files };
    this.#model = model;
    const shown = this.#shown(model);
    const total = model.files.reduce((sum, file) => sum + file.size, 0);
    const shownTotal = shown.reduce((sum, file) => sum + file.size, 0);
    const files = (count: number) => `${count} ${count === 1 ? "file" : "files"}`;
    this.#subtitle.textContent = model.files.length === 0
      ? "Nothing shared"
      : route.filter
        ? `${shown.length} of ${files(model.files.length)} · ${formatSize(shownTotal)}`
        : `${files(model.files.length)} · ${formatSize(total)}`;
    this.#renderFilter(model, shown);
    this.#sortButton.textContent = sharedSortLabels[model.sort];
    this.#renderPageMenu(model, shown);
    this.#tools.hidden = shown.length === 0;
    this.#columns.hidden = shown.length === 0 || model.sort === "agent";
    this.#renderEmpty(model, shown);
    this.#renderRows(model);
    // A sheet or file that is no longer listed closes.
    if (this.#sheet && this.#sheetFiles.some((id) => !model.files.some((file) => file.id === id))) this.#closeSheet();
    this.#renderViewer(model);
    if (firstRender && !route.file) this.#backButton.focus({ preventScroll: true });
  }

  #shown(model: ListedModel): SharedFile[] {
    return filesFor(model.files, model.route.filter);
  }

  #renderFilter(model: ListedModel, shown: SharedFile[]): void {
    const filter = model.route.filter;
    this.#filterBar.hidden = !filter;
    if (!filter) return;
    const document = this.#document;
    // A live terminal goes by its name on Home; a file may carry an older one.
    const from = model.terminalLabel ?? shown[0]?.from;
    const text = element(document, "p");
    text.append("From ");
    if (from) {
      text.append(element(document, "strong", undefined, placeText(from)));
      const agent = agentText(from);
      if (agent) text.append(" ", element(document, "span", "quiet-inline", `· ${agent}`));
    } else {
      text.append("this terminal");
    }
    const showAll = button(document, `Show all ${model.files.length}`, () => this.#actions.navigate({ ...(model.route.back ? { back: model.route.back } : {}) }));
    showAll.hidden = model.files.length === 0;
    this.#filterBar.replaceChildren(text, showAll);
  }

  #renderPageMenu(model: ListedModel, shown: SharedFile[]): void {
    const items: HTMLButtonElement[] = [];
    if (model.canAsk) items.push(this.#menuItem("Ask for a file", () => this.#actions.askForFile()));
    if (shown.length > 0) items.push(this.#menuItem(`Delete all shown (${shown.length})`, () => this.#confirmDeleteShown()));
    this.#pageMenu.hidden = items.length === 0;
    this.#pageMenuPopover.replaceChildren(...items);
    if (items.length === 0 && this.#openMenu === "") this.#closeMenus(false);
  }

  #renderEmpty(model: ListedModel, shown: SharedFile[]): void {
    const document = this.#document;
    this.#empty.hidden = shown.length > 0;
    if (shown.length > 0) return;
    if (!model.route.filter) {
      const command = element(document, "p");
      command.append("Ask an agent to run ", element(document, "code", undefined, "shepherdr share <file>"), ".");
      this.#empty.replaceChildren(
        element(document, "h2", undefined, "Nothing shared yet"),
        element(document, "p", undefined, "When an agent shares a file, it appears here, newest first, until you delete it."),
        command,
      );
      return;
    }
    const from = model.terminalLabel;
    const where = element(document, "p");
    where.append("Files shared from other terminals are under ", element(document, "strong", undefined, "Show all"), ".");
    this.#empty.replaceChildren(
      element(document, "h2", undefined, from ? `Nothing shared from ${placeText(from)}` : "Nothing shared from this terminal"),
      ...(model.files.length > 0 ? [where] : []),
      ...(model.canAsk ? [button(document, "Ask for a file", () => this.#actions.askForFile())] : []),
    );
  }

  #renderRows(model: ListedModel): void {
    const document = this.#document;
    const now = new Date();
    const shown = this.#shown(model);
    const live = new Set(shown.map((file) => file.id));
    for (const [id, row] of this.#rows) {
      if (!live.has(id)) {
        row.item.remove();
        this.#rows.delete(id);
        if (this.#openMenu === id) this.#openMenu = undefined;
      }
    }
    const lists: HTMLElement[] = [];
    const list = (files: SharedFile[]) => {
      const node = element(document, "ul", "shared-list");
      for (const file of files) node.append(this.#row(file, model, now).item);
      return node;
    };
    if (model.sort === "agent") {
      for (const group of groupByAgent(shown)) {
        const heading = element(document, "h2", "shared-group-heading");
        if (group.from) {
          heading.append(element(document, "strong", undefined, placeText(group.from)));
          const rest = [agentText(group.from), model.markHerdr(group.from.herdr) ? (group.from.herdr === "core" ? "Herdr Core" : "Herdr TUI") : undefined]
            .filter((part): part is string => Boolean(part));
          heading.append(` · ${[...rest, String(group.files.length)].join(" · ")}`);
        } else {
          heading.append(element(document, "strong", undefined, "Outside Herdr"), ` · ${group.files.length}`);
        }
        lists.push(heading, list(group.files));
      }
    } else if (shown.length > 0) {
      lists.push(list(sortFiles(shown, model.sort)));
    }
    // Moving a row keeps it, but can drop its focus; put focus back.
    const focused = document.activeElement as HTMLElement | null;
    this.#lists.replaceChildren(...lists);
    if (focused && focused !== document.body && !focused.isConnected) this.#backButton.focus({ preventScroll: true });
    else if (focused && focused !== document.activeElement) focused.focus({ preventScroll: true });
  }

  #row(file: SharedFile, model: ListedModel, now: Date): RowNodes {
    const document = this.#document;
    let row = this.#rows.get(file.id);
    if (!row) {
      const item = element(document, "li", "shared-item");
      const open = button(document, "", () => this.#open(file.id), "shared-open");
      const thumb = element(document, "span", "shared-thumb");
      if (file.kind === "image") {
        const image = element(document, "img");
        image.alt = "";
        image.loading = "lazy";
        image.decoding = "async";
        image.src = sharedFileURL(file.id);
        thumb.append(image);
      } else if (file.kind === "video") {
        thumb.classList.add("video");
        const video = element(document, "video");
        video.muted = true;
        video.preload = "metadata";
        video.playsInline = true;
        video.setAttribute("aria-hidden", "true");
        video.tabIndex = -1;
        video.src = `${sharedFileURL(file.id)}#t=0.1`;
        thumb.append(video);
      } else {
        thumb.append(element(document, "span", "file-kind", file.tag));
      }
      const main = element(document, "span", "shared-main");
      const name = element(document, "span", "shared-name");
      const from = element(document, "span", "shared-from");
      const when = element(document, "span", "shared-when");
      main.append(name, from, when);
      const fromColumn = element(document, "span", "shared-col");
      const whenColumn = element(document, "span", "shared-col mono");
      const size = element(document, "span", "shared-col mono size");
      open.append(thumb, main, fromColumn, whenColumn, size);
      const download = element(document, "a", "shared-download");
      download.href = sharedFileURL(file.id, true);
      download.download = "";
      download.append(icon(document, downloadPaths));
      const menu = element(document, "div", "workspace-menu");
      const menuTrigger = button(document, "⋮", () => this.#toggleMenu(file.id), "workspace-menu-trigger");
      menuTrigger.setAttribute("aria-haspopup", "menu");
      menuTrigger.setAttribute("aria-expanded", "false");
      menu.append(menuTrigger);
      item.append(open, download, menu);
      row = { download, from, fromColumn, item, menu, menuTrigger, name, open, size, when, whenColumn };
      this.#rows.set(file.id, row);
    }
    const fromLine = fromText(file.from, file.from ? model.markHerdr(file.from.herdr) : false);
    const when = formatShared(file.shared_at, now);
    const size = formatSize(file.size);
    row.name.textContent = file.name;
    row.from.textContent = fromLine;
    row.when.textContent = `${when} · ${size}`;
    row.fromColumn.textContent = fromLine;
    row.whenColumn.textContent = when;
    row.size.textContent = size;
    row.open.setAttribute("aria-label", `Open ${file.name}, from ${fromLine}, ${when}, ${size}`);
    row.download.setAttribute("aria-label", `Download ${file.name}`);
    row.menuTrigger.setAttribute("aria-label", `Actions for ${file.name}`);
    this.#renderRowMenu(file, row);
    return row;
  }

  #renderRowMenu(file: SharedFile, row: RowNodes): void {
    const open = this.#openMenu === file.id;
    row.menuTrigger.setAttribute("aria-expanded", String(open));
    row.menu.querySelector(".workspace-menu-popover")?.remove();
    if (!open) return;
    const popover = element(this.#document, "div", "workspace-menu-popover");
    popover.setAttribute("role", "menu");
    popover.append(
      this.#menuItem("Open", () => this.#open(file.id)),
      this.#menuItem("Download", () => this.#download(file.id)),
      ...(file.from?.current ? [this.#menuItem("Open terminal", () => this.#openTerminal(file))] : []),
      this.#menuItem("Delete", () => this.#confirmDelete(file)),
    );
    row.menu.append(popover);
  }

  #menuItem(text: string, run: () => void): HTMLButtonElement {
    const item = button(this.#document, text, () => {
      this.#closeMenus(false);
      run();
    });
    item.setAttribute("role", "menuitem");
    return item;
  }

  #toggleMenu(key: string): void {
    const opening = this.#openMenu !== key;
    this.#closeMenus(false);
    if (!opening) return;
    this.#openMenu = key;
    if (key === "") {
      this.#pageMenuPopover.hidden = false;
      this.#pageMenuTrigger.setAttribute("aria-expanded", "true");
      this.#pageMenuPopover.querySelector<HTMLButtonElement>("button")?.focus();
      return;
    }
    const model = this.#model;
    const file = model?.files.find((candidate) => candidate.id === key);
    const row = this.#rows.get(key);
    if (!file || !row) return;
    this.#renderRowMenu(file, row);
    row.menu.querySelector<HTMLButtonElement>(".workspace-menu-popover button")?.focus();
  }

  #closeMenus(restoreFocus: boolean): void {
    const key = this.#openMenu;
    this.#openMenu = undefined;
    this.#pageMenuPopover.hidden = true;
    this.#pageMenuTrigger.setAttribute("aria-expanded", "false");
    if (key) {
      const row = this.#rows.get(key);
      const file = this.#model?.files.find((candidate) => candidate.id === key);
      if (row && file) this.#renderRowMenu(file, row);
      if (restoreFocus) row?.menuTrigger.focus();
    } else if (key === "" && restoreFocus) {
      this.#pageMenuTrigger.focus();
    }
  }

  #open(id: string): void {
    this.#viewerReturn = id;
    if (this.#model) this.#actions.navigate({ ...this.#model.route, file: id });
  }

  #download(id: string): void {
    const link = element(this.#document, "a");
    link.href = sharedFileURL(id, true);
    link.download = "";
    link.hidden = true;
    this.#page.append(link);
    link.click();
    link.remove();
  }

  #openTerminal(file: SharedFile): void {
    if (file.from?.current) this.#actions.openTerminal(file.from.herdr, file.from.current);
  }

  #openSort(): void {
    const model = this.#model;
    if (!model) return;
    const document = this.#document;
    const options = element(document, "div", "sheet-options");
    options.setAttribute("role", "radiogroup");
    options.setAttribute("aria-label", "Sort");
    for (const sort of sharedSorts) {
      const choice = button(document, "", () => {
        this.#closeSheet();
        this.#actions.sort(sort);
      });
      choice.setAttribute("role", "radio");
      choice.setAttribute("aria-checked", String(sort === model.sort));
      if (sort === "agent") {
        const label = element(document, "span", undefined, sharedSortLabels[sort]);
        label.append(element(document, "small", undefined, "Grouped by the terminal that shared them"));
        choice.append(label);
      } else {
        choice.textContent = sharedSortLabels[sort];
      }
      options.append(choice);
    }
    const close = button(document, "Close", () => this.#closeSheet());
    const buttons = element(document, "div", "home-action-buttons");
    buttons.append(close);
    this.#showSheet("Sort shared files", [options, buttons], [], this.#sortButton);
    options.querySelector<HTMLButtonElement>('[aria-checked="true"]')?.focus();
  }

  #confirmDelete(file: SharedFile): void {
    this.#confirm(`Delete ${file.name}?`,
      "This removes Shepherdr's copy for every device. The file the agent shared from stays where it was.",
      "Delete", [file.id]);
  }

  #confirmDeleteShown(): void {
    const model = this.#model;
    if (!model) return;
    const shown = this.#shown(model);
    const count = shown.length;
    const from = shown[0]?.from;
    const copy = model.route.filter && from
      ? `This removes the ${count === 1 ? "file" : `${count} files`} shared from ${placeText(from)}, for every device. The agent's own files stay where they were.`
      : `This removes ${count === 1 ? "this file" : `all ${count} files`} for every device. The agents' own files stay where they were.`;
    this.#confirm(`Delete all ${count} shown?`, copy, count === 1 ? "Delete 1 file" : `Delete ${count} files`, shown.map((file) => file.id));
  }

  #confirm(title: string, copy: string, action: string, ids: string[]): void {
    const document = this.#document;
    const text = element(document, "p", "home-action-copy", copy);
    const feedback = element(document, "p", "home-action-help");
    feedback.setAttribute("role", "status");
    feedback.hidden = true;
    const confirm = button(document, action, () => {
      confirm.disabled = true;
      cancel.disabled = true;
      void this.#actions.remove(ids).then((removed) => {
        if (!this.#sheet) return;
        if (removed) {
          this.#closeSheet();
          return;
        }
        confirm.disabled = false;
        cancel.disabled = false;
        feedback.hidden = false;
        feedback.textContent = "The files weren't deleted. Try again.";
      });
    }, "home-action-primary");
    const cancel = button(document, "Cancel", () => this.#closeSheet());
    const buttons = element(document, "div", "home-action-buttons");
    buttons.append(confirm, cancel);
    this.#showSheet(title, [text, feedback, buttons], ids, this.#document.activeElement as HTMLElement | null);
    cancel.focus();
  }

  #showSheet(title: string, content: HTMLElement[], files: string[], returnFocus: HTMLElement | null): void {
    const document = this.#document;
    const layer = element(document, "div", "home-action-layer shared-sheet");
    const panel = element(document, "section", "home-action-panel");
    panel.setAttribute("role", "dialog");
    panel.setAttribute("aria-modal", "true");
    const heading = element(document, "h2", "home-action-title", title);
    heading.id = "shared-sheet-title";
    panel.setAttribute("aria-labelledby", heading.id);
    panel.append(heading, ...content);
    layer.append(panel);
    layer.addEventListener("click", (event) => { if (event.target === layer) this.#closeSheet(); });
    this.#layer.querySelector(".shared-sheet")?.remove();
    this.#layer.append(layer);
    this.#sheetFiles = files;
    this.#sheet = () => {
      layer.remove();
      if (returnFocus?.isConnected) returnFocus.focus({ preventScroll: true });
    };
  }

  #closeSheet(): void {
    const close = this.#sheet;
    this.#sheet = undefined;
    this.#sheetFiles = [];
    close?.();
  }

  #closeViewer(): void {
    if (this.#model) {
      const route = { ...this.#model.route };
      delete route.file;
      this.#actions.navigate(route);
    }
  }

  #renderViewer(model: ListedModel): void {
    const id = model.route.file;
    const file = id ? model.files.find((candidate) => candidate.id === id) : undefined;
    if (id && !file) {
      // The file was deleted or never listed: show the list.
      this.#removeViewer();
      this.#closeViewer();
      return;
    }
    if (!file) {
      this.#removeViewer();
      return;
    }
    if (this.#viewerFile === file.id) {
      this.#renderViewerInfo(file, model);
      return;
    }
    this.#removeViewer();
    this.#viewerFile = file.id;
    const document = this.#document;
    const backdrop = element(document, "div", "shared-backdrop");
    backdrop.addEventListener("click", () => this.#closeViewer());
    const viewer = element(document, "section", "viewer");
    viewer.setAttribute("role", "dialog");
    viewer.setAttribute("aria-modal", "true");
    viewer.setAttribute("aria-label", file.name);
    const header = element(document, "header", "terminal-header");
    const back = button(document, "Back", () => this.#closeViewer(), "terminal-home");
    back.setAttribute("aria-label", "Back to shared files");
    const title = element(document, "div", "terminal-title");
    title.append(element(document, "h1", "shared-title viewer-name", file.name), element(document, "span", "terminal-connection", `${file.tag} · ${formatSize(file.size)}`));
    header.append(back, title);
    const stage = element(document, "div", "viewer-stage");
    stage.append(this.#stage(file));
    const foot = element(document, "div", "viewer-foot");
    viewer.append(header, stage, foot);
    this.#layer.querySelectorAll(".shared-backdrop, .viewer").forEach((node) => node.remove());
    this.#layer.prepend(backdrop, viewer);
    this.#renderViewerInfo(file, model);
    back.focus({ preventScroll: true });
  }

  #renderViewerInfo(file: SharedFile, model: ListedModel): void {
    const foot = this.#layer.querySelector<HTMLElement>(".viewer-foot");
    if (!foot) return;
    const document = this.#document;
    const info = element(document, "div", "viewer-info");
    const where = element(document, "span");
    where.append("From ", element(document, "strong", undefined, fromText(file.from, file.from ? model.markHerdr(file.from.herdr) : false)));
    where.append(element(document, "br"), formatShared(file.shared_at, new Date()));
    info.append(where);
    if (file.from?.current) info.append(button(document, "Open terminal", () => this.#openTerminal(file)));
    const actions = element(document, "div", "viewer-actions");
    actions.append(
      button(document, "Download", () => this.#download(file.id), "home-action-primary"),
      button(document, "Delete", () => this.#confirmDelete(file)),
    );
    foot.replaceChildren(info, actions);
  }

  #stage(file: SharedFile): HTMLElement {
    const document = this.#document;
    const url = sharedFileURL(file.id);
    if (file.kind === "image") {
      const image = element(document, "img");
      image.src = url;
      image.alt = file.name;
      return image;
    }
    if (file.kind === "video") {
      const video = element(document, "video");
      video.src = url;
      video.controls = true;
      video.playsInline = true;
      video.preload = "metadata";
      return video;
    }
    if (file.kind === "text") {
      const text = element(document, "pre", "viewer-text", "Loading…");
      text.tabIndex = 0;
      void this.#loadText(url, text);
      return text;
    }
    const card = element(document, "div", "viewer-doc");
    card.append(element(document, "div", "doc-glyph", file.tag), element(document, "h2", undefined, file.name));
    if (file.kind === "pdf") {
      card.append(
        element(document, "p", undefined, "PDFs open in your browser's own viewer, in a new tab."),
        button(document, "Open PDF", () => { this.#document.defaultView?.open(url, "_blank", "noopener"); }, "home-action-primary"),
      );
    } else {
      card.append(
        element(document, "p", undefined, "Shepherdr doesn't show this kind of file. Download it to open it."),
        button(document, "Download", () => this.#download(file.id), "home-action-primary"),
      );
    }
    return card;
  }

  // Text shows as plain text, never rendered, up to its first 1 MiB.
  async #loadText(url: string, target: HTMLElement): Promise<void> {
    const limit = 1 << 20;
    this.#textAbort?.abort();
    const abort = new AbortController();
    this.#textAbort = abort;
    try {
      const response = await fetch(url, {
        cache: "no-store", credentials: "same-origin", headers: { Range: `bytes=0-${limit - 1}` }, signal: abort.signal,
      });
      if (!response.ok) throw new Error("text unavailable");
      const bytes = new Uint8Array(await response.arrayBuffer()).slice(0, limit);
      const total = Number(response.headers.get("Content-Range")?.split("/")[1] ?? bytes.length);
      target.textContent = new TextDecoder("utf-8", { fatal: false }).decode(bytes);
      if (Number.isFinite(total) && total > bytes.length) {
        target.append(this.#document.createTextNode("\n\n"), element(this.#document, "em", "viewer-text-note", "Showing the first 1 MB. Download the file to see the rest."));
      }
    } catch {
      if (!abort.signal.aborted) target.textContent = "This file couldn't be shown. Download it to open it.";
    }
  }

  #removeViewer(): void {
    if (this.#viewerFile === undefined) return;
    this.#textAbort?.abort();
    this.#layer.querySelectorAll(".shared-backdrop, .viewer").forEach((node) => node.remove());
    this.#viewerFile = undefined;
    const back = this.#viewerReturn ? this.#rows.get(this.#viewerReturn)?.open : undefined;
    this.#viewerReturn = undefined;
    back?.focus({ preventScroll: true });
  }
}

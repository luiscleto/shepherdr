import {
  accessibleTerminalName,
  allTerminals,
  allWorkspaces,
  showTabHeadings,
  terminalCount,
  visibleTabs,
  workspaceSets,
  type AgentCounts,
  type HomeState,
  type Tab,
  type TerminalEntry,
  type Workspace,
} from "./home-model";
import type { HomeReachability } from "./home-connection";

type HomeMode = "all" | "blocked";

interface HomeViewActions {
  onFocusPane: (paneID: string) => void;
  onOpen: (entry: TerminalEntry) => void;
  onReconnect: () => void;
  onShowAll: () => void;
  onShowBlocked: () => void;
}

export interface HomeViewRender {
  actionsAvailable: boolean;
  mode: HomeMode;
  reachability: HomeReachability;
  restore?: { anchorTop?: number; focusPane?: string; pane?: string; scroll: number };
  state: HomeState;
}

interface WorkspaceNodes {
  heading: HTMLHeadingElement;
  section: HTMLElement;
}

interface WorkspaceSetNodes {
  contents: HTMLElement;
  disclosure: HTMLButtonElement;
  heading: HTMLHeadingElement;
  meta: HTMLElement;
  section: HTMLElement;
  title: HTMLElement;
}

interface TabNodes {
  current: HTMLElement;
  group: HTMLElement;
  heading: HTMLElement;
  list: HTMLUListElement;
  title: HTMLElement;
}

interface RowNodes {
  agent: HTMLElement;
  chevron: HTMLElement;
  main: HTMLElement;
  name: HTMLElement;
  row: HTMLElement;
  status: HTMLElement;
}

interface ConnectionCopy {
  action?: "Reconnect";
  body: string;
  detail?: string;
  heading: string;
}

function element<K extends keyof HTMLElementTagNameMap>(
  document: Document,
  tag: K,
  className?: string,
  text?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function setText(node: Node, text: string): void {
  if (node.textContent !== text) node.textContent = text;
}

function setClass(node: HTMLElement, className: string): void {
  if (node.className !== className) node.className = className;
}

function setAttribute(node: HTMLElement, name: string, value: string): void {
  if (node.getAttribute(name) !== value) node.setAttribute(name, value);
}

function setHidden(node: HTMLElement, hidden: boolean): void {
  if (node.hidden !== hidden) node.hidden = hidden;
}

function reconcileChildren(parent: HTMLElement, desired: readonly Node[]): void {
  for (let index = 0; index < desired.length; index++) {
    const node = desired[index];
    if (parent.childNodes[index] !== node) parent.insertBefore(node, parent.childNodes[index] ?? null);
  }
  while (parent.childNodes.length > desired.length) parent.lastChild?.remove();
}

export class HomeView {
  readonly #actions: HomeViewActions;
  readonly #app: HTMLElement;
  readonly #attention: HTMLElement;
  readonly #attentionBlocked: HTMLButtonElement;
  readonly #attentionShowAll: HTMLButtonElement;
  readonly #attentionWorking: HTMLElement;
  readonly #connectionBody: HTMLElement;
  readonly #connectionAction: HTMLButtonElement;
  readonly #connectionDetails: HTMLDetailsElement;
  readonly #connectionDetail: HTMLElement;
  readonly #connectionHeading: HTMLElement;
  readonly #connectionPanel: HTMLElement;
  readonly #connectionSummary: HTMLElement;
  readonly #document: Document;
  readonly #empty: HTMLElement;
  readonly #expandAction: HTMLButtonElement;
  readonly #expandedSets = new Map<string, boolean>();
  readonly #header: HTMLElement;
  readonly #headerHeading: HTMLElement;
  readonly #loading: HTMLElement;
  readonly #loadingHeading: HTMLElement;
  readonly #rows = new Map<string, RowNodes>();
  readonly #items = new Map<string, HTMLLIElement>();
  readonly #tabs = new Map<string, TabNodes>();
  readonly #workspaces = new Map<string, WorkspaceNodes>();
  readonly #workspaceSets = new Map<string, WorkspaceSetNodes>();
  readonly #entries = new Map<string, TerminalEntry>();
  #lastRender: HomeViewRender | undefined;
  #workspaceHeadingSequence = 0;

  constructor(app: HTMLElement, actions: HomeViewActions) {
    this.#app = app;
    this.#actions = actions;
    this.#document = app.ownerDocument;

    this.#header = element(this.#document, "header", "masthead");
    const heading = element(this.#document, "div");
    this.#headerHeading = element(this.#document, "h1", undefined, "Home");
    heading.append(
      element(this.#document, "p", "eyebrow", "Shepherdr"),
      this.#headerHeading,
      element(this.#document, "p", "quiet", "Sign-in is off."),
    );
    this.#connectionPanel = element(this.#document, "section", "state-panel home-connection");
    this.#connectionPanel.setAttribute("role", "status");
    this.#connectionHeading = element(this.#document, "strong");
    this.#connectionBody = element(this.#document, "p");
    this.#connectionDetails = element(this.#document, "details");
    this.#connectionSummary = element(this.#document, "summary", undefined, "Technical details");
    this.#connectionDetail = element(this.#document, "p", "detail");
    this.#connectionAction = this.#button("Reconnect", actions.onReconnect);
    this.#connectionDetails.append(this.#connectionSummary, this.#connectionDetail);
    this.#connectionPanel.append(
      this.#connectionHeading,
      this.#connectionBody,
      this.#connectionAction,
      this.#connectionDetails,
    );
    this.#header.append(heading, this.#connectionPanel);

    this.#loading = element(this.#document, "section", "state-panel");
    this.#loadingHeading = element(this.#document, "strong", undefined, "Loading terminals");
    this.#loading.append(this.#loadingHeading);

    this.#empty = element(this.#document, "section", "state-panel");
    this.#empty.append(
      element(this.#document, "strong", undefined, "No terminals"),
      element(this.#document, "p", undefined, "Herdr is running, but nothing is open."),
    );

    this.#attention = element(this.#document, "aside", "attention-bar");
    this.#attention.setAttribute("aria-label", "Current attention");
    this.#attentionWorking = element(this.#document, "strong");
    this.#attentionBlocked = this.#button("", actions.onShowBlocked);
    this.#attentionShowAll = this.#button("Show all terminals", actions.onShowAll);
    this.#attention.append(this.#attentionWorking, this.#attentionBlocked, this.#attentionShowAll);

    this.#expandAction = this.#button("", () => {
      const expand = workspaceSets(this.#lastRender?.state.home ?? { blocked_count: 0, working_count: 0, workspaces: [] })
        .some((workspace) => !this.#expandedSets.get(workspace.id));
      for (const workspace of workspaceSets(this.#lastRender?.state.home ?? { blocked_count: 0, working_count: 0, workspaces: [] })) {
        this.#expandedSets.set(workspace.id, expand);
      }
      if (this.#lastRender) this.render(this.#lastRender);
    });
    this.#expandAction.className = "workspace-expand-action";
  }

  render(model: HomeViewRender): void {
    this.#lastRender = model.restore ? { ...model, restore: undefined } : model;
    const view = this.#document.defaultView;
    const oldScroll = model.restore?.scroll ?? view?.scrollY ?? 0;
    setClass(this.#app, "home");
    setText(this.#headerHeading, model.mode === "blocked" ? "Blocked" : "Home");

    const live = model.reachability === "current" && model.state.connection === "live" && !model.state.last_known;

    const desired: Node[] = [this.#header];
    const connection = this.#connectionCopy(model);
    this.#updateConnection(connection);

    const allWorkspaceValues = allWorkspaces(model.state.home);
    const usedWorkspaces = new Set(allWorkspaceValues.map((workspace) => workspace.id));
    const usedTabs = new Set<string>();
    const usedSets = new Set<string>();
    const completePaneIDs = new Set(allTerminals(model.state.home).map(({ terminal }) => terminal.pane_id));

    if (!model.state.has_home) {
      setText(
        this.#loadingHeading,
        model.state.connection === "reconnecting" && model.reachability !== "offline"
          ? "Loading terminals"
          : "Home unavailable",
      );
      desired.push(this.#loading);
    } else {
      const total = allTerminals(model.state.home).length;
      if (live && total === 0) desired.push(this.#empty);

      if (model.mode === "blocked") {
        for (const workspace of allWorkspaceValues) {
          const section = this.#renderWorkspace(workspace, true, model.actionsAvailable, usedTabs);
          if (section) desired.push(section);
        }
      } else {
        const sets = workspaceSets(model.state.home);
        for (const workspace of sets) {
          if (!this.#expandedSets.has(workspace.id)) {
            const counts = workspace.agent_counts ?? {};
            this.#expandedSets.set(workspace.id, (counts.working ?? 0) > 0 || (counts.blocked ?? 0) > 0);
          }
        }
        if (sets.length > 0) {
          const expand = sets.some((workspace) => !this.#expandedSets.get(workspace.id));
          setText(this.#expandAction, expand ? "Expand all" : "Collapse all");
          desired.push(this.#expandAction);
        }
        for (const workspace of model.state.home.workspaces) {
          if ((workspace.worktrees?.length ?? 0) > 0) {
            usedSets.add(workspace.id);
            desired.push(this.#renderWorkspaceSet(workspace, model.actionsAvailable, usedTabs));
            continue;
          }
          const section = this.#renderWorkspace(workspace, false, model.actionsAvailable, usedTabs);
          if (section) desired.push(section);
        }
      }

      setText(this.#attentionWorking, `${model.state.home.working_count} working`);
      const showAll = model.mode === "blocked";
      const showBlocked = !showAll && model.state.home.blocked_count > 0;
      setText(this.#attentionBlocked, `${model.state.home.blocked_count} blocked`);
      setHidden(this.#attentionBlocked, !showBlocked);
      setHidden(this.#attentionShowAll, !showAll);
      desired.push(this.#attention);
    }

    reconcileChildren(this.#app, desired);
    this.#prune(usedWorkspaces, usedTabs, usedSets, completePaneIDs);

    view?.scrollTo(0, oldScroll);
    if (model.restore?.anchorTop !== undefined && model.restore.pane) {
      const row = this.#rows.get(model.restore.pane)?.row;
      if (row) view?.scrollTo(0, (view?.scrollY ?? oldScroll) + row.getBoundingClientRect().top - model.restore.anchorTop);
    }
    if (model.restore?.focusPane) {
      this.#rows.get(model.restore.focusPane)?.row.focus({ preventScroll: true });
    }
  }

  row(paneID: string): HTMLElement | undefined {
    return this.#rows.get(paneID)?.row;
  }

  #button(text: string, action: () => void): HTMLButtonElement {
    const node = element(this.#document, "button", undefined, text);
    node.type = "button";
    node.addEventListener("click", action);
    return node;
  }

  #connectionCopy(model: HomeViewRender): ConnectionCopy {
    if (model.state.connection === "not_running") {
      return {
        heading: "Herdr is not running",
        body: model.state.last_known
          ? "Values below are last known. Start Herdr and Shepherdr will reconnect."
          : "Start Herdr. Shepherdr will reconnect.",
      };
    }
    if (model.state.connection === "incompatible") {
      return {
        heading: "Cannot use this Herdr",
        body: model.state.last_known
          ? "Values below are last known. This Herdr cannot be used. Start a supported Herdr on the machine."
          : "This Herdr cannot be used. Start a supported Herdr on the machine.",
        detail: model.state.detail,
      };
    }
    if (model.reachability === "offline") {
      return {
        heading: "Offline",
        body: model.state.has_home ? "State below may be stale." : "Shepherdr isn't connected.",
        action: "Reconnect",
      };
    }
    if (model.reachability === "reconnecting" || model.state.connection === "reconnecting") {
      return {
        heading: "Reconnecting",
        body: model.state.has_home ? "State below may be stale." : "Getting a fresh view from Herdr.",
      };
    }
    return { heading: "Live", body: "" };
  }

  #updateConnection(copy: ConnectionCopy): void {
    setText(this.#connectionHeading, copy.heading);
    setText(this.#connectionBody, copy.body);
    setHidden(this.#connectionBody, copy.body === "");
    setHidden(this.#connectionAction, copy.action !== "Reconnect");
    if (copy.detail) {
      setText(this.#connectionDetail, copy.detail);
      setHidden(this.#connectionDetails, false);
    } else {
      setHidden(this.#connectionDetails, true);
    }
  }

  #renderWorkspace(
    workspace: Workspace,
    blockedOnly: boolean,
    actionsAvailable: boolean,
    usedTabs: Set<string>,
    hideTitle = false,
  ): HTMLElement | undefined {
    const tabs = visibleTabs(workspace, blockedOnly);
    if (tabs.length === 0) return undefined;
    const nodes = this.#workspace(workspace);
    this.#updateWorkspace(nodes, workspace, tabs, actionsAvailable, usedTabs, hideTitle);
    return nodes.section;
  }

  #renderWorkspaceSet(workspace: Workspace, actionsAvailable: boolean, usedTabs: Set<string>): HTMLElement {
    const nodes = this.#workspaceSet(workspace);
    const expanded = this.#expandedSets.get(workspace.id) ?? false;
    setAttribute(nodes.disclosure, "aria-expanded", String(expanded));
    setText(nodes.title, workspace.label);
    setText(nodes.meta, this.#setSummary(workspace.agent_counts ?? {}, 1 + (workspace.worktrees?.length ?? 0)));
    setHidden(nodes.contents, !expanded);

    const contents: Node[] = [];
    const parent = this.#renderWorkspace(workspace, false, actionsAvailable, usedTabs, true);
    if (parent) contents.push(parent);
    for (const worktree of workspace.worktrees ?? []) {
      const section = this.#renderWorkspace(worktree, false, actionsAvailable, usedTabs);
      if (section) contents.push(section);
    }
    reconcileChildren(nodes.contents, contents);
    return nodes.section;
  }

  #workspaceSet(workspace: Workspace): WorkspaceSetNodes {
    const existing = this.#workspaceSets.get(workspace.id);
    if (existing) return existing;
    const section = element(this.#document, "section", "workspace-set");
    const heading = element(this.#document, "h2", "workspace-set-heading");
    const disclosure = this.#button("", () => {
      this.#expandedSets.set(workspace.id, !this.#expandedSets.get(workspace.id));
      if (this.#lastRender) this.render(this.#lastRender);
    });
    const title = element(this.#document, "span", "workspace-set-title");
    const meta = element(this.#document, "span", "workspace-set-meta");
    const marker = element(this.#document, "span", "workspace-set-marker", "⌄");
    marker.setAttribute("aria-hidden", "true");
    disclosure.className = "workspace-set-disclosure";
    disclosure.append(title, meta, marker);
    heading.append(disclosure);
    const contents = element(this.#document, "div", "workspace-set-contents");
    section.append(heading, contents);
    const nodes = { contents, disclosure, heading, meta, section, title };
    this.#workspaceSets.set(workspace.id, nodes);
    return nodes;
  }

  #setSummary(counts: AgentCounts, workspaceCount: number): string {
    const parts = [`${workspaceCount} workspaces`];
    for (const status of ["working", "blocked", "idle", "done", "unknown"] as const) {
      const count = counts[status] ?? 0;
      if (count > 0) parts.push(`${count} ${status}`);
    }
    return parts.join(" · ");
  }

  #workspace(workspace: Workspace): WorkspaceNodes {
    const existing = this.#workspaces.get(workspace.id);
    if (existing) return existing;
    const section = element(this.#document, "section", "workspace");
    const heading = element(this.#document, "h2");
    const headingID = `workspace-heading-${++this.#workspaceHeadingSequence}`;
    heading.id = headingID;
    section.setAttribute("aria-labelledby", headingID);
    const nodes = { heading, section };
    this.#workspaces.set(workspace.id, nodes);
    return nodes;
  }

  #updateWorkspace(
    nodes: WorkspaceNodes,
    workspace: Workspace,
    tabs: ReturnType<typeof visibleTabs>,
    actionsAvailable: boolean,
    usedTabs: Set<string>,
    hideTitle: boolean,
  ): void {
    const flattened = terminalCount(workspace) === 1;
    const tabsShown = showTabHeadings(workspace);
    if (flattened) {
      const terminal = tabs[0].terminals[0];
      const entry = { workspace, tab: tabs[0].tab, terminal };
      setClass(nodes.heading, "visually-hidden");
      setText(nodes.heading, terminal.title);
      reconcileChildren(nodes.section, [nodes.heading, this.#terminalRow(entry, false, actionsAvailable).row]);
      return;
    }

    setClass(nodes.heading, hideTitle ? "visually-hidden" : "workspace-title");
    setText(nodes.heading, workspace.label);
    const children: Node[] = [nodes.heading];
    for (const group of tabs) {
      const key = `${workspace.id}\u0000${group.tab.id}`;
      usedTabs.add(key);
      const tab = this.#tab(key);
      this.#updateTab(tab, workspace, group.tab, group.terminals, tabsShown, actionsAvailable);
      children.push(tab.group);
    }
    reconcileChildren(nodes.section, children);
  }

  #tab(key: string): TabNodes {
    const existing = this.#tabs.get(key);
    if (existing) return existing;
    const group = element(this.#document, "div", "tab-group");
    const heading = element(this.#document, "div", "tab-heading");
    const list = element(this.#document, "ul", "terminal-list");
    const title = element(this.#document, "h3");
    const current = element(this.#document, "span", "current-tab", "current");
    const nodes = { current, group, heading, list, title };
    this.#tabs.set(key, nodes);
    return nodes;
  }

  #updateTab(
    nodes: TabNodes,
    workspace: Workspace,
    tab: Tab,
    terminals: Tab["terminals"],
    tabsShown: boolean,
    actionsAvailable: boolean,
  ): void {
    setText(nodes.title, tab.label);
    const headingChildren: Node[] = [nodes.title];
    if (tab.current) headingChildren.push(nodes.current);
    reconcileChildren(nodes.heading, headingChildren);
    const items = terminals.map((terminal) => {
      const entry = { workspace, tab, terminal };
      const item = this.#item(terminal.pane_id);
      reconcileChildren(item, [this.#terminalRow(entry, tabsShown, actionsAvailable).row]);
      return item;
    });
    reconcileChildren(nodes.list, items);
    reconcileChildren(nodes.group, tabsShown ? [nodes.heading, nodes.list] : [nodes.list]);
  }

  #item(paneID: string): HTMLLIElement {
    const existing = this.#items.get(paneID);
    if (existing) return existing;
    const item = element(this.#document, "li");
    this.#items.set(paneID, item);
    return item;
  }

  #terminalRow(entry: TerminalEntry, tabsShown: boolean, actionsAvailable: boolean): RowNodes {
    this.#entries.set(entry.terminal.pane_id, entry);
    let nodes = this.#rows.get(entry.terminal.pane_id);
    if (!nodes) {
      const row = element(this.#document, "button");
      row.type = "button";
      row.addEventListener("click", () => {
        if (row.getAttribute("aria-disabled") === "true") return;
        const current = this.#entries.get(entry.terminal.pane_id);
        if (current) this.#actions.onOpen(current);
      });
      row.addEventListener("focus", () => this.#actions.onFocusPane(entry.terminal.pane_id));
      const main = element(this.#document, "span", "terminal-main");
      const name = element(this.#document, "span", "terminal-name");
      const agent = element(this.#document, "span", "terminal-agent");
      const status = element(this.#document, "span", "status");
      const chevron = element(this.#document, "span", "terminal-chevron", "›");
      main.append(name, agent, status);
      row.append(main, chevron);
      row.dataset.paneKey = entry.terminal.pane_id;
      row.dataset.paneId = entry.terminal.pane_id;
      nodes = { agent, chevron, main, name, row, status };
      this.#rows.set(entry.terminal.pane_id, nodes);
    }

    setClass(nodes.row, actionsAvailable ? "terminal-row" : "terminal-row terminal-row-stale");
    setAttribute(nodes.row, "aria-disabled", String(!actionsAvailable));
    setAttribute(nodes.row, "aria-label", accessibleTerminalName(entry, tabsShown));
    setText(nodes.name, entry.terminal.title);
    if (entry.terminal.agent) {
      const secondary = entry.terminal.agent.name === entry.terminal.title ? "" : entry.terminal.agent.name;
      setText(nodes.agent, secondary);
      setHidden(nodes.agent, secondary === "");
      setClass(nodes.status, `status status-${entry.terminal.agent.status}`);
      setText(nodes.status, entry.terminal.agent.status);
      setHidden(nodes.status, false);
    } else {
      setText(nodes.agent, "");
      setHidden(nodes.agent, true);
      setClass(nodes.status, "");
      setText(nodes.status, "");
      setHidden(nodes.status, true);
    }
    return nodes;
  }

  #prune(usedWorkspaces: Set<string>, usedTabs: Set<string>, usedSets: Set<string>, paneIDs: Set<string>): void {
    for (const key of this.#workspaces.keys()) if (!usedWorkspaces.has(key)) this.#workspaces.delete(key);
    for (const key of this.#workspaceSets.keys()) if (!usedSets.has(key)) this.#workspaceSets.delete(key);
    for (const key of this.#tabs.keys()) if (!usedTabs.has(key)) this.#tabs.delete(key);
    for (const key of this.#rows.keys()) {
      if (!paneIDs.has(key)) {
        this.#rows.delete(key);
        this.#items.delete(key);
        this.#entries.delete(key);
      }
    }
  }
}

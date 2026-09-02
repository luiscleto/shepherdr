import {
  accessibleTerminalName,
  allTerminals,
  allWorkspaces,
  showTabHeadings,
  terminalCount,
  visibleTabs,
  workspaceSets,
  type AgentCounts,
  type Home,
  type HomeState,
  type Tab,
  type TerminalAction,
  type TerminalEntry,
  type Workspace,
} from "./home-model";
import { settingsAction } from "./settings-action";
import type { HomeReachability } from "./home-connection";
import {
  type ConfirmationFacts,
  type PreparedWorkspaceAction,
  type PreparedWorkspaceActionResponse,
  type PrepareWorkspaceActionRequest,
  type RefusedWorkspaceAction,
  type RunWorkspaceActionRequest,
  type RunWorkspaceActionResponse,
  type WorkspaceAction,
} from "./workspace-actions";
import {
  type PreparedTerminalActionResponse,
  type PrepareTerminalCloseRequest,
  type RunTerminalActionRequest,
  type RunTerminalActionResponse,
  type TerminalActionTarget,
  type TerminalCloseFacts,
} from "./terminal-actions";

type HomeMode = "all" | "blocked";

interface HomeViewActions {
  isHomeActive: () => boolean;
  onFocusPane: (paneID: string) => void;
  onFocusWorkspace?: (workspaceID: string) => void;
  onOpen: (entry: TerminalEntry) => void;
  onOpenCreated: (target: TerminalActionTarget) => void;
  onNotifications?: () => void;
  onShowAll: () => void;
  onShowBlocked: () => void;
  prepareWorkspaceAction: (request: PrepareWorkspaceActionRequest) => Promise<PreparedWorkspaceActionResponse>;
  runWorkspaceAction: (request: RunWorkspaceActionRequest) => Promise<RunWorkspaceActionResponse>;
  prepareTerminalAction: (request: PrepareTerminalCloseRequest) => Promise<PreparedTerminalActionResponse>;
  runTerminalAction: (request: RunTerminalActionRequest) => Promise<RunTerminalActionResponse>;
}

export interface HomeViewRender {
  actionsAvailable: boolean;
  mode: HomeMode;
  reachability: HomeReachability;
  signInOff?: boolean;
  restore?: {
    anchorTop?: number;
    focusPane?: string;
    focusWorkspace?: string;
    pane?: string;
    scroll: number;
    workspace?: string;
  };
  state: HomeState;
}

interface WorkspaceNodes {
  heading: HTMLHeadingElement;
  section: HTMLElement;
}

interface WorkspacePickerRowNodes {
  badge: HTMLElement;
  chevron: HTMLElement;
  main: HTMLElement;
  name: HTMLElement;
  row: HTMLElement;
  summary: HTMLElement;
  summaryStatuses: Map<keyof AgentCounts, HTMLElement>;
}

interface WorkspaceMenuNodes {
  container: HTMLElement;
  items: Map<string, HTMLButtonElement>;
  menu: HTMLElement;
  menuRoot: HTMLElement;
  trigger: HTMLButtonElement;
}

interface WorkspaceSetNodes {
  body: HTMLElement;
  contents: HTMLElement;
  disclosure: HTMLButtonElement;
  header: HTMLElement;
  heading: HTMLElement;
  marker: HTMLElement;
  section: HTMLElement;
  summary: HTMLElement;
  summaryStatuses: Map<keyof AgentCounts, HTMLElement>;
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
  meta: HTMLElement;
  name: HTMLElement;
  row: HTMLElement;
  status: HTMLElement;
}

type UsedTabs = Map<string, Set<string>>;

interface TerminalPickerNodes {
  body: HTMLElement;
  close: HTMLButtonElement;
  count: HTMLElement;
  layer: HTMLElement;
  panel: HTMLElement;
  title: HTMLHeadingElement;
}

interface AutomaticallyClosedPicker {
  paneID?: string;
  returnFocus?: HTMLElement;
  scroll: number;
}

interface ConnectionCopy {
  heading: string;
  tone: "live" | "offline" | "reconnecting";
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
  readonly #connectionDot: HTMLElement;
  readonly #connectionHeading: HTMLElement;
  readonly #connectionPanel: HTMLElement;
  readonly #accessNotice: HTMLElement;
  readonly #document: Document;
  readonly #empty: HTMLElement;
  readonly #expandAction: HTMLButtonElement;
  readonly #expandedSets = new Map<string, boolean>();
  readonly #filterLabel: HTMLLabelElement;
  readonly #filterInput: HTMLInputElement;
  readonly #header: HTMLElement;
  readonly #headerHeading: HTMLElement;
  readonly #herdrVersion: HTMLElement;
  readonly #notificationsAction: HTMLButtonElement;
  readonly #homeTools: HTMLElement;
  readonly #loading: HTMLElement;
  readonly #loadingHeading: HTMLElement;
  readonly #newSpaceAction: HTMLButtonElement;
  readonly #noMatches: HTMLElement;
  readonly #picker: TerminalPickerNodes;
  readonly #actionLayer: HTMLElement;
  readonly #actionPanel: HTMLElement;
  readonly #rows = new Map<string, RowNodes>();
  readonly #items = new Map<string, HTMLLIElement>();
  readonly #tabs = new Map<string, Map<string, TabNodes>>();
  readonly #workspaces = new Map<string, WorkspaceNodes>();
  readonly #workspacePickerRows = new Map<string, WorkspacePickerRowNodes>();
  readonly #workspaceSets = new Map<string, WorkspaceSetNodes>();
  readonly #workspaceMenus = new Map<string, WorkspaceMenuNodes>();
  readonly #terminalMenus = new Map<string, WorkspaceMenuNodes>();
  readonly #workspaceValues = new Map<string, Workspace>();
  readonly #entries = new Map<string, TerminalEntry>();
  #actionCancel: (() => void) | undefined;
  #actionRunning = false;
  #filterValue = "";
  #lastRender: HomeViewRender | undefined;
  #openMenuKey: string | undefined;
  #pickerReturnFocus: HTMLElement | undefined;
  #pickerScroll = 0;
  #pickerWorkspaceID: string | undefined;
  #returnFocus: HTMLElement | undefined;
  #workspaceHeadingSequence = 0;
  #workspaceSetSequence = 0;

  constructor(app: HTMLElement, actions: HomeViewActions) {
    this.#app = app;
    this.#actions = actions;
    this.#document = app.ownerDocument;

    this.#header = element(this.#document, "header", "masthead");
    const heading = element(this.#document, "div");
    this.#headerHeading = element(this.#document, "h1", undefined, "Home");
    this.#herdrVersion = element(this.#document, "p", "herdr-version");
    this.#accessNotice = element(this.#document, "p", "quiet", "Sign-in is off.");
    heading.append(
      element(this.#document, "p", "eyebrow", "Shepherdr"),
      this.#herdrVersion,
      this.#headerHeading,
      this.#accessNotice,
    );
    this.#connectionPanel = element(this.#document, "section", "state-panel home-connection");
    this.#connectionPanel.setAttribute("role", "status");
    const connectionIndicator = element(this.#document, "span", "connection-indicator");
    this.#connectionDot = element(this.#document, "span", "connection-dot connection-dot-reconnecting");
    this.#connectionDot.setAttribute("aria-hidden", "true");
    this.#connectionHeading = element(this.#document, "span", "connection-label");
    connectionIndicator.append(this.#connectionDot, this.#connectionHeading);
    this.#notificationsAction = settingsAction(this.#document, () => actions.onNotifications?.(), "home-notifications");
    this.#connectionPanel.append(connectionIndicator, this.#notificationsAction);
    const mastheadActions = element(this.#document, "div", "masthead-actions");
    mastheadActions.append(this.#connectionPanel);
    this.#header.append(heading, mastheadActions);

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
      const home = this.#lastRender?.state.home ?? { blocked_count: 0, working_count: 0, workspaces: [] };
      const sets = workspaceSets(home);
      const expand = sets.some((workspace) => !this.#expandedSets.get(workspace.id));
      for (const workspace of sets) {
        this.#expandedSets.set(workspace.id, expand);
      }
      if (this.#lastRender) this.render(this.#lastRender);
    });
    this.#expandAction.className = "workspace-expand-action";

    this.#filterLabel = element(this.#document, "label", "home-filter");
    const filterIcon = element(this.#document, "span", "home-filter-icon");
    filterIcon.setAttribute("aria-hidden", "true");
    this.#filterInput = element(this.#document, "input");
    this.#filterInput.type = "search";
    this.#filterInput.placeholder = "Filter workspaces and terminals";
    this.#filterInput.setAttribute("aria-label", "Filter workspaces and terminals");
    this.#filterInput.addEventListener("input", () => {
      this.#filterValue = this.#filterInput.value;
      if (this.#lastRender) this.render(this.#lastRender);
    });
    this.#filterLabel.append(filterIcon, this.#filterInput);
    this.#homeTools = element(this.#document, "section", "home-tools");
    this.#homeTools.setAttribute("aria-label", "Home controls");
    this.#newSpaceAction = this.#button("New space", () => this.#openNewSpace());
    this.#newSpaceAction.className = "new-space-action";
    this.#homeTools.append(this.#filterLabel, this.#expandAction, this.#newSpaceAction);

    this.#noMatches = element(this.#document, "section", "state-panel home-no-matches");
    this.#noMatches.append(
      element(this.#document, "strong", undefined, "No matches"),
      element(this.#document, "p", undefined, "Try another filter."),
    );

    const pickerLayer = element(this.#document, "div", "terminal-picker-layer");
    pickerLayer.hidden = true;
    const pickerPanel = element(this.#document, "section", "terminal-picker-panel");
    pickerPanel.setAttribute("role", "dialog");
    pickerPanel.setAttribute("aria-modal", "true");
    pickerPanel.tabIndex = -1;
    const pickerHeader = element(this.#document, "header", "terminal-picker-header");
    const pickerHeading = element(this.#document, "div", "terminal-picker-heading");
    const pickerTitle = element(this.#document, "h2", "terminal-picker-title");
    pickerTitle.id = "terminal-picker-title";
    const pickerCount = element(this.#document, "p", "terminal-picker-count");
    pickerHeading.append(pickerTitle, pickerCount);
    const pickerClose = this.#button("×", () => this.#closeTerminalPicker());
    pickerClose.className = "terminal-picker-close";
    pickerClose.setAttribute("aria-label", "Close terminal picker");
    pickerHeader.append(pickerHeading, pickerClose);
    const pickerBody = element(this.#document, "div", "terminal-picker-body");
    pickerPanel.setAttribute("aria-labelledby", pickerTitle.id);
    pickerPanel.append(pickerHeader, pickerBody);
    pickerLayer.append(pickerPanel);
    pickerLayer.addEventListener("click", (event) => {
      if (event.target === pickerLayer) this.#closeTerminalPicker();
    });
    pickerLayer.addEventListener("keydown", (event) => this.#handlePickerKey(event));
    this.#picker = {
      body: pickerBody,
      close: pickerClose,
      count: pickerCount,
      layer: pickerLayer,
      panel: pickerPanel,
      title: pickerTitle,
    };

    this.#actionLayer = element(this.#document, "div", "home-action-layer");
    this.#actionLayer.hidden = true;
    this.#actionPanel = element(this.#document, "section", "home-action-panel");
    this.#actionPanel.setAttribute("role", "dialog");
    this.#actionPanel.setAttribute("aria-modal", "true");
    this.#actionPanel.tabIndex = -1;
    this.#actionLayer.append(this.#actionPanel);
    this.#actionLayer.addEventListener("keydown", (event) => this.#handleDialogKey(event));
    this.#document.addEventListener("click", (event) => {
      const target = event.target;
      if (target instanceof this.#document.defaultView!.Element && !target.closest(".workspace-menu")) this.#closeMenu();
    });
  }

  render(model: HomeViewRender): void {
    this.#lastRender = model.restore ? { ...model, restore: undefined } : model;
    const view = this.#document.defaultView;
    const oldScroll = model.restore?.scroll ?? view?.scrollY ?? 0;
    setClass(this.#app, "home");
    setText(this.#headerHeading, model.mode === "blocked" ? "Blocked" : "Home");
    setHidden(this.#headerHeading, model.mode === "all");
    setHidden(this.#accessNotice, !model.signInOff);
    const live = model.reachability === "current" && model.state.connection === "live" && !model.state.last_known;
    setText(this.#herdrVersion, live && model.state.herdr_version !== "" ? `Herdr ${model.state.herdr_version}` : "");

    const desired: Node[] = [this.#header];
    const connection = this.#connectionCopy(model);
    this.#updateConnection(connection);

    const filterActive = model.mode === "all" && this.#filterValue.trim() !== "";
    const visibleHome = filterActive ? this.#filteredHome(model.state.home, this.#filterValue) : model.state.home;
    const allWorkspaceValues = allWorkspaces(visibleHome);
    const usedWorkspaces = new Set(allWorkspaceValues.map((workspace) => workspace.id));
    const usedTabs: UsedTabs = new Map();
    const usedSets = new Set<string>();
    const completePaneIDs = new Set(allTerminals(model.state.home).map(({ terminal }) => terminal.pane_id));
    const managementAvailable = model.actionsAvailable && !this.#actionRunning;
    this.#workspaceValues.clear();
    for (const workspace of allWorkspaces(model.state.home)) this.#workspaceValues.set(workspace.id, workspace);

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
      if (live && total === 0 && allWorkspaceValues.length === 0) desired.push(this.#empty);

      if (model.mode === "blocked") {
        for (const workspace of allWorkspaceValues) {
          const section = this.#renderWorkspace(workspace, true, model.actionsAvailable, false);
          if (section) desired.push(section);
        }
      } else {
        const sets = workspaceSets(visibleHome);
        for (const workspace of sets) {
          if (!this.#expandedSets.has(workspace.id)) {
            const counts = workspace.group_agent_counts ?? {};
            this.#expandedSets.set(workspace.id, (counts.working ?? 0) > 0 || (counts.blocked ?? 0) > 0);
          }
        }
        setHidden(this.#expandAction, filterActive || sets.length === 0);
        reconcileChildren(this.#homeTools, [
          this.#filterLabel,
          this.#expandAction,
          ...(managementAvailable ? [this.#newSpaceAction] : []),
        ]);
        desired.push(this.#homeTools);
        if (sets.length > 0 && !filterActive) {
          const expand = sets.some((workspace) => !this.#expandedSets.get(workspace.id));
          setText(this.#expandAction, expand ? "Expand all" : "Collapse all");
        }
        if (filterActive && allWorkspaceValues.length === 0) desired.push(this.#noMatches);
        for (const workspace of visibleHome.workspaces) {
          if ((workspace.worktrees?.length ?? 0) > 0) {
            usedSets.add(workspace.id);
            desired.push(this.#renderWorkspaceSet(
              workspace,
              model.actionsAvailable,
              managementAvailable,
              filterActive,
            ));
            continue;
          }
          const section = this.#renderWorkspace(
            workspace,
            false,
            model.actionsAvailable,
            managementAvailable,
          );
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

    const automaticallyClosedPicker = this.#updateTerminalPicker(model.actionsAvailable, managementAvailable, usedTabs);
    if (this.#pickerWorkspaceID) usedWorkspaces.add(this.#pickerWorkspaceID);
    desired.push(this.#picker.layer, this.#actionLayer);

    reconcileChildren(this.#app, desired);
    this.#prune(usedWorkspaces, usedTabs, usedSets, completePaneIDs);

    view?.scrollTo(0, oldScroll);
    if (model.restore?.anchorTop !== undefined && model.restore.pane) {
      const row = this.#rows.get(model.restore.pane)?.row;
      if (row) view?.scrollTo(0, (view?.scrollY ?? oldScroll) + row.getBoundingClientRect().top - model.restore.anchorTop);
    } else if (model.restore?.anchorTop !== undefined && model.restore.workspace) {
      const row = this.#workspacePickerRows.get(model.restore.workspace)?.row;
      if (row) view?.scrollTo(0, (view?.scrollY ?? oldScroll) + row.getBoundingClientRect().top - model.restore.anchorTop);
    }
    if (model.restore?.focusPane) {
      this.#rows.get(model.restore.focusPane)?.row.focus({ preventScroll: true });
    } else if (model.restore?.focusWorkspace) {
      this.#workspacePickerRows.get(model.restore.focusWorkspace)?.row.focus({ preventScroll: true });
    }
    if (automaticallyClosedPicker) this.#restoreAutomaticallyClosedPicker(automaticallyClosedPicker);
  }

  row(paneID: string): HTMLElement | undefined {
    return this.#rows.get(paneID)?.row;
  }

  workspaceRow(workspaceID: string): HTMLElement | undefined {
    return this.#workspacePickerRows.get(workspaceID)?.row;
  }

  #button(text: string, action: () => void): HTMLButtonElement {
    const node = element(this.#document, "button", undefined, text);
    node.type = "button";
    node.addEventListener("click", action);
    return node;
  }

  #connectionCopy(model: HomeViewRender): ConnectionCopy {
    if (model.state.connection === "not_running") {
      return { heading: "Herdr is not running", tone: "offline" };
    }
    if (model.state.connection === "incompatible") {
      return { heading: "Cannot use this Herdr", tone: "offline" };
    }
    if (model.reachability === "offline") {
      return { heading: "Offline", tone: "offline" };
    }
    if (model.reachability === "reconnecting" || model.state.connection === "reconnecting") {
      return { heading: "Reconnecting", tone: "reconnecting" };
    }
    return { heading: "Live", tone: "live" };
  }

  #updateConnection(copy: ConnectionCopy): void {
    setText(this.#connectionHeading, copy.heading);
    setClass(this.#connectionDot, `connection-dot connection-dot-${copy.tone}`);
  }

  #renderWorkspace(
    workspace: Workspace,
    blockedOnly: boolean,
    actionsAvailable: boolean,
    managementAvailable: boolean,
    hideTitle = false,
    includeWorkspaceActions = true,
    summaryCounts?: AgentCounts,
  ): HTMLElement | undefined {
    const tabs = visibleTabs(workspace, blockedOnly);
    if (blockedOnly && tabs.length === 0) return undefined;
    const nodes = this.#workspace(workspace);
    this.#updateWorkspace(
      nodes,
      workspace,
      tabs,
      actionsAvailable,
      managementAvailable,
      hideTitle,
      includeWorkspaceActions,
      summaryCounts,
    );
    return nodes.section;
  }

  #renderWorkspaceSet(
    workspace: Workspace,
    actionsAvailable: boolean,
    managementAvailable: boolean,
    forceExpanded = false,
  ): HTMLElement {
    const nodes = this.#workspaceSet(workspace);
    const expanded = forceExpanded || (this.#expandedSets.get(workspace.id) ?? false);
    setAttribute(nodes.disclosure, "aria-expanded", String(expanded));
    setAttribute(nodes.disclosure, "aria-label", `${expanded ? "Collapse" : "Expand"} ${workspace.label} worktrees`);
    setText(nodes.marker, expanded ? "⌄" : "›");
    setText(nodes.title, workspace.label);
    this.#updateGroupSummary(nodes, workspace.group_agent_counts ?? {});
    setHidden(nodes.body, !expanded);

    const parentTerminalCount = terminalCount(workspace);
    const parent = parentTerminalCount > 0
      ? this.#renderWorkspace(
          workspace,
          false,
          actionsAvailable,
          managementAvailable,
          true,
          true,
          parentTerminalCount > 1 ? workspace.group_agent_counts : undefined,
        )
      : undefined;
    if (parentTerminalCount === 1 && parent) {
      const parentMain = parent.querySelector<HTMLElement>(".terminal-main");
      const parentMeta = parentMain?.querySelector<HTMLElement>(".terminal-meta");
      if (parentMain && (nodes.summary.parentElement !== parentMain || nodes.summary.nextSibling !== parentMeta)) {
        parentMain.insertBefore(nodes.summary, parentMeta ?? null);
      }
      const parentRow = parent.querySelector<HTMLElement>(".terminal-row");
      const summaryLabel = this.#groupSummaryLabel(workspace.group_agent_counts ?? {});
      const rowLabel = parentRow?.getAttribute("aria-label");
      if (parentRow && rowLabel && summaryLabel) {
        setAttribute(parentRow, "aria-label", `${rowLabel}, workspace totals: ${summaryLabel}`);
      }
      reconcileChildren(nodes.header, [nodes.disclosure, parent]);
    } else if (parentTerminalCount > 1 && parent) {
      nodes.summary.remove();
      reconcileChildren(nodes.header, [nodes.disclosure, parent]);
    } else {
      if (nodes.summary.parentElement !== nodes.heading) nodes.heading.append(nodes.summary);
      reconcileChildren(nodes.header, [
        nodes.disclosure,
        this.#workspaceActionRow(workspace, nodes.heading, managementAvailable),
      ]);
    }
    const contents: Node[] = [];
    for (const worktree of workspace.worktrees ?? []) {
      const section = this.#renderWorkspace(
        worktree,
        false,
        actionsAvailable,
        managementAvailable,
      );
      if (section) contents.push(section);
    }
    reconcileChildren(nodes.contents, contents);
    return nodes.section;
  }

  #workspaceSet(workspace: Workspace): WorkspaceSetNodes {
    const existing = this.#workspaceSets.get(workspace.id);
    if (existing) return existing;
    const section = element(this.#document, "section", "workspace-set");
    const header = element(this.#document, "div", "workspace-set-header");
    const body = element(this.#document, "div", "workspace-set-body");
    const disclosure = this.#button("", () => {
      this.#expandedSets.set(workspace.id, !this.#expandedSets.get(workspace.id));
      if (this.#lastRender) this.render(this.#lastRender);
    });
    const marker = element(this.#document, "span", "workspace-set-marker");
    marker.setAttribute("aria-hidden", "true");
    disclosure.className = "workspace-set-disclosure";
    const heading = element(this.#document, "span", "workspace-set-heading");
    const title = element(this.#document, "span", "workspace-set-title");
    const summary = element(this.#document, "span", "workspace-set-summary");
    heading.append(title, summary);
    disclosure.append(marker);
    const contents = element(this.#document, "div", "workspace-set-contents");
    body.id = `workspace-set-contents-${++this.#workspaceSetSequence}`;
    disclosure.setAttribute("aria-controls", body.id);
    body.append(contents);
    header.append(disclosure, heading);
    section.append(header, body);
    const summaryStatuses = new Map<keyof AgentCounts, HTMLElement>();
    const nodes = { body, contents, disclosure, header, heading, marker, section, summary, summaryStatuses, title };
    this.#workspaceSets.set(workspace.id, nodes);
    return nodes;
  }

  #updateGroupSummary(nodes: WorkspaceSetNodes, counts: AgentCounts): void {
    this.#updateSummary(nodes.summary, nodes.summaryStatuses, counts);
  }

  #updateSummary(
    summary: HTMLElement,
    summaryStatuses: Map<keyof AgentCounts, HTMLElement>,
    counts: AgentCounts,
    leading?: HTMLElement,
  ): void {
    const statuses = ["working", "blocked", "idle", "done", "unknown"] as const;
    const parts: Node[] = leading ? [leading] : [];
    for (const status of statuses) {
      const count = counts[status] ?? 0;
      if (count > 0) {
        let badge = summaryStatuses.get(status);
        if (!badge) {
          badge = element(this.#document, "span", `workspace-summary-status workspace-summary-${status}`);
          summaryStatuses.set(status, badge);
        }
        setText(badge, `${count} ${status}`);
        parts.push(badge);
      }
    }
    reconcileChildren(summary, parts);
  }

  #groupSummaryLabel(counts: AgentCounts): string {
    const statuses = ["working", "blocked", "idle", "done", "unknown"] as const;
    return statuses
      .filter((status) => (counts[status] ?? 0) > 0)
      .map((status) => `${counts[status]} ${status}`)
      .join(", ");
  }

  #workspace(workspace: Workspace): WorkspaceNodes {
    const existing = this.#workspaces.get(workspace.id);
    if (existing) return existing;
    const section = element(this.#document, "section", "workspace");
    const heading = element(this.#document, "h2");
    const headingID = `workspace-heading-${++this.#workspaceHeadingSequence}`;
    heading.id = headingID;
    section.setAttribute("aria-labelledby", headingID);
    section.append(heading);
    const nodes = { heading, section };
    this.#workspaces.set(workspace.id, nodes);
    return nodes;
  }

  #updateWorkspace(
    nodes: WorkspaceNodes,
    workspace: Workspace,
    tabs: ReturnType<typeof visibleTabs>,
    actionsAvailable: boolean,
    managementAvailable: boolean,
    hideTitle: boolean,
    includeWorkspaceActions: boolean,
    summaryCounts?: AgentCounts,
  ): void {
    const workspaceManagementAvailable = managementAvailable && includeWorkspaceActions;
    const fullWorkspace = this.#workspaceValues.get(workspace.id) ?? workspace;
    const fullTerminalCount = terminalCount(fullWorkspace);
    if (fullTerminalCount === 0) {
      setClass(
        nodes.section,
        workspaceManagementAvailable && workspace.actions.length > 0 ? "workspace workspace-heading-menu" : "workspace",
      );
      setClass(nodes.heading, hideTitle ? "visually-hidden" : "workspace-title");
      setText(nodes.heading, workspace.label);
      if (workspaceManagementAvailable && workspace.actions.length > 0) {
        const menu = this.#workspaceMenu(workspace.id);
        this.#updateWorkspaceMenu(menu, workspace);
        setClass(menu.menuRoot, "workspace-menu workspace-menu-heading");
        reconcileChildren(nodes.section, [nodes.heading, menu.menuRoot]);
      } else {
        reconcileChildren(nodes.section, [nodes.heading]);
      }
      return;
    }

    setClass(nodes.section, "workspace");
    if (fullTerminalCount === 1) {
      const singleGroup = tabs[0] ?? visibleTabs(fullWorkspace, false)[0];
      const terminal = singleGroup.terminals[0];
      const entry = { workspace: fullWorkspace, tab: singleGroup.tab, terminal };
      setClass(nodes.heading, "visually-hidden");
      setText(nodes.heading, terminal.title);
      reconcileChildren(nodes.section, [
        nodes.heading,
        this.#terminalActionRow(
          entry,
          this.#terminalRow(entry, false, actionsAvailable).row,
          managementAvailable,
          includeWorkspaceActions,
        ),
      ]);
      return;
    }

    setClass(nodes.section, "workspace workspace-multiple-terminals");
    setClass(nodes.heading, "visually-hidden");
    setText(nodes.heading, workspace.label);
    const pickerRow = this.#workspacePickerRow(
      fullWorkspace,
      actionsAvailable,
      summaryCounts ?? fullWorkspace.agent_counts ?? {},
    );
    reconcileChildren(nodes.section, [
      nodes.heading,
      this.#workspaceActionRow(workspace, pickerRow, workspaceManagementAvailable),
    ]);
  }

  #workspacePickerRow(
    workspace: Workspace,
    actionsAvailable: boolean,
    counts: AgentCounts,
  ): HTMLElement {
    let nodes = this.#workspacePickerRows.get(workspace.id);
    const rowIsAction = nodes?.row.localName === "button";
    if (!nodes || rowIsAction !== actionsAvailable) {
      let row: HTMLElement;
      if (actionsAvailable) {
        const button = element(this.#document, "button");
        button.type = "button";
        button.addEventListener("click", () => this.#openTerminalPicker(workspace.id, button));
        button.addEventListener("focus", () => this.#actions.onFocusWorkspace?.(workspace.id));
        row = button;
      } else {
        row = element(this.#document, "div");
      }
      const main = element(this.#document, "span", "terminal-main");
      const name = element(this.#document, "span", "terminal-name");
      const summary = element(this.#document, "span", "workspace-set-summary workspace-picker-summary");
      main.append(name, summary);
      const control = element(this.#document, "span", "terminal-picker-control");
      const badge = element(this.#document, "span", "terminal-count-badge");
      badge.setAttribute("aria-hidden", "true");
      const chevron = element(this.#document, "span", "terminal-chevron");
      chevron.setAttribute("aria-hidden", "true");
      control.append(badge, chevron);
      row.append(main, control);
      row.dataset.workspaceKey = workspace.id;
      nodes = {
        badge,
        chevron,
        main,
        name,
        row,
        summary,
        summaryStatuses: new Map<keyof AgentCounts, HTMLElement>(),
      };
      this.#workspacePickerRows.set(workspace.id, nodes);
    }

    const count = terminalCount(workspace);
    setClass(nodes.row, actionsAvailable
      ? "terminal-row workspace-terminal-trigger"
      : "terminal-row workspace-terminal-trigger terminal-row-stale");
    if (actionsAvailable) {
      const totals = this.#groupSummaryLabel(counts);
      setAttribute(
        nodes.row,
        "aria-label",
        `Choose a terminal in ${workspace.label}, ${count} terminals${totals ? `, ${totals}` : ""}`,
      );
    }
    setText(nodes.name, workspace.label);
    setText(nodes.badge, String(count));
    this.#updateSummary(nodes.summary, nodes.summaryStatuses, counts);
    return nodes.row;
  }

  #openTerminalPicker(workspaceID: string, returnFocus: HTMLElement): void {
    const workspace = this.#workspaceValues.get(workspaceID);
    if (!workspace || terminalCount(workspace) <= 1 || !this.#lastRender) return;
    this.#closeMenu();
    this.#pickerWorkspaceID = workspaceID;
    this.#pickerReturnFocus = returnFocus;
    this.#pickerScroll = this.#document.defaultView?.scrollY ?? 0;
    this.#picker.body.scrollTop = 0;
    const usedTabs: UsedTabs = new Map();
    this.#updateTerminalPicker(
      this.#lastRender.actionsAvailable,
      this.#lastRender.actionsAvailable && !this.#actionRunning,
      usedTabs,
    );
    this.#picker.close.focus({ preventScroll: true });
  }

  #updateTerminalPicker(
    actionsAvailable: boolean,
    managementAvailable: boolean,
    usedTabs: UsedTabs,
  ): AutomaticallyClosedPicker | undefined {
    if (!this.#pickerWorkspaceID) {
      setHidden(this.#picker.layer, true);
      return;
    }
    const workspace = this.#workspaceValues.get(this.#pickerWorkspaceID);
    if (!workspace || terminalCount(workspace) <= 1) {
      const remaining = workspace ? visibleTabs(workspace, false)[0]?.terminals[0] : undefined;
      const closed = {
        paneID: remaining?.pane_id,
        returnFocus: this.#pickerReturnFocus,
        scroll: this.#pickerScroll,
      };
      this.#pickerWorkspaceID = undefined;
      this.#pickerReturnFocus = undefined;
      reconcileChildren(this.#picker.body, []);
      setHidden(this.#picker.layer, true);
      return closed;
    }

    setText(this.#picker.title, workspace.label);
    setText(this.#picker.count, `${terminalCount(workspace)} terminals`);
    const tabs = visibleTabs(workspace, false);
    const tabsShown = showTabHeadings(workspace);
    const children: Node[] = [];
    let workspaceTabs = usedTabs.get(workspace.id);
    if (!workspaceTabs) {
      workspaceTabs = new Set();
      usedTabs.set(workspace.id, workspaceTabs);
    }
    for (const group of tabs) {
      workspaceTabs.add(group.tab.id);
      const tab = this.#tab(workspace.id, group.tab.id);
      this.#updateTab(
        tab,
        workspace,
        group.tab,
        group.terminals,
        tabsShown,
        actionsAvailable,
        managementAvailable,
      );
      children.push(tab.group);
    }
    reconcileChildren(this.#picker.body, children);
    setHidden(this.#picker.layer, false);
  }

  #closeTerminalPicker(): void {
    if (!this.#pickerWorkspaceID || this.#actionRunning) return;
    this.#closeMenu();
    const returnFocus = this.#pickerReturnFocus;
    this.#pickerWorkspaceID = undefined;
    setHidden(this.#picker.layer, true);
    reconcileChildren(this.#picker.body, []);
    this.#document.defaultView?.scrollTo(0, this.#pickerScroll);
    this.#pickerReturnFocus = undefined;
    this.#backgroundFocusTarget(returnFocus)?.focus({ preventScroll: true });
  }

  #restoreAutomaticallyClosedPicker(closed: AutomaticallyClosedPicker): void {
    this.#document.defaultView?.scrollTo(0, closed.scroll);
    const target = this.#backgroundFocusTarget(closed.returnFocus, closed.paneID);
    if (!target) return;
    if (!this.#actionLayer.hidden) {
      if (!this.#returnFocus?.isConnected || this.#picker.layer.contains(this.#returnFocus)) {
        this.#returnFocus = target;
      }
      return;
    }
    target.focus({ preventScroll: true });
  }

  #backgroundFocusTarget(preferred?: HTMLElement, paneID?: string): HTMLElement | undefined {
    if (preferred?.isConnected &&
      !this.#picker.layer.contains(preferred) &&
      !this.#actionLayer.contains(preferred)) return preferred;
    const row = paneID ? this.#rows.get(paneID)?.row : undefined;
    if (row?.isConnected && row.localName === "button") return row;
    if (this.#filterInput.isConnected) return this.#filterInput;
    if (this.#attentionShowAll.isConnected && !this.#attentionShowAll.hidden) return this.#attentionShowAll;
    return Array.from(this.#app.querySelectorAll<HTMLElement>("button, input")).find((node) =>
      node.closest("[hidden]") === null &&
      !this.#picker.layer.contains(node) &&
      !this.#actionLayer.contains(node),
    );
  }

  #handlePickerKey(event: KeyboardEvent): void {
    if (event.defaultPrevented || this.#actionLayer.hidden === false) return;
    if (event.key === "Escape") {
      event.preventDefault();
      this.#closeTerminalPicker();
      return;
    }
    if (event.key !== "Tab") return;
    const focusable = Array.from(this.#picker.panel.querySelectorAll<HTMLElement>("button"))
      .filter((node) => !node.hasAttribute("disabled") && node.closest("[hidden]") === null);
    if (focusable.length === 0) {
      event.preventDefault();
      return;
    }
    const current = focusable.indexOf(this.#document.activeElement as HTMLElement);
    const next = event.shiftKey
      ? (current <= 0 ? focusable.length - 1 : current - 1)
      : (current < 0 || current === focusable.length - 1 ? 0 : current + 1);
    event.preventDefault();
    focusable[next].focus({ preventScroll: true });
  }

  #tab(workspaceID: string, tabID: string): TabNodes {
    let workspaceTabs = this.#tabs.get(workspaceID);
    if (!workspaceTabs) {
      workspaceTabs = new Map();
      this.#tabs.set(workspaceID, workspaceTabs);
    }
    const existing = workspaceTabs.get(tabID);
    if (existing) return existing;
    const group = element(this.#document, "div", "tab-group");
    const heading = element(this.#document, "div", "tab-heading");
    const list = element(this.#document, "ul", "terminal-list");
    const title = element(this.#document, "h3");
    const current = element(this.#document, "span", "current-tab", "current");
    const nodes = { current, group, heading, list, title };
    workspaceTabs.set(tabID, nodes);
    return nodes;
  }

  #updateTab(
    nodes: TabNodes,
    workspace: Workspace,
    tab: Tab,
    terminals: Tab["terminals"],
    tabsShown: boolean,
    actionsAvailable: boolean,
    showManagement: boolean,
  ): void {
    setText(nodes.title, tab.label);
    const headingChildren: Node[] = [nodes.title];
    if (tab.current) headingChildren.push(nodes.current);
    reconcileChildren(nodes.heading, headingChildren);
    const items = terminals.map((terminal) => {
      const entry = { workspace, tab, terminal };
      const item = this.#item(terminal.pane_id);
      const row = this.#terminalRow(entry, tabsShown, actionsAvailable).row;
      reconcileChildren(item, [this.#terminalActionRow(entry, row, showManagement, false)]);
      return item;
    });
    reconcileChildren(nodes.list, items);
    reconcileChildren(nodes.group, tabsShown ? [nodes.heading, nodes.list] : [nodes.list]);
  }

  #workspaceActionRow(workspace: Workspace, primary: HTMLElement, managementAvailable: boolean): HTMLElement {
    if (!managementAvailable || workspace.actions.length === 0) return primary;
    const nodes = this.#workspaceMenu(workspace.id);
    this.#updateWorkspaceMenu(nodes, workspace);
    setClass(nodes.menuRoot, "workspace-menu");
    reconcileChildren(nodes.container, [primary, nodes.menuRoot]);
    return nodes.container;
  }

  #workspaceMenu(workspaceID: string): WorkspaceMenuNodes {
    const existing = this.#workspaceMenus.get(workspaceID);
    if (existing) return existing;
    const nodes = this.#actionMenu(`workspace:${workspaceID}`);
    this.#workspaceMenus.set(workspaceID, nodes);
    return nodes;
  }

  #terminalActionRow(
    entry: TerminalEntry,
    primary: HTMLElement,
    managementAvailable: boolean,
    includeWorkspace: boolean,
  ): HTMLElement {
    if (!managementAvailable ||
      (entry.terminal.actions.length === 0 && (!includeWorkspace || entry.workspace.actions.length === 0))) return primary;
    const nodes = this.#terminalMenu(entry.terminal.pane_id);
    this.#updateTerminalMenu(nodes, entry, includeWorkspace);
    setClass(nodes.menuRoot, "workspace-menu terminal-menu");
    reconcileChildren(nodes.container, [primary, nodes.menuRoot]);
    return nodes.container;
  }

  #terminalMenu(paneID: string): WorkspaceMenuNodes {
    const existing = this.#terminalMenus.get(paneID);
    if (existing) return existing;
    const nodes = this.#actionMenu(`terminal:${paneID}`);
    this.#terminalMenus.set(paneID, nodes);
    return nodes;
  }

  #actionMenu(key: string): WorkspaceMenuNodes {
    const container = element(this.#document, "div", "workspace-action-row");
    const menuRoot = element(this.#document, "div", "workspace-menu");
    const trigger = this.#button("⋮", () => this.#toggleMenu(key));
    trigger.className = "workspace-menu-trigger";
    trigger.setAttribute("aria-haspopup", "menu");
    trigger.setAttribute("aria-expanded", "false");
    const menu = element(this.#document, "div", "workspace-menu-popover");
    menu.setAttribute("role", "menu");
    menu.hidden = true;
    menu.addEventListener("keydown", (event) => this.#handleMenuKey(event, key));
    menuRoot.append(trigger, menu);
    const nodes = { container, items: new Map(), menu, menuRoot, trigger };
    return nodes;
  }

  #updateWorkspaceMenu(nodes: WorkspaceMenuNodes, workspace: Workspace): void {
    setAttribute(nodes.trigger, "aria-label", `Actions for ${workspace.label}`);
    const order: WorkspaceAction[] = ["create_worktree", "close_workspace", "close_group", "delete_checkout"];
    const labels: Record<WorkspaceAction, string> = {
      create_worktree: "New worktree",
      close_workspace: "Close workspace",
      close_group: "Close workspace",
      delete_checkout: "Delete checkout",
    };
    const items: Node[] = [];
    for (const action of order) {
      if (!workspace.actions.includes(action)) continue;
      let item = nodes.items.get(action);
      if (!item) {
        item = this.#button(labels[action], () => this.#chooseWorkspaceAction(workspace.id, action, nodes.trigger));
        item.setAttribute("role", "menuitem");
        nodes.items.set(action, item);
      }
      items.push(item);
    }
    reconcileChildren(nodes.menu, items);
    const open = this.#openMenuKey === `workspace:${workspace.id}`;
    setHidden(nodes.menu, !open);
    setAttribute(nodes.trigger, "aria-expanded", String(open));
  }

  #updateTerminalMenu(nodes: WorkspaceMenuNodes, entry: TerminalEntry, includeWorkspace: boolean): void {
    setAttribute(nodes.trigger, "aria-label", `Actions for ${includeWorkspace ? entry.workspace.label : entry.terminal.title}`);
    const items: Node[] = [];
    const terminalOrder: TerminalAction[] = ["split_terminal", "rename_terminal", "close_terminal"];
    const terminalLabels: Record<TerminalAction, string> = {
      split_terminal: "New terminal",
      rename_terminal: "Rename terminal",
      close_terminal: "Close terminal",
    };
    for (const action of terminalOrder) {
      if (!entry.terminal.actions.includes(action)) continue;
      const key = `terminal:${action}`;
      let item = nodes.items.get(key);
      if (!item) {
        item = this.#button(terminalLabels[action], () => this.#chooseTerminalAction(entry.terminal.pane_id, action, nodes.trigger));
        item.setAttribute("role", "menuitem");
        nodes.items.set(key, item);
      }
      items.push(item);
    }
    if (includeWorkspace) {
      const workspaceOrder: WorkspaceAction[] = ["create_worktree", "close_workspace", "close_group", "delete_checkout"];
      const labels: Record<WorkspaceAction, string> = {
        create_worktree: "New worktree",
        close_workspace: "Close workspace",
        close_group: "Close workspace",
        delete_checkout: "Delete checkout",
      };
      for (const action of workspaceOrder) {
        if (!entry.workspace.actions.includes(action)) continue;
        const key = `workspace:${action}`;
        let item = nodes.items.get(key);
        if (!item) {
          item = this.#button(labels[action], () => this.#chooseWorkspaceAction(entry.workspace.id, action, nodes.trigger));
          item.setAttribute("role", "menuitem");
          nodes.items.set(key, item);
        }
        items.push(item);
      }
    }
    reconcileChildren(nodes.menu, items);
    const open = this.#openMenuKey === `terminal:${entry.terminal.pane_id}`;
    setHidden(nodes.menu, !open);
    setAttribute(nodes.trigger, "aria-expanded", String(open));
  }

  #toggleMenu(key: string): void {
    this.#openMenuKey = this.#openMenuKey === key ? undefined : key;
    if (this.#lastRender) this.render(this.#lastRender);
    if (this.#openMenuKey === key) {
      const first = this.#menuForKey(key)?.menu.querySelector<HTMLButtonElement>("button");
      first?.focus({ preventScroll: true });
    }
  }

  #closeMenu(returnFocus = false): void {
    const key = this.#openMenuKey;
    if (!key) return;
    this.#openMenuKey = undefined;
    const nodes = this.#menuForKey(key);
    if (nodes) {
      nodes.menu.hidden = true;
      setAttribute(nodes.trigger, "aria-expanded", "false");
      if (returnFocus) nodes.trigger.focus({ preventScroll: true });
    }
  }

  #menuForKey(key: string): WorkspaceMenuNodes | undefined {
    const separator = key.indexOf(":");
    const kind = key.slice(0, separator);
    const id = key.slice(separator + 1);
    return kind === "workspace" ? this.#workspaceMenus.get(id) : this.#terminalMenus.get(id);
  }

  #handleMenuKey(event: KeyboardEvent, key: string): void {
    const nodes = this.#menuForKey(key);
    if (!nodes) return;
    if (event.key === "Escape") {
      event.preventDefault();
      this.#closeMenu(true);
      return;
    }
    const items = Array.from(nodes.menu.querySelectorAll<HTMLButtonElement>("button"));
    const current = items.indexOf(this.#document.activeElement as HTMLButtonElement);
    let next: number | undefined;
    if (event.key === "ArrowDown") next = current < 0 ? 0 : (current + 1) % items.length;
    if (event.key === "ArrowUp") next = current < 0 ? items.length - 1 : (current - 1 + items.length) % items.length;
    if (event.key === "Home") next = 0;
    if (event.key === "End") next = items.length - 1;
    if (next !== undefined && items[next]) {
      event.preventDefault();
      items[next].focus({ preventScroll: true });
    }
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
    const rowIsAction = nodes?.row.localName === "button";
    if (!nodes || rowIsAction !== actionsAvailable) {
      let row: HTMLElement;
      if (actionsAvailable) {
        const button = element(this.#document, "button");
        button.type = "button";
        button.addEventListener("click", () => {
          const current = this.#entries.get(entry.terminal.pane_id);
          if (current) this.#actions.onOpen(current);
        });
        button.addEventListener("focus", () => this.#actions.onFocusPane(entry.terminal.pane_id));
        button.dataset.paneId = entry.terminal.pane_id;
        row = button;
      } else {
        row = element(this.#document, "div");
      }
      const main = element(this.#document, "span", "terminal-main");
      const name = element(this.#document, "span", "terminal-name");
      const agent = element(this.#document, "span", "terminal-agent");
      const status = element(this.#document, "span", "status");
      const meta = element(this.#document, "span", "terminal-meta");
      const chevron = element(this.#document, "span", "terminal-chevron");
      chevron.setAttribute("aria-hidden", "true");
      meta.append(agent, status);
      main.append(name, meta);
      row.append(main, chevron);
      row.dataset.paneKey = entry.terminal.pane_id;
      nodes = { agent, chevron, main, meta, name, row, status };
      this.#rows.set(entry.terminal.pane_id, nodes);
    }

    setClass(nodes.row, actionsAvailable ? "terminal-row" : "terminal-row terminal-row-stale");
    if (actionsAvailable) setAttribute(nodes.row, "aria-label", accessibleTerminalName(entry, tabsShown));
    setText(nodes.name, entry.terminal.title);
    if (entry.terminal.agent) {
      const secondary = [
        ...(terminalCount(entry.workspace) > 1 || entry.terminal.title !== entry.workspace.label
          ? [entry.workspace.label]
          : []),
        ...(entry.terminal.agent.name !== entry.terminal.title && entry.terminal.agent.name !== entry.workspace.label
          ? [entry.terminal.agent.name]
          : []),
      ].join(" · ");
      setText(nodes.agent, secondary);
      setHidden(nodes.agent, secondary === "");
      setClass(nodes.status, `status status-${entry.terminal.agent.status}`);
      setText(nodes.status, entry.terminal.agent.status);
      setHidden(nodes.status, false);
    } else {
      const workspaceContext = terminalCount(entry.workspace) > 1 || entry.terminal.title !== entry.workspace.label
        ? entry.workspace.label
        : "";
      setText(nodes.agent, workspaceContext);
      setHidden(nodes.agent, workspaceContext === "");
      setClass(nodes.status, "");
      setText(nodes.status, "");
      setHidden(nodes.status, true);
    }
    return nodes;
  }

  #openNewSpace(): void {
    if (this.#actionRunning || !this.#lastRender) return;
    this.#returnFocus = this.#newSpaceAction;
    const copy = element(this.#document, "p", "home-action-copy", "Add a workspace for an existing folder.");
    const form = element(this.#document, "form", "home-action-form");
    const directoryLabel = element(this.#document, "label", "home-action-field");
    directoryLabel.append(element(this.#document, "span", undefined, "Working directory"));
    const directory = element(this.#document, "input");
    directory.name = "working_directory";
    directory.required = true;
    directory.value = "~";
    directory.autocomplete = "off";
    directory.spellcheck = false;
    directoryLabel.append(directory);

    const suggestions = Array.from(new Set(
      this.#lastRender.state.home.workspaces
        .map((workspace) => workspace.checkout_path)
        .filter((path): path is string => path !== undefined),
    ));
    const suggestionRegion = element(this.#document, "div", "checkout-suggestions");
    const updateSuggestions = (): void => {
      const entered = directory.value.toLocaleLowerCase();
      const query = entered === "~" ? "" : entered;
      const matches = suggestions.filter((path) => path.toLocaleLowerCase().includes(query));
      const buttons = matches.map((path) => {
        const suggestion = this.#button(path, () => {
          directory.value = path;
          updateSuggestions();
          directory.focus({ preventScroll: true });
        });
        suggestion.className = "checkout-suggestion";
        return suggestion;
      });
      reconcileChildren(suggestionRegion, buttons);
      setHidden(suggestionRegion, buttons.length === 0);
    };
    directory.addEventListener("input", updateSuggestions);
    updateSuggestions();

    const labelField = element(this.#document, "label", "home-action-field");
    labelField.append(element(this.#document, "span", undefined, "Label (optional)"));
    const label = element(this.#document, "input");
    label.name = "label";
    labelField.append(label);

    const actions = this.#formActions("Create space", () => this.#closeActionLayer());
    form.append(directoryLabel, suggestionRegion, labelField, actions.container);
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      if (!form.reportValidity()) return;
      const request: RunWorkspaceActionRequest = {
        action: "create_space",
        working_directory: directory.value,
        ...(label.value.trim() === "" ? {} : { label: label.value }),
      };
      void this.#submitAction(request);
    });
    this.#showActionLayer("New space", [copy, form], () => this.#closeActionLayer(), directory);
  }

  #openNewWorktree(workspace: Workspace, returnFocus: HTMLElement): void {
    if (this.#actionRunning) return;
    this.#returnFocus = returnFocus;
    const copy = element(this.#document, "p", "home-action-copy", "This adds a workspace in a new folder.");
    const form = element(this.#document, "form", "home-action-form");
    const branchField = element(this.#document, "label", "home-action-field");
    branchField.append(element(this.#document, "span", undefined, "Branch (optional)"));
    const branch = element(this.#document, "input");
    branch.name = "branch";
    branch.autocomplete = "off";
    branch.spellcheck = false;
    branchField.append(branch);
    const help = element(
      this.#document,
      "p",
      "home-action-help",
      "Branch is optional. Leave it blank to let Herdr choose.",
    );
    const actions = this.#formActions("New worktree", () => this.#closeActionLayer());
    form.append(branchField, help, actions.container);
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      const request: RunWorkspaceActionRequest = {
        action: "create_worktree",
        workspace_id: workspace.id,
        ...(branch.value.trim() === "" ? {} : { branch: branch.value }),
      };
      void this.#submitAction(request);
    });
    this.#showActionLayer(`New worktree for ${workspace.label}`, [copy, form], () => this.#closeActionLayer(), branch);
  }

  #formActions(primaryLabel: string, cancel: () => void): { container: HTMLElement; primary: HTMLButtonElement } {
    const container = element(this.#document, "div", "home-action-buttons");
    const primary = element(this.#document, "button", "home-action-primary", primaryLabel);
    primary.type = "submit";
    const cancelButton = this.#button("Cancel", cancel);
    container.append(primary, cancelButton);
    return { container, primary };
  }

  #chooseTerminalAction(paneID: string, action: TerminalAction, returnFocus: HTMLElement): void {
    const entry = this.#entries.get(paneID);
    if (!entry || !entry.terminal.actions.includes(action) || this.#actionRunning) return;
    this.#closeMenu();
    this.#returnFocus = returnFocus;
    if (action === "split_terminal") {
      this.#openSplitTerminal(entry);
    } else if (action === "rename_terminal") {
      this.#openRenameTerminal(entry);
    } else {
      void this.#prepareTerminalClose(this.#terminalTarget(entry));
    }
  }

  #terminalTarget(entry: TerminalEntry): TerminalActionTarget {
    return {
      workspace_id: entry.workspace.id,
      tab_id: entry.tab.id,
      pane_id: entry.terminal.pane_id,
      terminal_id: entry.terminal.terminal_id,
    };
  }

  #openSplitTerminal(entry: TerminalEntry): void {
    const copy = element(
      this.#document,
      "p",
      "home-action-copy",
      `This splits ${entry.terminal.title} and opens the new terminal.`,
    );
    const choices = element(this.#document, "div", "home-action-buttons terminal-split-actions");
    const beside = this.#button("Beside", () => void this.#submitTerminalAction({
      action: "split_terminal",
      ...this.#terminalTarget(entry),
      direction: "right",
    }));
    beside.className = "home-action-primary";
    const below = this.#button("Below", () => void this.#submitTerminalAction({
      action: "split_terminal",
      ...this.#terminalTarget(entry),
      direction: "down",
    }));
    below.className = "home-action-primary";
    const cancel = this.#button("Cancel", () => this.#closeActionLayer());
    choices.append(beside, below, cancel);
    this.#showActionLayer("Split terminal", [copy, choices], () => this.#closeActionLayer(), beside);
  }

  #openRenameTerminal(entry: TerminalEntry): void {
    const form = element(this.#document, "form", "home-action-form");
    const field = element(this.#document, "label", "home-action-field");
    field.append(element(this.#document, "span", undefined, "Name"));
    const name = element(this.#document, "input");
    name.name = "terminal_name";
    name.value = entry.terminal.manual_name ?? "";
    name.autocomplete = "off";
    field.append(name);
    const help = element(this.#document, "p", "home-action-help", "Leave the name blank to use the automatic name.");
    const actions = this.#formActions("Rename terminal", () => this.#closeActionLayer());
    form.append(field, help, actions.container);
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      void this.#submitTerminalAction({
        action: "rename_terminal",
        ...this.#terminalTarget(entry),
        name: name.value,
      });
    });
    this.#showActionLayer(`Rename ${entry.terminal.title}`, [form], () => this.#closeActionLayer(), name);
  }

  async #prepareTerminalClose(target: TerminalActionTarget): Promise<void> {
    if (this.#actionRunning) return;
    this.#actionRunning = true;
    this.#showWorking("Checking terminal");
    this.#renderAgain();
    let response: PreparedTerminalActionResponse | undefined;
    try {
      response = await this.#actions.prepareTerminalAction({ action: "close_terminal", ...target });
    } catch {
      // Preparing performs no mutation, so a failed check is not an unknown action result.
    }
    this.#actionRunning = false;
    this.#renderAgain();
    if (!response) {
      this.#showResult("Could not check this terminal", "Home has not changed. Check the connection, then open the action again.");
      return;
    }
    if (response.outcome === "refused") {
      this.#showRefusal(response);
      return;
    }
    this.#openTerminalCloseConfirmation(response.expected);
  }

  #openTerminalCloseConfirmation(facts: TerminalCloseFacts): void {
    const content: Node[] = [];
    const closesWorkspace = facts.workspace_expected !== undefined;
    if (closesWorkspace) {
      content.push(element(
        this.#document,
        "p",
        "home-action-copy",
        this.#closeConfirmationCopy(facts.workspace_expected!),
      ));
      const warning = this.#interruptionWarning(facts.workspace_expected!);
      if (warning) content.push(warning);
    } else {
      const sentences = ["Closing this terminal stops what is running there."];
      if (facts.agent) sentences.push(`${facts.agent.name} is ${facts.agent.status}.`);
      if (facts.closes_tab) sentences.push(`The ${facts.tab_label} tab will also close.`);
      content.push(element(this.#document, "p", "home-action-copy", sentences.join(" ")));
    }
    const actions = this.#formActions(closesWorkspace ? "Close workspace" : "Close terminal", () => this.#closeActionLayer());
    actions.primary.addEventListener("click", (event) => {
      event.preventDefault();
      void this.#submitTerminalAction({ action: "close_terminal", expected: facts });
    });
    content.push(actions.container);
    this.#showActionLayer(
      closesWorkspace ? `Close ${facts.workspace_label}?` : `Close ${facts.terminal_title}?`,
      content,
      () => this.#closeActionLayer(),
      actions.primary,
    );
  }

  async #submitTerminalAction(request: RunTerminalActionRequest): Promise<void> {
    if (this.#actionRunning) return;
    this.#actionRunning = true;
    const headings: Record<RunTerminalActionRequest["action"], string> = {
      split_terminal: "Splitting terminal",
      rename_terminal: "Renaming terminal",
      close_terminal: request.action === "close_terminal" && request.expected.workspace_expected
        ? "Closing workspace"
        : "Closing terminal",
    };
    this.#showWorking(headings[request.action]);
    this.#renderAgain();
    let response: RunTerminalActionResponse;
    try {
      response = await this.#actions.runTerminalAction(request);
    } catch {
      response = { outcome: "unknown" };
    }
    this.#actionRunning = false;
    this.#renderAgain();
    if (response.outcome === "refused") {
      this.#showRefusal(response);
      return;
    }
    if (response.outcome === "unknown") {
      this.#showResult(
        "Result unknown",
        request.action === "split_terminal"
          ? "Check Home before splitting this terminal again."
          : request.action === "rename_terminal"
            ? "Check Home before renaming this terminal again."
            : "Check Home before trying to close this terminal again.",
      );
      return;
    }
    if (request.action === "split_terminal") {
      if (!response.terminal) {
        this.#showResult("Result unknown", "Check Home before splitting this terminal again.");
        return;
      }
      this.#closeActionLayer();
      this.#actions.onOpenCreated(response.terminal);
      return;
    }
    if (request.action === "rename_terminal") {
      this.#showResult("Terminal renamed", "The new name is ready on Home.");
      return;
    }
    if (request.expected.workspace_expected) {
      this.#showResult("Workspace closed", "Its terminals are closed. The folder and branch remain.");
    } else {
      this.#showResult("Terminal closed", "The terminal is no longer open.");
    }
  }

  #chooseWorkspaceAction(workspaceID: string, action: WorkspaceAction, returnFocus: HTMLElement): void {
    const workspace = this.#workspaceValues.get(workspaceID);
    if (!workspace || !workspace.actions.includes(action) || this.#actionRunning) return;
    this.#closeMenu();
    if (action === "create_worktree") {
      this.#openNewWorktree(workspace, returnFocus);
      return;
    }
    this.#returnFocus = returnFocus;
    void this.#prepareAction({ action, workspace_id: workspace.id });
  }

  async #prepareAction(request: PrepareWorkspaceActionRequest): Promise<void> {
    if (this.#actionRunning) return;
    this.#actionRunning = true;
    this.#showWorking("Checking workspace");
    this.#renderAgain();
    let response: PreparedWorkspaceActionResponse | undefined;
    try {
      response = await this.#actions.prepareWorkspaceAction(request);
    } catch {
      // Preparing performs no mutation, so a failed check is not an unknown action result.
    }
    this.#actionRunning = false;
    this.#renderAgain();
    if (!response) {
      this.#showResult(
        "Could not check this workspace",
        "Home has not changed. Check the connection, then open the action again.",
      );
      return;
    }
    if (response.outcome === "refused") {
      this.#showRefusal(response);
      return;
    }
    this.#openConfirmation(response);
  }

  #openConfirmation(prepared: PreparedWorkspaceAction): void {
    const facts = prepared.expected;
    const content: Node[] = [];
    let heading: string;
    let confirmLabel: string;
    if (prepared.action === "close_group" || prepared.action === "close_workspace") {
      heading = `Close ${facts.workspace_label}?`;
      confirmLabel = "Close workspace";
      content.push(element(
        this.#document,
        "p",
        "home-action-copy",
        this.#closeConfirmationCopy(facts),
      ));
    } else {
      heading = `Delete checkout for ${facts.workspace_label}?`;
      confirmLabel = "Delete checkout";
      content.push(element(
        this.#document,
        "p",
        "home-action-copy",
        `This deletes the checkout and closes this workspace and ${this.#agentTotal(facts.agent_total)}. Its terminals will end, and unsaved work can be lost. The Git branch remains.`,
      ));
      const path = element(this.#document, "code", "checkout-path", facts.checkout_path ?? "");
      const pathLine = element(this.#document, "p", "checkout-path-line");
      pathLine.append(element(this.#document, "span", undefined, "Checkout: "), path);
      content.push(pathLine);
    }
    const warning = this.#interruptionWarning(facts);
    if (warning) content.push(warning);
    const actions = this.#formActions(confirmLabel, () => this.#closeActionLayer());
    actions.primary.addEventListener("click", (event) => {
      event.preventDefault();
      void this.#submitAction({
        action: prepared.action,
        workspace_id: prepared.workspace_id,
        expected: prepared.expected,
      });
    });
    content.push(actions.container);
    this.#showActionLayer(heading, content, () => this.#closeActionLayer(), actions.primary);
  }

  #closeConfirmationCopy(facts: ConfirmationFacts): string {
    const additionalWorkspaceCount = facts.scope_workspace_ids.length - 1;
    const sentences = ["This closes this workspace."];
    if (additionalWorkspaceCount > 0) {
      sentences.push(
        `${additionalWorkspaceCount} additional linked ${additionalWorkspaceCount === 1 ? "workspace" : "workspaces"} will also close.`,
      );
    }
    if (facts.agent_total > 0) sentences.push(`${this.#agentTotal(facts.agent_total)} will be affected.`);
    sentences.push(
      additionalWorkspaceCount > 0
        ? "Their terminals will end, and unsaved work can be lost. Linked checkout folders and branches remain."
        : "Its terminals will end, and unsaved work can be lost. The folder and branch remain.",
    );
    return sentences.join(" ");
  }

  #agentTotal(total: number): string {
    return `${total} ${total === 1 ? "agent" : "agents"}`;
  }

  #interruptionWarning(facts: ConfirmationFacts): HTMLElement | undefined {
    const statuses = (["working", "blocked", "unknown"] as const)
      .filter((status) => facts.interruption_counts[status] > 0)
      .map((status) => `${facts.interruption_counts[status]} ${status}`);
    if (statuses.length === 0) return undefined;
    return element(
      this.#document,
      "p",
      "home-action-warning",
      `Agents may be interrupted: ${statuses.join(", ")}.`,
    );
  }

  async #submitAction(request: RunWorkspaceActionRequest): Promise<void> {
    if (this.#actionRunning) return;
    this.#actionRunning = true;
    this.#showWorking(this.#workingHeading(request.action));
    this.#renderAgain();
    let response: RunWorkspaceActionResponse;
    try {
      response = await this.#actions.runWorkspaceAction(request);
    } catch {
      response = { outcome: "unknown" };
    }
    this.#actionRunning = false;
    this.#renderAgain();
    if (response.outcome === "refused") {
      this.#showRefusal(response);
    } else if (response.outcome === "unknown") {
      this.#showUnknown(request.action);
    } else {
      this.#showSuccess(request.action);
    }
  }

  #workingHeading(action: RunWorkspaceActionRequest["action"]): string {
    const headings: Record<RunWorkspaceActionRequest["action"], string> = {
      create_space: "Creating space",
      create_worktree: "Creating worktree",
      close_workspace: "Closing workspace",
      close_group: "Closing workspace",
      delete_checkout: "Deleting checkout",
    };
    return headings[action];
  }

  #showSuccess(action: RunWorkspaceActionRequest["action"]): void {
    const copy: Record<RunWorkspaceActionRequest["action"], [string, string]> = {
      create_space: ["Space created", "Home will update when the new space is ready."],
      create_worktree: ["Worktree created", "Home will update when the new worktree is ready."],
      close_workspace: ["Workspace closed", "Its folder and branch remain. Home will update when it is ready."],
      close_group: ["Workspace closed", "Linked checkout folders and branches remain. Home will update when it is ready."],
      delete_checkout: ["Checkout deleted", "The Git branch remains. Home will update when it is ready."],
    };
    this.#showResult(copy[action][0], copy[action][1]);
  }

  #showUnknown(action: RunWorkspaceActionRequest["action"]): void {
    const message = action === "create_worktree"
      ? "Result unknown. Check Home before starting another worktree."
      : action === "create_space"
        ? "Result unknown. Check Home before starting another space."
        : "Result unknown. Check Home before trying this action again.";
    this.#showResult("Result unknown", message);
  }

  #showRefusal(refusal: RefusedWorkspaceAction): void {
    if (refusal.reason === "checkout_has_changes") {
      this.#showResult(
        "Checkout not deleted",
        "This folder has changes. It was not removed. Resolve the changes in the terminal, then try again.",
      );
      return;
    }
    const messages: Record<Exclude<RefusedWorkspaceAction["reason"], "checkout_has_changes">, string> = {
      invalid_request: "Shepherdr could not use this request.",
      not_found: "This workspace is no longer here.",
      not_applicable: "This action is no longer available.",
      busy: "Another workspace action is running. Try again when it finishes.",
      stale: "This workspace changed. Open the action again to check the latest details.",
      herdr_unavailable: "Herdr is not available. Try again when it is running.",
      herdr_refused: "Herdr refused this action.",
    };
    this.#showResult("Action not completed", messages[refusal.reason], refusal.detail);
  }

  #showWorking(heading: string): void {
    const status = element(this.#document, "p", "home-action-copy", "Wait for Herdr to finish.");
    status.setAttribute("role", "status");
    this.#showActionLayer(heading, [status], undefined, this.#actionPanel);
  }

  #showResult(heading: string, message: string, detail?: string): void {
    const content: Node[] = [element(this.#document, "p", "home-action-copy", message)];
    if (detail) content.push(element(this.#document, "pre", "home-action-detail", detail));
    const done = this.#button("Done", () => this.#closeActionLayer());
    const buttons = element(this.#document, "div", "home-action-buttons");
    buttons.append(done);
    content.push(buttons);
    this.#showActionLayer(heading, content, () => this.#closeActionLayer(), done);
  }

  #showActionLayer(
    heading: string,
    content: readonly Node[],
    cancel: (() => void) | undefined,
    initialFocus: HTMLElement | undefined,
  ): void {
    const title = element(this.#document, "h2", "home-action-title", heading);
    title.id = "home-action-title";
    this.#actionPanel.setAttribute("aria-labelledby", title.id);
    reconcileChildren(this.#actionPanel, [title, ...content]);
    this.#actionCancel = cancel;
    this.#actionLayer.hidden = false;
    initialFocus?.focus({ preventScroll: true });
  }

  #closeActionLayer(): void {
    if (this.#actionRunning) return;
    const pickerOpen = this.#pickerWorkspaceID !== undefined && !this.#picker.layer.hidden;
    const returnFocus = this.#returnFocus;
    this.#actionLayer.hidden = true;
    this.#actionCancel = undefined;
    reconcileChildren(this.#actionPanel, []);
    this.#returnFocus = undefined;
    const target = pickerOpen
      ? (returnFocus?.isConnected && this.#picker.panel.contains(returnFocus) ? returnFocus : this.#picker.close)
      : this.#backgroundFocusTarget(returnFocus);
    target?.focus({ preventScroll: true });
  }

  #handleDialogKey(event: KeyboardEvent): void {
    if (event.key === "Escape" && this.#actionCancel) {
      event.preventDefault();
      this.#actionCancel();
      return;
    }
    if (event.key !== "Tab") return;
    const focusable = Array.from(this.#actionPanel.querySelectorAll<HTMLElement>("button, input"))
      .filter((node) => !node.hasAttribute("disabled") && !node.hidden);
    if (focusable.length === 0) {
      event.preventDefault();
      return;
    }
    const current = focusable.indexOf(this.#document.activeElement as HTMLElement);
    const next = event.shiftKey
      ? (current <= 0 ? focusable.length - 1 : current - 1)
      : (current < 0 || current === focusable.length - 1 ? 0 : current + 1);
    event.preventDefault();
    focusable[next].focus({ preventScroll: true });
  }

  #renderAgain(): void {
    if (this.#lastRender && this.#actions.isHomeActive()) this.render(this.#lastRender);
  }

  #filteredHome(home: Home, value: string): Home {
    const query = value.trim().toLocaleLowerCase();
    const workspaces = home.workspaces
      .map((workspace) => this.#filteredWorkspace(workspace, query))
      .filter((workspace): workspace is Workspace => workspace !== undefined);
    return { ...home, workspaces };
  }

  #filteredWorkspace(workspace: Workspace, query: string): Workspace | undefined {
    const matches = (value: string | undefined): boolean => value?.toLocaleLowerCase().includes(query) ?? false;
    if (matches(workspace.label)) return workspace;

    const tabsShown = showTabHeadings(workspace);
    const tabs = workspace.tabs
      .map((tab) => ({
        ...tab,
        terminals: tabsShown && matches(tab.label)
          ? tab.terminals
          : tab.terminals.filter((terminal) => matches(terminal.title) || matches(terminal.agent?.name)),
      }))
      .filter((tab) => tab.terminals.length > 0);
    const worktrees = (workspace.worktrees ?? [])
      .map((worktree) => this.#filteredWorkspace(worktree, query))
      .filter((worktree): worktree is Workspace => worktree !== undefined);
    if (tabs.length === 0 && worktrees.length === 0) return undefined;

    return { ...workspace, tabs, worktrees };
  }

  #prune(usedWorkspaces: Set<string>, usedTabs: UsedTabs, usedSets: Set<string>, paneIDs: Set<string>): void {
    for (const key of this.#workspaces.keys()) {
      if (!usedWorkspaces.has(key)) {
        this.#workspaces.delete(key);
        this.#workspacePickerRows.delete(key);
        this.#workspaceMenus.delete(key);
        if (this.#openMenuKey === `workspace:${key}`) this.#openMenuKey = undefined;
      }
    }
    for (const [key, nodes] of this.#workspaceSets) {
      if (!usedSets.has(key)) {
        nodes.summary.remove();
        this.#workspaceSets.delete(key);
      }
    }
    for (const [workspaceID, tabs] of this.#tabs) {
      const usedWorkspaceTabs = usedTabs.get(workspaceID);
      if (!usedWorkspaceTabs) {
        this.#tabs.delete(workspaceID);
        continue;
      }
      for (const tabID of tabs.keys()) {
        if (!usedWorkspaceTabs.has(tabID)) tabs.delete(tabID);
      }
    }
    for (const key of this.#rows.keys()) {
      if (!paneIDs.has(key)) {
        this.#rows.delete(key);
        this.#items.delete(key);
        this.#entries.delete(key);
        this.#terminalMenus.delete(key);
        if (this.#openMenuKey === `terminal:${key}`) this.#openMenuKey = undefined;
      }
    }
  }
}

# Pane support plan

Status: approved

This document defines how Shepherdr will show and manage Herdr panes. In the interface, they are called **terminals**.

This approves product and architecture direction only. Implementation starts from a separately approved, pinned wave brief.

## Herdr model

Shepherdr preserves Herdr's real hierarchy:

**workspace → tab → terminal**

Every real terminal appears exactly once. Shepherdr does not choose a representative or “current” terminal for a workspace with several terminals.

Workspace, tab, and pane IDs are opaque session-scoped keys. The pane ID and current terminal ID together identify the exact terminal to open or change. A complete Herdr snapshot replaces the previous Home state after reconnects and changes.

Status totals count real agents once. A terminal without an agent does not gain an invented status.

## Home

A workspace with one terminal keeps the compact row used today. Its main target opens that terminal, and its three-dot menu contains both terminal and workspace actions.

A workspace with several terminals gains an inline terminal section:

- the heading shows the workspace name, terminal count, and nonzero agent-status totals;
- expanding it shows every terminal under that workspace;
- the same rule applies inside worktree groups, including the top-level workspace;
- real tab headings appear only when more than one nonempty tab needs to be distinguished; and
- tabs are labels only in this work—there are no tab-management controls.

Terminal sections that contain a working or blocked agent start expanded. A person's later choice is kept for that visit. Filtering and **Blocked** reveal matching context without replacing saved choices. **Expand all** and **Collapse all** affect worktree groups and multi-terminal sections together.

Counts remain exact totals for their workspace or group while a filter is active. They do not claim to count only visible rows.

Opening a terminal uses the existing Terminal destination. This work does not redesign Terminal.

## Terminal rows and names

Each terminal row has:

- one main target to open the terminal;
- the existing terminal icon; and
- a separate 44px three-dot action target.

Accessible names distinguish expanding a workspace, opening a terminal, terminal actions, and workspace actions.

A manual Herdr pane label is the terminal name. Without one, use the existing useful context in this order: the compact workspace name for a single terminal, then a recognized agent name, Herdr's terminal title, and finally **Terminal**.

If automatic names collide within the same visible tab, add simple numeric distinctions. Never rewrite or number a person's manual label. Keep workspace, agent, and status context on the secondary line without making the row taller; long text truncates rather than expanding the row.

## Actions

All actions start from a fresh complete snapshot and verify the exact workspace, tab, pane, and current terminal. Shepherdr shows success only after Herdr confirms it and a complete snapshot contains the result.

### New terminal

**New terminal** belongs to an existing terminal's menu because Herdr needs an exact terminal to split.

The sheet is titled **Split terminal** and briefly says that it will split the named terminal. It offers:

- **Beside** for a right split; and
- **Below** for a downward split.

Use the selected terminal as the exact anchor, inherit its fresh working directory, keep Herdr's current focus unchanged, and use only fields shared by Herdr 0.8.0 and 0.8.2. After confirmation and a complete refresh, open only the new terminal returned by Herdr.

Creating and naming cannot be one confirmed Herdr action, so a new terminal starts with its automatic name and can be renamed afterward.

### Rename terminal

**Rename terminal** edits Herdr's manual pane label. A blank name clears the label and restores the automatic name.

Herdr does not reliably announce this change. After a Shepherdr rename succeeds, request and publish one complete snapshot. Do not add polling or an optimistic browser-only name.

### Close terminal

The confirmation says that closing a terminal stops what is running there. If Herdr recognizes an agent, include its name and exact status. If it is the tab's final terminal, state plainly that the named tab will also close.

When another terminal keeps the workspace open, use Herdr's exact pane close action. When this is the workspace's final terminal, show **Close workspace** and use the existing workspace-close preparation and confirmation, including any linked-worktree cascade and nonzero agent effects. Do not submit an implicit larger close through the pane action.

Re-read the impact immediately before closing. If it changed, refuse the stale confirmation. If the result is unknown, refresh from Herdr and do not retry automatically.

## Compatibility

The management actions use the confirmed common surface in Herdr 0.8.0 and 0.8.2. Do not use the 0.8.2-only split option or promise identical focus behavior after closing.

There is no canonical Herdr 0.8.1 release to verify. If a server literally reports 0.8.1, viewing and opening remain available, but these new terminal-management actions stay hidden until that version is verified.

## Implementation split

Keep the work sequential because the server projection and Home interface share the same contract:

1. Preserve the required Herdr fields and ordering, then add exact split, rename, close, and impact checks.
2. Add multi-terminal disclosure, names, menus, confirmations, filtering behavior, accessibility, and place preservation to Home.
3. Review the exact worker results independently, integrate only approved commits, and run the real workflow below.

Do not add a durable Home store, partial event handling, optimistic success, a second runtime, new Herdr APIs, or speculative tab features.

## Acceptance

Use a production Shepherdr build, a real phone, and disposable real Herdr workspaces.

- Compare Home with Herdr for one terminal, several terminals in one tab, several nonempty tabs, and worktree groups containing each case.
- Verify every terminal appears once, tab membership and status totals are exact, single-terminal rows stay compact, and Home does not scroll horizontally.
- Exercise both disclosure levels, global expand/collapse, filtering, **Blocked**, reconnect, rotation, focus, and returning from Terminal without losing place.
- Split a nonfocused terminal **Beside** and **Below**. Confirm the exact anchor, tab, directory, unchanged Herdr focus, complete refresh, and exact new terminal destination.
- Rename, clear a name, and check duplicate automatic names without changing manual names.
- Close an ordinary terminal, a tab's final terminal, a workspace's final terminal, and a workspace whose close affects linked worktrees. Confirm the exact warning and Herdr result each time.
- Repeat the common management actions against real Herdr 0.8.0 and 0.8.2. Confirm that a literal unverified 0.8.1 does not expose them.

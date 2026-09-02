# Pane support wave

Status: approved

This wave implements the approved direction in `docs/pane-support-plan.md`. It improves how Home shows and manages Herdr terminals without changing the existing Terminal screen.

The functional pane implementation is integrated at `dbe432568c9b5f4901b4958952795b55d8e728cc`. The human accepted its behavior and replaced the inline terminal sections with the terminal picker described here. The orchestrator will record the commit containing this approved update as the exact starting point for the focused interface worker.

## Scope

One focused worker owns the remaining Home presentation change. The reviewed Herdr projection and terminal-action server contract stay unchanged.

The worker will:

- keep exactly one consistent Home row per workspace;
- keep a one-terminal workspace opening directly through the existing terminal control;
- add a small count badge to that control when several terminals exist and open the approved terminal picker;
- use a bottom sheet on phones and a compact dialog on larger screens;
- show every terminal exactly once in the picker, with tab headings only when more than one nonempty tab needs to be distinguished;
- keep terminal name, agent/status, open action, and three-dot menu clear and compact inside the picker;
- keep status totals, filtering, blocked attention, expansion choices, focus, scroll, and return position truthful and stable;
- keep the existing approved terminal names, menus, action sheets, confirmations, and version gating; and
- remove the superseded inline terminal-section rendering, disclosure state, styles, and tests rather than leaving both patterns in place.

The worker may change the Home browser model, rendering, styles, actions, small relevant tests, and deterministic `web/dist` output. It must not change the Go Herdr adapter, projection, or action handlers.

## Boundaries

- Do not redesign or patch Terminal behavior. Home may only navigate to the existing accepted Terminal destination.
- Do not change the reviewed Herdr projection or terminal-action server contract.
- Do not add tab-management controls, a durable Home store, partial event handling, optimistic success, new Herdr interfaces, or speculative later-work structure.
- Do not change authentication, notifications, uploads, or unrelated workspace-management behavior.
- Keep full Herdr snapshots as the source of Home state.
- Use the repository's memory-capped browser test entry. Assertions must compare primitive values, not live DOM objects.
- If Herdr cannot confirm an action exactly as the approved plan requires, stop and report the missing capability instead of inventing a workaround.

## Delivery flow

1. The worker replaces only the Home presentation from the exact approved-update commit, validates it against real Herdr, commits it, and reports the exact commit and evidence.
2. One independent reviewer checks the exact result for code quality, removal of the superseded inline path, compact phone behavior, touch use, and accessibility. Findings return to the worker; the reviewer does not edit the result.
3. After review approval, an integrator starts from the pinned update commit, integrates only the approved worker result, runs the focused integration gate, and reports the exact commit and evidence.
4. The human performs the final real-phone gate. Integration is not product acceptance.

No other implementation stream should modify Home or the Herdr projection during this wave.

## Worker acceptance checks

Use a production browser build and disposable real Herdr workspaces. Check both Herdr 0.8.0 and 0.8.2 for the common management actions.

- Compare Home with Herdr for one terminal, several terminals in one tab, several nonempty tabs, and worktree groups containing those cases.
- Confirm Home keeps one row per workspace, every terminal appears once in its picker, tab membership and nonzero status totals are exact, single-terminal rows remain compact, and Home does not scroll horizontally.
- Confirm the terminal-count badge and picker work the same way for the top-level workspace and nested worktrees.
- Exercise worktree-group expansion, **Expand all**, **Collapse all**, opening and closing the picker, text filtering, **Blocked**, reconnect, rotation, focus, and returning from Terminal without losing place.
- Split a nonfocused terminal **Beside** and **Below**. Confirm the exact anchor, tab, inherited directory, unchanged Herdr focus, complete refresh, and exact new terminal destination.
- Rename a terminal, clear its name, and exercise duplicate automatic names without changing manual names.
- Close an ordinary terminal, a tab's final terminal, a workspace's final terminal, and a workspace whose close affects linked worktrees. Confirm the warning, fresh impact check, and real Herdr result.
- Confirm a literal unverified 0.8.1 can still be viewed and opened but does not show the new terminal-management actions.
- Confirm empty, offline, reconnecting, and "Herdr is not running" states remain truthful and visually stable.

## Integration and human gate

The integrator repeats the smallest representative production-path checks against real Herdr 0.8.2, verifies the embedded browser build, and confirms no unrelated feature changed.

The human then checks on a real phone:

- compact one-terminal workspace rows and count-badged multi-terminal workspace rows;
- the terminal picker for top-level and nested worktree workspaces;
- exact totals, blocked attention, filtering, expansion, rotation, and retained place;
- opening each terminal from Home; and
- splitting, renaming, and closing terminals, including the final-terminal workspace warning.

The next wave waits for that real-phone acceptance.

# Pane support wave

Status: approved

This wave implements the approved direction in `docs/pane-support-plan.md`. It improves how Home shows and manages Herdr terminals without changing the existing Terminal screen.

The brief was prepared against integrated commit `2e594002f19fb3eee81979caeb93d82090aeb2cd`. After this brief is approved and committed, the orchestrator will record that new commit as the exact worker starting point.

## Scope

One worker owns the complete change because the Herdr projection, actions, and Home interface share one contract.

The worker will:

- preserve Herdr's real workspace, tab, and terminal hierarchy and show every terminal exactly once;
- keep a one-terminal workspace as the compact row already used on Home;
- add the approved inline expandable terminal section for workspaces with several terminals, including worktree groups;
- show tab headings only when more than one nonempty tab needs to be distinguished;
- keep status totals, filtering, blocked attention, expansion choices, focus, scroll, and return position truthful and stable;
- apply the approved terminal naming and duplicate-name rules;
- add terminal menus for **New terminal**, **Rename terminal**, and **Close terminal**;
- implement **Split terminal** with **Beside** and **Below** using the selected terminal as the exact anchor;
- use the existing workspace-close flow when the last terminal would close its workspace;
- confirm actions only after Herdr accepts them and a fresh complete snapshot contains the result; and
- expose these management actions for confirmed Herdr 0.8.0 and 0.8.2, while keeping them hidden for a literal unverified 0.8.1.

The worker may change the Go Herdr adapter, projection, and action handlers; the Home browser model, rendering, styles, and actions; small relevant tests; and deterministic `web/dist` output.

## Boundaries

- Do not redesign or patch Terminal behavior. Home may only navigate to the existing accepted Terminal destination.
- Do not add tab-management controls, a durable Home store, partial event handling, optimistic success, new Herdr interfaces, or speculative later-work structure.
- Do not change authentication, notifications, uploads, or unrelated workspace-management behavior.
- Keep full Herdr snapshots as the source of Home state.
- Use the repository's memory-capped browser test entry. Assertions must compare primitive values, not live DOM objects.
- If Herdr cannot confirm an action exactly as the approved plan requires, stop and report the missing capability instead of inventing a workaround.

## Delivery flow

1. The worker implements the full scoped change from the exact approved-brief commit, validates it against real Herdr, commits it, and reports the exact commit and evidence.
2. An independent technical reviewer checks that exact commit against this brief and the pane-support plan. Findings return to the worker; the reviewer does not edit the result.
3. After technical approval, a focused interface reviewer checks the exact Home behavior for clarity, compactness, touch use, and accessibility without redesigning it. Material changes return to the worker.
4. After both reviews approve, an integrator starts from the pinned brief commit, integrates only the approved worker result, runs the integration gate, and reports the exact commit and evidence.
5. The human performs the final real-phone gate. Integration is not product acceptance.

No other implementation stream should modify Home or the Herdr projection during this wave.

## Worker acceptance checks

Use a production browser build and disposable real Herdr workspaces. Check both Herdr 0.8.0 and 0.8.2 for the common management actions.

- Compare Home with Herdr for one terminal, several terminals in one tab, several nonempty tabs, and worktree groups containing those cases.
- Confirm every terminal appears once, tab membership and nonzero status totals are exact, single-terminal rows remain compact, and Home does not scroll horizontally.
- Exercise nested expansion, **Expand all**, **Collapse all**, text filtering, **Blocked**, reconnect, rotation, focus, and returning from Terminal without losing place.
- Split a nonfocused terminal **Beside** and **Below**. Confirm the exact anchor, tab, inherited directory, unchanged Herdr focus, complete refresh, and exact new terminal destination.
- Rename a terminal, clear its name, and exercise duplicate automatic names without changing manual names.
- Close an ordinary terminal, a tab's final terminal, a workspace's final terminal, and a workspace whose close affects linked worktrees. Confirm the warning, fresh impact check, and real Herdr result.
- Confirm a literal unverified 0.8.1 can still be viewed and opened but does not show the new terminal-management actions.
- Confirm empty, offline, reconnecting, and "Herdr is not running" states remain truthful and visually stable.

## Integration and human gate

The integrator repeats the smallest representative production-path checks against real Herdr 0.8.2, verifies the embedded browser build, and confirms no unrelated feature changed.

The human then checks on a real phone:

- compact one-terminal workspaces and expandable multi-terminal workspaces;
- worktree groups containing multi-terminal workspaces;
- exact totals, blocked attention, filtering, expansion, rotation, and retained place;
- opening each terminal from Home; and
- splitting, renaming, and closing terminals, including the final-terminal workspace warning.

The next wave waits for that real-phone acceptance.

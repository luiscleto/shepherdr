# Shepherdr Home and workspace-management architecture

Status: approved

Approved simplification: 2026-08-20

## Scope

This document covers Home and its current workspace actions. Terminal and per-browser notifications have separate approved direction. Sign-in, device trust, and Chat remain later work.

The current shape is small:

1. One configured Herdr 0.8.0 session owns the real workspaces, terminals, agents, statuses, and worktree actions.
2. One Go process talks to Herdr, serves the embedded browser UI on `localhost`, and keeps only the latest complete Home in memory. The same process performs the work in `notification-architecture-proposal.md` and stores only its approved notification state outside the repository.
3. The phone renders Home and opens the existing Terminal destination.

There is no Home database, stored grouping, second runtime, or public hosting support.

## What Herdr provides

`session.snapshot` returns the complete state needed by Home: workspaces, tabs, panes, terminals, layouts, agents, focus, protocol information, and worktree provenance.

`events.subscribe` reports changes to that state. Events tell Shepherdr to read another complete snapshot; the browser does not need Herdr event details or partial updates.

Workspace actions use Herdr's confirmed workspace creation and close requests, worktree creation and removal, and current snapshot data. Shepherdr does not invent another interface or run its own Git workflow.

Herdr remains the authority for the status words `working`, `blocked`, `idle`, `done`, and `unknown`.

## The Home loop

Herdr 0.8.0 requires status subscriptions to name the panes they watch. Home therefore uses one small setup read, then one simple loop:

1. Read once to learn the current pane IDs. Do not show that setup read.
2. Open one event connection for global changes and those pane statuses.
3. After Herdr accepts the subscription, read one complete `session.snapshot` over a short-lived read connection.
4. Validate and publish that complete Home.
5. When a relevant change arrives, read another complete snapshot.

Only one snapshot read runs at a time. If another change arrives during that read, remember one pending refresh and read once more afterward. Do not cancel a complete snapshot, start parallel reads, or build a queue of refreshes.

Keep showing the last complete Home while the next one is read. Replace it once, only when the new complete Home is ready. If it is unchanged, send nothing. If no complete Home has ever been available, show one in-place loading or unavailable state.

The browser receives whole Home states, not read phases or Herdr events. There is no visible Home refresh state, banner, animation, timer, or second lifecycle.

After a disconnect, reconnect the Herdr subscription and read a new complete snapshot. Do not replay or guess missed state.

If a snapshot reveals new panes, update the one subscription and read once more so their status changes are covered. If the subscription is lost during a read, reconnect and read again before publishing. These are the only required gap guards.

## Grouping and Home behavior

Use the exact, nonempty `repo_key` and `is_linked_worktree` values in the snapshot. One ordinary checkout plus one or more linked worktrees with the same key forms a set. Missing, malformed, linked-only, or ambiguous provenance stays flat. Never group by labels, paths, branches, Git output, or list position.

Keep unrelated workspaces in Herdr order. An expanded set shows every workspace and every terminal exactly once. Agent presence never determines whether a terminal exists.

The browser may filter its last complete Home by visible workspace, tab, terminal, and agent names. This is a local projection only: it adds no server state or Herdr read, preserves global attention counts, and does not overwrite expansion choices.

A set shows only its nonzero agent status totals under the top-level name. When the top-level workspace has exactly one terminal, keep that real terminal row visible and place a separate disclosure action beside it; expanding reveals only the worktree rows. With zero or multiple top-level terminals, use a neutral disclosure heading and show all top-level terminal rows when expanded. Never select one terminal as a representative or fallback.

Sets with working or blocked agents start expanded; the rest start collapsed. A person's later choice wins for that browser visit. **Expand all** and **Collapse all** appear only when useful.

The persistent attention area shows the working count. **N blocked** opens the blocked terminals with enough workspace context. Returning restores the previous expansion, focus, and scroll position.

## Connection state

Connection state comes only from the real connection, not from Home reads.

- Valid current data, or a small liveness heartbeat when nothing changed, means **Live**.
- A broken connection means **Reconnecting**.
- The existing 45-second threshold leads to **Offline**, with the existing manual reconnect action.
- A stopped or incompatible Herdr reports its exact state.

Reading or replacing Home never changes the connection badge. Ordinary activity must not blink, animate, add a banner, or move the page. The badge has a stable place at the top right.

A heartbeat carries no Home or product meaning. It exists only to keep connection detection honest when Herdr state is unchanged.

Production browser files must revalidate so a normal refresh cannot combine an old interface with a new executable.

## Truth and trust

Publish only a complete snapshot that can be joined without duplicate or conflicting core identities. Missing or malformed worktree provenance leaves that case flat; uncertain layout or focus omits only what cannot be resolved. It does not hide otherwise valid workspaces and terminals.

Render names, IDs, paths, branches, repository keys, and all Herdr or terminal content as untrusted text. Displayed content cannot become markup, a route, a request, a command, or application authority.

An **Open** action targets the exact terminal represented by its row. If that target no longer exists, the action fails honestly; it never falls back to a different terminal.

## Workspace actions

Before an action, read current Herdr state and validate that exact target. Run one workspace action at a time. Send only the confirmed Herdr request. Report success only from its matching response; after an interrupted or unclear result, say the result is unknown and refresh Home.

- **New space** uses a freely entered directory and an optional label. Shepherdr expands only `~` and `~/...`; it does not interpret other shell syntax.
- **New worktree** uses either a confirmed repository workspace or an ordinary workspace's freshly resolved current terminal directory. A blank branch is omitted so Herdr chooses. Herdr decides whether the directory is a valid Git source.
- **Close workspace** applies to any workspace Herdr can close. The confirmation shows additional linked workspaces and nonzero agent counts that will be affected.
- **Delete checkout** applies only to a linked worktree checkout. It uses `force:false`, never deletes the branch, and reports a dirty refusal honestly.

Displayed names, paths, branches, and browser-supplied values never choose the operation or grant authority. The exact current contract and acceptance record live in `wave-02-workspace-actions.md`; do not duplicate its internal details here or keep speculative machinery for later actions.

## Work order and acceptance

Home, Terminal, workspace management, and per-browser notifications have passed their respective gates. Future work starts only from a human-approved brief. Tests should be few and useful; the production executable against real Herdr and the real-phone workflow remain the acceptance gate.

Home must continue to prove:

- flat and real worktree-nested cases match Herdr;
- every workspace and terminal appears exactly once;
- status totals, blocked attention, disclosure, focus, and place are correct;
- ordinary Herdr activity updates Home without a banner, badge flicker, or page movement;
- disconnect, reconnect, Herdr restart, empty Home, and incompatible Herdr are honest;
- a normal phone refresh loads the current embedded interface; and
- Terminal navigation reaches the existing exact destination without changing Terminal behavior.

Workspace actions must continue to prove real creation, close scope, clean deletion, dirty refusal, interruption, unknown outcomes, and hostile-input handling. They do not reopen Home or Terminal architecture.

## Risks

- Herdr may send many events during active terminal output. The one-read-plus-one-pending-refresh rule prevents parallel work and retry storms.
- Missing or malformed provenance keeps workspaces flat; Shepherdr never guesses.
- A connection can fail during an action. Unknown is safer than an automatic retry.
- Mobile browsers and private-network proxies can interrupt connections or cache files. Reconnect from current Herdr state, and make browser files revalidate.

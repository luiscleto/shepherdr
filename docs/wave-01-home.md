# Wave 01: read-only Home organization and attention

Status: approved

This corrected brief replaces the earlier overbuilt Home refresh design. The replacement worker starts from the exact commit containing this approved correction, based on stable `master` at `67cf338855848bdc493bfbdfdead61845a799569`. Do not start from or reuse the unaccepted H1 implementation.

## Outcome

Deliver a small read-only Home for one configured Herdr 0.8.0 session with sign-in off:

- keep ordinary cases compact and flat;
- organize real linked worktrees only from Herdr's snapshot;
- show every workspace and terminal exactly once;
- show exact agent status totals and blocked attention;
- preserve expansion, focus, and scroll when leaving and returning; and
- navigate to the accepted existing Terminal without changing it.

The stack remains one Go server with a small embedded TypeScript browser UI. There is no durable Home store, stored grouping, second runtime, or speculative support for later slices.

## Simple Home state loop

Use `events.subscribe` to learn that Home may have changed and `session.snapshot` to read the complete current state. Herdr requires pane IDs in status subscriptions, so setup has one small extra read:

1. Read once to learn the current panes; do not publish this setup read.
2. Open one event subscription for global changes and those pane statuses.
3. After subscription acknowledgement, read and publish one complete snapshot.
4. On a relevant event, read another complete snapshot.
5. Allow one read at a time. If another event arrives, remember one pending refresh and read once more afterward.
6. Keep the last complete Home visible until its complete replacement is ready.
7. Send nothing when the result did not change.

Do not invalidate and restart a read because another event arrived. Do not open parallel reads or queue every event. Do not expose reading, rebuilding, or retrying to the browser.

When a snapshot contains new panes, replace the one subscription and read once more. When the subscription is lost during a read, reconnect and obtain a post-subscription snapshot before publishing. Do not add other race machinery.

Home has no updating field, banner, animation, timer, debounce state, or visible refresh phase. There is no **Home is updating** copy. If no complete Home has ever loaded, show one in-place loading or unavailable state.

The real connection alone owns **Live**, **Reconnecting**, **Offline**, **Herdr is not running**, and **Cannot use this Herdr**. Valid current data or a small liveness heartbeat means **Live**. The heartbeat carries no Home state. Preserve the existing 45-second Offline threshold, keep retrying automatically through the same restrained connection path and cadence, and provide no manual reconnect action. Home reads never change or animate the badge.

Serve production browser files with revalidation so a normal refresh cannot retain an old interface after the executable changes.

## Grouping, attention, and Open

- Group only exact, nonempty `repo_key` matches containing one ordinary checkout and at least one linked worktree. Missing, malformed, linked-only, or ambiguous cases stay flat. Never infer grouping from names, paths, branches, Git, or order.
- Keep the parent at its original top-level place, move its linked worktrees inside it, and preserve Herdr order for everything else.
- An expanded set shows every workspace and terminal exactly once. Agent presence never decides whether a terminal exists.
- A set shows only nonzero exact agent totals under the parent name in `working`, `blocked`, `idle`, `done`, `unknown` order. Each total keeps its status word and there is no workspace-count badge.
- With exactly one parent terminal, keep that real **Open** row visible and give it a separate disclosure action that reveals only worktree rows. With zero or multiple parent terminals, use a neutral disclosure heading and show every parent terminal when expanded. Never choose a representative terminal.
- The local Home filter matches visible workspace, tab, terminal, and agent names without another Herdr read, changing global attention counts, renumbering titles, or overwriting expansion choices.
- Start sets containing working or blocked agents expanded and the others collapsed. Later manual choices win for the browser visit. Show **Expand all** or **Collapse all** only when useful.
- Keep the working count persistent. When agents are blocked, **N blocked** shows only those terminals with enough workspace context. **Show all terminals** restores prior expansion, focus, and scroll.
- Every target is at least 44 by 44 CSS pixels. Rotation and return from Terminal preserve a readable layout and the person's place.

An **Open** uses the exact terminal represented by its row. It never chooses the first terminal or another surviving terminal as a fallback. Hide actions when there is no complete usable Home.

## Truth and trust

Reject a whole snapshot only when its core identities are duplicate, conflicting, or cannot be joined safely. Bad or missing worktree provenance keeps that case flat instead of making unrelated Home content unavailable.

Names, IDs, paths, provenance, statuses, and all Herdr content are untrusted text. They cannot become markup, route syntax, requests, commands, or application authority.

Empty, stopped, incompatible, reconnecting, and offline states are real. Never invent data or success.

## Worker ownership

The replacement Home worker may change only:

- the small Herdr snapshot/event reader needed by Home;
- Home response wiring in the Go server;
- Home model, view, connection badge, and Home-owned styles;
- focused Home tests; and
- browser cache headers for embedded application files.

The worker should delete superseded refresh-state and candidate-retry machinery rather than adapt it. If `internal/herdr/projector.go` no longer expresses the simple loop clearly, replace or reduce it; do not preserve its structure merely because it exists.

Keep the existing internal current-snapshot information that the accepted Terminal target lookup needs, but do not change its contract or serialize its lifecycle to Home.

Do not change, test, or re-accept Terminal code or behavior. Do not add workspace actions, auth, devices, notifications, persistence, new Herdr requests, dependencies without a current need, or structure for later work.

## Tests and evidence

Prefer a few direct tests of the simple loop and Home result:

- complete snapshot mapping and exact grouping;
- one active read plus one pending refresh under an event burst;
- unchanged Home sends nothing;
- status and topology changes replace Home once;
- connection loss and recovery use the connection badge only;
- local filtering, disclosure, blocked attention, focus, scroll, and current asset revalidation; and
- hostile text stays inert.

Do not recreate Herdr with a large mock system. Do not test every possible event ordering. Real Herdr through the production executable is the acceptance path.

All browser tests must run through the repository's memory-capped `npm test --prefix web` entry. Never invoke Node, TSX, or individual browser test files without that cap. Assertion failures compare primitive values, not live DOM objects.

Run the capped browser tests once, TypeScript typecheck/build, focused Go tests, and a production Go build. Report exact commands and results. Do not run Terminal tests as evidence for this wave.

## Review and integration

One worker owns the replacement. An independent technical reviewer checks the exact commit for simplicity, deletion of superseded machinery, correctness, and no Terminal changes. A fresh language and interface reviewer checks only Home copy, density, movement, and ordinary words. Findings return to the worker.

After both approve, an integrator starts from this brief's pinned commit, integrates only the approved worker result, builds the production executable, and checks it against real Herdr and the Pixel 8a emulator. Integration is not final acceptance.

## Real-phone gate

The human checks:

1. Flat and linked-worktree cases match Herdr; every workspace, terminal, and status total is accounted for.
2. Local filtering, disclosure, Expand/Collapse all, blocked attention, rotation, focus, and return place work.
3. Ordinary agent activity updates Home without **Home is updating**, badge flicker, page movement, or a refresh storm.
4. A normal phone refresh loads the current interface without clearing site data or using Incognito.
5. Herdr disconnect and restart produce the truthful badge and recover through the existing reconnect path.
6. Each ordinary terminal row opens the already accepted exact Terminal destination; the check makes no Terminal behavior claim.

Human acceptance gates the next slice.

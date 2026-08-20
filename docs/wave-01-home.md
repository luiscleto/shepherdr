# Wave 01 / Slice 1: read-only Home organization and attention

Status: approved

Prepared against and rebased onto the exact integrated Terminal and documentation base `3fc53731e3da35ffb59b3875cc21366585c35a4f`.

The implementation starting commit is not this preparation base. After human approval, the orchestrator must commit the approved brief and pin that future commit for dispatch so the worker and integrator start with every governing document. This proposal does not invent that SHA.

## Outcome

Deliver the first approved read-only Home slice for one configured Herdr 0.8.0 session with sign-in off. Home remains a truthful, in-memory projection of Herdr: compact and flat when no valid worktree nest exists, nested only when fresh Herdr provenance establishes one, useful for attention, and safe to leave and return to on a phone.

The stack remains one Go server plus a small TypeScript browser UI whose production assets are embedded in the executable. Ordinary minimal libraries or tooling are allowed only within the approved architecture. Worktree organization is derived only from Herdr; there is no durable Home or disclosure store, no Shepherdr-owned grouping model, and no second runtime.

Terminal is an accepted existing navigation destination governed by `docs/terminal-direction.md` and the direct human R&D decisions recorded there. This brief governs only truthful Home resolution, navigation to that destination, and return to Home. It does not redesign, restate, test, or implement Terminal behavior.

## Worker brief

### Fresh projection, nesting, and order

- Keep the existing compact flat rows unchanged when there is no valid nest. Do not add nest chrome or a second Open action to the flat case.
- Derive nests in memory only from exact, nonempty, schema-valid opaque `repo_key` equality in a fresh coherent Herdr snapshot. A valid nest has exactly one nonlinked workspace and at least one linked worktree workspace. Null or malformed provenance, a linked-only set, or a set with multiple nonlinked/source candidates stays flat. Never group by labels, paths, branches, Git enumeration, or list proximity.
- Replace the nonlinked workspace at its original Herdr top-level position with the nest. Remove its linked workspaces from their prior top-level positions, retain their Herdr relative order inside the nest, and retain the relative order of all unrelated rows. This applies when linked entries are noncontiguous or appear before the parent.
- An expanded nest shows every workspace and every terminal in the set exactly once. Agent decoration never determines whether a terminal exists.
- Count each unique, exactly joined real agent once. A nest header shows its top-level workspace title once, the workspace total, and only nonzero agent totals in `working`, `blocked`, `idle`, `done`, `unknown` order. Attention totals are equally exact. Never infer a status or decorate an ordinary terminal with one.
- Publish only a complete coherent candidate under the approved subscribe-before-contributing-read protocol. A relevant event during a candidate build, a connection-generation change, subscription loss, or any missing, malformed, duplicate, ambiguous, or conflicting canonical join invalidates the candidate. Publication is atomic; partial or mixed Home data is never sent or rendered.

### Visible language, density, and targets

- A nested heading visibly contains the human workspace title once, `N workspaces`, then only nonzero `N working`, `N blocked`, `N idle`, `N done`, and `N unknown` totals in that order.
- Slice-owned action and update copy is `Open`, `Expand all`, `Collapse all`, `N blocked`, `Show all terminals`, and `Home is updating`. Preserve the existing `Reconnect` action when Offline. Do not rename these or add parallel labels for the same action or state.
- The stable top-right badge contains exactly one of `Live`, `Reconnecting`, `Offline`, `Herdr is not running`, or `Cannot use this Herdr`. Never repeat its label in a connection panel. Preserve the existing quiet `Sign-in is off` note.
- Empty Home copy is exactly `No terminals` and `Herdr is running, but nothing is open.`
- Every Open's accessible name is `Open` followed by the human terminal title and place. Visible action copy, when shown, remains `Open`; the title and place appear once in the surrounding visible context. Never expose `pane`, `linked`, `source`, `group`, `nest`, `header`, `repo_key`, or any ID in visible or accessibility text. Those terms remain implementation language only.
- Workspace and terminal rows inside an expanded set keep the current compact row: title, optional agent, and one Open. Do not show a path or internal provenance chrome.
- Heading totals are inert text. They never become a second `N blocked` action, an Open, or a filter; the persistent attention area's `N blocked` remains the only blocked action.
- Every interactive Home target owned by this slice is at least 44 by 44 CSS pixels: disclosure, top-level Open, `Expand all`, `Collapse all`, `N blocked`, `Show all terminals`, and ordinary terminal Open. Compact never means a tiny text link.
- Rotation preserves hierarchy, targets, and the reserved badge slot without layout shift. Portrait and landscape remain integrator/emulator and human validation of the responsive Home, not a separate worker feature.

### Disclosure, attention, and Open truth

- On the first load of the current browser visit, expand nests containing a working or blocked agent and collapse the rest. After initialization, the person's manual choice wins for that visit even if status changes. Do not persist it durably.
- Show `Expand all` when any nest is collapsed and `Collapse all` when all are expanded. Omit both when there are no nests and in Blocked. Collapse actions must leave focus on visible content; focus must never remain inside hidden rows.
- Keep the persistent working count. Show `N blocked` only when the exact count is nonzero. Blocked temporarily shows only blocked terminal rows with minimum workspace context. It has no nest disclosure, nest totals, header Open, Expand/Collapse all, or management controls. `Show all terminals` restores Home without changing remembered expansion.
- The nest disclosure and its top-level workspace current-terminal Open are separate sibling targets of at least 44 by 44 CSS pixels. Resolve that Open only through the exact approved chain: the workspace's `active_tab_id`, exactly one matching canonical layout, its `focused_pane_id`, and an exact workspace/tab/pane/terminal identity join. Omit it at any ambiguity or absence; never substitute the first terminal.
- Every ordinary terminal row continues to open its exact existing terminal route. Route construction uses the canonical current identity as an encoded structured parameter; displayed values cannot author route syntax or retarget the action. Every Open uses the same human terminal title and place in visible context and its accessibility name.
- Omit every terminal Open and header Open whenever the current Home projection is incomplete or invalid, including while prior coherent rows remain visible during an update.

### Connection and coherent Home presentation

- Reserve the pinned stable top-right badge slot without layout shift or a second connection panel carrying the same state label.
- Preserve the existing transport behavior and 45-second Offline threshold. Current-generation valid frames keep the badge Live during internal projection rebuilds and Home resume; internal work must not produce a Reconnecting flicker. Preserve the existing manual reconnect affordance when Offline.
- `Herdr is not running` and `Cannot use this Herdr` suppress Live and do not open the reconnect sheet.
- Treat transport health and coherent Home authority separately. During a rebuild, retain prior coherent rows with one short `Home is updating` note only while those rows remain honest; otherwise replace the list with clear updating or unavailable content. Never publish partial data. Remove all Opens until a complete current Home is published.
- Home resume triggers a fresh covered rebuild while retaining healthy Live evidence. Relevant events, races between reads and events, disconnects, Herdr restarts, stopped Herdr, and incompatible Herdr must converge on the exact truthful badge and Home state.
- A coherent empty Home shows exactly `No terminals` and `Herdr is running, but nothing is open.` Any ordinary terminal means Home is not empty.

### Place, trust, and terminal boundary

- Preserve Home mode, expansion, scroll/viewport anchor, and focus for a surviving keyed row across Home resume, navigation to an existing terminal route and back, browser Back, and Blocked then Show all. Restore after layout settles without a top flash. If the row disappeared, preserve the closest honest place without retargeting another terminal. Focus must remain visible.
- Treat every displayed name, status, ID, path, and provenance value as untrusted inert data. Render visible and accessible content as text. It cannot become markup, route syntax, grouping authority, an application request or control message, authorization, or any other application authority.
- This slice may navigate to the accepted existing Terminal destination only to prove exact target, deep-link truth, and Home return. It contains absolutely no terminal renderer, terminal WebSocket, attachment or child lifecycle, control, input, history, theme, Terminal UX, or Terminal behavior tests. The integrated Terminal implementation and `docs/terminal-direction.md` are outside ownership. Do not restate, patch, or re-accept Terminal behavior here.

### Excluded work

There is no `New worktree`, `Branch`, `Actions`, `Close`, `Remove`, disabled placeholder, empty menu, or speculative management scaffolding. There is no auth or device-trust work, notifications, Chat, images, attachments, multiple Herdr sessions, new persistence, new Herdr interface, architecture change, or Terminal work.

## Ownership and sequence

### Worker

**Worker H1 — read-only Home organization and attention** is the one implementation owner. The worker starts from the future pinned approved-brief commit, implements only this brief, validates the real behavior, makes one conventional commit, and reports its exact commit and evidence. The worker does not approve or integrate its own result.

The likely owned areas, based on the current tree, are:

| Area | Likely files | Ownership limit |
| --- | --- | --- |
| Herdr decoding and coherent Home projection | `internal/herdr/client.go`, `internal/herdr/types.go`, `internal/herdr/projector.go`, and narrowly related error handling | Add only the confirmed snapshot provenance, exact joins, derived nests, current-terminal resolution, status totals, and coherent publication needed by Home. Do not add Herdr interfaces or management/terminal semantics. |
| Home publication and production path | `internal/server/server.go`; `main.go` only if compile-time wiring is strictly required | Keep `/api/home`, localhost policy, same-origin server shape, and one configured projector. Do not change terminal endpoints or invent another runtime. |
| Browser Home state and presentation | `web/src/home-model.ts`, `web/src/home-view.ts`, `web/src/home-connection.ts`, Home-owned portions of `web/src/main.ts`, and Home selectors in `web/src/style.css` | Own nesting, disclosure, Blocked, badge, coherent-update presentation, accessibility, and place. Avoid semantic changes outside Home. |
| Focused tests and real-path evidence | Home-focused cases in `internal/herdr/*_test.go`, `internal/server/server_test.go`, `internal/server/real_test.go`, `web/src/home-model.test.ts`, `web/src/home-connection.test.ts`, and `web/src/browser-state.test.ts` | Small fixtures may force races, invalid joins, ordering, and hostile values. They support rather than replace a bounded real Herdr production check. Add, change, or claim no Terminal behavior tests. |
| User-facing documentation | `README.md` only if needed to describe the behavior that actually ships | Use the approved interface language. Do not edit approved direction, architecture, discovery, or wave documents. |

Likely overlap must be handled narrowly:

- The Go and TypeScript Home shapes are shared state; change both atomically and do not leak raw snapshot authority to the browser.
- `web/src/main.ts` contains both Home navigation and the accepted Terminal boundary. `web/src/terminal-route.ts`, `web/src/terminal-page.ts`, `web/src/terminal/**`, terminal reader/session/input/ownership and lab files, and Terminal-owned sections of `web/src/style.css` are outside ownership. Home work may prove an encoded destination and return state but must not change or test Terminal behavior.
- `internal/server/server.go` and `main.go` now share wiring with the accepted Terminal. `internal/server/terminal_bridge.go`, `internal/server/terminal_protocol.go`, `internal/server/terminal_target.go`, and their tests are outside ownership. Home changes must preserve their interfaces without assigning them Home semantics.
- `internal/server/server.go` shares Home transport with static asset serving. `main.go` embeds `web/dist`; `web/package.json` generates those assets. Production builds must exercise this path, but generated `web/dist/*` stays uncommitted and the embed pipeline is not redesigned.
- No dependency change is expected. Any ordinary minimal dependency must be justified by the current Home need, remain within the approved stack, and must not create speculative structure.

The worker must not make semantic changes beyond this Home projection and presentation ownership. A needed product, architecture, security, terminal, or cross-slice decision returns to the human.

### Independent review and integration

1. **Reviewer T1 — independent Home technical review** checks the exact worker commit against this brief, the approved documents, the production build, and a real compatible Herdr 0.8.0 session. The reviewer reports findings and does not fix them.
2. **Reviewer G1 — fresh independent Grok Home review** checks the exact worker commit for mobile language, density, visual hierarchy, 44-pixel targets, visible focus, and the absence of jargon, duplication, clutter, and future controls. The reviewer uses portrait and landscape emulator screenshots when practical and does not fix the result.
3. **Integrator I1 — Wave 01 / Slice 1 integration** starts from the same future pinned approved-brief commit and integrates only a worker commit approved by both reviewers. The integrator resolves only in-scope composition problems, runs the full gate below, reports the exact integrated commit and evidence, and hands the result to the human. Integration is not product acceptance.

## Acceptance gate

### Worker and reviewer evidence

Use the production executable against the real configured Herdr server and its one real session. A fake replacement is not acceptance. Focused fixtures are allowed only for bounded cases that real Herdr cannot safely or naturally produce.

1. **Flat and nested truth:** an ordinary flat case remains compact with no nest chrome or duplicate Open. Real linked worktrees nest under their one real top-level checkout. Null, malformed, linked-only, and multiple-source provenance stay flat. Noncontiguous children, including a linked entry before its parent, do not reorder unrelated rows.
2. **Exact visibility and targets:** expanded Home shows every real workspace and terminal exactly once. The top-level current-terminal Open resolves only through active tab, exact layout, and focused pane and disappears when any step is ambiguous. Every ordinary terminal row retains its exact existing Terminal destination. The check ends after proving exact navigation and Home return; it makes no Terminal behavior assertion.
3. **Counts and disclosure:** only exact nonzero actual-agent totals appear, in `working`, `blocked`, `idle`, `done`, `unknown` order, with no inferred status. First-visit working/blocked nests expand and the rest collapse. Later manual state wins status changes. Expand/Collapse all works, and focus never becomes hidden.
4. **Attention and place:** Blocked shows only blocked terminal rows plus minimum context and omits management, disclosure, header Open, totals, and bulk collapse controls. Show all, Home resume, terminal return, and browser Back preserve remembered expansion, scroll/anchor, and surviving focus without a top flash or route retarget.
5. **Connection and publication:** Live has no internal refresh or resume flicker. Exercise initial load, valid updates, browser background/resume, relevant event/read races, Herdr disconnect and restart, the unchanged 45-second Offline threshold, the existing manual reconnect path, `Herdr is not running`, `Cannot use this Herdr`, coherent empty/no-terminals state, and invalid, malformed, ambiguous, conflicting, or duplicate joins. `Home is updating` appears at most once, partial data never publishes, and every Open disappears while Home is invalid.
6. **Hostile content:** hostile workspace, terminal, and agent labels and identifiers, and every other value eligible for display, render only as inert text and cannot create markup, grouping, route syntax or retargeting, requests, control messages, or authorization. Invalid status or provenance prevents the affected candidate or nest from publishing as required; it is never interpreted into authority.
7. **Proportionate tests and build:** useful Home-focused tests cover projection, ordering, exact joins, disclosure state, place, connection state, and hostile values without a large Herdr mock system. Run the focused Home tests, `npm run typecheck --prefix web`, `npm run build --prefix web`, and a production `go build` after the browser build. Do not add, change, run as slice evidence, or claim Terminal behavior tests. The worker supplies bounded real-Herdr Home evidence through the production path and the exact commit reviewed.

### Integrator pre-human gate

The integrator repeats the Home-focused automated and real-Herdr production checks from the integrated commit. On the installed Android Studio Pixel 8a virtual device, check portrait and landscape, 44-pixel touch targets, disclosure and bulk collapse controls, Blocked/Show all, scroll and visible-focus restoration, the stable badge without layout shift, and browser background/resume. Capture Home screenshots for the fresh Grok and human evidence where practical. Do not exercise or assess Terminal behavior. This emulator pass supports confidence; it is not acceptance.

### Final human phone gate

The final gate is human-run or human-witnessed on a real phone using the production Shepherdr executable and real configured Herdr 0.8.0 session. It assesses Home and exact navigation/return only, not Terminal behavior:

1. Compare a compact flat Home and real worktree organization with Herdr. Expand it and account for every workspace, terminal, exact status total, and Open destination.
2. Exercise first-visit defaults, a manual collapse across a status change, Expand all, Collapse all, and visible focus.
3. Scroll until N blocked is off-screen, open Blocked, use Show all terminals, then open a real terminal and return. Expansion, row, focus, and scroll must come back without jumping to the top.
4. Trigger ordinary live updates and background/resume; confirm a stable Live badge, one coherent Home, and no stale Open.
5. Stop/restart Herdr and interrupt connectivity through 45 seconds. Confirm the truthful badge, existing Reconnect when Offline, Home returning as one complete view, and no Open while Home is not current.
6. Rotate the phone. Confirm a readable compact Home, usable targets, and no layout jump. Hostile labels stay ordinary text, add no internal words, and cannot change where Open goes.

Human acceptance of this complete workflow gates the next slice. If an accepted real workflow breaks, stop later slice work and reproduce and understand the break first.

## Assumptions, risks, and open decisions

Assumptions are limited to the approved architecture: one configured local Herdr 0.8.0 session, sign-in off, confirmed snapshot/event/provenance fields, localhost serving through the existing trusted private-network arrangement, and the accepted current Terminal destination integrated at the preparation base.

Primary risks are shared-file overlap at `web/src/main.ts` and `web/src/style.css`, stale authority during rapid event/read races, accidental reordering while extracting noncontiguous children, hidden focus after collapse, and treating opaque hostile identity as display or route syntax. The ownership limits and gate above address these without expanding architecture.

There is no unresolved product or architecture decision inside this proposal. Human approval and the resulting pinned brief commit are still required before dispatch. Any implementation need outside these recorded decisions blocks the worker pending human direction.

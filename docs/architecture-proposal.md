# Shepherdr architecture proposal

Status: approved

Original base commit: `5a6746c`

Human-approved pane-first Home amendment: 2026-08-17

## Scope and smallest shape

This proposal covers the current loop: enter in the configured sign-in mode, see every real Herdr terminal under its workspace and tab, see agent metadata when present, find blocked agents, and open the selected terminal. Passkeys, device trust, and per-device blocked-agent notifications fit around that loop. Chat, messaging APIs, image sending, message notifications, attachments, conversation storage, placeholder Chat UI, and a second agent runtime are absent.

The smallest shape has three live components:

1. The Herdr server remains authoritative for workspaces, tabs, panes, layouts, agents, statuses, focus, PTYs, and processes.
2. One Shepherdr process binds to `localhost`, serves the built UI from the same origin, keeps an in-memory Herdr projection, and bridges terminal streams.
3. A phone browser runs the UI.

Publishing the localhost service through a trusted private network, such as Tailscale Serve, is an operator step outside Shepherdr. A small local store is added only for approved passkey, device, and notification features; the first slice has no durable store.

Optional Herdr worktree metadata does not establish workspace parents. Worktree or repository grouping, workspace nesting, and collapsibility remain undecided later work. If approved later, grouping is an additive outer layer over the canonical workspace/tab/pane model; the current amendment adds no parent, group, or collapse structure.

## Processes and trust boundaries

**Herdr server.** It owns runtime truth. Shepherdr never recreates a missing target or infers status from terminal output.

**Shepherdr server.** It alone talks to Herdr, serves the UI, provides same-origin live-state and terminal bridges, and applies the configured access policy before terminal control.

**Browser/device.** This is outside the machine trust boundary. With sign-in off, anyone who reaches Shepherdr has operator authority and there is no device-trust step. With passkeys on, sign-in and approved-device checks gate Home and terminal access.

**Private-network publisher and optional notification delivery service.** These are external boundaries. Shepherdr neither configures private-network publication nor supports public exposure. No notification vendor or mechanism is selected here.

Herdr output, names, IDs, terminal bytes, repository content, attachments, and pasted content are untrusted data. Ordinary text is escaped; terminal bytes go only to a terminal renderer. A pane ID may be encoded only as opaque data inside a fixed Shepherdr route template, decoded as data, joined by exact equality, and resolved against the current coherent pane set on the server. It conveys no authority and cannot define route syntax, markup, command syntax, authorization, grouping, application events, or label-based retargeting. Other displayed content cannot become any of those things. Shepherdr control messages are typed separately, and the server authorizes browser input before forwarding it.

## Confirmed Herdr interfaces

Shepherdr targets one configured Herdr session/socket. It checks protocol compatibility, attempts only these required confirmed interfaces, and fails plainly if any required interface is unavailable; no explicit capability query is assumed.

- `session.snapshot` bootstraps and reconciles workspaces, tabs, panes, layouts, agents, focus, version, and protocol. Opaque IDs come from responses and remain scoped to the configured Herdr session.
- `events.subscribe` supplies the confirmed families needed by the pane-first semantic view: `workspace.created`, `workspace.updated`, `workspace.metadata_updated`, `workspace.renamed`, `workspace.moved`, `workspace.reordered`, `workspace.closed`, `workspace.focused`; `tab.created`, `tab.closed`, `tab.renamed`, `tab.moved`, `tab.focused`; `pane.created`, `pane.closed`, `pane.updated`, `pane.moved`, `pane.exited`, `pane.agent_detected`, `pane.focused`; `layout.updated`; and one `pane.agent_status_changed` subscription for every pane in the subscription basis. Shepherdr preserves Herdr's `working`, `blocked`, `idle`, `done`, and `unknown` words.
- `herdr terminal session observe <target>` supplies a full rendered frame followed by live frames and closure.
- `herdr terminal session control <target>` adds `terminal.input`, `terminal.resize`, `terminal.scroll`, and `terminal.release`; `--takeover` replaces the current controller.

The browser bridge transports these local socket/NDJSON semantics without adding agent or terminal semantics. Terminal parsing never drives Home or notifications.

## Truthful Home, attention, and recovery

Herdr has neither a durable event cursor nor a confirmed snapshot/subscription fence. During bootstrap or recovery, Shepherdr remains **reconnecting** while it establishes `events.subscribe` and takes fresh `session.snapshot` reads. Snapshots and overlapping events form a tentative projection. When confirmed per-resource sequence or revision data can order an overlap, Shepherdr uses it. When events overlap without enough ordering evidence, contradict a snapshot, or cannot be applied coherently, Shepherdr discards the tentative projection and resnapshots. Only a coherent current view is published; this is convergence, not a claim of a race-free handoff or global revision.

The canonical entity set is the snapshot `panes` array, never the agents or layouts. Workspace, tab, pane, and live terminal IDs must be unique. Every tab references one existing workspace. Every pane references one existing tab and workspace, and that tab belongs to the same workspace. Each pane appears exactly once. An agent may decorate at most one pane, and its workspace, tab, pane, and terminal IDs must all match; only these agent records create identity, status, working count, blocked count, or blocked-filter membership. Pane, tab, and workspace status rollups never create agent state. Broken uniqueness, joins, invalid status, or duplicate decoration is incoherent and never publishes falsely live.

Layouts only validate and order canonical panes; they never create, remove, duplicate, hide, or gate one. Workspaces order by Herdr number then workspace ID. Tabs order by Herdr number then tab ID. A tab uses rectangle reading order only when exactly one layout is unzoomed, covers every canonical pane exactly once, references no other pane, and has usable rectangles: `y`, then `x`, then pane ID. An absent, partial, duplicate, ambiguous, or zoomed layout falls back to all canonical pane IDs in deterministic order. This fallback is stable, not a claim that opaque IDs carry human meaning.

Each relevant subscribed event invalidates the semantic reading and requires a fresh snapshot before publication rather than invented event-payload semantics. If a pane appears outside the subscription basis, Shepherdr discards the tentative view and restarts subscription plus snapshot reconciliation so that pane has status coverage before live publication. Once live, a subscription end, unknown or inconsistent event, protocol mismatch, or gap marks the projection stale and restarts reconciliation. Shepherdr never claims to replay a gap. A Shepherdr restart discards all projected state. A Herdr disconnect also closes terminal bridges.

The semantic Home projection excludes agent session references, raw terminal revision/output, and worktree keys or paths. The `50990e5` stability guarantee remains: unchanged snapshots and replayed events do not republish or rerender Home, and terminal output/revision churn cannot flicker it. A real semantic change produces one stable keyed update that preserves scroll and focus.

Home shows every canonical pane once under its workspace and tab. A single-terminal workspace is one full-row destination while retaining workspace-heading semantics. Multi-terminal and multi-tab structure appears only when it adds place. Empty zero-pane workspaces leave no dead headings. Home shows no agent counts, repeated cards, repeated **Open terminal** buttons, duplicate blocked list, or zero-blocked copy. The whole row opens and is at least 44 by 44 CSS pixels.

Agent identity and status are optional pane metadata. Ordinary terminals have no invented status and do not contribute to attention. The persistent attention area shows the working count. When blocked agents exist, **N blocked** opens a **Blocked** view that filters and reuses the same pane rows with workspace/tab context. **Show all terminals** restores the prior all-terminals scroll and focused row.

Each pane has one human title shared by Home, Terminal, blocked reuse, and accessibility. A flattened row uses its workspace title; other rows prefer useful pane/title text and fall back to **Terminal**. Neutral displayed-order ordinals apply only within two collision sets from the complete all-terminals order: same-named flattened workspaces across Home, and same-titled rows within one workspace and, when a tab heading is shown, that tab. Unique titles remain unnumbered; unrelated ordinary rows are never numbered together; opaque IDs and workspace numbers are not disambiguators. Blocked filtering reuses these titles and never recomputes their ordinals. Accessible output begins with **Open**, includes the same title and place, and retains visible secondary text and agent status.

Home's non-happy states are distinct:

- A successful coherent view with no canonical panes is **No terminals**: “Herdr is running, but nothing is open.” An ordinary terminal prevents this state.
- A browser unable to reach Shepherdr is **Offline**; prior values are covered by one clear last-known connection message, are visibly not live, have no open affordance, and are unavailable for live actions. Do not repeat a last-known badge on every row.
- A reachable Shepherdr server unable to connect to its configured Herdr socket shows **Herdr is not running**.
- Initial and recovery work remains **Reconnecting**, not an indefinite spinner or falsely live view.
- An incompatible Herdr remains a distinct **Cannot use this Herdr** state with technical details.

## Real terminal and missing targets

Home and notification links use the fixed Shepherdr terminal-route template with configured-session scope plus the opaque current pane ID as encoded data, never agent session/name, label, title, or terminal ID. The server decodes the pane ID as data and resolves it by exact equality against the current coherent pane set. A route survives agent exit or replacement while the same pane and terminal remain. A cross-workspace move creates a new pane ID, so the old route becomes **Terminal unavailable** with a path to current Home; Shepherdr never retargets by label.

The current projection generation and `terminal_id` are freshness checks only. An unexpected terminal-ID change within the same live generation ends the old stream and shows **Terminal unavailable** until the person returns to fresh Home and explicitly reopens. Frames are never spliced and a replacement is never silently accepted. Across an acknowledged disconnect or cold restart generation, a surviving pane ID may resolve to a fresh terminal ID and receives a new full frame. Terminal IDs are neither persisted nor deep-linked.

The terminal opens an observer and receives a new full frame. It acquires control when control is free. If control is occupied, it stays an observer and says **Controlled elsewhere · observing**. Only an explicit confirmed **Take over** action may use `--takeover`; opening the page never steals control. Its confirmation remains “Take control? The current controller will lose input.” Leaving sends `terminal.release` when possible.

Frame sequence numbers are connection-local. Recovery opens a new stream and uses its full frame rather than resuming an old sequence. The bridge preserves input, control keys, paste, selection, scrolling, resize, keyboard, and rotation behavior. Moving between Home and Terminal preserves the Home scroll position and focused row.

## Sign-in, device trust, and notifications

Startup configuration selects sign-in off or passkeys; there is no UI mode picker. Sign-in off opens Home and quietly shows “Sign-in is off.”

With passkeys on, Shepherdr's eventual local store may hold approved passkey credentials, device identities, revocations, and necessary security metadata. Device onboarding is conditional on the selected approval option: the server implements only that approved flow. Regardless of mechanism, an untrusted browser receives no Home or terminal authority merely by reaching the service or starting onboarding. Approval must come from an already trusted authority or an explicit action on the Herdr machine. Revocation is server-enforced, and a local reset command remains required. Herdr content can never affect passkey or trust state. Team accounts, roles, organizations, and lost-device recovery remain out of scope.

Blocked notifications are fed only by Herdr status. An observed transition to `blocked` supports “Agent is blocked,” not “new message” or an interpretation of the prompt. After delivery decisions, the store may contain each endpoint's preference, delivery subscription/token, credentials or material required by the chosen delivery mechanism, and minimal deduplication metadata. It stores no terminal output or durable Herdr projection.

In sign-in-off mode, a notification record identifies only a delivery endpoint. It is not a trusted-device credential and grants no application authority. In passkey mode, tapping a notification still requires authentication and device trust. The server then resolves the scoped pane and opens a fresh terminal; a disappeared target remains unavailable. With no durable Herdr cursor, offline notification recovery cannot prove when a block began.

## Durable state and assumptions

Durable Shepherdr state is limited to approved startup/security configuration, passkey credentials, device approval/revocation, notification preferences and delivery material, and minimal delivery deduplication. Herdr workspaces, tabs, panes, layouts, agents, statuses, counts, terminal IDs/frames, and event positions are rebuildable in-memory projections. There is no conversation or terminal-history store.

Assumptions:

- The initial product uses one configured local Herdr Unix socket on a platform supporting direct attach.
- Its protocol is compatible and all required confirmed interfaces work; otherwise Shepherdr fails plainly.
- A modern phone browser supports the chosen same-origin streams through the operator's private-network route.
- Herdr's integration-sensitive blocked classification is accepted as current Herdr truth.

Risks:

- Continuous topology/status churn may repeatedly invalidate tentative projections and prolong **reconnecting**. Shepherdr must prefer an honest delayed view over an unsupported ordering claim.
- Herdr IDs are not move-proof or guaranteed across every future protocol, so deep links can truthfully become unavailable.
- Mobile browsers and private proxies may interrupt long-lived streams; reconnect always loses transient terminal/event position.
- Passkeys and background notifications add security and external-delivery boundaries that require the decisions below.

## Recorded human decisions

- The current product uses one configured Herdr session. Multiple-session selection and namespacing are outside the current product.
- Home is pane-first: it represents every current pane terminal exactly once. Agent identity/status is optional pane metadata, and routes are pane-centric.
- A single-terminal workspace is one compact full-row destination while retaining workspace-heading semantics.
- Complete coherent unzoomed layouts order panes by `y`, `x`, then pane ID; all other layout cases fall back to every canonical pane in pane-ID order without affecting reachability.
- Blocked attention is a filtered reuse of the same pane rows with place restoration, not a duplicate list.
- A pane route survives agent exit/replacement while the pane and terminal remain. A cross-workspace move/new pane ID is unavailable.
- An unexpected same-generation terminal-ID change is **Terminal unavailable** until explicit reopen from fresh Home.
- Neutral displayed-order ordinals appear only for same-named flattened workspaces across Home or same-titled pane rows within their workspace/tab context. The disambiguated title is shared by Home, Terminal, blocked reuse, and accessibility; opaque IDs never appear.
- A terminal acquires control when control is free. When another controller holds it, the terminal remains an observer. **Take over** is an explicit, confirmed action; merely opening a terminal never steals control.

## Deferred human decisions

These decisions remain deliberately unresolved. They do not block the first slice. Passkey, device-trust, and notification design and implementation wait until the human returns to the relevant decisions.

1. **Passkey ownership.** Local WebAuthn keeps one service and local reset but makes Shepherdr responsible for credential security. A hosted identity service, including an Internet Computer option, adds vendor, privacy, network, and availability boundaries. **Recommendation:** local WebAuthn after focused security review.
2. **Stable WebAuthn origin/RP ID.** Options are one stable operator-configured private HTTPS name proxied to localhost, or an external hosted origin/identity service. Changing private addresses or names cannot provide one dependable credential scope. **Recommendation:** one stable operator-configured HTTPS name and RP ID; certificate and private-network publication remain operator concerns, not Shepherdr features.
3. **New-device approval.** Options are approval from an already trusted device, a one-time local CLI action on the Herdr machine, or both. Remote approval is convenient but needs replay-safe sessions; local approval is simpler but requires machine access. **Recommendation:** local CLI approval first.
4. **Passkey/device relationship.** A synced passkey is not a physical-device trust record. Options are a separate revocable device credential after approval, or treating passkey use as trusted. The latter violates the trust invariant. **Recommendation:** a separate revocable device credential.
5. **Notification delivery.** Standards-based Web Push uses browser push infrastructure; a hosted/native provider adds another vendor; foreground-only notifications do not complete the phone loop. **Recommendation:** Web Push, explicitly accepting browser-vendor infrastructure.
6. **Notification recovery.** Live-transition-only delivery misses downtime; snapshot reconciliation can repeat “Agent is blocked” and cannot date the transition. **Recommendation:** reconcile current blocked agents with bounded duplicate suppression and no “new” claim.

## Current first-slice boundary

The corrected Wave 01 brief continues with one worker for sign-in-off Home, attention, connection states, and real terminals for one configured Herdr session. No notifications or durable store enter the wave. Independent technical and language review, separately owned integration, and the human-run or human-witnessed real-phone gate remain required. The orchestrator records the exact aligned-document starting commit in dispatch after that document commit exists.

Later access and notification work remains outside Wave 01 and waits on deferred decisions 1–6.

## First-slice real acceptance gate

Use a real phone through an operator-configured private-network route, production Shepherdr bound to localhost, and a real compatible Herdr server with agent and ordinary panes. Focused fixtures may support malformed/collision cases that are unsafe or impractical to manufacture live, but they cannot replace this gate.

1. Start with sign-in off and notifications absent. Home opens directly, quietly shows “Sign-in is off,” and compares with one coherent `session.snapshot`: every canonical pane appears exactly once under the matching workspace/tab, including ordinary panes; exact agent statuses and working/blocked counts match; layouts create or hide none.
2. In workspace `w2S`, show and open both `w2S:p1` and ordinary `w2S:p2` (`./bin/shepherdr`) exactly once. Only `p1` has helper identity/status/attention. Each full frame and input targets only its own Herdr PTY.
3. Verify compact single-terminal rows and useful multi-terminal/multi-tab structure; 44-pixel targets; heading, keyboard and screen-reader navigation; conditional **current**; shared Home/Terminal/accessibility titles; visible secondary/status output; and Home↔Terminal scroll/focus restoration. Same-named flattened workspaces and same-titled rows within one workspace/tab gain only collision-set ordinals; unrelated unique rows remain unnumbered and no opaque ID appears.
4. `N blocked` opens the filtered reuse with workspace/tab context. **Show all terminals** restores prior scroll/focus. There is no zero-blocked copy, duplicate attention list, invented ordinary-terminal status, or alternate blocked status.
5. Open an agent and an ordinary terminal. Both observe first, acquire control only when free, show **Controlled elsewhere · observing** when occupied, and use **Take over** only after “Take control? The current controller will lose input.” Verify release, input, control keys, paste, selection, scroll, resize, rotation, on-screen keyboard, and fresh frames.
6. Exercise workspace/tab/pane creation, closure, rename, move and focus; agent detection/exit/status; and layout resize, swap, move and zoom. Every relevant event reconciles fresh state; a new pane has status subscription coverage before live publication; missing/partial/zoomed layout falls back without hiding a pane; terminal text drives neither Home nor attention.
7. Disconnect the phone and stop/restart Herdr. Verify one truthful last-known connection message without repeated row badges, non-openable stale rows, **Offline**, **Reconnecting**, **Herdr is not running**, incompatible, **No terminals**, and recovery with a fresh coherent view/full frame. A surviving pane re-resolves across a restart; missing/cross-workspace-moved pane IDs are unavailable and never recreated or label-retargeted.
8. Verify fixed pane routes resolve only encoded opaque pane-ID data by exact current server equality. An agent replacement in the same pane remains pane-targeted. An unexpected same-generation terminal-ID change ends at **Terminal unavailable** until explicit reopen from fresh Home; `terminal_id` never becomes route identity and frames never splice.
9. Replay existing-pane creation and repeated relevant events against unchanged snapshots. After the first live publication, there is no resubscribe loop, duplicate publication, full-Home rerender, flicker, focus loss, or scroll jump. One real semantic change yields one stable keyed update; terminal output/revision churn yields none.
10. Present representative hostile names, IDs, metadata, and terminal output. They remain inert text, opaque route data, or terminal bytes and cannot become markup, route syntax, commands, grouping, application events, authorization, control messages, or label-based retargeting.

Tests and fixtures support the evidence, but only this real phone-to-Herdr workflow accepts the slice.

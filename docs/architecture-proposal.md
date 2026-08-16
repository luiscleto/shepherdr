# Shepherdr architecture proposal

Status: approved

Base commit: `5a6746c`

## Scope and smallest shape

This proposal covers the current loop: enter in the configured sign-in mode, see Herdr workspaces and agents, find blocked agents, and open their real terminals. Passkeys, device trust, and per-device blocked-agent notifications fit around that loop. Chat, messaging APIs, image sending, message notifications, attachments, conversation storage, placeholder Chat UI, and a second agent runtime are absent.

The smallest shape has three live components:

1. The Herdr server remains authoritative for workspaces, panes, agents, statuses, PTYs, and processes.
2. One Shepherdr process binds to `localhost`, serves the built UI from the same origin, keeps an in-memory Herdr projection, and bridges terminal streams.
3. A phone browser runs the UI.

Publishing the localhost service through a trusted private network, such as Tailscale Serve, is an operator step outside Shepherdr. A small local store is added only for approved passkey, device, and notification features; the first slice has no durable store.

## Processes and trust boundaries

**Herdr server.** It owns runtime truth. Shepherdr never recreates a missing target or infers status from terminal output.

**Shepherdr server.** It alone talks to Herdr, serves the UI, provides same-origin live-state and terminal bridges, and applies the configured access policy before terminal control.

**Browser/device.** This is outside the machine trust boundary. With sign-in off, anyone who reaches Shepherdr has operator authority and there is no device-trust step. With passkeys on, sign-in and approved-device checks gate Home and terminal access.

**Private-network publisher and optional notification delivery service.** These are external boundaries. Shepherdr neither configures private-network publication nor supports public exposure. No notification vendor or mechanism is selected here.

Herdr output, names, terminal bytes, repository content, attachments, and pasted content are untrusted data. Ordinary text is escaped; terminal bytes go only to a terminal renderer. Displayed content cannot become markup, routes, authorization, commands, or application events. Shepherdr control messages are typed separately, and the server authorizes browser input before forwarding it.

## Confirmed Herdr interfaces

Shepherdr targets one configured Herdr session/socket. It checks protocol compatibility, attempts only these required confirmed interfaces, and fails plainly if any required interface is unavailable; no explicit capability query is assumed.

- `session.snapshot` bootstraps and reconciles workspaces, tabs, panes, layouts, agents, focus, version, and protocol. Opaque IDs come from responses and remain scoped to the configured Herdr session.
- `events.subscribe` streams resource lifecycle and `pane.agent_status_changed` events. Shepherdr preserves Herdr's `working`, `blocked`, `idle`, `done`, and `unknown` words.
- `herdr terminal session observe <target>` supplies a full rendered frame followed by live frames and closure.
- `herdr terminal session control <target>` adds `terminal.input`, `terminal.resize`, `terminal.scroll`, and `terminal.release`; `--takeover` replaces the current controller.

The browser bridge transports these local socket/NDJSON semantics without adding agent or terminal semantics. Terminal parsing never drives Home or notifications.

## Truthful Home, attention, and recovery

Herdr has neither a durable event cursor nor a confirmed snapshot/subscription fence. During bootstrap or recovery, Shepherdr remains **reconnecting** while it establishes `events.subscribe` and takes fresh `session.snapshot` reads. Snapshots and overlapping events form a tentative projection. When confirmed per-resource sequence or revision data can order an overlap, Shepherdr uses it. When events overlap without enough ordering evidence, contradict a snapshot, or cannot be applied coherently, Shepherdr discards the tentative projection and resnapshots. Only a coherent current view is published; this is convergence, not a claim of a race-free handoff or global revision.

Once live, a subscription end, protocol mismatch, or inconsistent event marks the projection stale and restarts reconciliation. Shepherdr never claims to replay a gap. A Shepherdr restart discards all projected state. A Herdr disconnect also closes terminal bridges.

Home groups the agents Herdr reports under their workspaces, shows exact statuses, counts current `working` agents, and filters current `blocked` agents for the attention path. Its non-happy states are distinct:

- A successful coherent view with no relevant resources is **empty**.
- A browser unable to reach Shepherdr is **offline**; prior values are labelled last known and live actions are unavailable.
- A reachable Shepherdr server unable to connect to its configured Herdr socket shows **Herdr is not running**.
- Initial and recovery work remains **reconnecting**, not an indefinite spinner or falsely live view.

## Real terminal and missing targets

Home and notification links carry Herdr-session scope plus the opaque current pane ID, never agent name or terminal ID. Each open or reconnect resolves that pane against current state. A missing, moved, closed, or replaced target becomes **terminal unavailable** with a path to current Home; Shepherdr never retargets by label. A cold-restored pane may preserve its pane ID but receives a new `terminal_id`, so terminal IDs are neither persisted nor deep-linked.

The terminal opens an observer and receives a new full frame. It acquires control when control is free. If control is occupied, it stays an observer and says input is controlled elsewhere. Only an explicit confirmed **Take over** action may use `--takeover`; opening the page never steals control. Leaving sends `terminal.release` when possible.

Frame sequence numbers are connection-local. Recovery opens a new stream and uses its full frame rather than resuming an old sequence. The bridge preserves input, control keys, paste, selection, scrolling, resize, keyboard, and rotation behavior.

## Sign-in, device trust, and notifications

Startup configuration selects sign-in off or passkeys; there is no UI mode picker. Sign-in off opens Home and quietly shows “Sign-in is off.”

With passkeys on, Shepherdr's eventual local store may hold approved passkey credentials, device identities, revocations, and necessary security metadata. Device onboarding is conditional on the selected approval option: the server implements only that approved flow. Regardless of mechanism, an untrusted browser receives no Home or terminal authority merely by reaching the service or starting onboarding. Approval must come from an already trusted authority or an explicit action on the Herdr machine. Revocation is server-enforced, and a local reset command remains required. Herdr content can never affect passkey or trust state. Team accounts, roles, organizations, and lost-device recovery remain out of scope.

Blocked notifications are fed only by Herdr status. An observed transition to `blocked` supports “Agent is blocked,” not “new message” or an interpretation of the prompt. After delivery decisions, the store may contain each endpoint's preference, delivery subscription/token, credentials or material required by the chosen delivery mechanism, and minimal deduplication metadata. It stores no terminal output or durable Herdr projection.

In sign-in-off mode, a notification record identifies only a delivery endpoint. It is not a trusted-device credential and grants no application authority. In passkey mode, tapping a notification still requires authentication and device trust. The server then resolves the scoped pane and opens a fresh terminal; a disappeared target remains unavailable. With no durable Herdr cursor, offline notification recovery cannot prove when a block began.

## Durable state and assumptions

Durable Shepherdr state is limited to approved startup/security configuration, passkey credentials, device approval/revocation, notification preferences and delivery material, and minimal delivery deduplication. Herdr topology, agents, statuses, counts, terminal IDs/frames, and event positions are rebuildable in-memory projections. There is no conversation or terminal-history store.

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
- A terminal acquires control when control is free. When another controller holds it, the terminal remains an observer. **Take over** is an explicit, confirmed action; merely opening a terminal never steals control.

## Deferred human decisions

These decisions remain deliberately unresolved. They do not block the first slice. Passkey, device-trust, and notification design and implementation wait until the human returns to the relevant decisions.

1. **Passkey ownership.** Local WebAuthn keeps one service and local reset but makes Shepherdr responsible for credential security. A hosted identity service, including an Internet Computer option, adds vendor, privacy, network, and availability boundaries. **Recommendation:** local WebAuthn after focused security review.
2. **Stable WebAuthn origin/RP ID.** Options are one stable operator-configured private HTTPS name proxied to localhost, or an external hosted origin/identity service. Changing private addresses or names cannot provide one dependable credential scope. **Recommendation:** one stable operator-configured HTTPS name and RP ID; certificate and private-network publication remain operator concerns, not Shepherdr features.
3. **New-device approval.** Options are approval from an already trusted device, a one-time local CLI action on the Herdr machine, or both. Remote approval is convenient but needs replay-safe sessions; local approval is simpler but requires machine access. **Recommendation:** local CLI approval first.
4. **Passkey/device relationship.** A synced passkey is not a physical-device trust record. Options are a separate revocable device credential after approval, or treating passkey use as trusted. The latter violates the trust invariant. **Recommendation:** a separate revocable device credential.
5. **Notification delivery.** Standards-based Web Push uses browser push infrastructure; a hosted/native provider adds another vendor; foreground-only notifications do not complete the phone loop. **Recommendation:** Web Push, explicitly accepting browser-vendor infrastructure.
6. **Notification recovery.** Live-transition-only delivery misses downtime; snapshot reconciliation can repeat “Agent is blocked” and cannot date the transition. **Recommendation:** reconcile current blocked agents with bounded duplicate suppression and no “new” claim.

## Suggested build split (not approved)

1. The orchestrator will pin the wave to the architecture-approval commit after this document is committed. One worker builds sign-in-off Home, attention, connection states, and real terminal for one configured Herdr session. No notifications or durable store. Independent review and a separately owned integration gate follow.

Later access and notification work is not split or planned here. It waits on deferred decisions 1–6.

The human/orchestrator must approve this proposal and create wave briefs with exact integrated bases, owners, overlap, checks, reviewer, integrator, and real gates.

## First-slice real acceptance gate

Use a real phone through an operator-configured private-network route, production Shepherdr bound to localhost, and a real compatible Herdr server and agent. Mocks cannot replace this gate.

1. Start with sign-in off and notifications absent. Home opens directly and shows “Sign-in is off.”
2. Compare Home with `session.snapshot`: resources and exact statuses match, the working count is correct, and attention leads to every current blocked agent.
3. Open a blocked agent from attention. The phone receives the real full frame and live changes; input, control keys, paste, selection, scroll, resize, rotation, and on-screen keyboard operate the Herdr-owned PTY.
4. Add another controller. The phone observes without stealing control, reports the conflict, and takes over only after explicit confirmation. Opening the terminal alone never steals control. Release permits another controller.
5. Change real statuses/topology. Home follows `events.subscribe`; terminal text drives neither status nor attention. Exercise all five statuses where the integration can produce them.
6. Disconnect the phone: it shows offline/stale state and disables live actions. Reconnect: Home reconciles fresh state and terminal receives a new full frame.
7. Stop Herdr while Shepherdr remains reachable: Home says Herdr is not running and terminal is not live. Restart Herdr: Shepherdr reconciles. A surviving pane is re-resolved; a missing/moved target is unavailable, never recreated or retargeted.
8. Verify explicit empty state. Present representative hostile terminal output and confirm it remains confined to the terminal rendering surface and cannot become Shepherdr markup, routing, control messages, or authorization state. This is a boundary check, not bespoke terminal-emulator security engineering.

Tests support the evidence, but only this real phone-to-Herdr workflow accepts the slice.

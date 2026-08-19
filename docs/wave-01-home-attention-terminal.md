# Wave 01: Home, attention, and real terminal

Status: approved, amended, ready for corrected dispatch

Original architecture base: `ddba830aeb14fd50e9830a9a87166d32595d48c1`

Pane-first correction base: `50990e55ec8cc4ef9f6f381e9387912b84b875a1`

Worker starting point: the exact commit containing this aligned approved brief and governing documents. The orchestrator records its SHA in the corrected dispatch after this document commit exists. Do not invent that future SHA here.

## Outcome

On a real phone, a person can open Shepherdr without sign-in, see every current real Herdr terminal exactly once under its workspace and tab, see agent metadata when present, find blocked agents, and open any selected terminal. Home and Terminal remain truthful through disconnects, Herdr restarts, disappearing targets, and ordinary panes without agents.

This wave is accepted only through the real-phone gate below. Tests support that evidence but do not replace it.

## Approved stack

The server is Go. The small browser UI is TypeScript. The production browser build is embedded in the Go executable.

An all-TypeScript application could also be packaged as a standalone executable with current runtimes, so standalone deployment is not unique to Go. Go is chosen because the Shepherdr server is primarily a small local systems service around Herdr streams, socket and process lifecycle, and an embedded web UI. TypeScript stays confined to the browser side.

This decision does not select frameworks. The worker may choose the smallest ordinary libraries and build tooling needed within the approved architecture and this brief. Any choice that would materially change architecture, deployment, trust boundaries, or product behavior returns to the human before work continues.

## Scope

One worker owns the complete slice:

- correct the candidate at `50990e5` without replacing its approved Go server, TypeScript browser, embedded production build, localhost-only service, one configured Herdr session/socket, or sign-in-off boundary;
- retain complete workspace, tab, pane, layout, focus, and optional agent data from `session.snapshot` and the confirmed relevant `events.subscribe` families;
- build one coherent in-memory pane-first Home projection and truthful recovery behavior without a durable store;
- keep the same-origin browser transport for live Home state and real terminal frames/input;
- provide compact mobile Home, agent-derived attention, pane-scoped terminal routes, observer/control/release/explicit takeover, and the accessibility behavior below;
- add focused tests and gather evidence for independent technical review, independent language/mobile review, integration, and the real-phone gate.

The first UI stays small and includes only the Home, attention, connection-state, and terminal behavior required by this wave. Worktree/repository grouping, workspace nesting, and collapsibility remain undecided later work and add no structure or controls here.

Home uses the snapshot `panes` array as the canonical set and represents every pane exactly once. Workspace, tab, pane, and terminal IDs are unique; workspace/tab/pane references agree; every agent record matches one pane's workspace, tab, pane, and terminal IDs; and only agent records create identity/status/attention. An incoherent view never publishes falsely live. Layouts order only: one complete coherent unzoomed layout uses `y`, `x`, then pane ID; an absent, partial, duplicate, ambiguous, or zoomed layout falls back to every canonical pane in pane-ID order and never hides one.

Subscribe to the confirmed workspace lifecycle/metadata/order/focus family; tab lifecycle/name/focus family; pane lifecycle/title/occupant/focus family; `layout.updated`; and one `pane.agent_status_changed` per pane in the subscription basis. Every relevant event causes a fresh snapshot before semantic publication. A new pane outside the basis restarts subscription/snapshot convergence so it has status coverage. Preserve the candidate's `50990e5` guarantee: unchanged snapshots or replayed events do not republish/rerender Home, and terminal output/revision churn does not flicker it.

Home shows optional agent identity and only Herdr's `working`, `blocked`, `idle`, `done`, and `unknown` status on panes with actual agent records. Ordinary terminals have no invented status and still open. The bottom area always shows the working count; it shows no zero-blocked copy. When blocked agents exist, **N blocked** opens a **Blocked** filtered reuse of the same terminal rows with workspace/tab context. **Show all terminals** restores all-terminals scroll and focus; there is no duplicated attention list or alternate status.

A single-terminal workspace is one full-row destination with workspace-heading semantics. Multi-terminal/multi-tab structure appears only when needed; a lone default tab is omitted and **current** appears only when several tabs require it. There are no agent counts, repeated cards, repeated **Open terminal** buttons, or dead zero-pane workspace headings. The whole row opens and is at least 44 by 44 CSS pixels.

One human title is shared by Home, Terminal, blocked reuse, and accessible output. Neutral displayed-order ordinals appear only for same-named flattened workspaces across Home, or same-titled terminal rows within one workspace and, when shown, tab. Compute both sets from the complete all-terminals view so blocked filtering never renumbers. Do not number unrelated rows, use opaque IDs, or number secondary text. Accessible names begin with **Open**, include the same title/place, and retain visible secondary text and status.

A quiet persistent note says “Sign-in is off.” Truthful states include initial/recovery **Reconnecting**, browser **Offline**, one last-known connection message with non-openable rows and no per-row badges, **Herdr is not running**, **Cannot use this Herdr**, **No terminals** / “Herdr is running, but nothing is open,” and **Terminal unavailable** / “This terminal is no longer here.” An ordinary terminal prevents the empty state. Shepherdr does not infer status from terminal output, replay a gap, recreate a target, or retarget by label.

The fixed terminal route carries configured-session scope plus encoded opaque pane ID data and resolves by exact equality against the current coherent pane set on the server. It never resolves by label, title, agent name/session, or terminal ID. `terminal_id` and projection generation are freshness only. Agent replacement in a surviving pane remains pane-targeted; cross-workspace/new pane ID is unavailable. Unexpected terminal-ID replacement within one live generation becomes **Terminal unavailable** until explicit reopen from fresh Home; frames never splice.

The terminal opens as an observer, receives a fresh full frame, and acquires control only when control is free. If another controller holds it, the phone says **Controlled elsewhere · observing**. **Take over** is separate and confirms with “Take control? The current controller will lose input.” Opening never steals control. Leaving releases when possible. Preserve input, control keys, paste, selection, scrolling, resize, on-screen keyboard, rotation, reconnect, and Home↔Terminal scroll/focused-row restoration.

## Excluded

Do not add a durable store, passkeys, device trust or device screens, notifications, Chat, messaging, image sending, attachments, conversation or terminal history, multiple Herdr sessions, public-network support, speculative provider behavior, worktree/repository grouping, workspace nesting, collapsibility, or structure intended only for later work.

Do not invent Herdr interfaces or add a second runtime. Do not derive Home state, attention, or application commands from terminal output. Herdr remains the runtime authority.

## Ownership and overlap

Continuing worker: `wave-01-home-terminal-worker` — remains the sole worker and owns all correction implementation and worker-side validation listed in Scope. It starts from the exact aligned-document commit recorded by the orchestrator in corrected dispatch and implements on top of candidate `50990e5`. There is no second worker and no concurrent product-code ownership.

Independent technical reviewer: `wave-01-home-terminal-reviewer` — reviews the exact worker commit against this brief and the approved documents. The reviewer does not fix findings and does not approve integration or release readiness.

Independent language/mobile reviewer: `wave-01-language-reviewer` — reviews the exact worker result for approved language, compact phone hierarchy, heading/target/accessibility behavior, collision-only titles, attention filtering, and truthful ordinary/stale/empty states. The reviewer does not fix findings and does not approve integration or release readiness.

Integration owner: `wave-01-home-terminal-integrator` — starts from the exact worker starting point recorded in the dispatch, integrates only a result approved by both reviewers, resolves composition issues within this wave, runs the integration checks, coordinates the human-run or human-witnessed real-phone gate, and reports the integrated commit. The integrator does not approve their own result.

Likely internal overlap is concentrated at the live-state boundary: Home recovery and terminal recovery share Herdr connection lifecycle; Home and Terminal share the same-origin browser connection, pane identity, titles, and place restoration; mobile navigation, keyboard, safe areas, and terminal sizing share the application shell. One worker owns all of these seams. Approved product and architecture documents are read-only during implementation.

## Worker handoff

The worker starts from the exact commit recorded in the dispatch, implements only this brief, commits with a conventional commit, and reports:

- the exact worker commit;
- the files and behavior changed;
- tests and checks run;
- real Herdr and phone evidence gathered before review;
- known limitations or missing decisions; and
- confirmation that excluded capabilities and speculative later-work structure were not added.

The orchestrator does not inspect product code as a substitute for the worker, technical reviewer, language/mobile reviewer, or integrator reports.

## Independent reviews

Both reviewers verify the exact worker commit from the exact worker starting point recorded in the dispatch. Technical review covers scope, coherent exact-once projection, confirmed Herdr interfaces/events, convergence and no-flicker behavior, pane route/freshness semantics, truthful recovery, terminal control, the untrusted-content boundary, and absence of excluded work. Language/mobile review covers compact hierarchy, shared/disambiguated titles, attention filtering, ordinary/stale/empty copy, heading and 44-pixel target semantics, screen-reader output, and place preservation. Findings name the violated brief or approved rule and the observed evidence.

Any material finding from either review returns to the same continuing worker. Both approvals are required before integration. Review approval means the worker result matches the brief; it is not integration approval or acceptance of the real workflow.

## Real-phone acceptance gate

Use the production Shepherdr path bound to `localhost`, an operator-configured trusted private-network route, a real compatible Herdr server with agent and ordinary panes, and a real phone. The human collaborator runs or witnesses the gate. Focused fixtures may support malformed or collision cases impractical to create live, but mocks cannot replace the real workflow.

1. Start with sign-in off and notifications absent. Home opens directly and quietly shows “Sign-in is off.” Compare it with one coherent `session.snapshot`: every canonical pane appears exactly once under the matching workspace/tab, including ordinary panes; exact agent statuses and working/blocked counts match; layouts create or hide none.
2. In workspace `w2S`, show and open both `w2S:p1` and ordinary `w2S:p2` (`./bin/shepherdr`) exactly once. Only `p1` has helper identity/status/attention. Each full frame and input targets only its own Herdr PTY.
3. Verify compact single-terminal rows and useful multi-terminal/multi-tab structure; 44-pixel targets; heading, keyboard and screen-reader navigation; conditional **current**; shared Home/Terminal/accessibility titles; visible secondary/status output; and Home↔Terminal scroll/focused-row restoration. Same-named flattened workspaces and same-titled rows within one workspace/tab gain only collision-set ordinals; unrelated unique rows stay unnumbered and no opaque ID appears.
4. `N blocked` opens the filtered reuse with workspace/tab context. **Show all terminals** restores prior scroll/focus. There is no zero-blocked copy, duplicate attention list, invented ordinary-terminal status, or alternate blocked status.
5. Open an agent and an ordinary terminal. Both observe first, acquire control only when free, show **Controlled elsewhere · observing** when occupied, and use **Take over** only after “Take control? The current controller will lose input.” Verify release, input, control keys, paste, selection, scrolling, resize, rotation, on-screen keyboard, and fresh frames.
6. Exercise workspace/tab/pane creation, closure, rename, move and focus; agent detection/exit/status; and layout resize, swap, move and zoom. Every relevant event reconciles fresh state; a new pane has status subscription coverage before live publication; missing/partial/zoomed layout falls back without hiding a pane; terminal text drives neither Home nor attention.
7. Disconnect the phone and stop/restart Herdr. Verify one truthful last-known connection message without repeated row badges, non-openable stale rows, **Offline**, **Reconnecting**, **Herdr is not running**, **Cannot use this Herdr**, **No terminals**, and recovery with a fresh coherent view/full frame. A surviving pane re-resolves across restart; missing/cross-workspace-moved pane IDs are unavailable and never recreated or label-retargeted.
8. Verify fixed pane routes resolve only encoded opaque pane-ID data by exact current server equality. Agent replacement in the same pane remains pane-targeted. An unexpected same-generation terminal-ID change ends at **Terminal unavailable** until explicit reopen from fresh Home; `terminal_id` never becomes route identity and frames never splice.
9. Replay existing-pane creation and repeated relevant events against unchanged snapshots. After first live publication, there is no resubscribe loop, duplicate publication, full-Home rerender, flicker, focus loss, or scroll jump. A real semantic change yields one stable keyed update; terminal output/revision churn yields none.
10. Present representative hostile names, IDs, metadata, and terminal output. They remain inert text, opaque route data, or terminal bytes and cannot become markup, route syntax, commands, grouping, application events, authorization, control messages, or label-based retargeting.

The gate fails on any missing/duplicate terminal, false success, invented status, global/noisy disambiguation, automatic takeover, stale state presented as live, flicker regression, or replacement of the real Herdr workflow with stand-ins.

## Integration and next-wave gate

After both independent approvals, the integrator applies the worker result to the exact worker starting point recorded in the corrected dispatch, runs relevant automated checks, and coordinates every real-phone acceptance step with the human running or witnessing it. The integrator reports the exact integrated commit and evidence without declaring their own work approved.

No later wave begins until the human accepts the integrated real-phone workflow. If this accepted workflow later breaks, feature work stops until the break is reproduced and understood.

# Wave 01: Home, attention, and real terminal

Status: approved, ready for dispatch

Architecture base: `ddba830aeb14fd50e9830a9a87166d32595d48c1`

Worker starting point: the commit containing this approved brief. The orchestrator records its exact SHA in the dispatch.

## Outcome

On a real phone, a person can open Shepherdr without sign-in, see the current workspaces and agents in one configured Herdr session, find blocked agents, and open the selected agent's real terminal. Home and terminal remain truthful through disconnects, Herdr restarts, and disappearing targets.

This wave is accepted only through the real-phone gate below. Tests support that evidence but do not replace it.

## Approved stack

The server is Go. The small browser UI is TypeScript. The production browser build is embedded in the Go executable.

An all-TypeScript application could also be packaged as a standalone executable with current runtimes, so standalone deployment is not unique to Go. Go is chosen because the Shepherdr server is primarily a small local systems service around Herdr streams, socket and process lifecycle, and an embedded web UI. TypeScript stays confined to the browser side.

This decision does not select frameworks. The worker may choose the smallest ordinary libraries and build tooling needed within the approved architecture and this brief. Any choice that would materially change architecture, deployment, trust boundaries, or product behavior returns to the human before work continues.

## Scope

One worker owns the complete slice:

- the minimum application and production-build structure needed to run one Shepherdr process on `localhost`;
- configuration for exactly one Herdr session/socket and sign-in-off mode only;
- the confirmed Herdr integration through `session.snapshot`, `events.subscribe`, terminal observe, terminal control, release, and explicit `--takeover`;
- the in-memory Home projection and its recovery behavior;
- the same-origin browser transport for live Home state and terminal frames and input;
- the mobile Home, persistent attention control, blocked-agent path, and real terminal interface;
- focused tests and the evidence needed for independent review and the real-phone gate.

The first UI stays small and includes only the Home, attention, connection-state, and terminal behavior required by this wave.

Home shows workspaces and agents using only Herdr's `working`, `blocked`, `idle`, `done`, and `unknown` statuses. It shows the current working count and a persistent path to all currently blocked agents. A quiet persistent note says “Sign-in is off.”

Truthful states are part of the slice: initial and recovery **reconnecting**, successful **empty**, browser **offline** with prior values marked last known and live actions unavailable, **Herdr is not running**, and **terminal unavailable** for a target that cannot be resolved. Shepherdr does not infer status from terminal output, replay an event gap, recreate a target, or retarget by label.

The terminal opens as an observer, receives a fresh full frame, and acquires control only when control is free. If another controller holds it, the phone remains an observer and clearly reports that state. **Take over** is a separate confirmed action. Opening the terminal never steals control. Leaving releases control when possible.

## Excluded

Do not add a durable store, passkeys, device trust or device screens, notifications, Chat, messaging, image sending, attachments, conversation or terminal history, multiple Herdr sessions, public-network support, or structure intended only for later work.

Do not invent Herdr interfaces. Do not derive Home state, attention, or application commands from terminal output. Herdr remains the runtime authority.

## Ownership and overlap

Worker: `wave-01-home-terminal-worker` — owns all implementation and worker-side validation listed in Scope. There is no second worker and no concurrent product-code ownership.

Independent reviewer: `wave-01-home-terminal-reviewer` — reviews the exact worker commit against this brief and the approved documents. The reviewer does not fix findings and does not approve integration or release readiness.

Integration owner: `wave-01-home-terminal-integrator` — starts from the exact worker starting point recorded in the dispatch, integrates only a reviewer-approved worker result, resolves composition issues within this wave, runs the integration checks, coordinates the human-run or human-witnessed real-phone gate, and reports the integrated commit. The integrator does not approve their own result.

Likely internal overlap is concentrated at the live-state boundary: Home recovery and terminal recovery share Herdr connection lifecycle; Home and terminal share the same-origin browser connection and target identity; mobile navigation, keyboard, safe areas, and terminal sizing share the application shell. One worker owns all of these seams. Approved product and architecture documents are read-only during implementation.

## Worker handoff

The worker starts from the exact commit recorded in the dispatch, implements only this brief, commits with a conventional commit, and reports:

- the exact worker commit;
- the files and behavior changed;
- tests and checks run;
- real Herdr and phone evidence gathered before review;
- known limitations or missing decisions; and
- confirmation that excluded capabilities and speculative later-work structure were not added.

The orchestrator does not inspect product code as a substitute for the worker, reviewer, or integrator reports.

## Independent review

The reviewer verifies the exact worker commit from the exact worker starting point recorded in the dispatch. Review covers scope, confirmed Herdr interfaces, truthful state transitions, terminal controller behavior, mobile usability requirements, the untrusted-content boundary, and absence of excluded work. Findings name the violated brief or approved rule and the observed evidence.

Any material finding returns to the same worker. Review approval means the worker result matches the brief; it is not integration approval or acceptance of the real workflow.

## Real-phone acceptance gate

Use the production Shepherdr path bound to `localhost`, an operator-configured trusted private-network route, a real compatible Herdr server and agent, and a real phone. Mocks cannot replace this gate. The human collaborator will run or witness the gate.

1. Start with sign-in off and notifications absent. Home opens directly and shows “Sign-in is off.”
2. Compare Home with `session.snapshot`: workspaces, agents, and exact statuses match; the working count is correct; attention leads to every current blocked agent.
3. Open a blocked agent from attention. The phone receives the real full frame and live changes. Input, control keys, paste, selection, scrolling, resize, rotation, and the on-screen keyboard operate the Herdr-owned PTY.
4. Add another controller. The phone observes without stealing control and reports the conflict. It takes over only after the person chooses **Take over** and confirms. Opening alone never steals control. Release permits another controller.
5. Change real statuses and topology. Home follows `events.subscribe`; terminal text drives neither status nor attention. Exercise all five Herdr statuses where the real integration can produce them.
6. Disconnect the phone. It shows offline or last-known state and disables live actions. Reconnect: Home reconciles fresh state and the terminal receives a new full frame.
7. Stop Herdr while Shepherdr remains reachable. Home says “Herdr is not running” and the terminal is not live. Restart Herdr: Home reconciles. A surviving pane is resolved again; a missing or moved target is unavailable and is never recreated or retargeted.
8. Verify the explicit empty state. Present representative hostile terminal output and confirm it remains confined to the terminal rendering surface and cannot become Shepherdr markup, routing, control messages, or authorization state. This is a boundary check, not bespoke terminal-emulator security engineering.

The gate fails on any false success, invented status, automatic takeover, stale state presented as live, or replacement of the real Herdr workflow with stand-ins.

## Integration and next-wave gate

After independent approval, the integrator applies the worker result to the exact worker starting point recorded in the dispatch, runs relevant automated checks, and coordinates every real-phone acceptance step with the human running or witnessing it. The integrator reports the exact integrated commit and evidence without declaring their own work approved.

No later wave begins until the human accepts the integrated real-phone workflow. If this accepted workflow later breaks, feature work stops until the break is reproduced and understood.

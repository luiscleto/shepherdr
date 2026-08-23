# Wave 03: per-browser notifications

Status: approved

Prepared from the approved notification architecture at `52e53e835085ce092ca8451140c10121393bd39a`. After this brief is approved and committed, the orchestrator records that new commit as the worker's exact starting point.

## Outcome

Add the smallest complete notification loop to the existing Shepherdr process:

- quietly offer notifications on the first eligible browser visit;
- keep settings separate for each browser or installed web app;
- default `blocked` and `done` on;
- allow any Herdr status plus workspace opened and workspace closed to be selected;
- send standards-based Web Push notifications that name the relevant Herdr workspace;
- open the exact current Terminal from a status notification, or fail honestly when it is gone; and
- keep notification settings available from Home and Terminal.

This wave uses sign-in-off mode. A browser that can already use Shepherdr may subscribe after an explicit tap. A push subscription does not become a trusted-device identity.

There is no inbox, unread count, banner, visible-page suppression, presence tracking, cross-device seen state, event history, retry queue, replay, or delivery claim. The first slice sends subscribed pushes even while Shepherdr is visible.

## One existing process

Keep the current Go server and embedded TypeScript browser UI. Add no process, database, general event system, or second Herdr subscription.

The current complete-snapshot loop remains the only Herdr observation path. Beside its successful publication point, compare consecutive complete snapshots from one uninterrupted connection:

- an existing agent changing into a selected Herdr status creates one candidate for that exact pane and terminal;
- a newly observed agent creates no status transition;
- newly present workspace IDs produce one **opened** summary for that comparison;
- missing workspace IDs produce one **closed** summary for that comparison; and
- the first snapshot after start, reconnect, an event gap, or an incompatible period is a silent baseline.

Do not inspect terminal output, interpret prompts, replay gaps, infer skipped statuses, or block Home while sending push. Use a small concurrency limit and request timeout; miss an event rather than building an unbounded or durable queue.

## Browser experience

On the first visit where Push is supported and permission is undecided, show one quiet invitation with **Turn on notifications** and **Not now**.

- Only **Turn on notifications** requests browser permission.
- Remember **Not now** in that browser so the invitation does not return on every visit.
- Do not show the invitation when permission is already granted or blocked, or when Push is unavailable.
- Clearing that browser's site data may make it eligible for the invitation again.

The invitation appears only after the operator has configured a valid VAPID contact. Without one, keep the Notifications settings action but show **Notifications aren't set up** and explain: **On the machine running Shepherdr, start it with `-vapid-contact` and a contact email or website.** Do not request permission or expose subscription controls until setup exists.

Home and Terminal each have one **Notifications** settings action. The settings explain that they apply only to this browser or installed app. The person can enable notifications, change individual event choices, or turn notifications off later. It is not an inbox.

Defaults for a new subscription:

- On: `blocked`, `done`.
- Off: `working`, `idle`, `unknown`, workspace opened, workspace closed.

Saving settings succeeds only after the server confirms it. On failure, keep the prior settings and say so plainly. If browser permission is blocked, explain that it must be changed in browser settings rather than prompting again.

Keep the accepted Home and Terminal appearance and behavior. Adding the settings action and exact notification destination must not redesign Terminal, change its reader or writer, disturb Home position, or add visible lifecycle states.

## Push, links, and private state

Use standard Web Push with a maintained Go library. If the library choice would materially change deployment, licensing, trust boundaries, or product behavior, return it for human decision.

Use one stable private HTTPS origin for phone subscriptions. Shepherdr remains bound to localhost; publishing it through the private network remains an operator action. Agents must not start, stop, or reconfigure Tailscale.

Store the operator's VAPID contact, one VAPID key pair, and the minimum subscription and event settings for each browser in one versioned, atomically replaced file outside the repository. Its parent directory and file are readable and writable only by the Shepherdr operating-system user. Create it when the operator first configures push and keep the key stable across ordinary restarts. The VAPID public key and operator contact are browser-visible; the private key and subscription authentication values are secrets. Do not store device names, Herdr snapshots, events, notification bodies, history, seen state, or delivery results. A corrupt or unreadable store disables notifications without breaking Home or Terminal and without silently replacing its key.

Add `-vapid-contact` to normal startup. It accepts only a real `mailto:` or HTTPS URI, saves it, and starts Shepherdr. An invalid value stops startup with a plain error. Later starts reuse the saved value. Supplying a different valid value updates only the contact; it does not rotate the VAPID pair or remove subscriptions. The README explains that each open-source installation supplies its own operator contact and that browser push providers receive it.

Subscriptions belong to the Shepherdr deployment. They survive Shepherdr and machine restarts and continue if the operator later selects another Herdr socket. That new connection still begins with a silent baseline; only subsequent observed changes notify. Do not add session-binding or migration machinery.

Provide one local `-reset-notifications` command-line action. The operator stops Shepherdr before running it. It clears every stored subscription, the VAPID pair, and the saved contact, reports the result, and exits without starting the server. Push remains unavailable until the operator supplies `-vapid-contact` again, and browsers must then explicitly enable notifications again. It must affect no other Shepherdr state.

Subscription endpoints and keys are secrets and untrusted input. Keep them out of logs and source control. Require same-origin settings requests, strictly validate sizes and shapes, and ensure outbound push requests cannot reach local, private, link-local, or private-network addresses through redirects or name resolution. Use short request and response limits. Do not implement Web Push encryption or VAPID signing by hand.

Include the relevant Herdr workspace name in every status, workspace-opened, and workspace-closed notice when one usable name identifies the event. Use the existing generic wording otherwise. Treat that name only as untrusted display text. Do not include agent names, repository or path data, terminal content, output, prompts, or other content. Use a five-minute push expiry. One accepted request means only that the push service accepted it; do not say it was delivered. Do not retry an accepted, timed-out, or ambiguous send.

A status payload contains only its event kind, Herdr workspace display name, exact Herdr status, minimum opaque pane and terminal identifiers, and a same-origin relative destination. A notification click reuses the existing application startup, connection handling, and exact Terminal validation, with no notification-specific wait or timeout. The display name never selects the destination. Open Terminal only when those identifiers resolve in current state. Otherwise use the existing **Terminal unavailable** path back to Home. Workspace notifications open Home.

## Platform behavior

Provide the small web app manifest, icons, and push-only service worker required by standard browser notification support. The service worker has no fetch handler, offline cache, or background application sync.

Android Chrome is the required real-phone path and must not require installation. iOS and iPadOS support follows the approved standards-based design, including the 16.4-or-later Home Screen requirement and permission after a direct tap, but a real Apple device does not gate this wave when none is available. Do not claim Apple-device acceptance until it is exercised.

## Worker ownership

Worker `wave03-notifications-worker` owns this end-to-end slice:

- the small snapshot-transition evaluator;
- notification state and VAPID lifecycle;
- strict same-origin subscription and settings routes;
- bounded Web Push sending and endpoint safety;
- manifest and push-only service worker;
- the first-visit invitation and settings reached from Home and Terminal;
- exact notification-click routing and honest unavailable behavior;
- a few focused tests; and
- concise operator documentation needed to enable and reset notification state.

Likely overlap is limited to the Home masthead, Terminal header, application startup, routing, embedded assets, and server configuration. The worker must not alter Terminal rendering, control, sending, history, lifecycle, or tests except for a focused assertion that the Notifications settings action and notification destination do not disturb them.

Do not add authentication, trusted-device management, public hosting, agent names or content beyond the approved workspace display name in pushes, a push-vendor account, provider-specific logic, a general event model, speculative later-work structure, or changes to Herdr.

## Tests and worker evidence

Use a few focused tests for:

- exact contiguous-snapshot status and workspace differences;
- silent initial and post-gap baselines;
- per-browser defaults and independent settings;
- first-visit invitation and remembered **Not now**;
- missing, valid, invalid, persisted, and updated VAPID contact behavior;
- strict subscription input, secret handling, and outbound endpoint safety;
- persistent settings and VAPID identity across restart;
- workspace-named payloads with honest generic fallback, five-minute expiry, no automatic retry, and exact links; and
- removal of an expired subscription only after a definitive gone response, never after a timeout or ambiguous result;
- the local reset action and required browser re-enablement; and
- honest stale-target failure.

Do not build a fake push service or a large fake Herdr. Browser tests use only the repository's memory-capped test entry and compare primitive values rather than live DOM objects. The worker runs the focused Go tests, capped browser tests once, TypeScript typecheck/build, production Go build, and a bounded real-Herdr smoke check. It reports exact commands, results, and commit SHA. It does not merge or approve its own work.

## Review and integration

After the worker commits, fresh reviewer `wave03-notifications-reviewer` checks that exact result against this brief. It verifies simple code, truthful transition boundaries, subscription and VAPID secrecy, outbound-request safety, exact links, restart behavior, bounded work, useful tests, and no Home or Terminal regression. Findings return to the worker; the reviewer does not fix them.

After technical approval, fresh language and interface reviewer `wave03-notifications-language-reviewer` checks the invitation, settings, permission denial, notification text, Terminal-unavailable path, touch targets, and visual fit. It does not redesign accepted Home or Terminal. Material findings return to the worker.

Only after both reviews approve, integrator `wave03-notifications-integrator` starts from the exact commit containing this approved brief and integrates the exact worker commit. The integrator runs the focused production build, existing Home and Terminal smoke gates, the Android emulator interface check where useful, and real-Herdr transition checks. It does not claim push delivery from an emulator or repeat unrelated broad QA.

Integration is not product acceptance.

## Human gate

The human supplies the running private HTTPS route and witnesses the real Android phone checks. Agents do not operate Tailscale.

1. Start without a VAPID contact. Confirm Home and Terminal work, no invitation appears, and Notifications settings explain the local `-vapid-contact` setup. Stop Shepherdr, start it with a valid operator contact, then confirm the quiet first-visit invitation appears on Android Chrome. Choose **Not now**, refresh, and confirm it stays dismissed while settings remain reachable from Home and Terminal.
2. Open settings without triggering permission. Then deny the explicit permission request and confirm Shepherdr explains that notification permission must be changed in browser settings without prompting again.
3. Restore permission in Android browser settings, enable Android Chrome through an explicit tap, and confirm `blocked` and `done` start on while every other event starts off.
4. On Android Chrome, with Shepherdr in the background, drive real agents into `blocked` and `done`. Confirm notifications name the correct Herdr workspace and open the exact current Terminal. Repeat once while Shepherdr is visibly open and confirm the subscribed push still appears.
5. Independently enable and exercise `working`, `idle`, `unknown`, workspace opened, and workspace closed. Confirm one summary for a workspace burst and no notification for the initial snapshot.
6. Make a notified terminal stale before tapping. Confirm **Terminal unavailable** and the route to Home, with no fallback terminal.
7. Restart Shepherdr and interrupt/recover Herdr. Confirm settings survive, recovery is a silent baseline, and Home and Terminal still behave as accepted.
8. Configure a second browser or profile differently. Confirm settings remain independent and turning off one subscription does not change the other.
9. With disposable notification state, stop Shepherdr, run the local reset action, and restart it. Confirm the contact, keys, and subscriptions are gone; Home and Terminal remain usable; settings explain setup; and every browser must enable again after the operator configures a contact.
10. Keep a phone offline beyond five minutes and confirm Shepherdr makes no replay, history, read-state, or delivery claim.

Human acceptance gates the next slice.

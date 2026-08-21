# Notification architecture proposal

Status: approved
Base: `ebfab5aad645d0c54628573762b4f0d8680b022d`

## Smallest approved first slice

Use standard Web Push from the existing Shepherdr process. Each browser or installed web app has its own settings and push subscription. `blocked` and `done` are on by default; a person can also turn on `working`, `idle`, `unknown`, workspace opened, and workspace closed.

On the first eligible browser visit, Shepherdr quietly offers **Turn on notifications** and **Not now**. This is not a browser permission request; only the enable tap requests permission. Shepherdr remembers **Not now** for that browser.

Home and Terminal each offer one **Notifications** settings action afterward. It can enable notifications, change event choices, or turn notifications off. Opening settings does not itself ask for browser permission.

Lock-screen text is generic, for example **A workspace needs attention** or **A workspace finished**. A status notification links to the exact Terminal identified when the transition was observed. Shepherdr opens it only if that same terminal still exists in the current Herdr snapshot. Otherwise it shows the existing **Terminal unavailable** state with a way back to Home; it never guesses another terminal. Workspace notices open Home.

The first slice has no push history, inbox, banners, unread badge, visible-page suppression, presence, cross-device seen state, retry queue, or replay. It sends one best-effort push for each subscribed event it can truthfully observe.

## Why visible-page suppression should wait

The requested rule is clear: if an event is visibly presented in an active Shepherdr page in the same browser or device, do not push to that browser or device. The portable way to meet it is not to receive a push and silently discard it in the service worker. Web Push subscriptions require user-visible notifications, and WebKit states that a received push must result in a displayed notification.

Shepherdr would need to present the event in the page, receive a browser-specific visible acknowledgement, wait for it for a bounded time, and only then decide whether to push. That adds event correlation, presence, timing, and late-acknowledgement rules. “Page is open” is insufficient: the tab may be hidden, frozen, disconnected, or showing another workspace.

Approved direction: make suppression an optional later slice after banners and their visible acknowledgement are designed together. The first slice always sends subscribed push, even while Shepherdr is visible. Cross-device read suppression is a separate feature and remains out of scope.

Sources: [W3C Push API](https://www.w3.org/TR/push-api/), [Service Workers](https://w3c.github.io/ServiceWorker/), [Notifications API](https://notifications.spec.whatwg.org/), and [WebKit Web Push](https://webkit.org/blog/12945/meet-web-push/).

## Confirmed capabilities and limits

| Label | Evidence and consequence |
| --- | --- |
| **Confirmed — Shepherdr** | Shepherdr is one loopback-only Go process serving the embedded browser application and talking to one Herdr session. Home maintains the latest complete snapshot; Terminal already validates an exact pane and terminal before opening it. Notifications can stay in this process and reuse those current views. |
| **Confirmed — Herdr** | Herdr 0.8.0/protocol 19 exposes complete snapshots, subscription signals, exact pane and terminal identifiers, agent statuses, and `state_change_seq`. The status words are `working`, `blocked`, `idle`, `done`, and `unknown`. |
| **Confirmed — no Herdr replay** | The inspected Herdr schema exposes no replay cursor, durable notification stream, or delivery history. Subscription signals can prompt a fresh complete read but cannot reconstruct events missed while Shepherdr was stopped or disconnected. `state_change_seq` shows change, not what skipped values meant. |
| **Confirmed — browser platform** | Push and service workers require a secure context. `localhost` is treated specially on the same machine, but an ordinary private-network HTTP address is not enough for a phone. Notifications require browser permission and may also be disabled by operating-system settings. |
| **Inferred — identifier lifetime** | Exact Herdr pane and terminal identifiers are suitable for a link checked against the current snapshot, but are not a durable identity across a cold Herdr restart. A stale link must fail honestly. |
| **Unknown — delivery** | A push service accepting a message does not prove that the device displayed it. Browsers and operating systems may delay, collapse, reorder, or drop notifications. Shepherdr must not claim delivery or success. |

These labels come from the repository, live Herdr CLI and schema, and linked standards. `docs/herdr-integration-discovery.md` remains evidence, not product direction.

## Events Shepherdr can report truthfully

Shepherdr compares consecutive, complete, validated Herdr snapshots during one uninterrupted observation period.

| Setting | Notification event |
| --- | --- |
| `working`, `blocked`, `idle`, `done`, `unknown` | The same exact pane and terminal has an agent in both snapshots, and its status changed into the selected value. A newly observed agent has no known prior status, so its first value is not a transition. |
| Workspace opened | A workspace identifier is absent from one complete snapshot and present in the next. Say **A workspace opened.** Shepherdr cannot claim that it was newly created or explain how it was opened. |
| Workspace closed | A workspace identifier is present and then absent. Say **A workspace closed.** Shepherdr cannot claim that a checkout, branch, or files were deleted. |

The first valid snapshot after Shepherdr starts, Herdr reconnects, or an event gap is only a new baseline. It sends nothing. This prevents false transitions but means real changes during the gap may be missed. Likewise, two complete reads may skip a fast intermediate status; Shepherdr does not invent it.

For a burst of workspace openings or closings observed in one comparison, send one summary rather than a notification per workspace. Status transitions remain per exact terminal because each has a useful destination. Terminal output, messages, prompts, focus changes, renames, and ordering changes are not notification events.

## Per-browser settings and permission

Settings belong to a browser profile or installed Home Screen app at one origin, represented by its push subscription. They are not an account, hardware identity, or trusted-device system. Two profiles or origins may receive duplicates by design.

Defaults for a new subscription are:

- On: `blocked`, `done`.
- Off: `working`, `idle`, `unknown`, workspace opened, workspace closed.

Settings take effect after the server confirms them; on failure, the previous settings remain. Turning notifications off removes the server record and asks the browser to unsubscribe. Clearing browser data can leave a stale record, removed when the push service definitively says the subscription is gone.

The quiet invitation appears once when push is supported, permission is undecided, and that browser has not chosen **Not now**. Remember the choice locally; it is not an account, device identity, inbox state, or unread state. Clearing site data may make the browser eligible again.

Do not invite when push is unsupported or permission is granted or blocked. Settings show the truthful state and available action. If blocked, say **Notifications are blocked in this browser. Allow them in browser settings.** Only **Turn on notifications**, in the invitation or settings, requests permission. Do not repeatedly prompt or imply Shepherdr can change browser settings.

## HTTPS, Android, and iOS

A phone needs one stable private HTTPS origin. A private-network tool may provide it and terminate TLS; for example, Tailscale Serve can expose a local service through an automatically provisioned HTTPS name. This does not commit to a network or push vendor. Moving origins creates separate settings and subscriptions.

On Android, Chrome can receive standards-based site notifications without installing the site. Browser or Android settings can block them, unused permissions may be removed, and background delivery is best effort.

On iPhone and iPad, Web Push requires iOS/iPadOS 16.4 or later and a web app added to the Home Screen. Permission must follow a direct user tap. Shepherdr therefore needs a suitable web app manifest, stable start location, icons, and a service worker. It does not need an Apple developer membership. Say **Add Shepherdr to your Home Screen, then open it there** when the feature is unavailable in a normal browser tab. The service worker does not require an offline cache, fetch handler, or second application runtime.

Sources: [secure contexts](https://w3c.github.io/webappsec-secure-contexts/), [Web Push on iOS and iPadOS](https://webkit.org/blog/13878/web-push-for-web-apps-on-ios-and-ipados/), [Chrome Android notification settings](https://support.google.com/chrome/answer/3220216?co=GENIE.Platform%3DAndroid&hl=en), and [Tailscale Serve](https://tailscale.com/docs/reference/tailscale-cli/serve).

## Push flow, trust, and private state

After the explicit tap, the browser registers the same-origin service worker, requests permission, creates a Push API subscription, and sends it with the selected events to Shepherdr. The existing process observes snapshot differences, encrypts a small Web Push/VAPID payload, and sends it to the subscription endpoint. The service worker displays it and handles a click. The standards are [RFC 8030](https://www.rfc-editor.org/rfc/rfc8030), [RFC 8291](https://www.rfc-editor.org/rfc/rfc8291), and [RFC 8292](https://www.rfc-editor.org/rfc/rfc8292).

When sign-in is off, anyone who can reach Shepherdr already has operator authority under the approved trust model. Such a browser may subscribe only after an explicit tap. This does not establish device trust. It does mean the push service may continue delivering generic alerts after the device leaves the private network, so the operator must be willing to grant that continuing access.

Subscriptions belong to this Shepherdr deployment, not to one Herdr session identity. They survive Shepherdr and machine restarts and continue if the operator starts the deployment against another Herdr socket. Each start or reconnect still begins from a silent snapshot baseline, so only later observed transitions notify. The first slice adds no session-binding or migration system; the operator can clear all notification state before or after repointing the deployment.

Store one owner-readable local state file outside the repository: the VAPID key pair and each subscription's endpoint, browser keys, optional expiry, and event selections. Replace it atomically. Do not store snapshots, history, device names, seen state, or delivery claims.

The VAPID private key and subscription authentication values are secrets. The VAPID public key is intentionally browser-visible; the private key and subscription secrets are not. Keep secrets out of logs, source control, browser responses, and unprotected backups. Keep the VAPID key stable across restarts. With Shepherdr stopped, a local reset command clears every subscription and the VAPID pair, then exits. Browsers must enable notifications again. If state is corrupt or unreadable, disable notifications without breaking Home or Terminal and report the error instead of creating a new identity.

A subscription endpoint is an untrusted URL and a server-side request boundary. Accept only valid HTTPS endpoints, reject embedded credentials and fragments, do not follow redirects, resolve and reject local, private, link-local, and tailnet destinations, defend against DNS rebinding, and use tight request size and time limits. Use a maintained Web Push library for encryption and signing. These controls avoid turning subscription enrollment into SSRF while remaining independent of a particular push vendor.

Payloads stay generic and contain only the event kind plus the minimum opaque identifiers needed for a relative same-origin destination. Do not include workspace names, repository paths, terminal text, agent output, or messages. Push encryption protects payload content in transit, but the push provider still sees timing, frequency, and size, and the operating system may show text on a lock screen.

Use a five-minute time to live. Do not retry after acceptance, timeout, or an ambiguous result; a retry queue would add duplicates and imply reliability the system does not have. A push-service acceptance response means only accepted for possible delivery. Restarts, reconnects, permission changes, expired subscriptions, and offline phones can cause missed events. Duplicate delivery is also possible. The interface should promise neither notification history nor offline replay.

## Approved decisions

1. **Always send subscribed push in the first slice, even when Shepherdr is visible.** Suppression waits for acknowledged banners and a bounded wait.
2. **Allow any browser already able to use sign-in-off Shepherdr to subscribe after an explicit tap.** This does not establish device trust.
3. **Use generic notification text.** Keep sensitive content off lock screens and away from push providers.
4. **Use a five-minute TTL.** Prefer current signals to stale alerts.
5. **Send one summary per workspace-opened or workspace-closed burst.** Make no stronger lifecycle claim.
6. **Use one stable private HTTPS origin for phone settings.** Another origin is another browser installation.
7. **Omit the optional VAPID contact unless the operator configures a real one.** Never ship a placeholder.
8. **Keep the persistent Notifications control settings-only, with no unread badge.** The invitation is separate and temporary; no history or seen state exists.
9. **Offer one quiet invitation on the first eligible browser visit.** Provide **Turn on notifications** and **Not now**, remember **Not now** locally, and request permission only after the enable tap. Do not invite when unsupported, granted, or blocked.
10. **Gate the first slice on a real Android phone plus a second browser or profile.** Keep standards-based iOS support, but do not require an unavailable iPhone or claim untested acceptance.
11. **Keep subscriptions deployment-bound.** They survive restarts and a changed Herdr socket; each new connection starts from a silent baseline. A local reset command clears all notification subscriptions and keys.

These product and security choices are approved direction.

## Risks and exclusions

Risks include missed or duplicate alerts, stale links, permission removal, lock-screen metadata, leaked secrets, SSRF, and confusing a subscription with device trust. The design cannot make Web Push reliable or prove delivery.

Excluded are Chat or message notifications, passkeys, device-trust implementation, team accounts, hosted identity, public hosting, a push-vendor commitment, a second runtime, terminal parsing, a conversation store, a general event system, push history, delivery receipts, cross-device seen state, reliable offline replay, retry queues, offline application caching, and Herdr changes.

An inbox and dismissible banners should wait. Without stored history an inbox would be misleading; with stored history it would introduce retention, seen-state, deletion, and multi-browser questions. Banners become useful when the later suppression protocol is designed, not as an unrelated first-slice ornament.

## Suggested implementation and acceptance gate

Use one worker for the end-to-end slice in the existing process, followed by independent review of the browser flow, security boundary, and user language. Keep visible-page suppression as a separately approved follow-up.

Gate the slice with real Herdr, a real Android phone, and a second browser or browser profile, not only browser mocks:

- On Android Chrome over the stable private HTTPS origin, verify the first-visit invitation, **Not now** memory, explicit enable tap, denial guidance, and notification settings.
- Confirm a fresh browser defaults to `blocked` and `done`, and that each status plus workspace opened/closed can be independently selected without changing a second browser.
- Drive real Herdr transitions and workspace changes. Verify exact generic text, one workspace-burst summary, and no notification from the initial snapshot or after a reconnect baseline.
- Tap a current status notice and reach the exact Terminal. Then make the identifier stale and verify **Terminal unavailable** and the Home fallback, with no guessed terminal.
- Restart Shepherdr and confirm settings and subscriptions remain. Reset notification state and confirm browsers must enable again, with Home and Terminal still usable.
- Put a phone offline beyond the five-minute TTL and confirm the product makes no history, replay, read-state, or delivery-success claim. Allow a duplicate if the platform produces one.

iOS and iPadOS support remains required to follow the standards and constraints above, including the Home Screen requirement. A real iPhone or iPad is not a gate for this wave when none is available. Record iOS real-device acceptance only after it is actually exercised.

Passing shows the smallest useful loop works on its intended deployment; it does not claim guaranteed delivery.

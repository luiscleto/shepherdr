# Notification architecture

Status: implemented technical direction

## Current behavior

Shepherdr sends standard Web Push from its existing process. Notification settings belong to one browser profile or installed app at one origin, represented by its push subscription. Another profile, installed app, or origin has separate settings and may receive the same event.

`blocked`, `done`, trusted sign-in added, and trusted sign-in removed start on. `working`, `idle`, `unknown`, workspace opened, and workspace closed start off. Settings changes take effect only after the server confirms them; a failure leaves the previous choices in place. Turning notifications off removes the server record and asks the browser to unsubscribe. A push service's definite gone response removes a stale server record.

The first eligible visit quietly offers **Turn on notifications** and **Not now**. Only **Turn on notifications** requests browser permission. The offer appears when push is supported, permission is undecided, an operator-supplied VAPID contact exists, and that browser has not chosen **Not now**. That choice is stored in the browser, so clearing site data may make it eligible again. Shepherdr does not offer permission when push is unsupported, already granted, or blocked. When blocked, settings say **Notifications are blocked in this browser. Allow them in browser settings.**

Without a configured VAPID contact, Home and Terminal continue normally, the offer stays hidden, and Notifications settings explain how the operator can enable push. Home and Terminal both open the same **Notifications** settings, where the browser can enable or turn off notifications and choose events. Opening settings never requests permission.

Shepherdr reports events only from consecutive complete, validated Herdr snapshots during one uninterrupted observation period:

| Setting | Notification event |
| --- | --- |
| `working`, `blocked`, `idle`, `done`, `unknown` | The same exact pane and terminal has an agent in both snapshots, and its status changes into the selected value. A newly observed agent has no known transition. |
| Workspace opened | A workspace identifier is absent and then present. The notice does not claim the workspace was newly created. |
| Workspace closed | A workspace identifier is present and then absent. The notice does not claim that a checkout, branch, or files were deleted. |
| Trusted sign-in added | Protected access commits a new trusted passkey. Other selected subscriptions may name its untrusted display label; the new trust does not notify itself before owning a subscription. |
| Trusted sign-in removed | Protected access commits revocation of a passkey. Its subscriptions are removed; other selected active subscriptions may name its untrusted display label. |

The first valid snapshot after startup, reconnect, or an event gap is a silent baseline. Real changes during that gap and fast intermediate statuses may be missed. Terminal output, prompts, focus changes, renames, and ordering changes are not notification events.

One comparison that observes several workspace openings or closings sends one summary for that burst, not one notice per workspace. Status transitions remain per exact terminal because each has a useful destination.

Notification text names the relevant Herdr workspace when its name is usable and otherwise uses generic wording. A status notice opens only the exact terminal identified at the transition and only if it is still current. Otherwise Shepherdr shows **Terminal unavailable** with a way back to Home. Workspace and trusted-sign-in notices open Home. Workspace and device labels are untrusted display text and cannot choose a route, target, control, or authority.

There is no push history, inbox, banner, unread badge, visible-page suppression, presence, cross-device seen state, retry queue, or replay. Shepherdr sends subscribed push even while a page is visible.

## Browser and platform boundaries

Push and service workers require a secure context. A phone therefore needs one stable private HTTPS origin; moving to another origin creates separate browser settings and subscriptions. A private-network TLS service such as Tailscale Serve can provide that origin without making Shepherdr public or committing the product to a network or push vendor.

On Android, Chrome can receive standards-based site notifications without installing the site. Browser or operating-system settings can block them, unused permission may be removed, and background delivery remains best effort.

On iPhone and iPad, Web Push requires iOS or iPadOS 16.4 or later and a web app added to the Home Screen. Permission must follow a direct user action. The current manifest, start location, icons, and service worker support that path without an offline cache, fetch handler, Apple developer membership, or second application runtime. When a normal browser tab cannot use push, the interface says **Add Shepherdr to your Home Screen, then open it there**.

The Push API requires a received push to result in a user-visible notification, and WebKit applies that must-show rule. The service worker therefore cannot receive and silently discard push merely because a Shepherdr page might be visible. Page-open state would not prove that the event was shown because the tab may be hidden, frozen, disconnected, or displaying other work. Current behavior has neither in-page event acknowledgement nor visible-page suppression.

Relevant standards and platform references are [W3C Push API](https://www.w3.org/TR/push-api/), [Service Workers](https://w3c.github.io/ServiceWorker/), [Notifications API](https://notifications.spec.whatwg.org/), [WebKit Web Push](https://webkit.org/blog/12945/meet-web-push/), [Web Push on iOS and iPadOS](https://webkit.org/blog/13878/web-push-for-web-apps-on-ios-and-ipados/), and [secure contexts](https://w3c.github.io/webappsec-secure-contexts/).

## Trust, storage, and delivery

After the explicit enable action, the browser registers the same-origin service worker, requests permission, creates a Push API subscription, and sends that subscription and its selected events to Shepherdr. The existing process observes snapshot differences, encrypts a small Web Push/VAPID payload, sends it to the subscription endpoint, and handles clicks through the service worker.

In protected mode, each subscription belongs to the trusted sign-in that authorized it. Only a current session for that trust can read, change, or remove it. Revocation makes the trust inactive before removing its subscriptions. Access reset removes all subscriptions while preserving the existing VAPID keys and configured contact, and startup suppresses and removes subscriptions with no active owner. Notification state never grants application access.

With sign-in off, any browser that can reach Shepherdr already has operator authority and may subscribe after an explicit tap. Subscribing does not establish device trust. The push service may continue delivering workspace-named alerts after the device leaves the private network, so the operator must be willing to grant that continuing access.

Subscriptions belong to the Shepherdr deployment rather than one Herdr session. They survive Shepherdr and machine restarts and remain if the deployment starts against another Herdr socket. Every new start or connection still begins with a silent baseline. The operator can clear notification state before or after changing the Herdr socket.

One owner-readable local state file holds the VAPID key pair and each subscription's endpoint, browser keys, optional expiry, event choices, and protected-mode owning trust ID. Atomic replacement protects the file. It contains no snapshots, history, device names, seen state, or delivery claims.

The operator enables push with `-vapid-contact` and a real `mailto:` or HTTPS URI. Shepherdr validates and saves it. Supplying the flag later updates only the contact; it does not rotate keys or remove subscriptions. The contact identifies the operator of that installation and is visible to browser push providers.

The VAPID private key and subscription authentication values remain secret. The VAPID public key and operator contact are intentionally browser-visible. Secrets stay out of logs, source control, browser responses, and unprotected backups, and the VAPID key remains stable across restarts. With Shepherdr stopped, the notification reset command clears all subscriptions, the VAPID key pair, and the saved contact. If the state is corrupt or unreadable, Shepherdr disables notifications and reports the error without breaking Home or Terminal or silently creating new keys.

A subscription endpoint is an untrusted server-side request target. Shepherdr accepts only valid HTTPS endpoints, rejects embedded credentials and fragments, does not follow redirects, resolves and rejects local, private, link-local, and tailnet destinations, defends against DNS rebinding, and applies tight request-size and time limits. A maintained Web Push library performs encryption and signing.

Payloads contain the event kind, the relevant usable Herdr workspace display name, and only the opaque identifiers needed for a relative same-origin destination. They contain no agent names, repository paths, terminal text, agent output, prompts, or other content. Encryption protects payload content in transit, but the push provider still sees timing, frequency, and size, and the operating system may show text on a lock screen.

Push uses a five-minute time to live. Shepherdr does not retry after acceptance, timeout, or an ambiguous result. Acceptance by a push service means only accepted for possible delivery. Permission changes, expired subscriptions, restarts, reconnects, and offline phones can cause missed events; duplicate delivery is also possible. Shepherdr does not claim notification delivery, history, or offline replay.

The underlying standards are [RFC 8030](https://www.rfc-editor.org/rfc/rfc8030), [RFC 8291](https://www.rfc-editor.org/rfc/rfc8291), and [RFC 8292](https://www.rfc-editor.org/rfc/rfc8292).

## Boundaries

This direction does not add team accounts, hosted identity, public hosting, a chosen push vendor, a second runtime, terminal parsing, a general event system, push history, delivery receipts, cross-device seen state, reliable offline replay, retry queues, or offline application caching. Expected risks include missed or duplicate alerts, stale links, removed permission, lock-screen metadata, leaked secrets, and malicious subscription endpoints.

## Ongoing acceptance guidance

Changes to notification behavior use real Herdr, a real Android phone, and a second browser or browser profile:

- Check the stable private HTTPS origin, first eligible offer, **Not now** memory, explicit permission action, denial guidance, and browser-local settings.
- Confirm the default choices, change every event independently in two browsers, and exercise sign-in-off subscription behavior.
- Drive real status, workspace, and trusted-sign-in changes; verify workspace wording, generic fallback, one workspace-burst summary, and silent startup and reconnect baselines.
- Open a current status notice, then make its terminal stale and verify **Terminal unavailable** and the Home fallback without a guessed terminal.
- Revoke a trusted sign-in and confirm its authority ends before subscription cleanup; exercise access reset, notification reset, startup orphan cleanup, corrupt state, restart, and a changed Herdr socket.
- Keep a phone offline beyond the five-minute TTL and confirm the product makes no history, replay, read-state, or delivery-success claim. Duplicate or missing delivery remains an allowed platform outcome.

iOS and iPadOS follow the constraints above. Real-device acceptance is recorded only after that path is exercised.

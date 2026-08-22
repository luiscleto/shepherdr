# Wave 04: passkey-protected access

Status: proposed

Architecture-approval base: `0cb8e04ae19411d068406f9ce0ba3bb7812d9227`

Worker base: **PENDING — after human approval, the orchestrator must replace this line with the exact commit containing the approved brief before dispatch.**

Do not dispatch implementation from this proposal. Human approval and the pinned worker base are required first.

## Outcome

Make passkey-protected access the default for the existing one-operator Shepherdr process while keeping explicit `-no-sign-in` behavior. This is one complete protected cutover across startup, local state, routes, sockets, Terminal reads, notifications, command-line administration, the small browser UI, and operator documentation.

The approved source of truth is [Passkey sign-in and trusted-device architecture](passkey-device-architecture-proposal.md). Apply it without redesigning access, Home, Terminal, workspace actions, notifications, or Herdr authority. Internal checkpoints are not independently releasable or accepted.

## Approved boundary to implement

- Protected mode remains behind a trusted private network. Shepherdr stays loopback-bound, accepts exactly one configured canonical private HTTPS hostname and origin, rejects localhost, IP literals, aliases, and noncanonical protected access, and never operates Tailscale. The operator owns private DNS, TLS, Tailscale Serve or another private publisher, and a hostname with no unrelated HTTPS service on any port.
- There is one operator with many trusted passkeys. Trust belongs to one passkey record, not proven physical hardware; synced copies share that record and revoke together. Registration is discoverable, requires user verification, requests no attestation, and does not restrict authenticator attachment.
- The first protected start is bootstrap-only and prints one ten-minute link plus a terminal QR code. The link works on desktop and the QR on a phone. A new browser never authorizes itself. Later invitations come from stopped-service `access invite` or a trusted browser after passkey verification no more than five minutes old. Any active passkey may verify and becomes the actual authorizer.
- Invitation use is single-shot. Its reservation lasts at most five minutes and never past the invitation's expiry. Expired, used, replayed, racing, self-authorized, or revoked-authorizer cases fail without granting access.
- `-session-lifetime` defaults to `30d`; `none` removes Shepherdr's clock-based expiry. Browsers may still discard or cap either kind of cookie. All time behavior uses an injectable clock.
- One deny-by-default route inventory enforces exact Host and Origin rules. Mutations are JSON-only, GET never changes state, WebSocket upgrades check exact Origin, and the approved cookie constraints hold: host-only `Secure`, `HttpOnly`, and `SameSite=Strict`, with no separate CSRF token.
- Stopped-service administration is exactly `access invite`, `access devices`, `access revoke`, and `access reset`. A trusted browser can list trusted sign-ins, invite, and revoke online after fresh verification. Neither browser nor local revoke removes the final active passkey; local reset may clear all trust.
- Reset preserves the configured public origin and VAPID identity while clearing passkeys, sessions, invitations, and notification subscriptions. Revocation and every other authorization invalidation close bound Home and Terminal connections, cancel and collect control children, and cancel/final-check ordinary Terminal read children before returning success.
- Notification subscriptions in protected mode belong to the authorizing passkey. Revocation and reset preserve the approved access-first cutoff, cleanup, restart, and explicit re-enable behavior. Sign-in-off keeps today's notification semantics.
- Use Go 1.26 and reverify the current maintained `go-webauthn` release at implementation start before pinning it. Record the version and review; do not hand-roll WebAuthn or add a hosted identity service.

Excluded are teams, roles, organizations, public hosting, remote recovery, Chat, a hosted identity provider, a second runtime, new Herdr interfaces, or changes to accepted Home, Terminal, workspace, and notification behavior.

## One worker and internal checkpoints

Worker `wave04-passkey-access-worker` owns the complete slice. It may use these internal checkpoints in order:

- **A — foundation:** Go 1.26 and reviewed library line; canonical-origin startup; versioned owner-only state, locking, migration, and stopped-service commands.
- **B — authority:** invitations, WebAuthn flows, sessions, central route gate, commit-time checks, connection and Terminal-read invalidation, and notification ownership/cleanup.
- **C — browser:** the small TypeScript **Sign in**, **Trust this device**, and **Devices** flows, invitation link/QR, revoke, and sign-out while preserving Home and exact Terminal.
- **D — cutover:** focused tests, README/operator documentation, production build, and every complete gate below.

The worker owns the Go toolchain and dependencies, startup/configuration and local access state, relevant HTTP and WebSocket boundaries, Home/Terminal authorization hooks, notification ownership changes, command-line access administration, access-owned TypeScript/assets/styles, focused tests, and cutover documentation.

A feeds B, B feeds C, and all three feed D. Likely overlap includes application startup, local notification state, route registration and middleware, Home and Terminal connection lifecycles, Terminal child ownership, workspace and notification mutations, embedded browser assets, navigation, shared phone layout, tests, and README. One owner keeps those invariants coherent; no checkpoint is a partial protected release.

Do not redesign Home or notifications, change accepted Terminal reading/control behavior, add placeholder access controls, or preserve an unprotected fallback in protected mode.

## Browser experience

Follow [Interface direction](ui-direction.md). With `-no-sign-in`, open Home and quietly show **Sign-in is off.** Protected ordinary visits show **Sign in**. A valid setup or invitation shows **Trust this device**, then asks the person to create a passkey.

Show **Devices** only in protected mode. Describe trusted sign-ins without promising physical-device identity; explain synced copies plainly. Allow a freshly verified person to make a short-lived link/QR, revoke every entry except the last, and sign out. Expired or used links and lost browser data get short recovery guidance: sign in again, or create a new invitation locally or from a trusted device.

Do not put protocol or storage terms on screen unless the person needs the exact term to act. Keep the current narrow-phone density, touch targets, Settings entry, notification behavior, Home, and Terminal.

## Tests and evidence

Use a few direct tests around the real boundaries: canonical configuration, route classification, state safety and migration, invitation races and replay, passkey policy, session timing and rotation, last-passkey refusal, authorization invalidation, connection/child cleanup, notification ownership, hostile text, and restart/reset behavior. Time tests use an injectable clock. Do not build a large fake Herdr, authenticator, browser, or push service.

All Node/browser tests run only through the repository's memory-limited `npm test --prefix web` entry. Never invoke uncapped Node, npm test, npx, tsx, Vitest, Playwright, or individual browser-test commands. Browser assertions compare primitive results, not live DOM objects. Run focused Go tests, the capped browser suite once, TypeScript typecheck/build, production Go build, and bounded production-path checks. Report exact commands, results, and the worker commit.

## Complete cutover gate

Before protected mode becomes the default, exercise the production executable with real Herdr and prove all of the following together:

1. Start protected from empty state and inspect the one printed link and terminal QR. Confirm bootstrap-only access and first trust once from the desktop link and once after reset by scanning the QR with Android/Pixel.
2. Sign in without a username. Verify the default `30d` policy and `none`, no sliding extension, restart persistence, accurate cookie behavior, injected-clock expiry, and honest acknowledgement that a browser may discard either cookie earlier.
3. Through an authenticated session, exercise full Home, the exact existing Terminal on phone and desktop, every workspace action, notification setup/settings/delivery destination, and hostile displayed content. Displayed names, output, paths, labels, attachments, and pasted content stay inert and grant no access or navigation authority.
4. From a trusted browser after fresh passkey verification, create a second short-lived invitation/link/QR, trust a second sign-in, list both entries, and revoke one. Confirm the passkey actually used for verification is the recorded authorizer.
5. Confirm used and expired invitations cannot replay, simultaneous use has one winner, abandoned reservation timing is bounded, a new browser cannot authorize itself, and stale or revoked authorizers fail at commit.
6. Confirm browser and local revoke refuse the final passkey. Confirm sign-out, rotation, expiry, capacity removal, revocation, and reset immediately deny authority, close Home and Terminal connections, release control, and cancel/final-check Terminal read children without returning protected output.
7. Restart and verify origin, session, passkey, invitation, and VAPID persistence as applicable. Reset and verify only the approved trust/session/invitation/subscription state is cleared, the origin and VAPID identity remain, notifications require explicit re-enablement, and the next protected start creates the new bootstrap invitation.
8. Exercise every stopped-service access command, refusal while Shepherdr is running, explicit `-no-sign-in`, return to protected mode on restart, legacy notification-subscription suppression, and missing, corrupt, unsafe, or unsupported state failing closed.
9. Reject wrong or noncanonical Host/Origin, localhost, IP, aliases, alternate ports, forwarded-header forgeries, non-JSON mutations, state-changing GETs, unknown APIs, oversized bodies, and cross-origin Home, Terminal, and optional lab WebSockets. Verify every current route is classified and protected.
10. Drive real Herdr transitions and confirm accepted Home, Terminal, workspace, stale-target, and notification behavior has not changed. Revocation/reset cleanup must preserve notification ownership and prevent any new push request after the durable cutoff.

The existing Pixel 8a emulator may support preflight. The human supplies and witnesses the real-phone flow on their phone through their private HTTPS service. Agents do not start, stop, or reconfigure Tailscale, and passing emulator checks alone is not acceptance.

## Documentation cutover

Until this entire slice passes, README must continue to say that the runnable build starts without sign-in. In the same reviewed result that changes the default, update README with the protected default, `-public-origin`, printed link/QR, `-no-sign-in`, session lifetime including `none` and browser limits, stopped-service `access` commands, the trusted-private-network recommendation, and the Go 1.26 requirement. Do not publish partial operator guidance as if it works.

## Review and integration

Fresh independent technical/security reviewer `wave04-passkey-access-reviewer` reviews the exact worker commit against the approved architecture and this brief. The reviewer verifies the complete route inventory, canonical-host boundary, WebAuthn policy and dependency, state/locking/crash invariants, timing and concurrency bounds, actual-authorizer checks, last-passkey rule, notification cutoff/cleanup, and every connection/child invalidation path. Findings return to the worker; the reviewer does not fix them.

After technical approval, fresh Grok plain-language/UI reviewer `wave04-passkey-access-grok-reviewer` checks the exact access flow and copy on narrow phone and desktop: direct Home in sign-in-off mode, ordinary sign-in, trust from link/QR, expired/used recovery, browser-data loss, synced-passkey explanation, Devices administration, touch targets, Settings/notifications, and preservation of Home and Terminal. Findings return to the worker; this reviewer does not redesign accepted screens.

Only after both reviewers approve, integrator `wave04-passkey-access-integrator` starts from the pinned approved-brief base and integrates the exact approved worker commit. The integrator resolves composition issues only within this brief, runs the capped automated gates and production build, then repeats the real Herdr and protected desktop/Android workflow. The integrator reports its exact commit and evidence and does not approve its own integration.

Integration is not product acceptance. The witnessed human real-phone/private-HTTPS gate must pass before cutover or any downstream wave.

## Risks and decision required before approval

- The hostname is a lasting passkey boundary; this slice has no hostname migration.
- Invitation links are temporary bearer authority and can leak through screens, QR tools, extensions, clipboard, or observers.
- A synced passkey cannot identify or revoke one physical copy. A stolen session remains useful until invalidation, and `none` has no Shepherdr clock deadline.
- Access state and notification state cross at revocation/reset; a wrong commit order can retain authority or push ownership.
- The WebAuthn library is pre-v1 and may require a different current tag or coordinated Go update when implementation begins.
- This cutover touches every present product path, so a locally green subsystem is not evidence that the complete boundary works.

One human decision remains before this brief can be approved and pinned: choose the maximum accepted finite `-session-lifetime`. Recommendation: **365d**, with the existing `30d` default unchanged. A smaller fixed cap limits stolen-session exposure but gives operators less flexibility; no finite cap makes overflow validation and the intended safety boundary less clear. The worker must not choose this value during implementation.

# Wave 04: passkey-protected access

Status: approved

Approved: 2026-08-22

Architecture-approval base: `0cb8e04ae19411d068406f9ce0ba3bb7812d9227`

Worker base: recorded by the orchestrator in the Herdr dispatch. It must be the exact commit containing this approved brief.

## Outcome

Make passkey-protected access the default for the existing one-operator Shepherdr process while keeping explicit `-no-sign-in` behavior. This is one complete protected cutover across startup, local state, routes, sockets, Terminal reads, notifications, command-line administration, the small browser UI, and operator documentation.

The approved source of truth is [Passkey sign-in and trusted-device architecture](passkey-device-architecture-proposal.md). Apply it without redesigning access, Home, Terminal, workspace actions, notifications, or Herdr authority. Internal checkpoints are not independently releasable or accepted.

## Approved boundary to implement

- Protected mode remains behind a trusted private network. Shepherdr stays loopback-bound, accepts exactly one configured canonical private HTTPS hostname and origin, rejects localhost, IP literals, aliases, and noncanonical protected access, and never operates Tailscale. The operator owns private DNS, TLS, Tailscale Serve or another private publisher, and a hostname with no unrelated HTTPS service on any port.
- There is one operator with many trusted passkeys. Trust belongs to one passkey record, not proven physical hardware; synced copies share that record and revoke together. Registration is discoverable, requires user verification, requests no attestation, and does not restrict authenticator attachment.
- The first protected start is bootstrap-only and prints one ten-minute link plus a terminal QR code. The link works on desktop and the QR on a phone. A new browser never authorizes itself. Later invitations come from stopped-service `access invite` or a trusted browser after passkey verification no more than five minutes old. Any active passkey may verify and becomes the actual authorizer.
- Invitation use is single-shot. Its reservation lasts at most five minutes and never past the invitation's expiry. Expired, used, replayed, racing, self-authorized, or revoked-authorizer cases fail without granting access.
- `-session-lifetime` defaults to `30d`; the maximum accepted finite value is `365d`, and larger finite values are rejected. `none` removes Shepherdr's clock-based expiry. Browsers may still discard or cap either kind of cookie. All time behavior uses an injectable clock.
- One deny-by-default route inventory requires exact Host on every request, including health and fixed assets. Every POST/PUT/PATCH/DELETE API request—including read-shaped POSTs such as notification settings reads and every ceremony—requires exact canonical Origin and `Content-Type: application/json`; absent, `null`, duplicate, malformed, or noncanonical Origin is rejected. Every WebSocket upgrade requires exact canonical Origin and rejects those same cases. Only authenticated GET/HEAD API reads may legitimately omit Origin, while still requiring exact Host and a valid session. GET/HEAD never mutate, and the approved cookie constraints hold: host-only `Secure`, `HttpOnly`, and `SameSite=Strict`, with no separate CSRF token.
- `-no-sign-in` applies only to that start, creates or changes no protected state, and cannot be combined with `-public-origin` or `-session-lifetime`.
- Stopped-service access administration is exactly `access invite`, `access devices`, `access revoke`, and `access reset`. In the browser, viewing **Devices** needs only a valid session; creating an invitation or revoking a trusted sign-in also requires fresh passkey verification. Neither browser nor local revoke removes the final active passkey; local access reset may clear all trust.
- Access reset rotates the operator user handle, preserves the configured public origin and VAPID identity, and clears passkeys, sessions, invitations, and notification subscriptions. The separate existing `-reset-notifications` operation remains unchanged: it clears notification subscriptions and VAPID identity without changing protected access state. Both operations require distinct tests.
- Revocation and every other authorization invalidation close bound Home and Terminal connections, cancel and collect every affected per-socket Terminal observation and control child, and cancel, collect, and final-check ordinary Terminal-read children before returning success.
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

Follow [Interface direction](ui-direction.md). With `-no-sign-in`, open Home and quietly show **Sign-in is off.** When passkeys are on, ordinary visits show **Sign in** and ask for a passkey, not a name, email address, or account. First setup prints a link and QR code; open the link on that computer or scan the QR on a phone. A valid setup or invitation link, including one opened through the QR, shows **Trust this device** and then creates a passkey. This path only trusts that browser; a new browser cannot trust itself.

Show **Devices** only when passkeys are on. A valid session may view it. Describe trusted sign-ins without promising physical-device identity; explain synced copies plainly. If copy or backup state appears, say **last reported by this passkey** with its observation time and make no provider or live-device claim. After the person uses a passkey, allow invitation and revocation mutations. Offer **Revoke** only while another trusted sign-in remains, never as a disabled final action. Clearing all access is a stopped-service local reset. A person may also sign out.

Every unusable invitation gets the same message: **This invitation can't be used. Create a new invitation on the machine running Shepherdr or from a trusted device.** Lost browser data for someone who already has a passkey gets **Sign in again.** Do not combine these cases or put protocol or storage terms on screen unless the person needs the exact term to act. Keep the current narrow-phone density, touch targets, existing **Notifications** settings, Home, and Terminal.

## Tests and evidence

Use a few direct tests around the real boundaries: canonical configuration, route classification, state safety and migration, invitation races and replay, passkey policy, session timing and rotation including the `365d` boundary, last-passkey refusal, authorization invalidation, complete connection/child cleanup, notification ownership, hostile text, and both reset operations. Time tests use an injectable clock. Do not build a large fake Herdr, authenticator, browser, or push service.

All Node/browser tests run only through the repository's memory-limited `npm test --prefix web` entry. Never invoke uncapped Node, npm test, npx, tsx, Vitest, Playwright, or individual browser-test commands. Browser assertions compare primitive results, not live DOM objects. Run focused Go tests, the capped browser suite once, TypeScript typecheck/build, production Go build, and bounded production-path checks. Report exact commands, results, and the worker commit.

## Complete cutover gate

Before protected mode becomes the default, exercise the production executable with real Herdr and prove all of the following together:

1. Start protected from empty state and inspect the one printed link and terminal QR. Confirm bootstrap-only access and first trust once from the desktop link and once after reset by scanning the QR with Android/Pixel.
2. Sign in with a passkey and no name, email address, or account. Verify the default `30d` policy, acceptance of `365d`, rejection of every larger finite value, and `none`; also verify no sliding extension, restart persistence, accurate cookie behavior, injected-clock expiry, and honest acknowledgement that a browser may discard either cookie earlier.
3. Through an authenticated session, exercise full Home, the exact existing Terminal on phone and desktop, every workspace action, notification setup/settings/delivery destination, and hostile displayed content. Displayed names, output, paths, labels, attachments, and pasted content stay inert and grant no access or navigation authority.
4. View **Devices** with a valid session and no fresh verification. Then use a passkey to create a second short-lived invitation/link/QR and revoke one of two trusted sign-ins. Confirm each mutation requires freshness and the passkey actually used is the recorded authorizer.
5. Confirm used, expired, malformed, racing, self-authorized, and revoked-authorizer invitations cannot replay or grant access, all show the same generic invitation recovery, simultaneous use has one winner, and abandoned reservation timing is bounded. Confirm lost browser data tells an existing passkey holder to sign in again.
6. Confirm browser and local revoke refuse the final passkey and the browser omits rather than disables that **Revoke** action. Confirm sign-out, rotation, expiry, capacity removal, revocation, and access reset immediately deny authority, close Home and Terminal connections, and cancel and collect every affected Terminal observation, control, and ordinary read child before success. No protected output may be returned after invalidation.
7. Restart and verify origin, session, passkey, invitation, and VAPID persistence as applicable. Run access reset and verify it rotates the operator user handle, clears only the approved trust/session/invitation/subscription state, preserves the origin and VAPID identity, requires notification re-enablement, and lets only the next protected start create a bootstrap invitation.
8. Exercise every stopped-service access command and its refusal while Shepherdr is running. Separately run `-reset-notifications` and verify subscriptions plus VAPID identity are cleared without changing protected access. Exercise `-no-sign-in`, confirm it creates or changes no protected state, reject its combination with `-public-origin` or `-session-lifetime`, and return to protected mode on restart. Confirm legacy notification-subscription suppression and missing, corrupt, unsafe, or unsupported state fail closed.
9. Require exact Host on health, fixed assets, authenticated reads, mutations, and WebSockets. Require exact canonical Origin and JSON on every POST/PUT/PATCH/DELETE API request, including notification settings reads and every ceremony; reject absent, `null`, duplicate, malformed, and noncanonical Origin. Require every Home, Terminal, and optional lab WebSocket upgrade to meet the same exact-Origin rule and rejections. Confirm only authenticated GET/HEAD API reads may legitimately omit Origin, while still requiring exact Host and a valid session. Also reject localhost, IP, aliases, alternate ports, forwarded-header forgeries, non-JSON requests for those methods, state-changing GET/HEAD, unknown APIs, oversized bodies, and cross-origin WebSockets, and verify every current route is classified.
10. Drive real Herdr transitions and confirm accepted Home, Terminal, workspace, stale-target, and notification behavior has not changed. Revocation/reset cleanup must preserve notification ownership and prevent any new push request after the durable cutoff.

The existing Pixel 8a emulator may support preflight. The human supplies and witnesses the real-phone flow on their phone through their private HTTPS service. Agents do not start, stop, or reconfigure Tailscale, and passing emulator checks alone is not acceptance.

## Documentation cutover

Until this entire slice passes, README must continue to say that the runnable build starts without sign-in. In the same reviewed result that changes the default, update README with the protected default, `-public-origin`, printed link/QR, `-no-sign-in`, session lifetime including `none` and browser limits, stopped-service `access` commands, the trusted-private-network recommendation, and the Go 1.26 requirement. Do not publish partial operator guidance as if it works.

## Review and integration

Fresh independent technical/security reviewer `wave04-passkey-access-reviewer` reviews the exact worker commit against the approved architecture and this brief. The reviewer verifies the complete route inventory, Host/Origin edge cases, WebAuthn policy and dependency, the `365d` limit, state/locking/crash invariants, sign-in-off isolation, both reset operations, actual-authorizer checks, Devices read authority, last-passkey rule, notification cutoff/cleanup, and every connection/child invalidation path. Findings return to the worker; the reviewer does not fix them.

After technical approval, fresh Grok plain-language/UI reviewer `wave04-passkey-access-grok-reviewer` checks the exact access flow and copy on narrow phone and desktop: direct Home in sign-in-off mode; passkey-only ordinary sign-in; trust from desktop link or phone QR without implying self-trust; one generic unusable-invitation recovery; separate browser-data-loss recovery; synced-passkey and last-reported wording; available-only Revoke; local reset guidance; touch targets; existing Notifications settings; and preservation of Home and Terminal. Findings return to the worker; this reviewer does not redesign accepted screens.

Only after both reviewers approve, integrator `wave04-passkey-access-integrator` starts from the pinned approved-brief base and integrates the exact approved worker commit. The integrator resolves composition issues only within this brief, runs the capped automated gates and production build, then repeats the real Herdr and protected desktop/Android workflow. The integrator reports its exact commit and evidence and does not approve its own integration.

Integration is not product acceptance. The witnessed human real-phone/private-HTTPS gate must pass before cutover or any downstream wave.

## Risks

- The hostname is a lasting passkey boundary; this slice has no hostname migration.
- Invitation links are temporary bearer authority and can leak through screens, QR tools, extensions, clipboard, or observers.
- A synced passkey cannot identify or revoke one physical copy. A stolen session remains useful until invalidation, and `none` has no Shepherdr clock deadline.
- Access state and notification state cross at revocation/reset; a wrong commit order can retain authority or push ownership.
- The WebAuthn library is pre-v1 and may require a different current tag or coordinated Go update when implementation begins.
- This cutover touches every present product path, so a locally green subsystem is not evidence that the complete boundary works.

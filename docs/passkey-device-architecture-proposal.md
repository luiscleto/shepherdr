# Passkey sign-in and trusted-device architecture proposal

Status: approved

Approved: 2026-08-22

Base: `fc8facb72f7a854b078f0b7ff6ffa273211d9fe2`

## Decision status and scope

This proposal adds an access boundary to the existing one-Go-process, loopback-only, same-origin Shepherdr application. It does not change Herdr authority, Home, Terminal, workspace behavior, notification meaning, or the requirement for a trusted private network.

The human has approved the following inputs to this proposal:

- Passkeys are the recommended stronger mode. Sign-in does not make public hosting safe.
- Trust is credential-scoped. A discoverable passkey may be device-bound or synced; all copies with the same credential ID share one Shepherdr trust and revocation record.
- Protected mode starts in bootstrap-only state until first trust exists. Running without sign-in requires `-no-sign-in` and gives operator authority to every browser that can reach Shepherdr.
- Protected mode has one canonical, dedicated private HTTPS hostname. It rejects localhost, IP-literal, and noncanonical origins, and the hostname has no unrelated HTTPS service on any port.
- Registration requires a discoverable credential and user verification, requests no attestation, and does not restrict authenticator attachment.
- Ten-minute, single-use invitations may be issued by explicit stopped-service local CLI action or by a trusted browser after passkey verification no more than five minutes old. A reservation lasts at most five minutes and never beyond invitation expiry. Any active trusted passkey may perform the reauthentication and is recorded as the actual authorizer.
- The browser boundary is one deny-by-default exact Host/Origin gate, host-only `Secure`, `HttpOnly`, `SameSite=Strict` cookies, JSON-only mutations, explicit WebSocket Origin checks, no state-changing GETs, and no separate CSRF token.
- Local `access invite|devices|revoke|reset` commands require the service to be stopped. Trusted-browser device administration works while it is online. A browser cannot remove the final credential.
- Reset is destructive local administration: it preserves public-origin and VAPID identity configuration while clearing credentials, sessions, invitations, and notification subscriptions.
- The implementation baseline is Go 1.26 with the current maintained `go-webauthn` release.

In the rest of this document, **confirmed** describes repository or standards behavior, **proposed** fills in an implementation detail within those decisions, and **assumption** names a security boundary. The document itself remains a proposal; it does not approve an implementation brief.

Excluded are passwords, recovery codes, remote or self-service lost-device recovery, teams, roles, organizations, public hosting, hosted identity, Chat, attestation allowlists, authenticator-vendor policy, a second runtime, and Herdr protocol changes.

## Confirmed boundaries and platform behavior

- **Current Shepherdr:** one Go process listens only on loopback, talks to one Herdr session, serves the embedded TypeScript application, bridges Home and Terminal WebSockets, performs workspace mutations, and owns local Web Push state. It currently has no authentication middleware.
- **Origin and RP ID:** WebAuthn credentials are scoped to an RP ID. The RP ID is a domain name without scheme or port; the server must verify both it and the expected browser origin. WebAuthn normally requires HTTPS. See [WebAuthn Level 3](https://www.w3.org/TR/webauthn-3/) and [Secure Contexts](https://www.w3.org/TR/secure-contexts/).
- **Passkeys are not physical-device identity:** a backup-eligible credential may be synced or copied. Shepherdr cannot tell which physical copy asserted or selectively revoke one copy. See [WebAuthn credential backup state](https://www.w3.org/TR/webauthn-3/#sctn-credential-backup) and [FIDO passkey terminology](https://fidoalliance.org/passkeys/).
- **Counters are a signal, not identity proof:** an authenticator may keep its counter at zero; a non-increasing counter can indicate a clone, malfunction, or race. See [WebAuthn signature-counter considerations](https://www.w3.org/TR/webauthn-3/#sctn-sign-counter).
- **Private HTTPS proxy:** Tailscale Serve can terminate HTTPS at a tailnet DNS name and reverse-proxy to `127.0.0.1`. Naming, TLS, ACLs, and publishing remain operator-owned; Funnel is out of scope. See [Tailscale Serve](https://tailscale.com/docs/reference/tailscale-cli/serve).
- **WebSockets:** browsers send an `Origin` header in the opening handshake and a server for selected sites must validate it. See [RFC 6455 section 10.2](https://www.rfc-editor.org/rfc/rfc6455#section-10.2).
- **Cookie retention has a browser boundary:** `Max-Age`/`Expires` set a maximum, not a retention guarantee, and a cookie without them has a user-agent-defined session lifetime. Browsers may discard either kind earlier. See [RFC 6265 sections 4.1.2.1 and 6.1](https://www.rfc-editor.org/rfc/rfc6265.html).

## Smallest architecture

Keep one Shepherdr process and its same-origin embedded UI. The Go listener remains loopback-only; the operator-owned private HTTPS proxy is the only protected browser entry. Add a small access manager, one versioned access-state file, native browser WebAuthn calls, and a maintained Go verifier. Do not add an identity service, database, second frontend origin, or authentication state to Herdr.

One active WebAuthn credential record is one trusted “device” row. The UI may retain the familiar word **Devices**, but it must say that a passkey can sync and that all synced copies share this row. Revocation disables the credential ID in Shepherdr; it does not remove passkey copies from an authenticator or password manager. Backup eligibility is stored, while backup state is displayed as **last reported by this passkey** with its observation time, not as live provider or physical-device state.

With zero credentials, protected mode exposes only the fixed bootstrap/sign-in assets, invitation ceremony, and health endpoint. It exposes no Home, Terminal, workspace, notification, subscription, or device-administration authority. The first valid invitation completion atomically creates the credential and browser session; the same process then serves the protected application.

## Modes, configuration, and canonical origin

### Protected default

First protected initialization is:

    ./bin/shepherdr -public-origin https://shepherdr-host.example.ts.net

`-public-origin` is persisted after first initialization. Later starts may omit it; supplying a different value fails closed. There is no separate RP-ID flag. The RP ID is the canonical hostname.

The accepted public-origin serialization is exactly `https://lowercase-ascii-dns-name[:nondefault-port]`:

- scheme must be lowercase `https`;
- host must be a lowercase ASCII/IDNA DNS name, with no trailing dot, `localhost` label, or IPv4/IPv6 literal;
- user information, query, fragment, and any path including a trailing `/` are rejected;
- explicit `:443` is rejected and a non-default port is preserved; and
- the supplied string must already equal that canonical serialization. Shepherdr does not silently rewrite an almost-matching security setting.

Before listening, protected mode validates the persisted value by the same rules. Every request must have the canonical authority in `Host`. Every API mutation, ceremony POST, and WebSocket upgrade must have an `Origin` whose parsed canonical serialization exactly equals the stored public origin; absent, `null`, duplicate, malformed, or noncanonical values fail. Same-origin reads that browsers legitimately send without `Origin` still require exact `Host` and a valid session. Forwarded host, scheme, client, and identity headers are never security inputs.

The configured origin, including a non-default port, is Shepherdr's only accepted HTTP and WebSocket origin. Ports do not isolate either relevant browser credential: the WebAuthn RP ID cannot contain a port, and host-only cookies are also sent by hostname without port isolation ([RFC 6265 section 8.5](https://www.rfc-editor.org/rfc/rfc6265.html#section-8.5)). An assertion obtained by another port still carries that other origin and Shepherdr rejects it, but a separate HTTPS service there can prompt for the same RP ID and receive Shepherdr cookies. Exact Origin checks therefore do not make cross-port sharing safe. The hostname must be dedicated to Shepherdr across all ports and must not host an unrelated HTTPS service. Direct loopback, localhost, IP-literal, hostname-alias, and alternate-port access never become protected fallbacks.

The existing `-listen` loopback validation remains. Private DNS, TLS, Tailscale ACLs, Serve configuration, and keeping the hostname private remain operator-owned.

“Public origin” means browser-visible origin, not Internet-public service.

### Explicit sign-in-off mode

    ./bin/shepherdr -no-sign-in

`-no-sign-in` applies only to that process start and is never persisted. It preserves current behavior and the visible **Sign-in is off** notice: every browser that reaches Shepherdr has operator authority. It does not erase access state. A later start without the flag returns to protected mode. Missing, corrupt, unsupported, or unsafe access state never falls back to sign-in-off mode.

`-no-sign-in` cannot be combined with `-public-origin` or `-session-lifetime`. Sign-in-off starts do not create or modify protected configuration.

### Configurable session duration

The proposed smallest spelling is:

    ./bin/shepherdr -session-lifetime 30d
    ./bin/shepherdr -session-lifetime none

The first protected initialization defaults to `30d`. The selected value is persisted, and omission on later starts reuses it. A supplied change affects sessions created or rotated afterward; existing sessions retain their issuance policy. Finite values use positive whole days; malformed, zero, negative, or overflowing values fail before listening. `none` means no Shepherdr clock-based expiry, not “stay signed in forever.”

## Bootstrap and stopped-service local administration

On the first protected start, Shepherdr creates the stable random operator user handle and one ten-minute bootstrap invitation, prints its private HTTPS link and a terminal QR code, and remains bootstrap-only. The same link can be opened on that computer or scanned by a phone. Plaintext invitation tokens are displayed once and never written to ordinary logs or persistent state.

If the process restarts while the invitation is still valid, the already printed link remains usable but is not reprinted because only its digest was stored. After expiry, the local operator stops Shepherdr and creates another. No browser can create the first invitation and a new browser can never authorize its own trust.

The exact stopped-service commands are:

    ./bin/shepherdr access invite
    ./bin/shepherdr access devices
    ./bin/shepherdr access revoke <trust-id>
    ./bin/shepherdr access reset

- `access invite` creates a ten-minute single-use link and QR from the persisted origin.
- `access devices` lists trust ID, untrusted label, creation and last-use times, backup eligibility, and last-reported backup state/time. It does not claim hardware, browser, location, or physical-device identity.
- `access revoke` disables the selected credential and its authority. It refuses to remove the final active credential and directs the operator to `access reset` for that destructive transition.
- `access reset` clears all credentials, sessions, invitations, and notification subscriptions, rotates the operator user handle, and preserves the configured public origin plus VAPID key, subject/contact, and other VAPID identity configuration. It creates no invitation itself. The next protected start observes zero credentials and prints a new bootstrap invitation.

The server holds an exclusive service/state lock for its lifetime. Every `access` command takes that lock non-blockingly and fails with “Stop Shepherdr first” if it is held; there is no online CLI control socket and no concurrent direct writer. While Shepherdr is online, trusted-browser administration provides list, invite, and revoke.

Machine-local shell access as the service's operating-system account is the approved local authority. If all passkey copies are lost, that authority may stop the service and issue a new invitation, or destructively reset trust. This is not remote or self-service account recovery, and reset does not recover the old credential or identity—it discards them. There is no remote recovery path.

## WebAuthn, invitations, and ceremonies

### WebAuthn policy

Use one stable random, non-identifying WebAuthn user handle for the sole operator. Registration sets `residentKey: required` and the compatible `requireResidentKey: true`, requires user verification, leaves authenticator attachment unset, requests `attestation: none`, and excludes active credential IDs. Usernameless sign-in requires user verification and accepts only an active stored credential for the user handle and RP ID.

Use native `navigator.credentials.create()` and `navigator.credentials.get()`. The Go library creates challenges/options and verifies challenge, type, exact origin, RP-ID hash, user presence, user verification, signature, credential ID, user handle, backup flags, and counter. Shepherdr does not implement CBOR, COSE, attestation, or signature verification.

Persist the complete library-required credential record. After a successful assertion, update counter, last use, backup eligibility, and backup state/time as last reported by that assertion. Counter anomalies are diagnostics, not automatic proof of copying or revocation.

### Invitation authorization and single use

An invitation is authorized either by local CLI authority or by a trusted browser with successful passkey reauthentication within the preceding five minutes. Reauthentication is usernameless: any active trusted credential may satisfy it. The resulting fresh-authorization grant records that credential's trust ID as the actual authorizer, rotates the browser session to that credential, and carries that ID into each invitation or revocation authorized during the five-minute window. The commit rechecks that the actual authorizer remains active; it never substitutes the session's older credential or a client-supplied ID.

The server generates a 32-byte cryptographically random bearer token, stores only its SHA-256 digest with issuer and absolute expiry, and renders:

    https://configured-origin/#trust=<base64url-token>

The invitation expires ten minutes after issue and can be consumed once. The fragment keeps it out of the initial HTTP request and ordinary `Referer` data, but it is still bearer authority visible to page JavaScript, browser extensions, the address bar before removal, QR scanners, screenshots, clipboard/history/session restoration, and anyone who sees terminal output. Immediate `history.replaceState` reduces accidental retention; it does not make leakage impossible.

The trust-begin JSON POST atomically verifies the digest, expiry, unused state, and active browser issuer where applicable. It reserves the invitation to that ceremony client for `min(now + 5 minutes, invitation expiry)`. One reservation can exist; the first racing client wins. A new challenge by the same client does not extend the original reservation deadline. Abandonment releases the unconsumed invitation after that deadline if the ten-minute outer expiry remains. Successful registration atomically adds the credential, consumes the invitation, and creates the session. Persistence failure accepts none of those changes; two completions serialize and only one succeeds. Unknown, malformed, expired, consumed, reserved-by-another-client, and revoked-issuer tokens receive the same generic response.

### Ceremony cookies, concurrency, and bounds

Sign-in, trust, and reauthentication use separate random host-only cookies such as `__Host-shepherdr_ceremony_signin`, `__Host-shepherdr_ceremony_trust`, and `__Host-shepherdr_ceremony_reauth`. Each has `Secure; HttpOnly; SameSite=Strict; Path=/`, no `Domain`, and matching `Max-Age` and `Expires` no later than five minutes; trust is additionally capped by invitation expiry. Server challenges are memory-only and single-use, so restart cancels unfinished ceremonies without consuming an invitation.

Successful completion, cancellation, fatal failure, or attempt exhaustion deletes the server record and overwrites the cookie with the same attributes, `Max-Age=0`, and a past `Expires`. Ordinary authentication failure consumes that challenge but may issue a new one inside the same reservation until the attempt bound; it never extends the cookie or outer deadline. Expiry is enforced server-side even if a browser retains a cookie.

Cookies are shared by tabs. There is one live ceremony per browser cookie and kind; a later begin in one tab invalidates the earlier challenge, whose finish receives the same expired-ceremony response and cannot change state. Proposed hard bounds for this one-operator service are 128 live ceremonies globally, one per client per kind, and five challenge attempts per ceremony; expired records are pruned before capacity is checked, and new begins fail closed rather than evicting an active ceremony. Persistent bounds are 32 active credentials, 32 live invitations globally/eight per authorizer, 256 sessions globally/32 per credential. Creating a session at capacity atomically invalidates the oldest created session in the affected scope and applies the normal socket/child cleanup.

## Sessions, reauthentication, and invalidation

A successful sign-in or registration issues a 32-byte opaque token in `__Host-shepherdr_session` with `Secure; HttpOnly; SameSite=Strict; Path=/` and no `Domain`. Only its SHA-256 digest is stored with trust ID, creation time, issuance lifetime, optional absolute expiry, and latest passkey-verification time/actual authorizer.

For a finite session, the cookie's `Max-Age` and `Expires` match the remaining absolute server expiry when created or rotated. Reads do not refresh either value. The access manager schedules expiry across restart; at the server deadline it invalidates the record even if a browser retains the cookie. A browser may delete or cap the cookie earlier.

For `-session-lifetime none`, the server record has no clock expiry and the cookie omits `Max-Age` and `Expires`. This is a browser-session cookie: browser shutdown policy, session restore behavior, private browsing, storage eviction, clearing site data, platform policy, or a lost profile can still require sign-in again. The server record survives restart until sign-out, rotation, cap eviction, credential revocation, or reset. Shepherdr does not promise a browser will retain it forever.

The exact ceremony, session, and fresh-verification routes are:

- `POST /api/auth/sign-in/begin` and `POST /api/auth/sign-in/finish`;
- `POST /api/auth/trust/begin` and `POST /api/auth/trust/finish` for invitation enrollment;
- `POST /api/auth/reauthenticate/begin` and `POST /api/auth/reauthenticate/finish`; and
- `POST /api/auth/sign-out` with a JSON body.

Authentication and reauthentication rotate the session token. Reauthentication may use any active passkey, binds the replacement session to the actual credential used, and records a five-minute fresh-verification time. Sign-out invalidates only the current session. There are no state-changing auth GETs.

Successful registration and sign-in also record their asserting credential and time as the initial fresh verification. Every invalidation—sign-out, authentication or reauthentication rotation, capacity eviction, finite expiry, credential revoke, and reset—first makes the old session unusable, then closes its bound Home and Terminal WebSockets, stops their per-socket control/subprocess children, and cancels any in-flight ordinary Terminal read plus its per-request Herdr child before the operation reports success. A process crash closes them by process death. A timer enforces finite expiry even without a new HTTP request. Each Terminal input/takeover and each Home or workspace authority use rechecks the bound session immediately before forwarding; authority accepted by Herdr before invalidation cannot be recalled.

The ordinary `GET /api/terminal/read` and `GET /api/terminal-lab/read` handlers bind their request/child context to the authenticated session as well as ordinary request cancellation. They buffer the result and recheck session authority immediately before committing response headers or body. After sign-out, rotation, expiry, credential revocation, or reset, the child is canceled and no Terminal output is returned.

Sign-out deletes the presented cookie with its original host-only attributes, `Max-Age=0`, and a past `Expires`. Rotation replaces it. Expiry or revocation on another connection cannot delete browser storage remotely, but the token is already unusable; the next HTTP response that presents it sends the same deletion cookie. Reset similarly invalidates server records even though the stopped process cannot edit remote browser storage.

Browser revocation requires a valid session plus fresh verification within five minutes. At commit, one locked operation rechecks the session, actual authorizer, target, freshness, and active-credential count. It succeeds only if at least one active credential will remain. Concurrent attempts cannot both remove the last credential. If the target is the current authorizer, the revocation may succeed only when another credential remains, then invalidates that browser's newly bound session as normal. Stopped-service `access revoke` also refuses the final credential; `access reset` is the only command that deliberately clears all trust.

A credential revocation disables that credential and invalidates all of its sessions and browser-issued invitations in the same access-state replacement. Notification subscriptions are then removed under the cross-store rules below.

## Deny-by-default application boundary

One outer access middleware owns route classification. Unknown routes and any unclassified `/api/` route fail closed. Exact `Host` validation happens before handlers. Exact `Origin` plus `Content-Type: application/json` is mandatory for every POST/PUT/PATCH/DELETE API request, including read-shaped POSTs, ceremonies, notification deletion, and sign-out. GET and HEAD never mutate. No CORS response grants another origin, and no synchronizer CSRF token is added.

| Current or added surface | Protected-mode rule |
| --- | --- |
| `GET /api/home` WebSocket | Valid session and exact WebSocket `Origin` at upgrade; closure on any bound-session invalidation. |
| `GET /api/terminal` WebSocket | Valid session and exact WebSocket `Origin` at upgrade; recheck before input, takeover, release, or control forwarding; stop child/control ownership on invalidation. |
| `GET /api/terminal/read` ordinary JSON fetch | Valid session; bind the Herdr read child to session cancellation, buffer output, and recheck authority before committing the response. No WebSocket `Origin` rule applies. |
| `GET /api/terminal-lab` WebSocket when explicitly enabled | Same WebSocket rules as `/api/terminal`; enabling the lab never bypasses access. |
| `GET /api/terminal-lab/read` ordinary JSON fetch when explicitly enabled | Same session-bound child cancellation and final authority check as `/api/terminal/read`. No WebSocket `Origin` rule applies. |
| `POST /api/workspace-actions/prepare` and `POST /api/workspace-actions` | Valid session, exact `Origin`, JSON, existing size/target checks, and a final authority recheck immediately before the Herdr action. |
| `POST /api/notifications/config`, `POST /api/notifications/settings/read`, `POST /api/notifications/settings`, and `DELETE /api/notifications/settings` | Valid session, exact `Origin`, JSON, bounded body, and commit-time credential/session recheck. A protected subscription records its authorizing trust ID. |
| `GET /api/devices`, `POST /api/devices/invitations`, and `POST /api/devices/revoke` | Valid session; mutations also require exact `Origin`, JSON, fresh passkey verification, and active actual-authorizer recheck at commit. Browser revoke cannot remove final trust. |
| `POST /api/auth/sign-in/begin`, `POST /api/auth/sign-in/finish`, `POST /api/auth/trust/begin`, and `POST /api/auth/trust/finish` | No application session; exact `Origin`, JSON, bounded body, ceremony cookie, one-use challenge, and no application data. |
| `POST /api/auth/reauthenticate/begin` and `POST /api/auth/reauthenticate/finish` | Valid session, exact `Origin`, JSON, bounded body, and the reauthentication ceremony rules; finish verifies an active passkey and rotates the session. |
| `POST /api/auth/sign-out` | Valid session, exact `Origin`, JSON body, and state-changing invalidation of the current session. |
| Static sign-in/bootstrap assets, manifest, service worker, and `/healthz` | Exact Host, fixed content only, no Herdr/application data or mutation authority. |

The application keeps `frame-ancestors 'none'`, `form-action 'none'`, no-referrer policy, no CORS, and text-only display handling. Herdr output, agents, Terminal content, repository files, paths, IDs, attachments, pasted content, device labels, and all browser strings are untrusted. None may choose an access record, authorizer, redirect, route, Terminal target, subscription owner, invitation, or mutation. Authentication destinations are fixed same-origin paths.

## Persistent state, locking, and crash invariants

Add an owner-only versioned `access.json` beside existing Shepherdr configuration. It stores only:

- exact canonical public origin, derived RP ID, session-lifetime configuration, schema version, and random operator user handle;
- trust ID, untrusted label, timestamps, and complete library credential record, including backup eligibility and last-reported backup state/time;
- hashed session tokens with credential owner, issuance policy, optional absolute expiry, and fresh-verification authorizer/time; and
- hashed invitation tokens with actual issuer, issue/expiry/reservation/consumption state.

Private keys and biometric data never reach Shepherdr. Raw session/invitation tokens and ceremony challenges are not persisted. There is no profile, password, email, recovery secret, hardware inventory, IP history, conversation store, or general audit log.

Create the configuration directory `0700` and state/lock files `0600`. Refuse symlinks, wrong ownership, non-regular or multiply linked files, unsafe permissions, oversized state, unsupported schema, unknown security-critical fields, and invalid records. Use a same-directory temporary file, file sync, atomic rename, and directory sync. Corruption or a failed migration prevents protected startup.

The lifetime service lock excludes CLI writers. Inside the process, one authority gate and deterministic store order cover access and notification changes:

1. acquire the authority/access write lock;
2. recheck session, credential, fresh authorizer, invitation, and last-credential rules at the actual commit boundary;
3. durably commit access invalidation or enrollment first;
4. durably update notification subscriptions second; and
5. release only after the in-memory authority view matches durable access state.

Invitation consume plus credential/session creation is one access-file replacement. Session rotation adds the replacement and invalidates the old record in one replacement. If either fails, the old authority remains. Revocation disables the credential and its sessions/invitations in one access replacement; reset clears all credential/session/invitation records and rotates the user handle in one replacement while retaining approved configuration. Both commit access inactivity before notification cleanup. A crash before that commit leaves old authority unchanged; a crash after it may leave orphan notification records, but those records are inert and are removed on restart. Notification state can never grant access.

Push dispatch shares the authority gate through its bounded Web Push request: dispatch takes a read lease, rechecks the subscription and its active owning trust, and holds the lease until the request is accepted or fails; revoke/reset takes the write lease and therefore waits for earlier dispatches. The exact cutoff is the successful access invalidation commit: no new Web Push request begins afterward. A request accepted by a push service before the commit may still arrive within the existing five-minute push TTL and cannot be recalled.

Subscription creation/removal rechecks authority while holding the write lock. Revocation removes the target's subscriptions after access commit. Reset preserves the VAPID key/contact identity but empties subscriptions. On startup, any subscription without an active owner is suppressed and cleaned. These rules make cross-file crashes fail closed without introducing a database transaction.

**Assumption:** the operating-system account running Shepherdr and its owner-only configuration directory are trusted. A same-account attacker can already reach Herdr, run the stopped-service CLI, or replace the executable. Encryption with a key stored under that same account does not change this boundary.

## Maintained library boundary

Use [`github.com/go-webauthn/webauthn`](https://github.com/go-webauthn/webauthn) on Go 1.26 and pin the current reviewed maintained tag in the implementation brief. As of this revision, [`v0.17.4`](https://github.com/go-webauthn/webauthn/releases/tag/v0.17.4) is current; its [`go.mod`](https://github.com/go-webauthn/webauthn/blob/v0.17.4/go.mod) supports this baseline. It supplies relying-party ceremonies, discoverable/usernameless flows, storage types, backup flags, and a security process, but remains pre-v1, so upgrades require release-note and compatibility review.

Use the standard browser API without a hosted SDK. Use a maintained QR encoder/terminal renderer such as [`github.com/mdp/qrterminal/v3`](https://github.com/mdp/qrterminal) rather than implementing QR encoding. Go `crypto/rand` and `crypto/sha256` are appropriate for opaque tokens and stored digests; they do not replace WebAuthn verification.

## Migration and protected cutover

The first release implementing this proposal changes an unflagged start to protected mode. An installation without access state must supply `-public-origin` or startup exits with the exact command required. An operator intentionally retaining present authority supplies `-no-sign-in` on every start.

No existing browser, private-network member, Herdr identity, cookie, Terminal, displayed content, or notification subscription becomes trusted. Preserve VAPID identity, but treat legacy subscriptions without a credential owner as inactive; after sign-in the browser must explicitly enable notifications again. Sign-in-off mode retains today's notification semantics without rewriting protected trust.

Origin/RP-ID changes cannot migrate existing passkeys. `access reset` deliberately preserves origin, so changing the private hostname is not an implicit reset side effect and has no supported first-slice workflow. It needs a separately reviewed local migration procedure. Lost passkeys can be replaced only through explicit machine-local authority; there is no remote/self-service recovery.

There must be no partial protected cutover. Before the default changes, the first protected slice includes:

- access-state migration, canonical origin, Go/library baseline, CLI administration, bootstrap, invitation, registration, sign-in/sign-out/reauthentication, configurable sessions, reset, and sign-in-off behavior;
- the outer gate and commit-time authority checks for every current HTTP/JSON path listed above, both production Home and Terminal WebSockets, the optional Terminal lab, and every workspace/notification/subscription mutation;
- notification ownership, cross-store crash behavior, push cutoff, socket closure, and Terminal child/control cleanup for every invalidation path; and
- authenticated regressions for all existing Home, Terminal, workspace, notification, service-worker, and real Herdr workflows plus the desktop/Android protected flow.

Internal commits may stage that work, but protected default is not reviewable or releasable until the complete route inventory and real gates pass together. There is no migration-only or device-management follow-up that leaves current authority partially protected.

## Risks

- Invitation URLs are temporary bearer authority. Fragment transport reduces server/referrer exposure but not browser, extension, screen, clipboard, QR, terminal, or observer leakage.
- Synced copies deliberately share one trust record. Credential revocation cannot target a physical copy or delete it from its provider.
- A hostname loss makes passkeys unusable. There is no origin-change workflow in this slice.
- A stolen session works until invalidation; `none` can make server-side duration indefinite. Browser protections and private networking reduce but do not eliminate compromise.
- A push accepted before revocation may arrive after it. No push begins after the durable access cutoff.
- A local same-account compromise and an operator's reverse-proxy/tailnet exposure mistakes are outside the application boundary.
- `go-webauthn` is pre-v1 and maintenance may require coordinated Go upgrades.
- Browser/authenticator behavior varies. Shepherdr reports verified ceremony results and last-reported signed backup flags, not provider, biometric, hardware, or physical-device claims.

## Real acceptance checks

Use the production executable, real Herdr, one desktop browser, and a real Android phone at the canonical private HTTPS origin. Automated browser checks use the repository's memory-capped entry and compare primitive values.

- From empty state, confirm exactly one local link/QR and bootstrap-only HTTP behavior. Direct localhost/IP, hostname aliases, alternate ports, malformed/noncanonical `-public-origin` values, wrong Host/Origin, forwarded-header forgeries, cross-origin WebSockets, non-JSON mutations, state-changing GETs, oversized bodies, and replays all fail before authority.
- Enroll once through the desktop link and once after reset by scanning on Android. Confirm discoverability and user verification, no attachment restriction, ten-minute outer expiry, reservation capped at five minutes/outer expiry, single consumption, same-client retry bounds, concurrent-tab invalidation, racing-client exclusion, restart behavior, and generic failure responses.
- From a trusted browser, reauthenticate with a different active passkey where available, create an invitation, and revoke a credential. Confirm the actual asserting trust is recorded, stale/now-revoked authorizers fail at commit, and two concurrent revokes cannot remove the final credential.
- Sign in usernameless and exercise `30d` plus `none` sessions. Confirm no sliding extension, restart persistence, exact finite cookie attributes, browser-session attributes for `none`, rotation/sign-out/cap/expiry/revoke/reset invalidation, and closure of all bound Home/Terminal sockets and child/control processes. Use an injectable clock rather than real waits.
- List records with truthful synced-passkey language and last-reported backup state/time. If platform sync is available, assert from another copy and confirm it remains the same trust/revocation record.
- Exercise every listed production route while authenticated: full Home, exact Terminal desktop read and phone send/control/takeover, workspace prepare/create/close/delete, notification config/settings/subscription, service-worker click, stale Terminal handling, and a real Herdr state transition.
- Revoke a non-current credential while HTTP, Home, Terminal, invitation, and push activity is live. Confirm immediate authority denial, access-first crash invariants, socket/child shutdown, invalid invitations, subscription cleanup, no push request after cutoff, and only a pre-cutoff accepted push can arrive within TTL.
- Feed hostile Herdr names/output, paths, IDs, labels, attachments, and pasted content through real views. Confirm none can authorize, redirect, enroll, select a credential, subscribe, open another Terminal, or mutate.
- Migrate today's state. Confirm no implicit trust, VAPID identity preservation, legacy-subscription suppression, explicit notification re-enable, `-no-sign-in` current behavior/warning, protected return on restart, stopped-service CLI refusal while running, and corrupt-state fail-closed behavior.
- Run stopped-service invite/list/revoke/reset. Confirm revoke refuses the final credential, reset clears exactly the approved state while preserving origin/VAPID identity, and only the next protected start creates a new bootstrap invitation.

## Suggested implementation split after approval

Use one worker for one complete protected-cutover slice. It may organize internal changes by store, ceremony, gate, and UI, but its deliverable is the full migration and every affected HTTP/JSON/WebSocket/workspace/notification/Home/Terminal path above. The first gate is a real desktop-link and Android-QR enrollment followed by authenticated Home, Terminal, workspace, notification, restart, revoke, reset, and sign-in-off workflows.

An independent security reviewer verifies the exact route inventory, canonical-origin cases, ceremony/session bounds, atomic last-credential and commit-time checks, cross-store crash points, push cutoff, and socket/child cleanup. An integrator then runs the repository gates and repeats the production desktop/Android/Herdr workflow from the reviewed commit. No later wave starts from a worker branch, and this proposal does not approve any brief or integration.

## Remaining assumptions and implementation details

- The finite `-session-lifetime` upper bound and the user-facing wording for invalid values remain implementation-brief details; the accepted syntax, default, persistence, and `none` semantics are fixed above.
- Platform ceremony screens and whether a test credential actually syncs depend on the desktop/Android authenticators. Acceptance must record observed behavior without inferring a provider or physical identity.
- A future private-hostname/RP-ID migration procedure remains out of scope. Reset preserves the existing origin as approved.
- Exact UI copy for the Devices explanation, last-reported backup state, bootstrap errors, and browser cookie loss still needs human UI review under `docs/ui-direction.md`; it cannot weaken the security behavior.

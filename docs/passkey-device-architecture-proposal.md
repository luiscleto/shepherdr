# Passkey sign-in and trusted-device architecture proposal

Status: proposed for human review; not approved

Base: `fc8facb72f7a854b078f0b7ff6ffa273211d9fe2`

## Scope and labels

This proposal adds an access boundary to the existing one-Go-process, loopback-only, same-origin Shepherdr application. It does not change Herdr authority, Home, Terminal, workspace behavior, notification semantics, or the private-network requirement.

The labels below are deliberate:

- **Confirmed** describes current repository or browser-platform behavior.
- **Proposed** is a recommendation that needs human approval.
- **Assumption** names a boundary this design depends on.
- **Human decision** is unresolved and blocks an implementation brief.

The preserved human direction is:

- Passkey sign-in is the recommended stronger mode and still requires a trusted private network.
- Starting without sign-in requires an explicit CLI flag; anyone who can reach that instance has operator authority.
- First protected initialization prints both a one-time link and terminal QR code. The link works on that computer through the configured browser origin, and a phone can scan the QR code.
- Later one-time links and QR codes can be created by an explicit local CLI action or an already trusted device. Merely reaching Shepherdr never authorizes enrollment, and a new device cannot authorize itself.
- There is one operator and many trusted devices, with list, revoke, and full local reset operations.
- There is no lost-device recovery, team, role, organization, public hosting, Chat, or hosted identity provider.

## Confirmed boundaries and platform behavior

- **Confirmed — current Shepherdr:** one Go process listens only on loopback, talks to one Herdr session, serves the embedded TypeScript application, bridges Home and Terminal WebSockets, performs workspace mutations, and owns local Web Push state. There is currently no authentication middleware. Some mutation handlers check origin, but the rules differ and the read and WebSocket surfaces are intentionally open in sign-in-off mode.
- **Confirmed — origin and RP ID:** WebAuthn credentials are scoped to an RP ID. The RP ID is a domain name without scheme or port and must equal, or be a registrable suffix of, the calling origin's effective domain. WebAuthn normally requires HTTPS; `http://localhost` is a defined exception. The relying party must verify the expected origin and RP ID. See [WebAuthn Level 3](https://www.w3.org/TR/webauthn-3/) and [Secure Contexts](https://www.w3.org/TR/secure-contexts/).
- **Confirmed — passkeys are not physical-device identity:** WebAuthn signs backup eligibility and backup state. A backup-eligible credential may be synced or otherwise copied to another authenticator. The relying party cannot identify which physical copy made a valid assertion or selectively revoke one copy. FIDO calls these synced passkeys and distinguishes them from device-bound passkeys. See [WebAuthn credential backup state](https://www.w3.org/TR/webauthn-3/#sctn-credential-backup) and [FIDO's passkey terminology](https://fidoalliance.org/passkeys/).
- **Confirmed — counters are only a signal:** authenticators may keep a signature counter at zero, and a non-increasing counter can mean a clone, malfunction, or race. It is not proof of which physical authenticator acted. See [WebAuthn signature-counter considerations](https://www.w3.org/TR/webauthn-3/#sctn-sign-counter).
- **Confirmed — private HTTPS proxy:** Tailscale Serve can terminate HTTPS at a stable tailnet DNS origin and reverse-proxy to `127.0.0.1`; publishing and DNS naming remain operator-owned. See [Tailscale Serve](https://tailscale.com/docs/reference/tailscale-cli/serve). Funnel remains unsupported.
- **Confirmed — WebSockets:** browsers send an `Origin` header in the opening handshake and servers intended for selected sites should verify it. See [RFC 6455, section 10.2](https://www.rfc-editor.org/rfc/rfc6455#section-10.2).

## Smallest recommended shape

**Proposed:** keep the current process and same-origin UI. Make protected mode the default. Add one versioned local access-state file, one access manager around the existing HTTP handler, native browser WebAuthn calls, and a maintained Go WebAuthn verifier. The public browser address stays private-network HTTPS while the Go listener stays on loopback.

One passkey credential record is one Shepherdr trust record. It may represent one device-bound passkey or every physical device holding a synced copy. A successful passkey assertion creates a bounded browser session. All existing real application authority sits behind that session. Enrollment requires a one-time invitation issued locally or by an already trusted, freshly authenticated session.

When no credential exists, the process exposes only static setup/sign-in assets, the invitation ceremony, and health. Home, Terminal, workspace actions, notification settings and subscriptions, device management, and every WebSocket remain unavailable. The first successful invitation atomically creates the operator's first credential and session; the normal application then becomes available without starting another runtime.

## Human decisions and smallest useful options

| Question | Smallest options and trade-off | Recommendation, still unapproved |
| --- | --- | --- |
| What is a trusted “device”? | **Credential-scoped:** smallest and works with ordinary synced passkeys, but one row and one revocation cover every synced copy. **Browser-installation binding:** add a non-syncing browser secret beside the passkey; it permits per-browser revocation but is brittle when site data is cleared and still is not physical attestation. **Device-bound-only:** reject backup-eligible credentials; reduces sync ambiguity but excludes normal platform passkeys and still does not prove a physical-device identity. | Credential-scoped trust. Call the screen **Devices**, but explain that a passkey may sync and that revocation removes the passkey from Shepherdr, not from a password manager. If per-installation revocation is a requirement, choose the binding option before implementation. |
| Behavior before first trust | Run a **bootstrap-only service** that can complete the printed invitation, or refuse all HTTP service until a separate initialization command has completed. | Bootstrap-only. It supports desktop link and phone QR enrollment while disclosing no Herdr data or authority. |
| Browser origin and RP ID | One exact stable private HTTPS origin, or multiple origins/related-origin support. Multiple origins add policy and migration work and can confuse the authenticator UI. | One exact origin. Derive and persist the RP ID as that origin's hostname. Do not support direct localhost as a second protected origin in the first slice. |
| Invitation authorization | A high-entropy bearer link issued by existing authority, or a live two-device approval protocol. Live approval narrows link theft but adds pairing and simultaneous-device state. | A ten-minute, single-use bearer link in the URL fragment, plus a five-minute server-side WebAuthn ceremony. Require fresh passkey verification for browser-issued links. |
| Session behavior | In-memory sessions end on restart, persistent bounded sessions survive restart, or sessions last until revocation. | Persistent, 30-day absolute sessions with no sliding extension. Restart preserves them; credential revocation and reset invalidate them. |
| Browser request and WebSocket defense | Keep mixed handler-local checks, add per-route auth wrappers, or put one deny-by-default session/origin gate around the application. A separate CSRF token adds state but helps when cookie/origin controls are weaker. | One central gate, exact configured origin, host-only `SameSite=Strict` cookie, mandatory same-origin JSON for ceremonies and mutations, and explicit WebSocket origin checks. No separate CSRF token in the first slice. |
| Local administration | Stop the server for every command, let two processes edit one file, or use an owner-only Unix control socket while running and an exclusive file lock while stopped. | Unix control socket online; exclusive direct state access offline. Never allow concurrent direct edits. |
| Persistent store | One bounded atomic JSON file, or an embedded database. | One JSON access file, matching the scale and existing notification-store pattern. Add a database only if a confirmed concurrency or scale need appears. |
| Go WebAuthn library | Upgrade Go and use the current maintained `go-webauthn`; pin an older release compatible with Go 1.22; or adopt a smaller alternative with less release history. | Upgrade the project baseline and pin a reviewed current `github.com/go-webauthn/webauthn` release. Do not freeze an old security library merely to retain Go 1.22. |
| Last credential | Permit remote removal and rely on local reset, or prevent the browser from removing the last credential. | A browser may revoke itself only when another credential remains and may never revoke the last credential. Local CLI revoke/reset may remove the last credential and return the service to bootstrap-only state. |

## Exact proposed modes, origin, and CLI

### Server modes

**Proposed protected default:**

```sh
./bin/shepherdr -public-origin https://shepherdr-host.example.ts.net
```

`-public-origin` is required only on first protected initialization and is then persisted. It must be one absolute HTTPS origin with no user information, path other than `/`, query, or fragment. A non-default port is part of the origin. The RP ID is the lower-cased ASCII hostname only; there is no separate `-rp-id` flag in the first slice. A later start may omit `-public-origin`; a supplied mismatch fails closed.

On first protected initialization, the process creates the operator user handle and exactly one local bootstrap invitation, prints its link and QR code, and serves only the bootstrap surface until it succeeds. If the process restarts first, the unexpired invitation remains valid but cannot be reprinted because its plaintext is not stored; `access invite` replaces that bootstrap invitation and prints a new one. If it expires, the process stays bootstrap-only and tells the local operator to run that command.

The name “public origin” describes the browser-visible origin, not public hosting. The address must remain restricted to the trusted private network. The existing `-listen` validation continues to accept loopback only. The reverse proxy must preserve the browser `Host`; Shepherdr compares `Host` and every browser `Origin` to the configured value rather than trusting `X-Forwarded-Proto` or forwarded identity headers.

The local computer opens the same private HTTPS link printed in the terminal. `http://localhost` is valid WebAuthn platform behavior but would be a different origin and RP ID from the phone's HTTPS address. Supporting both would weaken the one-origin model, so it is excluded from this proposal.

**Proposed explicit sign-in-off mode:**

```sh
./bin/shepherdr -no-sign-in
```

`-no-sign-in` applies to that start only and is never persisted. It preserves current behavior and the visible **Sign-in is off** notice: every reachable browser has operator authority. It does not erase protected state. Starting later without the flag returns to protected mode. A corrupt or unreadable access store never falls back to this mode.

### Local administration

**Proposed command names:**

```sh
./bin/shepherdr access invite
./bin/shepherdr access devices
./bin/shepherdr access revoke <device-id>
./bin/shepherdr access reset
```

- `access invite` creates and prints one link, one terminal QR code, and its expiry. The plaintext token is shown only once and never enters ordinary logs.
- `access devices` lists a short Shepherdr trust ID, human label, created and last-used times, and whether the credential is backup-eligible or currently backed up. It does not claim a hardware model, browser identity, location, or unique physical device.
- `access revoke` invalidates that credential, its sessions, its outstanding invitations, and its notification subscriptions. Removing the last credential returns the service to bootstrap-only state and the command immediately creates and prints a new first-device invitation, whether the server is running or stopped.
- `access reset` rotates the operator user handle and removes all credentials, sessions, invitations, and notification subscriptions. It preserves the configured VAPID key/contact and browser origin, then immediately creates and prints a new first-device invitation. Changing origin requires stopping the server and running `./bin/shepherdr access reset -public-origin https://new-private-name`, because existing WebAuthn credentials cannot move to another RP ID.
- The existing notification-only reset remains separate because it also deletes VAPID keys and contact.

While the server runs, these commands use an owner-only Unix socket under the protected Shepherdr configuration directory. The server is the only state writer. While stopped, a command takes an exclusive lock and edits state directly. If the service lock is held but the control socket cannot be reached, the command fails instead of guessing that the service is stopped. The control protocol is local, bounded, and never exposed through HTTP.

An offline `access invite` uses the persisted origin; before any state exists it requires `-public-origin`. Shell access as the service's operating-system account is the explicit local authority already allowed by the human direction. It is not a remote lost-device recovery channel.

## Enrollment and sign-in protocol

### WebAuthn policy

**Proposed:** use one stable random, non-identifying WebAuthn user handle for the one operator. Registration requests a discoverable credential (`residentKey: required` and the compatible `requireResidentKey: true`), requires user verification, leaves authenticator attachment unset so platform and roaming authenticators work, requests `attestation: none`, and excludes every already registered credential ID. Authentication is usernameless, requires user verification, and accepts only an active stored credential for that user handle and RP ID.

Use native `navigator.credentials.create()` and `navigator.credentials.get()` in the embedded UI. Use the Go library to create options and challenges, parse browser responses, and verify challenge, type, exact origin, RP-ID hash, user presence, user verification, signature, credential ID, user handle, backup flags, and counter state. Do not implement CBOR, COSE, attestation, or signature verification in Shepherdr.

Persist the complete credential record the selected library requires and update its counter, backup state, and last-used time after successful authentication. A counter warning is recorded as a diagnostic; because the standard says it is not proof, this proposal does not automatically revoke or claim a clone.

### Invitation lifecycle

**Proposed exact flow:**

1. Local CLI authority, or an authenticated browser session reverified by its own passkey within the last five minutes, asks the server for an invitation.
2. The server creates a 32-byte random bearer token with the Go cryptographic random source, stores only its SHA-256 digest with issuer and expiry, and returns `https://configured-origin/#trust=<base64url-token>`. A maintained QR encoder renders exactly that link. The fragment keeps the bearer out of reverse-proxy requests and referrers; the UI immediately removes it from browser history after reading it.
3. The new browser supplies a short device label and posts it with the token to the same origin. The label is untrusted display text only. The server atomically validates that the digest is unused and unexpired and, for a browser-issued invitation, that its issuing credential is still active. It reserves the invitation to a short-lived, random, `HttpOnly` ceremony cookie and returns library-created registration options. A link preview that only fetches the URL cannot reserve it because the token is in the fragment and reservation requires a JSON POST.
4. Only that ceremony cookie may submit the registration result. One active reservation lasts five minutes. Abandoning it releases the invitation until its ten-minute outer expiry; starting a new ceremony in the same reservation invalidates the prior challenge.
5. On valid WebAuthn verification, one locked state replacement adds the credential, consumes the invitation, and creates the browser session. If persistence fails, none of those changes is accepted. Two racing completions serialize; only the first can succeed.
6. A consumed, expired, revoked-issuer, malformed, or unknown token receives the same generic invalid-invitation response. Challenges are one-use and memory-only. A restart cancels ceremonies but leaves an unconsumed, unexpired invitation usable from a new ceremony.

Enrollment without a valid invitation does not exist. A physical device holding a synced copy of an already trusted credential can authenticate under the credential-scoped recommendation; that is use of existing transferred authority, not a new Shepherdr enrollment. If the human meaning of “new device” must exclude this case, credential-scoped trust is the wrong option.

### Browser sessions and revocation

**Proposed:** after a successful assertion, issue a server-generated 32-byte opaque token in `__Host-shepherdr_session` with `Secure`, `HttpOnly`, `SameSite=Strict`, `Path=/`, and no `Domain`. Store only its SHA-256 digest, absolute expiry, credential trust ID, creation time, and latest passkey-verification time. Authentication and reauthentication rotate the token. Sign out deletes only the current session.

Sign-in begin and finish are bounded same-origin JSON POSTs using a separate random, `HttpOnly` five-minute ceremony cookie and one-use memory-only library challenge. Begin returns usernameless request options with no credential allow-list; finish accepts only a returned active credential with the stable operator handle, atomically updates its library record, and creates the session. A restart cancels an unfinished sign-in without affecting credentials or existing sessions.

Sessions expire 30 days after creation and do not slide on ordinary reads. They survive Shepherdr restart. Expired records are pruned. Every HTTP request and WebSocket upgrade resolves the session and confirms that its credential remains active.

Revocation first marks the credential inactive in the access manager, then invalidates its sessions and outstanding invitations, removes or disables its notification subscriptions, and closes its active Home and Terminal sockets before returning success. Terminal input handlers recheck the bound session before forwarding each browser command. A Herdr mutation or push already accepted before revocation cannot be recalled; a queued push may still arrive within the approved notification TTL. Revocation does not erase a passkey from a password manager or authenticator.

## Authorization, CSRF, origin, and untrusted content

**Proposed:** place one deny-by-default authorization middleware outside the existing mux rather than adding optional checks handler by handler.

| Surface | Protected-mode rule |
| --- | --- |
| Home state and `GET /api/home` WebSocket | Valid session at upgrade, exact configured `Origin`, and closure on expiry or revocation. |
| Terminal read, observe, control, takeover, release, and both production WebSocket paths | Valid session; exact `Origin` for WebSockets; session recheck before input or takeover. The development lab, when explicitly enabled, is protected too. |
| Workspace prepare and every mutation | Valid session, exact `Origin`, JSON content type, existing size and target validation. |
| Notification config, settings read/save/delete, and subscription creation/removal | Valid session, exact `Origin`, JSON content type. A protected subscription records the credential trust ID that authorized it. |
| Device list, invitation creation, and revocation | Valid session and exact `Origin`; invitation creation and revocation additionally require a passkey verification no older than five minutes. |
| Static assets, manifest, service worker, sign-in begin/finish, invitation begin/finish, and `/healthz` | No application session required. Ceremony POSTs still require exact `Host` and `Origin`, bounded JSON, one-use server challenge, and no CORS. These routes expose no Herdr data or mutation authority before authentication. |

Use the configured origin as the one comparison source even though TLS terminates at the private proxy. Do not infer security decisions from forwarded headers. Reject absent, `null`, cross-origin, user-info, path-bearing, or mismatched origins on every ceremony and state-changing request. `SameSite=Strict` cookies and mandatory exact-origin JSON requests are the CSRF defense; a separate synchronizer token adds no useful first-slice protection. Never use a state-changing GET. WebSockets check the session during the HTTP upgrade and an explicit `CheckOrigin` function checks exact configured origin.

Keep the existing no-CORS policy, `frame-ancestors 'none'`, `form-action 'none'`, no-referrer policy, and text-only handling of Herdr and terminal content. Herdr names, output, paths, IDs, agents, repository files, attachments, pasted content, device labels, and browser-supplied strings remain untrusted data. They cannot choose an access record, invitation issuer, redirect, route, terminal target, notification owner, or mutation. Authentication redirects are fixed same-origin paths rather than browser-supplied URLs.

## Minimum persistent state and filesystem boundary

**Proposed:** add `${userConfigDir}/shepherdr/access.json`; do not merge it with Home or terminal state. Store only:

- schema version, exact browser origin, derived RP ID, and random operator user handle;
- one trust ID, untrusted display label, timestamps, and the complete library-required WebAuthn credential record per credential;
- hashed session tokens with credential owner and absolute expiry; and
- hashed invitation tokens with issuer and expiry.

WebAuthn private keys and biometric material never reach Shepherdr. Ceremony challenges and raw session or invitation tokens are not persisted. There is no account profile, password, email, recovery secret, hardware inventory, IP history, conversation data, or audit log.

Create the configuration directory as owner-only (`0700`) and the regular state and lock files as owner-read/write (`0600`). Refuse symlinks, wrong ownership, non-regular files, oversized files, unknown fields, unsupported versions, invalid credential records, and unsafe permissions. Replace state through a same-directory temporary file, file sync, atomic rename, and directory sync. Bound record counts and request sizes. Corruption or an unsupported migration disables protected startup; it never creates a new identity or enables sign-in-off mode.

**Assumption:** the operating-system account running Shepherdr and its owner-only configuration directory are trusted. A same-account attacker can already reach the Herdr socket, run the local CLI, or replace the executable. Encrypting the credential record with a key stored under that same account does not change that boundary. If protection from other same-account processes or untrusted backups becomes a requirement, OS-keystore-backed encryption is a separate human decision.

The current notification file remains separate. Protected subscriptions gain an owning trust ID. Send-time lookup suppresses subscriptions whose owner is missing or revoked, so a partial cleanup cannot continue sending future notifications.

## Library recommendation

**Proposed:** use [`github.com/go-webauthn/webauthn`](https://github.com/go-webauthn/webauthn) and pin a reviewed tagged release. It provides full relying-party ceremonies, passkey/usernameless flows, credential storage types, backup flags, and a published security process. As of this proposal, [`v0.17.4`](https://github.com/go-webauthn/webauthn/releases/tag/v0.17.4) is current, while its [`go.mod`](https://github.com/go-webauthn/webauthn/blob/v0.17.4/go.mod) requires Go 1.25 and the library is still pre-v1. The repository currently declares Go 1.22.

The recommended path is to approve a Go baseline upgrade, pin the dependency, serialize its supported credential type rather than duplicating cryptographic fields, and review release notes before every upgrade. Pinning `v0.11.0` would retain Go 1.22 but intentionally misses later validation and security work; it is not recommended. `github.com/pomerium/webauthn` is a possible lower-level alternative, but its lower-level ceremony API and lack of a tagged release line would leave more integration and upgrade policy in Shepherdr.

Use the standard browser API without a hosted SDK. Use a maintained QR encoder/terminal renderer, such as [`github.com/mdp/qrterminal/v3`](https://github.com/mdp/qrterminal), instead of writing QR encoding. Ordinary Go `crypto/rand`, `crypto/sha256`, `net/http`, and constant-time comparisons are sufficient for opaque tokens and cookies; they are not substitutes for WebAuthn verification.

## Migration from today's sign-in-off installations

1. The first release with this direction changes an unflagged start to protected mode. An installation without access state must supply `-public-origin`; otherwise startup exits with the exact protected-start command to run. Operators who intentionally retain current authority use `-no-sign-in` on every start.
2. No existing browser, private-network member, push subscription, Herdr identity, terminal, cookie, or displayed content becomes trusted automatically. The first protected run uses only the locally printed invitation.
3. Preserve existing VAPID keys and contact. On first protected start, legacy subscriptions without a trust owner become inactive and the browser must enable notifications again after sign-in. In explicit sign-in-off mode, current subscription semantics remain.
4. Preserve existing Home, Terminal, workspace, and notification behavior behind the access gate. Do not add auth state to Herdr or use Herdr identities for authorization.
5. Changing the configured origin or RP ID cannot transparently migrate passkeys. The supported first-slice path is local reset and reenrollment at the new origin. There is no remote recovery if every usable passkey is lost.

## Risks and exclusions

- An invitation is temporary bearer authority. Screen capture, terminal logs outside Shepherdr, clipboard history, or another person scanning it can give the first successful user trust. Short expiry, fragment transport, single-use persistence, fresh issuing authority, and local regeneration limit but do not remove that risk.
- A synced passkey deliberately expands one trust record across its copies. Revocation is reliable at the credential ID, not at a physical-device copy. Backup flags improve truthful description but are not device inventory.
- Losing or renaming the private HTTPS origin makes its credentials unusable. Local reset is the only recovery in scope.
- A stolen live session acts until absolute expiry or revocation. `HttpOnly`, `Secure`, `SameSite=Strict`, exact-origin checks, CSP, token hashing, and bounded lifetime reduce exposure; they do not make a compromised browser trustworthy.
- A local same-account compromise is outside the proposed boundary. Filesystem protections prevent accidental or cross-account disclosure, not a hostile operator account.
- Reverse-proxy or tailnet mistakes can expose the service more broadly than intended. Passkeys are an extra lock, not approval for Funnel or public hosting.
- Revocation cannot undo a mutation already sent to Herdr or retract a push already accepted by a push service.
- WebAuthn library upgrades may be breaking before v1 and security maintenance may require newer Go versions.
- Browser and authenticator UX varies. Shepherdr must report only ceremony success or failure and signed backup flags, not guessed provider, hardware, biometric, or device state.

Excluded are password fallback, emailed codes, recovery codes, lost-device recovery, attestation allowlists, physical-device attestation, teams, roles, organizations, hosted identity, public hosting, cross-origin/related-origin WebAuthn, Chat, a second runtime, and Herdr protocol changes.

## Real acceptance checks

Use the production executable, real Herdr, one desktop browser, and a real Android phone at the one configured private HTTPS origin. Automated browser checks use the repository's memory-capped entry and compare primitive results.

- Start from empty access state. Confirm the CLI prints one link and scannable QR, while unauthenticated Home, Terminal read/control, workspace actions, notification APIs, Devices, and both WebSockets reveal or perform nothing.
- Complete first trust once from the desktop link and once in a separate reset run by scanning with Android. Confirm user verification is required, the invitation is consumed only after valid registration, replay fails, an abandoned reservation can resume before outer expiry, and an invented or expired token cannot enroll.
- From a trusted Android session, reverify with its passkey, create a later invitation, and enroll the desktop. Confirm an untrusted browser has no enrollment action without that link.
- Sign out and sign in usernameless on desktop and Android. Restart Shepherdr and confirm unexpired sessions survive. Use an injectable clock in focused tests to prove challenge, invitation, fresh-auth, and 30-day session expiry without real waits.
- List both trust records with truthful labels and backup state. If the test passkey is backup-eligible, exercise a synced or cross-device assertion when the platform permits and confirm it remains one trust record; do not report that as a newly identified physical device.
- Revoke a non-current credential and verify its HTTP requests fail, active Home and Terminal sockets close, terminal input stops before forwarding, its outstanding invitations fail, and no later push is sent to its subscriptions. Confirm the browser cannot revoke the last credential and local CLI reset can.
- Exercise every existing production path while authenticated: complete Home, exact Terminal read and phone send, takeover rules, workspace creation/close/deletion, notification enable/settings/subscription, a real Herdr transition, notification click, and stale-terminal handling. This proves the central gate did not replace the real workflow with mocks.
- Send cross-site, absent-origin, `null`-origin, wrong-Host, forged forwarded-header, cross-origin WebSocket, non-JSON, oversized, and replayed ceremony requests. Confirm they fail before application authority. Direct loopback Host access must not become a second protected origin.
- Exercise hostile Herdr names, terminal output, paths, identifiers, device labels, and pasted content. Confirm none can create or consume an invitation, choose a trust record, redirect, subscribe, open another terminal, or perform a mutation.
- Start with `-no-sign-in` and confirm the current sign-in-off workflow and warning. Restart without the flag and confirm protected mode returns without losing trust state. Confirm corrupt state fails closed.
- Migrate a current installation with notification state. Confirm protected mode preserves VAPID configuration but sends nothing to an unowned legacy subscription until the browser signs in and explicitly enables it again.

## Suggested implementation split after approval

Prefer one worker until the first protected desktop-and-phone loop passes, then independent review and integration from the approved integrated commit.

1. **Protected first loop:** canonical origin, access store and locking, current WebAuthn library, initial/local invitation, first credential, sign-in/sign-out, persistent session, reset, `-no-sign-in`, central HTTP/WebSocket gate, and protected notification ownership. Gate on a real desktop link and Android QR registration followed by real Home and Terminal use.
2. **Many-device management:** trusted-device invitation, Devices list, fresh passkey verification, CLI list/revoke, browser revocation, immediate socket/session cleanup, and truthful synced-passkey language. Gate on desktop plus Android enrollment, list, sign-in, and revoke.
3. **Migration and full workflow gate:** legacy notification migration, origin/reset failure cases, hostile-input and CSRF coverage, restart and expiry checks, then the full real Herdr, workspace-action, Terminal, and notification workflow above.

Each result still requires an independent reviewer, an integrator, and the real workflow gate. This proposal does not approve those briefs or their ordering.

## Unresolved human decisions

Implementation is waiting on approval or replacement of these recommendations:

1. Whether a trusted device is credential-scoped, accepting that synced copies share one trust and revocation, or whether Shepherdr must add a browser-installation binding or reject backup-eligible credentials.
2. Whether protected mode requires exactly one stable private HTTPS `-public-origin`, derives the RP ID from its host, and excludes direct `http://localhost` as a second origin.
3. Whether invitations use the proposed ten-minute bearer-link/five-minute reservation rules and whether browser creation and revocation require passkey verification within five minutes.
4. Whether sessions persist across restart for a non-sliding 30-day absolute lifetime.
5. Whether one central session/origin gate, strict same-origin cookies and JSON, explicit WebSocket origin checks, and no separate CSRF token are the accepted browser boundary.
6. Whether the proposed `access invite|devices|revoke|reset` CLI, owner-only control socket, offline lock behavior, and last-credential rules are acceptable.
7. Whether to raise the Go baseline to use the current maintained `go-webauthn` line rather than pinning an older compatible version.
8. Whether access reset should preserve the configured browser origin and VAPID identity while clearing every credential, session, invitation, and notification subscription, as proposed.

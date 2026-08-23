# Wave 05: mobile Terminal file uploads

Status: approved

Approved architecture: [Mobile terminal file uploads architecture](mobile-terminal-file-uploads-architecture-proposal.md)

Worker base: the orchestrator records the exact SHA in the Herdr dispatch. It must be the commit containing the human-approved version of this brief. The worker must start in a proper Herdr worktree created from that SHA, verify its HEAD before changing files, and never implement from the orchestrator's shared checkout or another worker result.

## Outcome

Add the smallest complete file-send path to the existing mobile **Write text** composer. A person can select arbitrary files, optionally add text, and send one Terminal batch containing server-staged absolute paths. Files are opaque local bytes; Shepherdr claims neither provider-native attachment delivery nor that an agent read them.

This is one contained slice owned by one worker. It must preserve the approved Home, Terminal, workspace, notification, protected-access, and `-no-sign-in` behavior and use only Herdr 0.8.0 capabilities already confirmed by the approved architecture.

## Exact implementation scope

Worker `wave05-mobile-terminal-file-uploads-worker` owns the complete slice across these areas:

- **Go server:** the small durable workspace-ID-to-directory association, exact marker verification, one process-stable gate per workspace ID, owner-only staging and safe conflict names, startup reconciliation, and one-shot cleanup after a complete state observes workspace removal.
- **HTTP and access:** one exactly classified base64-JSON mutation; the 50 MiB decoded default, accepted fixed 1 MiB metadata/JSON allowance, and explicit `none`; existing protected `SessionLease` cancellation/revocation/release behavior; existing `-no-sign-in` authority and mode-specific Host/Origin rules; and proportional request handling.
- **Terminal composition:** one reusable server batch-send primitive shared with the current Terminal bridge. It preserves exact target validation, bracketed paste plus Enter, acknowledgement and release, ordinary conflict, confirmed takeover, cancellation, definite-pre-forward versus post-forward-unknown results, and no automatic retry.
- **TypeScript mobile UI:** recognized-agent-only **Add files**, arbitrary-file selection, text-plus-files and file-only sends, pending filename/size chips with **Remove**, scoped browser-local image thumbnails, object-URL cleanup, narrow `img-src blob:` CSP support, and the existing draft/unknown/retry behavior.
- **Production cutover:** startup/config integration, production browser asset generation and Go embedding, focused tests, README, and relevant current product/interface/Terminal/access documentation using the exact implemented configuration names.

Configuration remains limited to the two approved values: upload parent, defaulting to the platform temporary directory, and total decoded file bytes per send, defaulting to 50 MiB with explicit `none`. Choose plain names consistent with the current CLI/config style, report them exactly, and add no third setting or hidden limit.

Likely overlap is application startup and local state, the complete-snapshot publication edge, workspace closure, HTTP route inventory and access middleware, `SessionLease`, the Terminal bridge/control path, Terminal composer state and styles, CSP, embedded production assets, existing Terminal tests, README, and current direction documents. The single worker owns all in-scope overlap and must preserve unrelated behavior.

## Required behavior

- Manager lookup atomically returns one stable gate object per exact Herdr workspace ID for the process lifetime. Upload and workspace cleanup use that gate; cleanup clears only the association beneath it.
- After acquiring the gate, upload rechecks complete current Herdr state before writing. The exact workspace and terminal must still exist and that terminal must currently show a recognized agent. It rechecks again immediately before forwarding.
- One owned `0700` workspace directory has a short safe label plus random suffix, an exact `0600` marker, and a durable owner-only association. Marker, random ownership, exact absolute path, and workspace ID must match before reuse or deletion. Never take over or delete a mismatched candidate.
- Uploaded files are `0600`, preserve a safe recognizable basename, and resolve conflicts as `name (1).ext`. Browser names never supply a path. Generated prefix paths accept safe spaces and Unicode but reject invalid encoding, line breaks, controls, and bracketed-paste delimiter injection.
- Workspace disappearance in one complete-state transition starts one cleanup call only after snapshot projection/publication returns, so waiting for an upload cannot stall Home or notifications. Cleanup is best effort; on error it logs and retains the association for the next startup attempt. It does not disable other workspaces.
- Startup compares durable associations with the first complete workspace set from the one configured Herdr session. Missing workspaces get one best-effort cleanup; present workspaces may reuse verified associations. Do not scan temporary storage or add crash-orphan recovery.
- For a decoded limit `L`, calculate each file's base64 contribution separately as `4 * ceil(n / 3)`. Finite mode accepts at most `L` decoded bytes and a body no larger than those encoded contributions plus the fixed 1 MiB non-base64 JSON allowance. `none` removes both upload-route bounds and documents operator-accepted memory/disk risk.
- Protected requests use the existing `SessionLease` runtime context exactly. Revocation removes authority and cancels immediately, then completion waits for handler cancellation handling, appropriate best-effort rollback, and lease release. Sign-in-off has no session lease and keeps its existing route boundary.
- The server alone creates the exact `User uploaded files:` prefix and absolute path lines. A file-only send needs no person text. Herdr acknowledgement means Terminal input was forwarded, not that an attachment was delivered or read.

## Exclusions

Do not add multipart or octet-stream transport, provider APIs, Chat, native attachments, clipboard-image promotion, content parsing, MIME trust, agent epochs, per-agent directories or cleanup, cleanup on agent exit/replacement/reconnect, Herdr session generations, timers, cleanup workers, retry loops, quarantine, file-count or per-file limits, storage quotas, global concurrency, background drafts, offline uploads, a second Terminal control state machine, new Herdr interfaces, public hosting, or Tailscale operations.

Do not redesign Home, Terminal Reader, persistent desktop control, notifications, access ceremonies, Devices, workspace actions, or accepted interface language outside the narrow file-send additions and required documentation.

## Focused tests and worker evidence

Use small real filesystem fixtures, narrow protocol fakes, and primitive browser assertions. Do not build a large fake Herdr or Terminal runtime.

Focused Go tests cover association/marker reuse and refusal, owner-only modes, basename/path/prefix validation, duplicate naming, finite decoded and 1 MiB metadata accounting with tiny configured limits, `none`, partial rollback, stable gate serialization, post-projection cleanup, startup reconciliation, shared Terminal batch results, and protected versus sign-in-off access behavior.

Focused browser tests cover recognized-agent-only visibility, picker cancel, chips and removal, file-only and combined sends, thumbnail scope and object-URL revocation, disconnect, definite failure, unknown result, and deliberate retry.

All browser tests must use only the repository's memory-capped browser test entry, currently `npm test --prefix web`. All other JavaScript/TypeScript build or verification commands must use repository scripts that explicitly enforce the memory cap. Never run uncapped `node`, `npm`, `npx`, `tsx`, Vitest, Playwright, or an individual browser-test command. If a required production-asset command is not capped, fix the repository script within scope before running it rather than invoking the underlying tool directly.

The worker runs focused Go tests, the capped browser entry once, capped TypeScript checks and production asset build, the production Go build with current browser assets embedded, and one bounded real-Herdr preflight. Generated build output and dependencies are not committed. The report names exact commands, results, observed behavior, and the worker commit SHA. The worker does not merge, review, or approve its own work.

Do not add a large-file/memory stress suite, crash-perfect tests, disk-full matrix, elaborate filesystem-corruption matrix, or broad regression mock system.

## Review and integration

Fresh independent technical reviewer `wave05-mobile-terminal-file-uploads-reviewer` reviews the exact worker commit against the approved architecture and this brief. It verifies the workspace gate and post-projection cleanup, durable association/marker safety, JSON/base64 accounting, `SessionLease` and sign-in-off behavior, shared Terminal primitive, cancellation/result boundaries, filename/prefix safety, CSP/object-URL handling, production embedding, focused evidence, and absence of excluded machinery. Findings return to the worker; the reviewer does not fix them.

After technical approval, fresh Grok documentation/language reviewer `wave05-mobile-terminal-file-uploads-grok-reviewer` reviews the exact UI and documentation result because user-facing behavior and operator configuration change. It checks recognized-agent-only visibility, file-only and pending-file language, failure/unknown/retry wording, mobile density and touch use, local-thumbnail behavior, platform-dependent picker claims, exact implemented configuration names/defaults/risks, workspace cleanup language, and the absence of provider-native or agent-read claims. It also confirms README and current direction documents describe only the working feature. Findings return to the worker; this reviewer does not redesign accepted behavior or edit the result.

Only after both reviews approve, integrator `wave05-mobile-terminal-file-uploads-integrator` starts from the pinned approved-brief commit and integrates the exact approved worker commit. It resolves composition only within this brief, runs the focused Go and memory-capped browser gates, capped production asset build, production Go build, and bounded real-Herdr workflow, then reports the exact integrated commit and evidence. The integrator does not approve its own result.

Integration is not product acceptance.

## Documentation cutover

Do not publish operator instructions before the feature works. In the same reviewed result that embeds the working browser assets, update README and the relevant current product, interface, Terminal, and access documents with the exact implemented CLI/configuration names, platform-default upload root, 50 MiB/`none` behavior and 1 MiB finite metadata allowance, workspace-scoped lifetime and best-effort cleanup, recognized-agent-only UI, file-only sends, thumbnail/CSP behavior, observed picker behavior, same-account assumption, and local-path rather than provider-attachment meaning.

## Human phone gate

The human supplies and witnesses the production path on a real phone and real Herdr through their existing private route. Agents do not start, stop, or reconfigure Tailscale.

1. Open an ordinary terminal and an exact recognized-agent terminal. Confirm only the latter shows **Add files**. Exercise the phone's actually offered Files/camera/gallery choices, picker cancel, pending chips, image thumbnail, **Remove**, and preserved text draft.
2. Send one small arbitrary file without text, then image plus text. Verify exact opaque bytes, owner-only local files, recognizable and duplicate basenames, absolute-path prefix, bracketed-paste/Enter behavior, acknowledgement, and honest local-path language.
3. Exercise another controller, decline and then confirm takeover, lose the target/agent before forwarding, and interrupt one response. Confirm definite failure, unknown result, preserved deliberate retry, release, and no automatic input retry or fallback terminal.
4. Confirm agent exit/replacement and reconnect do not clean the workspace directory. Delete that workspace outside Shepherdr and confirm cleanup waits for any active send and removes only the verified association/directory without disturbing Home, notifications, or another workspace. Restart once to exercise stale-record cleanup or verified reuse.
5. Exercise the route in protected mode and `-no-sign-in`, confirm the documented finite limit using a small configured test value, and inspect the production refresh to prove the new TypeScript assets are embedded. Confirm the final README/current docs match the actual names and behavior.

Human acceptance of this workflow gates any downstream wave.

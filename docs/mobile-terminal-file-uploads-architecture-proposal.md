# Mobile terminal file uploads architecture

Status: implemented and accepted technical direction

Approved: 2026-08-23

Date: 2026-08-23

This direction records the implemented file-send behavior within the current Terminal and access architecture. `docs/mobile-terminal-attachments-discovery.md` remains historical pre-implementation evidence. The implementation does not add Chat, provider-native attachments, an agent-read claim, or another agent runtime.

## Decision status and scope

The human has approved these inputs:

- Upload/send and workspace cleanup are the only coordinated operations. Association, synchronization, storage, and cleanup are workspace-scoped by exact Herdr workspace ID.
- Base64 JSON is the transport for this version. One configurable total decoded file-bytes limit applies per send: 50 MiB by default, with an explicit no-limit setting.
- Cleanup is best effort. Files remain until the exact workspace is observed deleted; agent exit, replacement, status change, reconnect, or a topology gap does not clean them.
- One owner-only directory belongs to each associated workspace. Shepherdr and the Herdr agents are assumed to use the same operating-system account.
- The mobile composer permits file-only sends and shows compact pending attachment cards in a horizontal strip. Selected images may have scoped browser-local thumbnails.
- Files are staged on the machine running Shepherdr. Shepherdr sends their absolute local paths as terminal text and makes no provider-native attachment or agent-read claim.

This document does not reopen those decisions. The implemented operator settings are `-upload-parent` and `-upload-limit`; the exact route, state, and interface behavior below describe the current product.

## Small architecture

One small upload manager runs in the existing Shepherdr process. Its lookup atomically returns one stable gate object for each exact Herdr workspace ID; that object remains in the manager for the process lifetime. A nullable association beneath the gate links the workspace to its directory. Cleanup may clear that association but never replaces or removes the gate. The durable side is a small owner-only association file mapping the ID to one exact absolute directory and its random ownership value. There is no agent record, agent epoch, topology generation, per-agent directory, global queue, quota service, cleanup worker, or durable browser draft.

The design relies narrowly on the current product's one configured Herdr session and that session's opaque workspace IDs. Herdr 0.8.0 workspace IDs are the best confirmed handle in this scope, but no durable Herdr server/session generation identity is confirmed. The implementation assumes an ID restored or returned by that one configured session continues to identify the same workspace for association purposes. It does not generalize the record across Herdr sessions or invent generation machinery. If that assumption later proves false, durable reuse needs a new human-approved design.

## The two coordinated operations

### Upload and send

One workspace gate serializes sends only for that workspace. Different workspaces may send concurrently without a global admission scheduler.

1. Resolve the exact current workspace, tab, pane, terminal, and recognized-agent observation from a complete truthful Herdr state. Atomically look up the stable workspace gate, then find or lazily create its association while holding it.
2. After obtaining the gate, read/check current complete Herdr state again. The exact workspace and target terminal must still exist and that terminal must currently show a coherent Herdr-recognized agent. Otherwise fail before creating a directory or file.
3. Verify or create the owned workspace directory, reserve safe final names, and stage all request files while holding the gate. Any normal validation or write failure attempts to remove everything newly created by that request and sends no terminal input.
4. Immediately before forwarding, revalidate the same exact workspace, pane, terminal, and recognized-agent presence. If any no longer matches, send nothing and best-effort roll back the request files.
5. Build the server-derived file-list prefix and pass one batch to the shared Terminal send primitive described below. Hold the workspace gate through its definite or unknown result.

The gate controls Shepherdr's upload and cleanup work only. It cannot prevent Herdr or another operator from deleting the workspace, changing the terminal, or ending the agent. If external deletion occurs while an upload holds the gate, cleanup begins after a complete Herdr state reports the absence, waits for the upload, and deletes afterward. The accepted final validation-to-terminal-write race remains; no agent lifecycle model is added to disguise it.

### Workspace cleanup

Workspace closure, whether initiated through Shepherdr or observed from outside, has the same result. One present-to-absent transition in a complete truthful state triggers one cleanup call after the projector/snapshot callback has returned and the complete state is available to Home and notifications. Cleanup therefore cannot wait on an upload inside that callback. The call obtains the stable workspace gate, waits for any upload/send, verifies the durable record and marker, best-effort deletes the exact owned directory, and clears only the association after successful deletion. Absence of an already deleted directory counts as successful cleanup; the gate remains available for the process lifetime.

A cleanup error is logged and the association remains, so a later startup can try that exact record again. The failure does not disable uploads for other workspaces and does not create a global unsafe state. This is one bounded call handed off outside projection, not a scheduler or durable worker. There are no cleanup timers, retry loops, quarantine, background sweepers, or promises of immediate external deletion detection.

At startup, Shepherdr loads the small association file and obtains the first current complete workspace set from its configured Herdr session. A record whose exact workspace is absent gets the same best-effort cleanup operation. A present workspace may reuse its directory only after record and marker verification. If Herdr is unavailable, no cleanup conclusion is drawn from absence of data; reconciliation waits for a complete state, and file sending is unavailable because no current recognized agent can be established.

The state is written with the existing owner-only local-state discipline and atomic replacement. Catastrophic crashes can still leave an unrecorded directory or a partially completed rollback. Those orphans are accepted; Shepherdr does not scan the platform temporary directory to find them, and the platform may clear them on reboot. This is intentionally not crash-perfect cleanup.

## Directory, linkage, and filenames

The configured upload root is a parent directory and defaults to the platform temporary directory, normally `/tmp` on Linux. A short common path is:

    <upload-root>/shepherdr-<safe-workspace-label>-<random>/
      .shepherdr-upload
      <safe recognizable filename>

The directory is `0700`; its marker and uploaded files are `0600`. The marker and durable record both contain the random ownership value and exact workspace ID, and the record contains the exact absolute directory. All must match before reuse or deletion. A label or pathname is never ownership.

Create a candidate directory exclusively. If it already exists, is unowned, has a missing or mismatched marker, or is not the expected owner-only directory, never take it over or delete it; choose another random suffix and persist the new exact association. A missing recorded directory may be replaced with a newly owned candidate. A workspace rename does not move its established directory. Label collisions are harmless because the suffix and exact association differ.

Derive each basename from the browser-supplied final name, never by joining a supplied path. Remove path components, line breaks, controls, path traversal, and reserved marker names while retaining ordinary readable characters, Unicode, spaces, dots, parentheses, dashes, and underscores when safe. An empty result becomes `file`. Resolve conflicts exclusively as `name.ext`, `name (1).ext`, `name (2).ext`, and so on. The bytes remain opaque: Shepherdr does not sniff, parse, execute, or rewrite content based on name, MIME, or extension.

File creation and deletion stay beneath the verified directory and do not follow symbolic links or use non-regular entries as upload files. Normal request rollback is best effort; failure is logged without claiming the bytes were removed. Owner-only access assumes Shepherdr and the agent run as the same trusted OS account. A different-account deployment requires separate approved architecture rather than broader modes.

## JSON route, limit, and access boundary

One exact JSON mutation carries the exact Terminal target, optional person text, untrusted filenames, and base64 file bytes. The one configurable limit is the sum of decoded file bytes in that send:

- default: 50 MiB;
- explicit no-limit setting: no Shepherdr file-byte ceiling for that request.

There is no separate per-file limit, file-count limit, active-storage quota, hard ceiling, process-wide concurrency limit, or admission scheduler. Finite mode uses a fixed 1 MiB allowance for all non-base64 JSON bytes: target fields, person text, filenames after JSON escaping, keys, delimiters, and other structural metadata. For each file of decoded size `n`, its encoded contribution is calculated separately as `4 * ceil(n / 3)`, so every file's base64 padding is counted. A request is accepted only when decoded file bytes total at most the configured limit and the HTTP body is no larger than the sum of those per-file encoded contributions plus the 1 MiB allowance. It may therefore fail at the transport boundary because names, count, text, or JSON structure exhaust the allowance even when decoded bytes remain below the file-byte limit. This is an honest transport bound, not a file-count or quota system.

The implemented bounded reader uses four times the configured decoded-byte limit plus 1 MiB as a conservative outer bound, followed by the exact per-file calculation. Tests may configure a tiny decoded limit while keeping the fixed allowance; they need not allocate 50 MiB.

With no limit, both the decoded-byte limit and this upload route's metadata/body allowance are unbounded. The operator explicitly accepts that a request may consume large memory, temporary storage, transfer time, or all available disk. Ordinary decoding, filesystem, proxy, or operating-system failures remain possible and are reported as failures; this setting does not create a storage manager.

The route is listed exactly in the deny-by-default route inventory. It keeps `Content-Type: application/json`, applicable body handling, and the existing mode-specific access rules:

- In protected mode, exact canonical Host and Origin, a valid session, and current route rules apply. Reuse the existing `SessionLease` and runtime cancellation semantics without another auth lifecycle. Body handling, staging, Terminal work, and rollback use the lease's runtime context. Revocation removes authority and cancels that context immediately, then waits for the handler to notice cancellation, stop or classify any possible forwarding, best-effort roll back when appropriate, and release the lease. The handler performs the existing pre-forward authority/cancellation check; a possible post-forward cancellation remains unknown rather than becoming success.
- In `-no-sign-in` mode there is no authenticated session, credential owner, or protected-session lease. Every browser that can reach Shepherdr retains the approved operator authority, while the route continues to use that mode's existing Host/Origin boundary, exact classification, JSON type, target checks, and configured body handling. Do not import protected canonical-origin/session claims into this mode.

This is an application of the approved access architecture, not a new access product decision. Transfers materially larger than the ordinary 50 MiB default may later justify a narrowly reviewed multipart route. Multipart and octet-stream are not designed or reserved here.

## One shared Terminal batch-send primitive

Extract or define one reusable server-side batch-send primitive shared by the current Terminal bridge and file-send route. It takes the exact target, completed text batch, ordinary or explicitly confirmed takeover intent, request/session cancellation context, and the existing control ownership. It preserves the accepted Terminal behavior in one place:

- exact pane and terminal targeting with no fallback;
- one bracketed-paste batch followed by Enter and acknowledgement;
- ordinary control conflict and the existing confirmed **Take over and send** path;
- release after the phone batch;
- definite-pre-forward failure versus post-forward unknown outcome;
- cancellation and protected-session invalidation behavior; and
- no automatic input retry.

The upload handler does not create a second Terminal control state machine. If ordinary send meets a controller conflict, that is a definite pre-forward failure: best-effort remove this request's staged files and let the existing UI offer confirmed takeover. A confirmed retry is a new complete upload/send request. If forwarding may already have happened but acknowledgement is lost, the result is unknown and the staged files remain until workspace cleanup because the terminal may contain their paths.

For files plus text, the server constructs:

    User uploaded files:
    - <absolute path>
    - <absolute path>

    <person text>

A file-only send emits the file list without requiring person text. Before calling the primitive, validate the complete generated prefix and every final path as valid UTF-8 with no embedded line breaks, terminal controls, or bracketed-paste start/end delimiter sequence. Ordinary configured paths containing spaces and safe Unicode remain valid. Browser-supplied paths are never accepted, and the person's text continues through the current Terminal text validation and normalization.

Herdr's acknowledgement proves only that terminal input was forwarded. The recognized agent can exit between the final check and write while the terminal remains. Shepherdr does not claim atomic agent delivery, native attachment delivery, or that an agent/provider/model read a file.

## Mobile interface and honest failures

The phone command is **Message**. Its composer shows the compact **Add files** icon only while the exact current terminal displays a Herdr-recognized agent; it does not show a disabled placeholder on an ordinary terminal. **Add files** offers **Photos** and **Files**, and the composer may send text, files, or both.

Selected files are prepared in current page memory. **Preparing files…** keeps send unavailable until that work finishes. Prepared files appear as compact cards in a contained horizontal scrolling strip, with recognizable filename, size, an accessible compact remove icon, and a small local thumbnail for browser-decodable images. The narrow content-security policy permits `blob:` only for image sources. Every object URL is revoked on removal, replacement, successful send, draft discard, or component teardown. No preview URL or file draft is persisted.

Use the ordinary file input behavior needed for arbitrary files. Camera, gallery, and Files choices, ordering, multi-select, returned metadata, and focus restoration remain browser/platform-dependent; do not force capture or claim that every phone offers every source.

Picker cancel leaves the existing text draft intact. A temporary disconnect preserves the existing local draft and pending files, but send is unavailable until the exact current agent is observed again. A target or agent lost before forwarding produces a clear not-sent result and keeps the local draft for correction. A confirmed send clears it through existing composer behavior. An unknown result preserves the draft and offers only deliberate retry; there is no automatic retry, and a retry can create `(1)` filenames or duplicate terminal text if the first send actually reached the terminal.

Interface language may say files are pending or that terminal input was sent. It must not say attached to the provider, delivered to the model, or read by the agent.

## Focused acceptance

Keep evidence proportional to this small design.

Focused Go tests use small files and configurable tiny limits to cover marker-plus-record creation/reuse/refusal, safe names and `(1)` conflicts, finite decoded/body limits and no-limit configuration, rollback, fresh target/agent revalidation, workspace-gate serialization, cleanup waiting, startup reconciliation from a complete workspace set, shared batch-send outcomes, and protected versus sign-in-off route behavior. Use real small filesystem fixtures and narrow fakes, not a large mock runtime.

Memory-capped browser tests cover recognized-agent-only **Add files**, text-plus-files and file-only sends, attachment chips/removal, scoped image thumbnails and URL revocation, picker cancel, disconnect, definite failure, unknown result, and deliberate retry. Assertions compare primitive results rather than retaining live DOM objects.

The accepted production workflow on a real phone and real Herdr remains the check for affected changes: select an actually offered image source and an arbitrary small file, inspect/remove/reselect, perform file-only and text-plus-file sends, verify exact local bytes and generated paths, exercise duplicate names, ordinary conflict and confirmed takeover, lose the recognized agent before a send, delete the workspace externally, restart for stale-record cleanup, and repeat the route once in protected and once in `-no-sign-in` mode. Record only sources and behavior the phone actually offered.

Do not add a large-file or memory stress suite, crash-perfect cleanup tests, a disk-full matrix, an elaborate filesystem-corruption matrix, or big Herdr mocks. Hostile filenames, control/delimiter rejection, opaque small content, and unrelated temp-directory preservation remain focused checks.

## Ongoing acceptance guidance

Keep the focused Go and memory-capped browser coverage above. Changes to mobile behavior also require production Shepherdr with real Herdr and a real phone. Preserve exact targeting, workspace-scoped synchronization, marker-and-record ownership, safe local paths, same-account access, normal rollback, best-effort workspace cleanup, protected and sign-in-off boundaries, finite and `none` limit behavior, file-only sends, browser-local thumbnails, and truthful acknowledgement language.

Acceptance records only picker sources and behavior the phone actually offered. It does not turn local paths into provider-native attachments or prove that an agent read a file.

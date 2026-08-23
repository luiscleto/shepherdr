# Mobile terminal file uploads discovery

Status: historical evidence, not current architecture or product direction

Date: 2026-08-23

Pre-implementation base: `335c0e87428034b05c5f5b19edf670523f119018`

This document is retained as evidence from that exact base. Every observation described as “current” below refers to the pre-implementation base, not to the product now in this repository. Later approved direction and implementation resolved questions that intentionally remain open in this historical record.

## Answer in brief

The human-selected feature is arbitrary file upload through the accepted mobile **Write text** flow. It is not native image attachment behavior:

- Shepherdr stages each selected file locally on the machine that must make it readable to the agent.
- When the person sends, Shepherdr prefixes their terminal text with a short heading and one staged path per line.
- The agent receives text containing filesystem paths. Shepherdr must not claim that Herdr, Codex, another provider, or the model received a provider-native attachment.
- Codex image placeholders, image-path promotion, and clipboard-image paste are not requirements or architecture gates.

Herdr 0.8.0 confirms the needed transport distinction: its public agent and terminal input surfaces carry text or PTY bytes, not file bytes or attachment metadata. Local staging is therefore new Shepherdr-owned behavior; the existing terminal path can carry only the resulting path text.

File sending is offered only while the exact open terminal is observed to contain a Herdr-recognized agent. Shepherdr must revalidate that same pane, terminal, and observed occupant immediately before send. This visibility rule and revalidation reduce mistakes, but Herdr 0.8.0 does not atomically bind the later terminal input to that observed agent occupancy. They cannot guarantee that an ordinary shell never receives the generated prefix.

## Evidence labels

- **Confirmed** — shown by current repository code, installed Herdr 0.8.0 interfaces, version-matched evidence, or a primary specification.
- **Human decision** — supplied directly in the current conversation.
- **Unknown** — existing evidence does not settle it.
- **Not exposed** — Herdr 0.8.0 has no public interface for it.

## Decided submission contract

The submitted terminal text has this shape:

    User uploaded files:
    /temporary/root/readable-place/notes.txt
    /temporary/root/readable-place/diagram (1).svg

    Please compare these files.

The rules already decided are:

- **User uploaded files:** is the short prefix.
- Each staged absolute path appears on its own line, followed by the person's submitted text.
- The final basename remains safely recognizable so the person can refer to it. Unsafe path components and control characters cannot pass through as path structure.
- A visible collision uses ** (1)**, ** (2)**, and so on before the extension: **diagram.svg**, **diagram (1).svg**, **diagram (2).svg**.
- The prefix is terminal prompt text, not an attachment envelope. Whether and when the agent opens a file depends on that agent and its filesystem access.
- The file-send action and generated prefix are available only while the exact terminal is observed to contain a recognized agent; the remaining delivery race is described below.

The existing phone flow has no picker, upload route, preview state, staged-file state, or removal action. Its current textarea becomes one normalized bracketed text paste followed by Enter. The terminal WebSocket carries bounded text/base64 PTY chunks, and the server forwards them through Herdr terminal control. Base64 in that protocol means PTY bytes; it is not a file-transfer or attachment field.

Herdr agent.prompt is also text-only. It validates live agent occupancy as part of its own operation and rejects a pane that no longer contains a recognized agent, but it has no expected terminal_id. It therefore offers stronger occupancy timing but a weaker exact-target guarantee, not a combined exact-terminal-and-agent guarantee. This discovery does not select it as a second input route.

## Observed target requirement and remaining race

Herdr's snapshot associates a recognized agent with workspace_id, tab_id, pane_id, and terminal_id. Current Shepherdr already rechecks pane_id plus terminal_id and invalidates the terminal lease after a topology gap or replacement.

The file-send precondition is stricter than merely finding an agent elsewhere:

1. The exact open pane and terminal must still match the person's current Terminal view.
2. The current complete Herdr state must contain one coherent recognized-agent record for that same workspace, tab, pane, and terminal.
3. That full relationship must be revalidated immediately before the prefixed batch is sent.

The current exact-terminal lease validates pane, terminal, and topology continuity. Agent occupancy is observed separately and is not an atomic precondition on the terminal-input operation. After the occupancy check, the agent may exit back to the same PTY shell before Shepherdr writes the batch; the pane and terminal identities can remain valid throughout. Immediate revalidation narrows this interval but does not close it.

This is a known target-safety race, independently of whether the agent later reads the files. Herdr's terminal acknowledgement confirms only that terminal input was forwarded.

## Mobile-web affordance facts

The baseline is a normal web file picker that permits arbitrary files, not an image-only input.

- **Files:** Android and iOS can present their platform file sources. Exact providers, menu order, multi-selection behavior, and returned metadata remain browser/device-dependent.
- **Camera and gallery:** an image-specific choice may offer camera or photo-library sources. The capture attribute is only a hint and can open the camera directly on some Android devices, so it does not replace the arbitrary-file picker.
- **Image preview:** a selected image File/Blob may have a browser-local object-URL preview and removal action. The URL should be revoked when that local preview is discarded. This does not give images different server-side storage or submission semantics.
- **Validation:** picker accept filters, MIME strings, extensions, and filenames are hints or untrusted metadata. Arbitrary upload means the server stores opaque hostile files; it must not interpret a type merely because the browser names it.
- **Keyboard and focus:** camera, gallery, and Files leave the editing context and commonly hide the software keyboard. Draft, selection, cancel, removal, failure, disconnect, and return behavior still need real-device checks. iOS does not promise that programmatic focus will reopen the keyboard.
- **Clipboard images:** browser clipboard-image support may remain a progressive enhancement, but it is not required by this contract.

## Upload root, names, and directory linkage

The default upload root is the platform's sane temporary directory—normally /tmp on Linux—and is configurable in the same manner as other Shepherdr configuration. The configured root is storage location, not authority.

A human-readable directory shape may include an escaped workspace label, and a file path preserves a safe recognizable final basename. Those names are cosmetic:

- Workspace labels can collide, change, and contain hostile text.
- Workspaces can be created, renamed, or deleted outside Shepherdr.
- Stale upload directories may outlive the Herdr workspace they once accompanied.
- A label match never proves ownership or linkage.

The required invariant is stronger: Shepherdr must never reuse or delete a directory based only on its label or apparent pathname. A reusable directory needs both a Shepherdr-created ownership marker and an exact link recorded to Herdr workspace identity, and Shepherdr must verify both before reuse or deletion. If a candidate directory is unowned, has no valid marker, or carries a different marker/link, Shepherdr must not take it over; it chooses another directory.

The exact marker format, creation atomicity, permissions, identity fields, and persistence model are unresolved architecture. Paths, labels, marker contents, directory entries, and links must all be treated as untrusted until verified; uploaded names must not enable traversal or link-following outside the owned directory.

## What Herdr identity can support

Existing Herdr 0.8.0 evidence does not reveal one identity confirmed durable enough by itself across reconnect, restart, external deletion, and later recreation:

| Situation | Existing evidence | Consequence |
| --- | --- | --- |
| Browser or Shepherdr reconnect | Opaque public workspace_id values are read from a fresh Herdr snapshot and are the best available reconnect handles within a Herdr session. | **Confirmed for the current session.** Do not derive identity from label, position, or directory name. |
| Herdr 0.8.0 cold restart | The 0.8.0 implementation persists and restores public workspace/tab/pane IDs. Herdr documentation does not promise this as a compatibility guarantee for every future version. terminal_id is newly allocated. | **Implementation fact, contract strength unknown.** workspace_id is stronger than terminal_id for a workspace link; terminal_id remains a send-time identity. |
| Named or recreated Herdr session | Named sessions have separate sockets and state. No stable server/session identity that safely scopes otherwise reusable public IDs is confirmed in the inspected interface. | **Unknown.** workspace_id alone is insufficient as a universally durable disk-link key. |
| Workspace deleted outside Shepherdr | A live close emits workspace.closed; later lookups return not found. After a connection gap or process restart, a fresh snapshot is the source of current truth. | **Confirmed absence detection.** Evidence does not supply a durable tombstone or a guaranteed identity for delete/recreate history. |
| Agent exit or replacement | The live agent alias clears, while the workspace may continue and later contain another agent. | **Confirmed distinction.** Agent exit is not workspace deletion and cannot automatically define workspace-file lifetime. |

Therefore the exact Herdr workspace identity available today is the session-scoped opaque workspace_id, refreshed from current state. It is sufficient for current-session correlation and is restored by the version-matched cold-restart implementation, but there is no confirmed durable session scope or persistence contract that makes it sufficient alone for long-lived directory ownership. The marker must link to an exact approved identity representation, but this discovery cannot yet name that representation without inventing the persistence model.

## Cleanup and startup reconciliation

Cleanup must cover workspaces deleted outside Shepherdr and gaps where the live workspace.closed event was missed. On startup, Shepherdr must reconcile its own marked directories against fresh current Herdr state rather than labels. A stale folder is not proof that a workspace still exists, and a newly created workspace with the same label is not its owner.

Agent exit must not be treated as workspace deletion. A workspace can contain or restart agents, so whether agent exit starts cleanup, shortens retention, or changes nothing remains a product/lifetime decision.

Other unresolved cleanup cases include local removal before send, canceled or failed upload, target replacement, canceled send, timeout, Shepherdr shutdown/restart, configured-root changes, partial deletion, and deletion failure. Terminal input acknowledgement is not proof that an agent opened the file and is therefore not by itself a safe deletion point.

## Security and architecture decisions still required

The following are deliberately unresolved:

1. **Limits and quota:** per-file size, request size, concurrent upload count, per-draft total, total disk storage, and behavior when storage is full.
2. **Retention:** lifetime before and after send; removal semantics; whether agent exit matters; cleanup grace periods; and whether a later agent follow-up is expected to retain access.
3. **Root changes:** whether old configured roots are reconciled, retained, migrated, or left for explicit cleanup.
4. **Deletion failure:** retries, reporting, quarantine, and what prevents a failed or partially deleted directory from later being trusted.
5. **Persistent linkage:** whether the marker/link must survive a Shepherdr restart and, if so, how it is safely scoped to the relevant Herdr session given the missing confirmed durable session identity.
6. **Browser ownership:** with sign-in enabled, how staged files bind to the authenticated browser session; with -no-sign-in, every reachable browser has operator authority and there is no authenticated session, so ownership, quota, cancellation, and cleanup need another approved scope.
7. **Filesystem safety:** owner-only storage permissions, symlink/hard-link policy, atomic name allocation, safe basename escaping, and readable access for the exact agent runtime without broader exposure.
8. **Hostile content and authority:** file bytes, metadata, filenames, paths, labels, markers, and the submitted prefix are untrusted. None may grant Shepherdr authority, select another target, authorize workspace/device actions, or be rendered as active application markup.
9. **Truthful status:** distinguish selected, locally previewed, uploaded, staged, path forwarded, and removed. Do not say provider-native attachment, agent-read, or model-read without evidence.

## Next smallest architecture question

**Which guarantee should govern delivery of the generated file-list text?**

The smallest options are:

1. Accept the existing text-send race while requiring the generated file-list text to be harmless if a shell interprets it. The formatting mitigation is deliberately unspecified here.
2. Use the weaker exact-target Herdr agent.prompt route, which validates live agent occupancy during its operation but has no expected terminal_id.
3. Require a new Herdr capability that atomically combines expected exact-terminal identity with recognized-agent occupancy and text submission.

This document does not choose an option. The separate persistence question also remains: whether workspace-directory linkage must survive a Shepherdr restart or prior correctly marked directories become retained orphans under an approved cleanup policy.

## Sources and checks

Repository evidence:

- docs/terminal-direction.md, docs/north-star.md, docs/ui-direction.md, and the evidence-only docs/herdr-integration-discovery.md.
- Current Reader/composer path: web/src/terminal-page.ts, web/src/terminal/reader-view.ts, web/src/terminal/reader-input.ts, web/src/terminal/session.ts, and web/src/terminal-input.ts.
- Current exact-target path: internal/server/terminal_protocol.go, internal/server/terminal_bridge.go, internal/server/terminal_target.go, and internal/herdr/types.go.

Installed and version-matched evidence already recorded in the Herdr discovery:

- Herdr client/server 0.8.0, protocol 19; session snapshot and agent records; text-only agent.prompt; terminal-session input; public-ID persistence/restore implementation; fresh terminal IDs after cold restart; workspace.closed and not-found behavior.
- Herdr's private remote clipboard-image staging and pane graphics are unrelated to this generic upload contract and are not public arbitrary-file input interfaces.

Primary mobile-web sources retained from the earlier discovery:

- [WHATWG file-upload input](https://html.spec.whatwg.org/dev/input.html#file-upload-state-(type=file)), [W3C HTML Media Capture](https://www.w3.org/TR/html-media-capture/), and Google's [mobile image-capture guidance](https://web.dev/articles/media-capturing-images).
- Relevant WebKit evidence: [image file-input choices](https://bugs.webkit.org/show_bug.cgi?id=236981), [Photos-picker type conversion](https://bugs.webkit.org/show_bug.cgi?id=239001), and [file-picker focus restoration](https://bugs.webkit.org/show_bug.cgi?id=281209).

No attachment experiment, terminal input, upload, server start, browser automation, or mobile-device test was run for this revision. No production data or runtime behavior was changed.

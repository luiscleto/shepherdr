# Shepherdr terminal direction

Status: approved current behavior; base Terminal integrated at `03d64421fe3c02d2d9203d3823d214a525619fcc`; Wave 05 file sending awaits independent review and the human phone gate

This document records the human's direct terminal R&D decisions. It supersedes older terminal architecture and interface claims, including `wave-01-human-gate-correction.md`. The Home and workspace-management direction remains unchanged.

## Current experience

Shepherdr opens only the exact current terminal chosen from Home. If that terminal is missing, stale, replaced, or does not match, it is unavailable; Shepherdr never falls back to another terminal.

On a phone, Terminal uses Reader and observes without taking control between sends. Reader supports browser text selection and can load older output that Herdr still supplies. Shepherdr promises no terminal history beyond that output.

During a temporary disconnect, an already-open text composer stays open and locally editable, preserving its draft, selection, and focus. **Send** and remote shortcuts are unavailable until recovery. Reconnecting does not reopen or refocus a composer the person closed or unfocused.

Sending text or a shortcut obtains ordinary control only long enough to send one batch and receive its acknowledgement, then releases. If someone else has control, Shepherdr sends nothing and offers only **Take over and send**, after confirming that the current controller will lose input. Shepherdr never retries terminal input automatically. Persistent **Control**, **Take over**, and **Release** actions are desktop-only.

While the exact current terminal has a Herdr-recognized agent, the phone composer also shows **Add files**. Its ordinary picker accepts arbitrary files; the phone and browser decide which Files, camera, or photo-library sources, multi-selection, and focus behavior they offer. Pending rows show filename, size, **Remove**, and a browser-local thumbnail for selected images when the browser can decode one. Picker cancel, a temporary disconnect, definite failure, and an unknown result preserve the text and pending files. Files may be sent without text.

On send, Shepherdr stages opaque bytes in the exact workspace's owner-only local directory and prefixes the Terminal text with **User uploaded files:** plus one absolute path per line. Duplicate basenames use ` (1)`, ` (2)`, and so on before the extension. The path list is terminal input, not an attachment envelope. Acknowledgement means the input was forwarded; it does not prove that an agent, provider, or model opened or read a file.

File send uses the same ordinary or confirmed-takeover control and release semantics as phone text. The server rechecks the exact workspace, tab, pane, terminal, and recognized-agent observation after taking the workspace gate and again immediately before forwarding. Herdr 0.8.0 cannot atomically bind that observation to the later terminal write, so the recognized agent can still exit in the final interval. A definite pre-forward failure best-effort removes that request's files. A post-forward unknown result retains them and requires a deliberate new send; there is no automatic retry.

On a desktop, Terminal uses the full terminal renderer. It observes first and can keep control for interactive use until the person chooses **Release**. Takeover remains confirmed, and release leaves observation running.

Connection and input messages describe the terminal connection, not the agent's status. Only actual agents use Herdr's `working`, `blocked`, `idle`, `done`, and `unknown` words.

## Runtime boundary

Shepherdr uses Herdr 0.8.0's existing snapshot, terminal read, observe, control, input acknowledgement, release, and takeover capabilities. Herdr remains authoritative for the terminal process, output, and control.

The server rechecks the exact pane and terminal identity for reads and sessions. Observation and control use separate Herdr child processes. Each stream starts with a full frame and accepts later frames only in order; a replacement starts a new sequence. Closing or replacing a browser session cancels and collects the exact child it started. The file route shares the same guarded Terminal batch writer, acknowledgement boundary, release, conflict, takeover, cancellation, and unknown-result classification rather than adding another control state machine.

One stable gate per exact workspace ID coordinates only upload/send and workspace-deletion cleanup. Files remain through agent exit, replacement, reconnect, and Shepherdr restart. After a complete published Herdr state observes the workspace missing, one best-effort cleanup waits for any send and removes only the exact directory whose durable association and marker match. Startup reconciles recorded associations against its first complete workspace set. There are no timers, cleanup workers, scans, quotas, file-count limits, per-file limits, or agent-lifecycle records.

`-upload-parent` defaults to the platform temporary directory. `-upload-limit` defaults to `50MiB` decoded bytes per send; finite JSON allows the separately calculated base64 contribution of every file plus a fixed 1 MiB metadata allowance. `none` removes both upload-route bounds and accepts the corresponding memory, disk, and transfer risk. Shepherdr and Herdr agents are assumed to run as the same operating-system account.

The renderer comparison lab is development-only and unavailable without its explicit flag. Shepherdr has no durable terminal store and does not parse terminal output into Home, agent state, authorization, or application actions.

Existing product scope, private-network requirements, and untrusted-content rules in `north-star.md` and `architecture-proposal.md` remain unchanged.

## Real-phone gate

Status: **PASS**

The human gate confirmed mobile rendering, sending, native selection, scrolling and loading older output, continued live output, and shortcut commands.

Wave 05 file-send status: **PENDING HUMAN GATE**. Automated capped browser tests, focused Go tests, production asset embedding, and a bounded real-Herdr preflight support the worker result but do not establish phone picker sources, real staged-byte/path behavior, takeover, target loss, cleanup, restart, or protected/`-no-sign-in` acceptance on a real phone.

## Ongoing acceptance guidance

Future terminal changes still require checks proportionate to the behavior they affect. A real-device acceptance report must name only the cases actually exercised. Automated tests, desktop mobile emulation, and Android emulators may support confidence but do not replace a required real-device check.

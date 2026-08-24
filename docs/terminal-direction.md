# Shepherdr terminal direction

Status: approved current direction

This records the accepted Terminal Reader, file-send behavior, and their real-phone gates. It does not reopen the Terminal architecture.

## Current experience

Shepherdr opens only the exact current terminal chosen from Home. A missing, stale, replaced, or mismatched terminal is unavailable; Shepherdr never falls back to another terminal.

On a phone, Terminal uses Reader and observes without taking control between sends. Reader supports browser text selection, can load older output that Herdr still supplies, continues to show live output, and provides **Message** plus horizontally scrollable shortcut commands. Shepherdr promises no durable terminal history.

During a temporary disconnect, an open composer remains locally editable and keeps its draft, selection, pending files, and focus. **Send** and remote shortcuts are unavailable until recovery. Reconnecting does not reopen or refocus a composer the person closed or unfocused.

Sending text or a shortcut takes ordinary control only long enough to send one batch and receive acknowledgement, then releases it. If someone else has control, Shepherdr sends nothing and offers **Take over and send** after confirmation that the current controller will lose input. Shepherdr never retries Terminal input automatically.

While the exact current terminal has a Herdr-recognized agent, the phone composer shows **Add files**. Its **Photos** and **Files** choices use platform pickers; **Files** accepts arbitrary files. The phone and browser decide the sources, multi-selection, and focus behavior they offer.

**Preparing files…** appears while selected files are read. Prepared files appear as compact, horizontally scrolling attachment cards with filename, size, a remove action, and a browser-local thumbnail for an image when available. Picker cancel, temporary disconnect, definite failure, and an unknown result preserve text and pending files. Files may be sent without text.

On send, Shepherdr stages opaque bytes in the exact workspace's owner-only local directory and starts one Terminal batch in this form:

```text
User uploaded files:
- /absolute/path
```

Each file has its own `- ` line. Duplicate basenames gain ` (1)`, ` (2)`, and so on before the extension. The path list is Terminal input, not an attachment envelope. Acknowledgement means the input was forwarded; it does not prove that an agent, provider, or model opened or read a file.

File send uses the same ordinary or confirmed-takeover control and release behavior as phone text. If the exact target or recognized agent is gone before input is handed off, Shepherdr sends nothing and best-effort removes that request's files. If handoff cannot be confirmed, Shepherdr preserves the files and requires a deliberate new send.

On a desktop, Terminal uses the full terminal renderer. It observes first and can keep control for interactive use until **Release**. Takeover remains confirmed, and release leaves observation running.

Connection and input messages describe the terminal connection, not the agent's status. Only actual agents use Herdr's `working`, `blocked`, `idle`, `done`, and `unknown` words.

## Runtime boundary

Shepherdr supports Herdr 0.8.0 through 0.8.2 and uses their existing snapshot, terminal read, observe, control, acknowledgement, release, and takeover capabilities. Herdr remains authoritative for the terminal process, output, and control.

The server rechecks the exact target for reads and sends. The file path shares the accepted Terminal batch writer and its acknowledgement, release, conflict, takeover, cancellation, and unknown-result behavior. Shepherdr rechecks the exact current workspace, terminal, and recognized agent immediately before sending. Herdr cannot bind agent presence atomically to the terminal write, so a recognized agent can still exit in the final interval; Shepherdr makes no stronger delivery claim.

One workspace-scoped gate coordinates file send with workspace cleanup. Files remain through agent exit or replacement, reconnect, and Shepherdr restart. After a complete Herdr state observes the workspace missing, best-effort cleanup waits for any send and removes only the exactly recorded and verified directory. Startup reconciles recorded associations against the first complete workspace set. There are no cleanup timers, directory scans, storage quotas, file-count limits, or per-file limits.

`-upload-parent` defaults to the platform temporary directory. `-upload-limit` defaults to `50MiB` decoded bytes per send; `none` removes Shepherdr's upload-request bound and accepts the corresponding memory, disk, and transfer risk. Shepherdr and Herdr agents run as the same operating-system account.

The renderer comparison lab remains development-only behind its explicit flag. Shepherdr has no durable terminal store and does not parse terminal output into Home, agent state, access, or application actions.

## Accepted real-phone gates

The Reader and file-send real-phone gates are accepted current direction. They cover mobile rendering and selection, older-output scrolling, continued live output, text and shortcut sending, file preparation and removal, actual platform picker behavior, file-only and text-plus-file sends, exact staged bytes and paths, conflict and confirmed takeover, target loss, workspace cleanup, restart, and protected and sign-in-off operation.

Acceptance records only the sources and behavior the phone actually offered. Automated tests, desktop mobile emulation, and Android emulators support confidence but do not replace a required real-device check.

## Ongoing acceptance guidance

Changes to Terminal behavior require checks proportionate to the behavior they affect. Preserve exact targeting, Reader's observe-between-sends model, truthful acknowledgement and unknown-result language, deliberate takeover, no automatic input retry, local-path file semantics, and real-phone evidence for mobile behavior.

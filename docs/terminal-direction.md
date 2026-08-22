# Shepherdr terminal direction

Status: approved current behavior; integrated at `03d64421fe3c02d2d9203d3823d214a525619fcc`

This document records the human's direct terminal R&D decisions. It supersedes older terminal architecture and interface claims, including `wave-01-human-gate-correction.md`. The Home and workspace-management direction remains unchanged.

## Current experience

Shepherdr opens only the exact current terminal chosen from Home. If that terminal is missing, stale, replaced, or does not match, it is unavailable; Shepherdr never falls back to another terminal.

On a phone, Terminal uses Reader and observes without taking control between sends. Reader supports browser text selection and can load older output that Herdr still supplies. Shepherdr promises no terminal history beyond that output.

During a temporary disconnect, an already-open text composer stays open and locally editable, preserving its draft, selection, and focus. **Send** and remote shortcuts are unavailable until recovery. Reconnecting does not reopen or refocus a composer the person closed or unfocused.

Sending text or a shortcut obtains ordinary control only long enough to send one batch and receive its acknowledgement, then releases. If someone else has control, Shepherdr sends nothing and offers only **Take over and send**, after confirming that the current controller will lose input. Shepherdr never retries terminal input automatically. Persistent **Control**, **Take over**, and **Release** actions are desktop-only.

On a desktop, Terminal uses the full terminal renderer. It observes first and can keep control for interactive use until the person chooses **Release**. Takeover remains confirmed, and release leaves observation running.

Connection and input messages describe the terminal connection, not the agent's status. Only actual agents use Herdr's `working`, `blocked`, `idle`, `done`, and `unknown` words.

## Runtime boundary

Shepherdr uses Herdr 0.8.0's existing snapshot, terminal read, observe, control, input acknowledgement, release, and takeover capabilities. Herdr remains authoritative for the terminal process, output, and control.

The server rechecks the exact pane and terminal identity for reads and sessions. Observation and control use separate Herdr child processes. Each stream starts with a full frame and accepts later frames only in order; a replacement starts a new sequence. Closing or replacing a browser session cancels and collects the exact child it started.

The renderer comparison lab is development-only and unavailable without its explicit flag. Shepherdr has no durable terminal or conversation store and does not parse terminal output into Home, agent state, authorization, application actions, or Chat.

Existing product scope, private-network requirements, and untrusted-content rules in `north-star.md` and `architecture-proposal.md` remain unchanged.

## Real-phone gate

Status: **PASS**

The human gate confirmed mobile rendering, sending, native selection, scrolling and loading older output, continued live output, and shortcut commands.

## Ongoing acceptance guidance

Future terminal changes still require checks proportionate to the behavior they affect. A real-device acceptance report must name only the cases actually exercised. Automated tests, desktop mobile emulation, and Android emulators may support confidence but do not replace a required real-device check.

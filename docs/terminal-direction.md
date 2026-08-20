# Shepherdr terminal direction

Status: approved current behavior; integrated at `997edbc8ed720d9d7eb88ac3089bd741193de7bd`; real-device acceptance pending

This document records the human's direct terminal R&D decisions. It supersedes older terminal architecture and interface claims, including `wave-01-human-gate-correction.md`. The Home and workspace-management direction remains unchanged.

## Current experience

Terminal opens only from a current Home target whose pane and terminal identities resolve exactly. The server checks that same target again for terminal reads and attachments. A missing, stale, replaced, or mismatched target is unavailable; Shepherdr never falls back to another terminal.

Opening Terminal establishes an observer so output can remain visible without control. Observation and control are separate Herdr sessions. A new attachment begins with a full frame, later frames must be in order, and a replacement attachment starts a new frame sequence. Shepherdr uses Herdr's current output and does not promise durable history or offline replay.

On a phone or another primary coarse-pointer device, Terminal uses Reader:

- Reader observes between sends and supports ordinary browser text selection.
- **Write text** and each terminal shortcut form one input batch.
- Sending implicitly requests ordinary control, forwards that one batch only after control is acquired, waits for Herdr's matching forwarding acknowledgement, and immediately releases control.
- If someone else has control, nothing is sent. Reader says so and offers only **Take over and send** for that pending batch. The takeover requires confirmation that the current controller will lose input.
- If delivery cannot be confirmed after forwarding begins, Reader does not queue or send the batch again automatically. The person must check the terminal before acting again.
- Reader never shows persistent **Control**, **Take over**, or **Release** controls and never holds control between batches.

On a desktop primary-pointer device, Terminal uses the full terminal renderer. It observes first, requests ordinary free control, and may keep control for interactive use. Persistent **Control**, **Take over**, and **Release** actions belong only to this desktop experience. Takeover remains confirmed, and release leaves observation running.

Connection and input messages describe this terminal attachment, not the agent's status. Only actual agents use Herdr's `working`, `blocked`, `idle`, `done`, and `unknown` words.

## Runtime boundary

Shepherdr uses Herdr 0.8.0's existing snapshot, terminal read, observe, control, input acknowledgement, release, and takeover capabilities. Herdr remains authoritative for the terminal process, output, and control. Shepherdr does not add another terminal runtime.

The server owns exact target checks and the exact Herdr child processes started for an attachment. Closing or replacing a browser attachment cancels and collects its child. Browser disconnect, navigation, release, server shutdown, and target replacement must not leave a Shepherdr-started controller behind.

The renderer comparison lab is development-only. Its page, assets, and APIs are unavailable unless Shepherdr starts with the explicit terminal-lab flag.

There is no durable terminal store or conversation store. Terminal output is not parsed or reconstructed as Chat and never drives Home organization, agent state, authorization, or application actions. Terminal output, ANSI data, names, identities, repository content, attachments, selected text, and pasted text are untrusted.

## Scope kept separate

The approved Home and workspace-management direction continues independently. Sign-in and device trust, notifications, and Chat remain later work. When sign-in is off, anyone who can reach Shepherdr has operator authority, so the trusted-private-network requirement remains unchanged.

No team accounts, roles, organizations, public hosting, provider-specific prompt handling, new Herdr interface, or second agent runtime is introduced here.

## Acceptance

The implementation and independent code review are complete, but product acceptance still requires the human real-phone gate. On the real device, verify that Reader remains an observer between sends, sends one text or shortcut batch and releases, sends nothing when occupied, confirms **Take over and send**, permits useful reading and selection, and never exposes persistent mobile ownership controls. Also check ordinary phone viewport, rotation, keyboard, reconnect, and return-to-Home behavior.

Automated tests, a desktop browser's mobile emulation, and an Android emulator may support confidence. None replaces the real-device gate.

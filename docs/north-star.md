# Shepherdr product direction

Status: proposed

Scope: the current product

## The promise

Shepherdr is a simple way to check on Herdr from a phone. A person can reach every real Herdr terminal, see what agents are doing when they are present, and open a terminal for attention or direct control.

Shepherdr manages Herdr. It does not replace Herdr.

## The complete phone loop

From a phone, a person can:

1. Open Shepherdr in the chosen sign-in mode.
2. Trust this device when passkeys are on and the device is new.
3. See every current terminal under Herdr's workspace and tab structure, with agent identity and status when an agent is present.
4. See how many agents are working and which agents are blocked.
5. Open any current real Herdr terminal.
6. Choose whether this device receives blocked-agent notifications.
7. Tap a blocked-agent notification and open that agent's terminal.

The daily return path is short: open Shepherdr, see who is blocked, and open that terminal.

This whole loop is the product bar. This document does not decide the order in which it will be built.

## Home and attention

Home shows every real Herdr terminal exactly once under the workspace and tab that Herdr reports. Agent identity and status are optional metadata on a terminal; they never determine whether the terminal exists. Agent status uses these words unchanged:

- **working**
- **blocked**
- **idle**
- **done**
- **unknown**

Do not replace them with more precise-sounding words or give an ordinary terminal an invented status.

A persistent attention area shows how many agents are working. When any agent is blocked, its blocked count opens a filtered reuse of those terminal rows with their workspace and tab context. It does not add a second list, show a zero-blocked control, or create another status.

A single-terminal workspace is one compact, full-row destination. Home reveals tab and multi-terminal structure only when it helps distinguish place. It does not repeat agent counts, cards, or large “Open terminal” buttons.

Human titles remain consistent between Home, Terminal, and accessibility. Neutral displayed-order numbers appear only when same-named flattened workspaces collide across Home or same-titled terminal rows collide within their workspace and, when shown, tab. Matching titles in unrelated workspace/tab contexts stay unnumbered. The sets come from the complete all-terminals view, so blocked filtering never renumbers them. Opaque IDs never appear as disambiguation.

**No terminals**, offline, reconnecting, last-known, and “Herdr is not running” are real states. Show them plainly. An ordinary terminal means Home is not empty.

## Terminal

The terminal is the full-control view into the same work Herdr owns. Home opens the selected real terminal. A blocked-agent notification opens that agent's terminal.

Use only terminal capabilities Herdr already exposes. Do not build another agent runtime or infer new product state from terminal output.

## Notifications

Notification settings belong to each device. A person can choose whether to receive blocked-agent notifications. Permission is requested when it makes sense, and the setting remains available later.

Tapping a blocked-agent notification opens that agent's terminal.

## Sign-in and devices

Starting without sign-in is the common daily mode for one person on a private network. Anyone who can reach Shepherdr can act as the operator. The interface quietly keeps “Sign-in is off” visible.

Passkeys are the stronger mode. They are useful as a second lock, when more than one trusted person uses Shepherdr, or in a business. Each device must be trusted before it can be used.

A person can view trusted devices and revoke one. They can also reset sign-in entirely from the command line and set it up again. Exact command names come later.

Reaching Shepherdr is never enough to trust a new device. Trust must come from an already trusted authority or an explicit action on the machine running Herdr. The exact method comes later.

Lost-device recovery comes later. Do not design it now. Do not add team accounts, roles, or organizations. One operator with many devices is enough for the current product.

## Private network

Shepherdr gives access to real terminals. Always make it reachable only through a trusted private network, including when passkeys are on. A trusted private network contains only users and devices you are willing to give access to the machine running Herdr. Tailscale Serve is one practical example. Tailscale Funnel and other public internet exposure are not supported.

Sign-in is an extra lock. It is not a reason to make the tool public.

## Honest state

Show only actions and state that exist. Never claim success before Herdr or the service confirms it. Never invent a more precise agent status than Herdr provides.

Reconnects, restarts, offline devices, and interrupted actions must leave the person with a clear and truthful view of what is known.

Treat Herdr output, names, agents, terminal identities, terminal content, repository files, attachments, and pasted content as untrusted. Merely displaying that content must never give it Shepherdr application authority. A terminal's identity cannot grant authority or redirect to another terminal.

The current product uses only capabilities Herdr already exposes. Do not invent Herdr interfaces, reconstruct conversations from terminal output, or add a Shepherdr conversation store.

## Outside this product

- Project planning and architecture management.
- A replacement for Herdr.
- Public hosting.
- Lost-device recovery in the current design.
- Team accounts, roles, and organizations.
- Provider-specific handling for blocked prompts.
- A replacement or second agent runtime beside Herdr.
- Chat, image sending, and new-message notifications. Chat may return later through a deliberate agent-used channel, but it must not be a live, parsed, scraped, or reconstructed view of terminal output. That channel is not designed or approved.
- Worktree or repository grouping, workspace nesting, and collapsibility. Current Herdr metadata does not establish workspace parents; any later grouping layer must be separately approved and additive.

## How we will know it works

Use a real phone and a real Herdr server. Compare Home with Herdr, open agent and ordinary terminals, use blocked attention, preserve place between Home and Terminal, exercise observer and takeover behavior, reconnect, restart the service, and receive a real blocked-agent notification when notification work exists. At every step, the interface must tell the truth about what Herdr has confirmed and what remains unknown.

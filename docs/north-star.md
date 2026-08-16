# Shepherdr product direction

Status: proposed

Scope: the current product

## The promise

Shepherdr is a simple way to check on Herdr from a phone. A person can see what agents are doing and open a real terminal when one needs attention or direct control.

Shepherdr manages Herdr. It does not replace Herdr.

## The complete phone loop

From a phone, a person can:

1. Open Shepherdr in the chosen sign-in mode.
2. Trust this device when passkeys are on and the device is new.
3. See workspaces and agents with Herdr's real status words.
4. See how many agents are working and which agents are blocked.
5. Open the real terminal for an agent's Herdr work.
6. Choose whether this device receives blocked-agent notifications.
7. Tap a blocked-agent notification and open that agent's terminal.

The daily return path is short: open Shepherdr, see who is blocked, and open that agent's terminal.

This whole loop is the product bar. This document does not decide the order in which it will be built.

## Home and attention

Home shows the workspaces and agents that Herdr reports. Agent status uses these words unchanged:

- **working**
- **blocked**
- **idle**
- **done**
- **unknown**

Do not replace them with more precise-sounding words. Supporting text may say that a blocked agent “needs you,” but its status remains **blocked**.

A persistent attention control shows how many agents are working. When any agent is blocked, it calls for attention and gives a direct path to those agents.

Empty, offline, and “Herdr is not running” are real states. Show them plainly.

## Terminal

The terminal is the full-control view into the same work Herdr owns. Home and blocked-agent notifications open the relevant agent's real terminal.

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

Treat Herdr output, agents, terminal content, repository files, attachments, and pasted content as untrusted. Merely displaying that content must never give it Shepherdr application authority.

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

## How we will know it works

Use a real phone and a real Herdr server. Open Home, use the attention control, open a blocked agent's terminal, reconnect, restart the service, and receive a real blocked-agent notification. At every step, the interface must tell the truth about what Herdr has confirmed and what remains unknown.

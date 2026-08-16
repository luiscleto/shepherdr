# Shepherdr product direction

Status: proposed

Scope: the complete product

## The promise

Shepherdr is a simple way to check on Herdr from a phone. A person can see what agents are doing, talk to one in chat, and open a real terminal when direct control is needed.

Shepherdr manages Herdr. It does not replace Herdr.

## The complete phone loop

From a phone, a person can:

1. Open Shepherdr in the chosen sign-in mode.
2. Trust this device when passkeys are on and the device is new.
3. See workspaces and agents with Herdr's real status words.
4. See how many agents are working and which blocked agents need a reply or choice.
5. Open one agent in chat and exchange messages.
6. Paste text or an image, preview it, and send it in chat.
7. Open the same Herdr work in a real terminal.
8. Choose whether this device receives notifications for messages and blocked agents.
9. Tap a notification and go straight to the relevant agent.

The daily return path is short: open Shepherdr, see who is blocked, and go straight to that agent.

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

## Chat and terminal

Chat is one agent at a time. On a phone, it is the usual way into an agent. The terminal is the full-control escape hatch.

Chat and terminal are two ways into the same Herdr work, not two conversations and not a second way to run agents.

If a blocked agent is waiting for a choice or reply, chat may collect it only when Herdr exposes the choice and input in a general way. Do not add special behavior for one provider. When Herdr does not expose the input in a general way, open the blocked agent in the terminal.

Chat must accept pasted text and images. Show an image preview before sending. Image paste in the terminal is welcome only when it stays clear and reliable.

## Notifications

Notification settings belong to each device. A person can choose notifications for messages and for blocked agents. Permission is requested when it makes sense, and the setting remains available later.

Tapping a notification opens the relevant agent. On a phone, it opens chat when chat can handle the next step and the terminal when it cannot.

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

## Outside this product

- Project planning and architecture management.
- A replacement for Herdr.
- Public hosting.
- Lost-device recovery in the current design.
- Team accounts, roles, and organizations.
- Provider-specific handling for blocked prompts.
- A separate chat or agent runtime beside Herdr.

## How we will know it works

Use a real phone and a real Herdr server. Complete the full loop, reconnect, restart the service, receive a real notification, move between chat and terminal, and recover from an interrupted action. At every step, the interface must tell the truth about what Herdr has confirmed and what remains unknown.

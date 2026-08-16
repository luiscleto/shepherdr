# Shepherdr

Shepherdr is a mobile-first web tool for viewing and managing Herdr. From a phone, you can check on agents and open a real terminal when one needs you. Shepherdr does not replace Herdr.

The project is still being designed. There is no runnable service yet.

## Starting Shepherdr

Shepherdr will have two ways to start.

### Start without sign-in

This is the common daily setup for one person on a private network. Open Shepherdr and use it. You do not need to add or trust devices. Anyone who can reach Shepherdr can act as the operator.

### Start with passkeys

Use this when you want a second lock, when more than one trusted person uses Shepherdr, or in a business. Sign in with a passkey and trust each device before using it.

You will be able to view trusted devices, revoke one, and reset sign-in from the command line so you can set it up again. The exact commands will be added when the tool is runnable.

Reaching Shepherdr is never enough to trust a new device. Trust must come from an already trusted authority or an explicit action on the machine running Herdr. The exact method comes later.

## Keep it on a private network

Always make Shepherdr reachable only on a trusted private network, even when passkeys are on. A trusted private network contains only users and devices you are willing to give access to the machine running Herdr. Public internet access is not supported.

Tailscale is one practical way to do this:

1. Start Shepherdr so it listens only on `localhost`.
2. Note the local port that Shepherdr shows.
3. Run `tailscale serve <port>`, replacing `<port>` with that port.
4. Open the private address printed by Tailscale from another device on the same private network.

Do not use `tailscale funnel`. Funnel makes the service public. Sign-in is an extra lock, not a reason to put a Herdr terminal on the public internet.

## What it will do

- Show workspaces and agents using Herdr's statuses: working, blocked, idle, done, and unknown.
- Show how many agents are working, and take you to anyone who is blocked.
- Open the real terminal for an agent's Herdr work.
- Send blocked-agent notifications, with settings for each device.
- Open the blocked agent's terminal when you tap its notification.

Herdr output, agent content, terminal content, repository files, attachments, and pasted content are untrusted. Displaying them must never give them control over Shepherdr.

## Product documents

- [Product direction](docs/north-star.md)
- [Interface direction](docs/ui-direction.md)

Apache License 2.0.

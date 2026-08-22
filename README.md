# Shepherdr

Shepherdr is a mobile-first web tool for viewing and managing Herdr. From a phone, you can see every real Herdr terminal, check which agents need attention, and open and use any current terminal. Shepherdr does not replace Herdr.

## Start Shepherdr

This build supports one local Herdr session with sign-in off and listens only on localhost.

Requirements:

- Go 1.22 or later
- Node.js and npm for the browser build
- Herdr 0.8.0 (socket protocol 19)

Build Shepherdr:

```sh
npm ci --prefix web
npm run build --prefix web
go build -o bin/shepherdr .
```

Start it for the default Herdr session:

```sh
./bin/shepherdr
```

Or select the Unix socket for one other configured Herdr session:

```sh
./bin/shepherdr -herdr-socket /absolute/path/to/herdr.sock
```

Shepherdr prints its localhost address. Open that address directly on the machine, or publish it through a trusted private-network route as described below.

Sign-in is off. Anyone who can reach Shepherdr can view the configured Herdr terminals and send terminal input.

## Turn on notifications

Each Shepherdr installation needs a real operator contact before browsers can enable notifications. Start Shepherdr with either a `mailto:` address or an HTTPS website:

```sh
./bin/shepherdr -vapid-contact mailto:you@example.com
```

The contact is saved outside the repository with the installation's notification keys and subscriptions. Later starts reuse it. Supplying a different valid contact updates the contact without changing notification keys or browser subscriptions. Browser push providers receive this operator contact as part of standard Web Push; it is not a Shepherdr project contact.

Without a configured contact, Home and Terminal continue to work and Notifications settings explain the local setup command. After configuration, each browser or installed app enables and configures its own notifications from **Notifications** on Home or Terminal. `blocked` and `done` start on; the other Herdr statuses and workspace opened or closed notices start off.

A phone needs one stable private HTTPS address. Android Chrome does not require installation. On iPhone or iPad, add Shepherdr to the Home Screen and open it there before enabling notifications.

To clear the saved contact, notification keys, and every browser subscription, stop Shepherdr and run:

```sh
./bin/shepherdr -reset-notifications
```

Then configure `-vapid-contact` again and explicitly enable each browser again. Notifications are best effort: browsers and operating systems may delay, duplicate, or miss them, and Shepherdr keeps no notification history.

## Keep it on a private network

Always make Shepherdr reachable only on a trusted private network. A trusted private network contains only users and devices you are willing to give access to the machine running Herdr. Public internet access is not supported.

Tailscale is one practical way to do this:

1. Start Shepherdr. It refuses non-localhost listen addresses.
2. Note the local port that Shepherdr shows.
3. Run `tailscale serve <port>`, replacing `<port>` with that port.
4. Open the private address printed by Tailscale from another device on the same private network.

Do not use `tailscale funnel`. Funnel makes the service public.

## What it does

- Show every current terminal under Herdr's workspace and tab structure.
- Show agent identity and Herdr's working, blocked, idle, done, and unknown status when a terminal has an agent. Ordinary terminals have no invented status.
- Show how many agents are working. When agents are blocked, show those same terminal rows in a filtered list without duplicating them on Home.
- Filter the current Home locally by workspace, tab, terminal, or agent name without making another Herdr read or changing the global attention counts.
- Keep a single-terminal workspace compact and make the whole terminal row the open action.
- Keep a single-terminal worktree parent as a real open row with a separate disclosure action, and show compact nonzero status totals under its name.
- Add small displayed-order numbers only to same-named single-terminal workspaces across Home or same-titled terminal rows in the same workspace and, when shown, tab. Matching titles in unrelated workspace/tab contexts stay unnumbered, and filtering never renumbers them.
- Create a workspace at a freely entered directory, with `~` as the starting value and open repository paths as suggestions.
- Create a worktree from a repository workspace or from an ordinary workspace's current directory. Herdr decides whether that directory is a valid Git source.
- Close a workspace after showing the additional linked workspaces and nonzero agent counts that will also be affected.
- Delete a clean linked-worktree checkout without force and without deleting its branch. Herdr refuses a checkout that has changes.
- Open any current real Herdr terminal, whether or not it has an agent.
- From a phone, send one text or shortcut batch and release control after it is acknowledged. See [Terminal direction](docs/terminal-direction.md).
- Keep Home up to date after a connection drops. Shepherdr shows clearly labelled last-known values without presenting them as openable until it reconnects.
- Send per-browser notifications that name the relevant Herdr workspace for selected status transitions and workspace openings or closings.
- Open the exact current terminal from a status notification, or show **Terminal unavailable** when that exact terminal is gone.

If Herdr stops, start it again and Shepherdr will reconnect automatically.

Herdr output, names, IDs, agent content, terminal content, repository files, attachments, and pasted content are untrusted. Displaying them must never give them control over Shepherdr.

## Checks

```sh
go test ./...
npm run typecheck --prefix web
npm run build --prefix web
```

## Product documents

- [Product direction](docs/north-star.md)
- [Interface direction](docs/ui-direction.md)
- [Terminal direction](docs/terminal-direction.md)

Apache License 2.0.

# Shepherdr

Shepherdr is a mobile-first web tool for viewing and managing Herdr. From a phone, you can see every real Herdr terminal, check which agents need attention, and open and use any current terminal. Shepherdr does not replace Herdr.

## Start Shepherdr

This build supports one local Herdr session, protects it with passkeys by default, and listens only on localhost.

Requirements:

- Go 1.26 or later
- Node.js and npm for the browser build
- Herdr 0.8.0 (socket protocol 19)

Build Shepherdr:

```sh
npm ci --prefix web
npm run build --prefix web
go build -o bin/shepherdr .
```

On the first protected start, give Shepherdr its one lasting private HTTPS address:

```sh
./bin/shepherdr -public-origin https://shepherdr.example-private.net
```

Use the exact address that browsers will open. Shepherdr saves it, prints a ten-minute setup link and terminal QR code, and will not reveal that bearer link again after a restart. Open the link on the computer or scan the QR code on a phone, give this device a name, then choose **Trust this device** to create the first passkey. A new browser cannot trust itself without one of these invitations.

Later protected starts reuse the saved address:

```sh
./bin/shepherdr
```

Or select the Unix socket for one other configured Herdr session:

```sh
./bin/shepherdr -herdr-socket /absolute/path/to/herdr.sock
```

Protected sessions last 30 days by default without sliding extension. A new session can use a whole number from `1d` through `365d`, or `none` for a browser-session cookie with no Shepherdr clock deadline:

```sh
./bin/shepherdr -session-lifetime 365d
./bin/shepherdr -session-lifetime none
```

The selected lifetime is saved for new sessions. Browsers may discard any cookie earlier because of shutdown, private browsing, site-data clearing, storage policy, or a lost profile.

To run explicitly without passkeys:

```sh
./bin/shepherdr -no-sign-in
```

Home opens directly and shows **Sign-in is off.** Anyone who can reach Shepherdr can act as the operator in this mode.

## Manage trusted sign-ins

Stop Shepherdr before using local access commands. Create another ten-minute invitation and QR code with:

```sh
./bin/shepherdr access invite
```

List trusted sign-ins and their opaque trust IDs:

```sh
./bin/shepherdr access devices
```

Labels in this list are untrusted display text. A passkey may sync; synced copies share one row and are revoked together. Backup state is only the state last reported by that passkey at the displayed time.

Revoke one trusted sign-in with:

```sh
./bin/shepherdr access revoke <trust-id>
```

Revoke refuses to remove the final passkey. If every passkey must be discarded, use the destructive local reset:

```sh
./bin/shepherdr access reset
```

Access reset rotates the operator identity, clears passkeys, sessions, invitations, and notification subscriptions, and preserves the private origin and notification identity. It creates no invitation itself; the next protected start prints a new bootstrap invitation.

While signed in, open **Settings**, then **Devices**, to see the same trusted sign-ins. Device settings are hidden when sign-in is off. Viewing them needs a valid session. Creating an invitation or revoking a trusted sign-in asks for a passkey if the latest verification is more than five minutes old. **Sign out** invalidates only that browser session.

## Turn on notifications

Each Shepherdr installation needs a real operator contact before browsers can enable notifications. Start Shepherdr with either a `mailto:` address or an HTTPS website:

```sh
./bin/shepherdr -vapid-contact mailto:you@example.com
```

The contact is saved outside the repository with the installation's notification keys and subscriptions. Later starts reuse it. Supplying a different valid contact updates the contact without changing notification keys or browser subscriptions. Browser push providers receive this operator contact as part of standard Web Push; it is not a Shepherdr project contact.

Without a configured contact, Home and Terminal continue to work and Notifications settings explain the local setup command. After configuration, each browser or installed app enables and configures its own notifications from **Settings** on Home or Terminal. `blocked`, `done`, trusted sign-in added, and trusted sign-in removed start on; the other Herdr statuses and workspace opened or closed notices start off. Existing enabled subscriptions safely gain the two trusted-sign-in choices. When an older installation first turns on protected access, subscriptions without a trusted-sign-in owner become inactive and each browser must explicitly enable notifications again.

A phone needs one stable private HTTPS address. Android Chrome does not require installation. On iPhone or iPad, add Shepherdr to the Home Screen and open it there before enabling notifications.

To clear the saved contact, notification keys, and every browser subscription without changing passkeys or protected sessions, stop Shepherdr and run:

```sh
./bin/shepherdr -reset-notifications
```

Then configure `-vapid-contact` again and explicitly enable each browser again. Notifications are best effort: browsers and operating systems may delay, duplicate, or miss them, and Shepherdr keeps no notification history.

## Keep it on a private network

Always make Shepherdr reachable only on a trusted private network. A trusted private network contains only users and devices you are willing to give access to the machine running Herdr. Passkeys do not make public hosting supported.

The protected hostname is a lasting passkey boundary. Dedicate that hostname to Shepherdr across every port; do not host another HTTPS service on it. Direct loopback, IP addresses, `localhost`, aliases, alternate ports, and forwarded host headers are not protected fallbacks.

Tailscale is one practical way to do this:

1. Choose the stable private HTTPS hostname that Tailscale will serve.
2. Start Shepherdr with that exact `-public-origin`. Shepherdr refuses non-localhost listen addresses.
3. Publish Shepherdr's local port with `tailscale serve <port>`.
4. Open that exact private HTTPS address from another device on the same private network.

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
- Keep the last complete Home after a connection drops. Terminals are not openable until Shepherdr reconnects.
- Send per-browser notifications that name the relevant Herdr workspace for selected status transitions and workspace openings or closings.
- Open the exact current terminal from a status notification, or show **Terminal unavailable** when that exact terminal is gone.

If Herdr stops, start it again and Shepherdr will reconnect automatically.

Herdr output, names, IDs, agent content, terminal content, repository files, attachments, and pasted content are untrusted. Displaying them must never give them control over Shepherdr.

## Checks

```sh
go test ./...
npm test --prefix web
npm run typecheck --prefix web
npm run build --prefix web
```

## Product documents

- [Product direction](docs/north-star.md)
- [Interface direction](docs/ui-direction.md)
- [Terminal direction](docs/terminal-direction.md)
- [Notification architecture](docs/notification-architecture-proposal.md)
- [Passkey access architecture](docs/passkey-device-architecture-proposal.md)

Apache License 2.0.

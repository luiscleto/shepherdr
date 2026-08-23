# Shepherdr

Shepherdr is a runnable, mobile-first web interface for one local Herdr session. It shows Herdr's current workspaces, terminals, agents, and statuses; opens and controls exact terminals; manages workspaces and worktrees; sends per-browser notifications; and can stage files from a phone for an agent to open. Herdr remains the runtime authority.

## Requirements

- Go 1.26 or later.
- Node.js 20 or later and npm. Node 20 is the highest minimum required by the locked browser dependencies.
- Herdr 0.8.0 (socket protocol 19), with the `herdr` executable on `PATH` and the intended local Herdr session available.

Run Shepherdr and Herdr as the same operating-system account. Terminal file paths are local paths on the machine running them; they are not remote or provider-native attachments.

## Build and start

```sh
npm ci --prefix web
npm run build --prefix web
go build -o ./shepherdr .
```

Shepherdr listens on `127.0.0.1:8787` by default and accepts only loopback listen addresses. Its default Herdr socket is the `herdr/herdr.sock` path under the platform user configuration directory, normally `~/.config/herdr/herdr.sock` on Linux. Select another absolute socket path when needed:

```sh
./shepherdr -herdr-socket /absolute/path/to/herdr.sock
```

### Protected access (default)

The first protected start needs the exact private HTTPS origin that browsers will open:

```sh
./shepherdr -public-origin https://shepherdr.example-private.net
```

The origin must be a canonical lowercase HTTPS DNS name with no path. Shepherdr saves it, prints a ten-minute setup link and terminal QR code, and does not print that same bearer link again after restart. Open the link on the computer or scan the QR code on a phone, name the trusted sign-in, and choose **Trust this device** to create its passkey. A new browser cannot trust itself without an invitation.

Later starts reuse the saved origin:

```sh
./shepherdr
```

Protected sessions last 30 days by default without sliding extension. Set the duration for newly created sessions to a whole number from `1d` through `365d`, or use `none` for a browser-session cookie with no Shepherdr clock deadline:

```sh
./shepherdr -session-lifetime 365d
./shepherdr -session-lifetime none
```

The choice is saved. A browser can still discard either kind of cookie earlier.

To start explicitly without passkeys:

```sh
./shepherdr -no-sign-in
```

The interface shows **Sign-in is off.** Anyone who can reach Shepherdr can act as the operator in this mode.

## Trusted sign-ins

While signed in, open **Settings**, then **Devices**, to list trusted sign-ins, invite another device, revoke a sign-in when another remains, or sign out this browser. Creating an invitation or revoking a sign-in asks for a passkey when the latest verification is more than five minutes old. A passkey can sync; its copies share one entry and are revoked together.

Local access commands require Shepherdr to be stopped:

```sh
./shepherdr access invite
./shepherdr access devices
./shepherdr access revoke <trust-id>
./shepherdr access reset
```

Invitations expire after ten minutes. Revoke refuses to remove the final passkey. The destructive reset clears passkeys, sessions, invitations, and notification subscriptions while preserving the private origin and notification identity; the next protected start prints a new setup invitation.

## Notifications

Configure a real operator contact before a browser can enable Web Push:

```sh
./shepherdr -vapid-contact mailto:you@example.com
```

An absolute HTTPS website is also accepted. The contact, notification keys, and subscriptions are stored outside the repository. Later starts reuse them; supplying another valid contact updates it without changing the keys or subscriptions. The contact is shared with browser push providers as part of Web Push.

Each browser or installed app manages its own choices under **Settings**. `blocked`, `done`, trusted sign-in added, and trusted sign-in removed start on; `working`, `idle`, `unknown`, workspace opened, and workspace closed start off. Android Chrome does not require installation. On iPhone or iPad, add Shepherdr to the Home Screen and open it there before enabling notifications.

To clear the saved contact, notification keys, and all subscriptions without changing access state, stop Shepherdr and run:

```sh
./shepherdr -reset-notifications
```

Then configure the contact and enable each browser again. Notifications are best effort and have no history or delivery guarantee.

## Send files from Terminal

On a phone, **Message** opens the Reader composer. **Add files** appears only while the exact terminal has a Herdr-recognized agent. The **Photos** and **Files** choices use the browser's ordinary pickers; **Files** accepts arbitrary files. The phone and browser decide which sources and multi-selection behavior are available. Pending cards can be removed, and files can be sent with or without message text.

Shepherdr stages opaque bytes on its own machine and sends one Terminal batch containing absolute paths under **User uploaded files:**. These paths are for the agent to open. Terminal acknowledgement does not prove that an agent, provider, or model opened or read a file. Uploaded content, names, and paths are untrusted, and an upload can consume memory, disk, and transfer time.

The staging parent defaults to the platform temporary directory, normally `/tmp` on Linux. The request limit is the total decoded file bytes in one send and defaults to `50MiB`:

```sh
./shepherdr -upload-parent /absolute/staging/parent
./shepherdr -upload-limit 100MiB
./shepherdr -upload-limit none
```

Positive limits accept bytes or the `KiB`, `MiB`, and `GiB` suffixes. `none` removes Shepherdr's request bound and can exhaust available memory, time, or disk.

One owner-only staging directory belongs to each workspace. Files remain across agent exit or replacement, reconnect, and Shepherdr restart. When a complete Herdr state shows that workspace has been removed, Shepherdr makes a best-effort attempt to delete only its recorded, verified directory; startup retries recorded cleanup after it receives a complete workspace set. It does not scan the temporary directory or promise crash-perfect cleanup.

Treat the staging directory as temporary, operator-owned storage, not durable file history. The platform may clear it, a cleanup can fail, and an abnormal crash can leave an orphan. Shepherdr and the Herdr agents need the same account so the agent can read the owner-only files.

## Private-network deployment

Keep Shepherdr reachable only on a trusted private network containing only users and devices you are willing to give access to the machine running Herdr. Passkeys are an extra lock; they are not permission to publish Shepherdr publicly.

The protected hostname is a lasting passkey boundary. Dedicate it to Shepherdr across every port, and always open that exact origin. Loopback, IP addresses, `localhost`, aliases, alternate ports, and forwarded host headers are not protected fallbacks.

Tailscale Serve is one practical deployment. With Shepherdr using its default local port, configure the stable private HTTPS hostname as `-public-origin`, start Shepherdr, then publish the loopback service:

```sh
tailscale serve 8787
```

Open the resulting private HTTPS address from another trusted tailnet device. Do not use Tailscale Funnel or any other public exposure.

## Product behavior

Home groups only worktrees that Herdr identifies, shows every current terminal exactly once, and uses Herdr's status words unchanged: `working`, `blocked`, `idle`, `done`, and `unknown`. It offers current workspace creation, worktree creation, workspace close, and clean linked-checkout deletion without force. Terminal opens only the exact current target and never substitutes another terminal. If Herdr stops, restart it and Shepherdr reconnects automatically.

Herdr output, agents, terminal content, repository files, uploaded files, attachments, pasted content, names, and identifiers are untrusted. Displaying them never grants Shepherdr authority.

Current product and interface direction:

- [Product direction](docs/north-star.md)
- [Interface direction](docs/ui-direction.md)

Apache License 2.0.

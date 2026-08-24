# Shepherdr

Shepherdr is now a runnable, mobile-first web interface for one local Herdr session. From a phone, you can check agents, manage workspaces, use terminals, send files, and choose notifications. Herdr remains the runtime.

## Requirements

- Go 1.27 or later.
- Node.js 20 or later and npm. The locked browser dependencies require at least Node 20.
- Herdr 0.8.0 (socket protocol 19), with the `herdr` executable on `PATH` and the intended local Herdr session available.

Files sent through Terminal become local paths on the machine running Shepherdr. They are not sent directly to a model, so run Shepherdr and the Herdr agents as the same operating-system account so the agents can read them.

## Build and start

```sh
npm ci --prefix web
npm run build --prefix web
go build -o bin/shepherdr .
```

Shepherdr listens on `127.0.0.1:8787` by default and accepts only loopback listen addresses. Its default Herdr socket is the `herdr/herdr.sock` path under the platform user configuration directory, normally `~/.config/herdr/herdr.sock` on Linux. Select another absolute socket path when needed:

```sh
bin/shepherdr -herdr-socket /absolute/path/to/herdr.sock
```

### Protected access (default)

The first protected start needs the exact private HTTPS origin that browsers will open:

```sh
bin/shepherdr -public-origin https://shepherdr.example-private.net
```

The address must be a lowercase HTTPS DNS name with no path. Shepherdr saves it, prints a private setup link and terminal QR code that expire after ten minutes, and does not print that same link again after restart. Open the link on the computer or scan the QR code on a phone, name the trusted sign-in, and choose **Trust this device** to create its passkey. A new browser cannot trust itself without an invitation.

After setup, protected visits use **Sign in with a passkey**. You can instead start Shepherdr with sign-in off, as described below.

Later starts reuse the saved origin:

```sh
bin/shepherdr
```

Protected sign-ins expire after 30 days by default, and using Shepherdr does not extend that time. Set the duration for new sign-ins to a whole number from `1d` through `365d`, or use `none` to let the browser decide how long its sign-in cookie remains:

```sh
bin/shepherdr -session-lifetime 365d
bin/shepherdr -session-lifetime none
```

The choice is saved. A browser can still discard the cookie earlier.

To start explicitly without passkeys:

```sh
bin/shepherdr -no-sign-in
```

The interface shows **Sign-in is off.** Anyone who can reach Shepherdr can act as the operator in this mode.

## Trusted sign-ins

While signed in, open **Settings**, then **Devices**, to list trusted sign-ins, invite another device, revoke a sign-in when another remains, or sign out this browser. Creating an invitation or revoking a sign-in asks for a passkey when the latest verification is more than five minutes old. A passkey can sync; its copies share one entry and are revoked together.

Local access commands require Shepherdr to be stopped:

```sh
bin/shepherdr access invite
bin/shepherdr access devices
bin/shepherdr access revoke <trust-id>
bin/shepherdr access reset
```

- `access invite` prints a new ten-minute link and QR code for trusting another device.
- `access devices` lists trusted sign-ins and the trust IDs used by the revoke command.
- `access revoke <trust-id>` removes one trusted sign-in but refuses to remove the final passkey.
- `access reset` removes every passkey, sign-in, invitation, and notification subscription.

Reset keeps the private address, notification keys, and configured notification contact. It does not create an invitation; the next protected start prints a new setup invitation.

## Notifications

Configure a real operator contact before a browser can enable Web Push:

```sh
bin/shepherdr -vapid-contact mailto:you@example.com
```

An absolute HTTPS website is also accepted. The contact, notification keys, and subscriptions are stored outside the repository. Later starts reuse them; supplying another valid contact updates it without changing the keys or subscriptions. The contact is shared with browser push providers as part of Web Push.

Each browser or installed app manages its own choices under **Settings**. `blocked`, `done`, trusted sign-in added, and trusted sign-in removed start on; `working`, `idle`, `unknown`, workspace opened, and workspace closed start off. Android Chrome does not require installation. On iPhone or iPad, add Shepherdr to the Home Screen and open it there before enabling notifications.

To clear the saved contact, notification keys, and all subscriptions without changing access state, stop Shepherdr and run:

```sh
bin/shepherdr -reset-notifications
```

Then configure the contact and enable each browser again. Notifications are best effort and have no history or delivery guarantee.

## Send files from Terminal

On a phone, **Message** opens the terminal composer. **Add files** appears only while the exact terminal has a Herdr-recognized agent. The **Photos** and **Files** choices use the browser's ordinary pickers; **Files** accepts arbitrary files. The phone and browser decide which sources and multi-selection behavior are available. Pending cards can be removed, and files can be sent with or without message text.

Shepherdr stores the selected files temporarily on its own machine and sends their absolute paths through Terminal in this form:

```text
User uploaded files:
- /absolute/path/to/file
```

These paths are for the agent to open. Shepherdr does not claim the files were delivered to a model or read by an agent. Uploaded content, names, and paths are untrusted, and an upload can consume memory, disk, and transfer time.

The staging parent defaults to the platform temporary directory, normally `/tmp` on Linux. The request limit is the total decoded file bytes in one send and defaults to `50MiB`:

```sh
bin/shepherdr -upload-parent /absolute/staging/parent
bin/shepherdr -upload-limit 100MiB
bin/shepherdr -upload-limit none
```

Positive limits accept bytes or the `KiB`, `MiB`, and `GiB` suffixes. `none` removes Shepherdr's request bound and can exhaust available memory, time, or disk.

Files stay with their workspace when an agent exits or changes and when Shepherdr restarts. After the workspace is gone, Shepherdr tries to delete only the staging folder it created for that workspace. Cleanup can fail, and a crash may leave files behind. This temporary storage is not durable file history.

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

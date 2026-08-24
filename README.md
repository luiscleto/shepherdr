# Shepherdr

Shepherdr is now a runnable, mobile-first web interface for one local Herdr session. From a phone, you can check agents, manage workspaces, use terminals, send files, and choose notifications. Herdr remains the runtime.

## Requirements

- Herdr 0.8.0 through 0.8.2, with the `herdr` executable on `PATH` and the intended local Herdr session available. Older releases are refused; newer releases continue best-effort with a warning.

Run Shepherdr and Herdr as the same operating-system account. Shepherdr needs that account's owner-only local Herdr socket, and Herdr agents need to be able to read files uploaded through Shepherdr. Files sent through Terminal become local paths on the machine running Shepherdr; they are not sent directly to a model.

## Install

Release archives are the recommended installation. Open the stable [latest release](https://github.com/luiscleto/shepherdr/releases/latest) and download its checksum file plus the archive for this machine:

| System | Archive |
| --- | --- |
| Linux x86-64 | `shepherdr_0.1.0_linux_amd64.tar.gz` |
| Linux ARM64 | `shepherdr_0.1.0_linux_arm64.tar.gz` |
| macOS Intel | `shepherdr_0.1.0_darwin_amd64.tar.gz` |
| macOS Apple silicon | `shepherdr_0.1.0_darwin_arm64.tar.gz` |

Each archive contains one same-named directory with `shepherdr`, `README.md`, `CHANGELOG.md`, and `LICENSE`. Verify the selected archive against `shepherdr_0.1.0_checksums.txt` before extracting it. For example:

```sh
archive=shepherdr_0.1.0_linux_amd64.tar.gz
grep "  ${archive}$" shepherdr_0.1.0_checksums.txt | shasum -a 256 -c -
tar -xzf "$archive"
install -m 0755 "${archive%.tar.gz}/shepherdr" "$HOME/.local/bin/shepherdr"
shepherdr --version
```

Choose a directory already on `PATH` if `$HOME/.local/bin` is not on it.

### Build from source

Source builds require Go 1.27 or later, Node.js 20 or later, and npm. Build the browser application from the locked dependencies before building the Go executable:

```sh
npm ci --prefix web
npm run build --prefix web
go build -o bin/shepherdr .
```

### Optional Go installation

The release archives remain the recommended path. As a convenience, the tagged browser assets are also included in the Go module, so `go install` does not run npm:

```sh
go install github.com/luiscleto/shepherdr@latest
go install github.com/luiscleto/shepherdr@v0.1.0
```

The first command follows the latest published module version; the second pins the first release. Go writes the executable to `GOBIN`, or to the Go bin directory when `GOBIN` is unset.

## Upgrade

Stop Shepherdr before replacing its executable. Leave its configuration and state in place, including the configured upload staging parent, so trusted sign-ins, notification settings, upload associations, and staged files remain available. Download the new archive, verify it against that release's checksum file, extract it, and replace the stopped executable. Then confirm the installed version and restart Shepherdr under the same operating-system account and with the same Herdr socket and configuration:

```sh
shepherdr --version
```

Do not remove the previous executable until the verified replacement is ready.

## Start

Shepherdr listens on `127.0.0.1:8787` by default and accepts only loopback listen addresses. Its default Herdr socket is the `herdr/herdr.sock` path under the platform user configuration directory, normally `~/.config/herdr/herdr.sock` on Linux. Select another absolute socket path when needed:

```sh
shepherdr -herdr-socket /absolute/path/to/herdr.sock
```

### Protected access (default)

The first protected start needs the exact private HTTPS origin that browsers will open:

```sh
shepherdr -public-origin https://shepherdr.example-private.net
```

The address must be a lowercase HTTPS DNS name with no path. Shepherdr saves it, prints a private setup link and terminal QR code that expire after ten minutes, and does not print that same link again after restart. Open the link on the computer or scan the QR code on a phone, name the trusted sign-in, and choose **Trust this device** to create its passkey. A new browser cannot trust itself without an invitation.

After setup, protected visits use **Sign in with a passkey**. You can instead start Shepherdr with sign-in off, as described below.

Later starts reuse the saved origin:

```sh
shepherdr
```

Protected sign-ins expire after 30 days by default, and using Shepherdr does not extend that time. Set the duration for new sign-ins to a whole number from `1d` through `365d`, or use `none` to let the browser decide how long its sign-in cookie remains:

```sh
shepherdr -session-lifetime 365d
shepherdr -session-lifetime none
```

The choice is saved. A browser can still discard the cookie earlier.

To start explicitly without passkeys:

```sh
shepherdr -no-sign-in
```

The interface shows **Sign-in is off.** Anyone who can reach Shepherdr can act as the operator in this mode.

## Trusted sign-ins

While signed in, open **Settings**, then **Devices**, to list trusted sign-ins, invite another device, revoke a sign-in when another remains, or sign out this browser. Creating an invitation or revoking a sign-in asks for a passkey when the latest verification is more than five minutes old. A passkey can sync; its copies share one entry and are revoked together.

Local access commands require Shepherdr to be stopped:

```sh
shepherdr access invite
shepherdr access devices
shepherdr access revoke <trust-id>
shepherdr access reset
```

- `access invite` prints a new ten-minute link and QR code for trusting another device.
- `access devices` lists trusted sign-ins and the trust IDs used by the revoke command.
- `access revoke <trust-id>` removes one trusted sign-in but refuses to remove the final passkey.
- `access reset` removes every passkey, sign-in, invitation, and notification subscription.

Reset keeps the private address, notification keys, and configured notification contact. It does not create an invitation; the next protected start prints a new setup invitation.

## Notifications

Configure a real operator contact before a browser can enable Web Push:

```sh
shepherdr -vapid-contact mailto:you@example.com
```

An absolute HTTPS website is also accepted. The contact, notification keys, and subscriptions are stored outside the repository. Later starts reuse them; supplying another valid contact updates it without changing the keys or subscriptions. The contact is shared with browser push providers as part of Web Push.

Each browser or installed app manages its own choices under **Settings**. `blocked`, `done`, trusted sign-in added, and trusted sign-in removed start on; `working`, `idle`, `unknown`, workspace opened, and workspace closed start off. Android Chrome does not require installation. On iPhone or iPad, add Shepherdr to the Home Screen and open it there before enabling notifications.

To clear the saved contact, notification keys, and all subscriptions without changing access state, stop Shepherdr and run:

```sh
shepherdr -reset-notifications
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
shepherdr -upload-parent /absolute/staging/parent
shepherdr -upload-limit 100MiB
shepherdr -upload-limit none
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

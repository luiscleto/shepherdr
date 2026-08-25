# Shepherdr

Shepherdr is a phone-friendly web interface for one local Herdr session. From a phone, you can check agents, manage workspaces, use terminals, send files, and choose notifications. Herdr still does the work.

https://github.com/user-attachments/assets/233f6030-c8c4-4ae8-827d-02913e2c4982

---

## Requirements

Shepherdr works with Herdr 0.8.0 through 0.8.2. The `herdr` executable must be on `PATH`, and Herdr must be running locally. Shepherdr does not start with older versions. It starts with newer versions but warns that the version has not been tested.

Run Shepherdr and Herdr as the same operating-system account. Shepherdr needs that account's owner-only local Herdr socket, and Herdr agents need to be able to read files uploaded through Shepherdr. Files sent through Terminal become local paths on the machine running Shepherdr; they are not sent directly to a model.

## Install

Release archives are the recommended installation. Open the stable [latest release](https://github.com/luiscleto/shepherdr/releases/latest) and download its checksum file plus the archive for this machine:

| System | Archive |
| --- | --- |
| Linux x86-64 | `shepherdr_0.1.0_linux_amd64.tar.gz` |
| Linux ARM64 | `shepherdr_0.1.0_linux_arm64.tar.gz` |
| macOS Intel | `shepherdr_0.1.0_darwin_amd64.tar.gz` |
| macOS Apple silicon | `shepherdr_0.1.0_darwin_arm64.tar.gz` |

The macOS builds are unsigned and not notarized, so macOS may show a system warning before the first run.

Each archive unpacks to a folder with the same name as the archive. The folder contains `shepherdr`, `README.md`, `CHANGELOG.md`, and `LICENSE`. Verify the selected archive against `shepherdr_0.1.0_checksums.txt` before extracting it. For example:

```sh
archive=shepherdr_0.1.0_linux_amd64.tar.gz
grep "  ${archive}$" shepherdr_0.1.0_checksums.txt | shasum -a 256 -c -
tar -xzf "$archive"
install -m 0755 "${archive%.tar.gz}/shepherdr" "$HOME/.local/bin/shepherdr"
shepherdr --version
```

Choose a directory already on `PATH` if `$HOME/.local/bin` is not on it.

### Optional Go installation

The release archives remain the recommended path. Go users can install Shepherdr without a separate browser build:

```sh
go install github.com/luiscleto/shepherdr@latest
go install github.com/luiscleto/shepherdr@v0.1.0
```

The first command follows the latest published module version; the second pins the first release. Go writes the executable to `GOBIN`, or to the Go bin directory when `GOBIN` is unset.

### Build from source

Source builds require Go 1.27 or later, Node.js 20 or later, and npm. Build the browser application from the locked dependencies before building the Go executable:

```sh
npm ci --prefix web
npm run build --prefix web
go build -o bin/shepherdr .
```

## Upgrade

Stop Shepherdr before replacing its executable. Keep its configuration, state, and uploaded files in place so trusted sign-ins, notification settings, and workspace files remain available.

Download the new archive, verify it against that release's checksum file, extract it, and replace the stopped executable. Check the installed version, then restart Shepherdr with the same operating-system account, Herdr socket, and settings:

```sh
shepherdr --version
```

Do not remove the previous executable until the verified replacement is ready.

## Start

Start Herdr first. Sign-in needs a stable private HTTPS address. Tailscale Serve is one way to provide it; see [Private network](#private-network). Then start Shepherdr with sign-in and notifications configured. This example uses a Tailscale address:

```sh
shepherdr \
  -public-origin https://machine-name.tailnet-name.ts.net \
  -vapid-contact mailto:you@example.com
```

Replace `https://machine-name.tailnet-name.ts.net` with Shepherdr's stable private HTTPS address. `-public-origin` must be exactly `https://` plus a lowercase DNS name, optionally followed by a nondefault port. Do not add a path, trailing slash, or `:443`.

Replace `mailto:you@example.com` with a real contact address for browser notifications, or use an absolute HTTPS website.

Shepherdr saves the private address and the notification contact. The address cannot be changed later. A new valid contact updates the saved contact.

On the first protected start, Shepherdr prints a private setup link and terminal QR code that expire after ten minutes. The printed link still works until it expires, but a restart does not print it again. If it expires or you miss it, stop Shepherdr and run `shepherdr access invite`. Open the link on the computer or scan the QR code on a phone, name the trusted sign-in, and choose **Trust this device** to create its passkey. A new browser cannot trust itself without an invitation.

### Later starts

After setup, protected visits use **Sign in with a passkey**. Later starts reuse the saved private address and notification contact:

```sh
shepherdr
```

Open the same private HTTPS address that you set with `-public-origin`. `http://127.0.0.1:8787` is only the default local listen address; it is not a protected way to use Shepherdr.

### Without sign-in (not recommended)

To start without passkeys for this run only:

```sh
shepherdr -no-sign-in
```

The interface shows **Sign-in is off**. Anyone who can reach Shepherdr has operator authority. The flag is not saved; the next start without it is protected again. Do not combine it with `-public-origin` or `-session-lifetime`.

### Additional options

Protected sign-ins expire after 30 days by default, and using Shepherdr does not extend that time. Set the duration for new sign-ins to a whole number from `1d` through `365d`, or use `none` so Shepherdr does not expire the sign-in:

```sh
shepherdr -session-lifetime 365d
shepherdr -session-lifetime none
```

The choice is saved. The browser may still drop the sign-in earlier, including when it closes.

Shepherdr listens on `127.0.0.1:8787` by default. Use `-listen` to select another loopback address or port. Shepherdr does not accept non-loopback addresses:

```sh
shepherdr -listen 127.0.0.1:8788
```

Normal Herdr setups use the default socket and do not need this option. If Herdr uses another socket, give its absolute path:

```sh
shepherdr -herdr-socket /absolute/path/to/herdr.sock
```

The default is the `herdr/herdr.sock` file in your user configuration folder, normally `~/.config/herdr/herdr.sock` on Linux.

## Private network

Never make Shepherdr reachable outside a trusted private network containing only users and devices you are willing to give access to the machine running Herdr. Passkeys are an extra lock; they are not permission to publish Shepherdr publicly.

Use one private HTTPS name and open only that exact address. Give that name to Shepherdr on every port; do not put another HTTPS service on it. Do not open Shepherdr by loopback, IP address, `localhost`, another name, or another port.

Tailscale Serve is one practical deployment. In one terminal, publish Shepherdr's default local port only inside the tailnet:

```sh
tailscale serve 8787
```

In another terminal, use the Serve private HTTPS address as the exact `-public-origin` value in the Start command above. Open that same address from another trusted tailnet device. Never use Tailscale Funnel or any other public exposure.

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

Before a browser can turn on notifications, set a real contact address:

```sh
shepherdr -vapid-contact mailto:you@example.com
```

An absolute HTTPS website is also accepted. The contact, notification keys, and subscriptions are stored outside the repository. Later starts reuse them; supplying another valid contact updates it without changing the keys or subscriptions. The contact is shared with browser push providers when notifications are set up.

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

By default, Shepherdr keeps selected files in the system temporary directory, normally `/tmp` on Linux. The total file size in one send defaults to `50MiB`:

```sh
shepherdr -upload-parent /absolute/staging/parent
shepherdr -upload-limit 100MiB
shepherdr -upload-limit none
```

Positive limits accept bytes or the `KiB`, `MiB`, and `GiB` suffixes. `none` removes Shepherdr's size limit and can exhaust available memory, time, or disk.

Files stay with their workspace when an agent exits or changes and when Shepherdr restarts. After the workspace is gone, Shepherdr tries to delete only the temporary folder it created for that workspace. Cleanup can fail, and a crash may leave files behind. This temporary storage is not durable file history.

## Product behavior

Home shows every current terminal once, groups only worktrees that Herdr identifies, and uses Herdr's status words unchanged: `working`, `blocked`, `idle`, `done`, and `unknown`. You can create workspaces and worktrees, close workspaces, and delete clean linked checkouts without force. Terminal opens only the selected current target and never substitutes another terminal. If Herdr stops, restart it and Shepherdr reconnects automatically.

Shepherdr treats Herdr output, agents, terminal content, repository files, uploaded files, attachments, pasted content, names, and identifiers as untrusted. Displaying them never grants Shepherdr authority.

Current product and interface direction:

- [Product direction](docs/north-star.md)
- [Interface direction](docs/ui-direction.md)

Apache License 2.0.

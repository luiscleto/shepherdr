# Shepherdr

Shepherdr is a mobile-first web tool for viewing and managing Herdr. From a phone, you can see every real Herdr terminal, check which agents need attention, and open any current terminal. Shepherdr does not replace Herdr.

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

Sign-in is off. Anyone who can reach Shepherdr can view and control the configured Herdr terminals.

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
- Keep a single-terminal workspace compact and make the whole terminal row the open action.
- Add small displayed-order numbers only to same-named single-terminal workspaces across Home or same-titled terminal rows in the same workspace and, when shown, tab. Matching titles in unrelated workspace/tab contexts stay unnumbered, and filtering never renumbers them.
- Open any current real Herdr terminal, whether or not it has an agent.
- Open as an observer, acquire control when it is free, and require confirmation before taking control from someone else.
- Keep Home up to date after a connection drops. Shepherdr shows clearly labelled last-known values without presenting them as openable until it reconnects.

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

Apache License 2.0.

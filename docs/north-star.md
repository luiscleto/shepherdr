# Shepherdr product direction

Status: proposed

Scope: the current product

The current product includes read-only Home and Terminal. The active non-terminal stream covers Home workspace organization and, in later slices, workspace management. Sign-in, devices, notifications, and Chat remain later work.

## The promise

Shepherdr is a simple way to check on Herdr from a phone. A person can see Herdr's real workspaces and terminals, understand what agents are doing when they are present, and reach the right work without Shepherdr inventing another organization or runtime.

Shepherdr manages Herdr. It does not replace Herdr.

## The complete phone loop

The broader product direction remains:

1. Open Shepherdr in the chosen sign-in mode.
2. Trust this device when passkeys are on and the device is new.
3. See Herdr's current workspaces and terminals, with agent identity and status when an agent is present.
4. See how many agents are working and which agents are blocked.
5. Open the intended current real terminal.
6. Choose whether this device receives blocked-agent notifications.
7. Tap a blocked-agent notification and open that agent's terminal.

The daily return path is short: open Shepherdr, see who is blocked, and reach that work. This is the product bar, not an implementation order. The current product implements read-only Home and Terminal; workspace management, sign-in, devices, notifications, and Chat remain separate or later work.

## Home and attention

Home shows every real Herdr workspace and terminal exactly once. Agent identity and status are optional terminal information; they never determine whether a terminal exists. Agent status uses these words unchanged:

- **working**
- **blocked**
- **idle**
- **done**
- **unknown**

Do not replace them with more precise-sounding words or give an ordinary terminal an invented status.

Ordinary flat Home stays compact. When Herdr establishes a worktree relationship, Home mirrors it as one nested set: one top-level workspace and its worktree workspaces. Shepherdr does not store or invent another organization. A set opens to show every workspace and every terminal. Its heading uses the top-level workspace title once, the workspace total, and only nonzero real agent totals in Herdr's fixed status order. Those totals are information, not controls.

The set's disclosure and the top-level workspace's current-terminal **Open** action are separate targets. **Open** appears only when that current terminal resolves exactly; Shepherdr never substitutes the first terminal. Every other terminal remains visible in its workspace.

On the first load of a visit, sets with working or blocked agents are expanded and the rest are collapsed. A person's later expand or collapse choice wins for that loaded visit even if agent statuses change. Home offers one compact **Expand all** or **Collapse all** action when useful.

A persistent attention area shows the working count. When any agent is blocked, **N blocked** opens a temporary **Blocked** view showing only blocked terminal rows with enough workspace context. It does not add a second stored list or change the person's remembered Home expansion, scroll, or focus. Returning restores that place.

## Truthful connection and content

A stable badge at the top right says **Live**, **Reconnecting**, **Offline**, **Herdr is not running**, or **Cannot use this Herdr**. Internal Home refresh does not turn a healthy connection into **Reconnecting**, and the badge never makes the page jump.

Connection health and a coherent Home are separate truths. Shepherdr never publishes a partial or mixed Home. While a fresh coherent Home is being rebuilt, show one short **Home is updating** note above the list when the prior coherent rows remain honest. Otherwise replace the list with clear updating or unavailable content. Open and management actions remain absent until the coherent Home is current.

**No terminals** is a real coherent state. An ordinary terminal means Home is not empty.

## Workspace management

The first slice is read-only Home. It has no management controls, placeholders, or empty menus.

Later slices add one operation at a time: create a workspace in a new worktree folder, close an applicable worktree workspace while retaining its folder and branch, and remove an eligible clean worktree folder without deleting its branch. Each operation checks fresh Herdr state, runs one at a time, preserves focus where required, and reports an interrupted or unconfirmed result as unknown. Shepherdr never fabricates a preview or success, retries automatically, forces removal, or lets displayed names, paths, branches, or identifiers choose what happens.

## Terminal

Home offers **Open** only for a current real terminal it can resolve exactly and truthfully. Terminal keeps observing without requiring control.

On a phone, sending text or a shortcut requests ordinary control for that one batch, forwards it only after acquisition, waits for the matching acknowledgement, and releases immediately. If the terminal is occupied, nothing is sent; the only takeover path for that batch is confirmed **Take over and send**. Persistent **Control**, **Take over**, and **Release** controls are desktop-only and must never appear in mobile Reader.

The current behavior and trust boundary are recorded in `terminal-direction.md`. Real-device product acceptance remains pending.

Use only terminal capabilities Herdr already exposes. Do not build another agent runtime or infer product state from terminal output.

## Notifications

Notifications are later work. Notification settings belong to each device. A person may eventually choose whether to receive blocked-agent notifications, and tapping one opens that agent's terminal only when the target still resolves truthfully.

## Sign-in and devices

Sign-in and device trust are later work. Starting without sign-in remains the common daily mode for one person on a private network. Anyone who can reach Shepherdr can act as the operator. The interface quietly keeps **Sign-in is off** visible.

Passkeys are the stronger future mode. Reaching Shepherdr is never enough to trust a new device. Trust must come from an already trusted authority or an explicit action on the machine running Herdr; the exact method comes later. Lost-device recovery, team accounts, roles, and organizations are not part of the current product.

## Private network

Shepherdr gives access to real work. Always make it reachable only through a trusted private network, including when passkeys are on. A trusted private network contains only users and devices you are willing to give access to the machine running Herdr. Tailscale Serve is one practical example. Tailscale Funnel and other public internet exposure are not supported.

Sign-in is an extra lock. It is not a reason to make the tool public.

## Honest state

Show only actions and state that exist. Never claim success before Herdr or the service confirms it. Never invent a more precise agent status than Herdr provides.

Reconnects, restarts, offline devices, and interrupted actions must leave the person with a clear and truthful view of what is known.

Treat Herdr output, names, agents, terminal identities, terminal content, repository files, attachments, and pasted content as untrusted. Merely displaying that content must never give it Shepherdr application authority. A displayed identity cannot grant authority or redirect to other work.

The current product uses only capabilities Herdr already exposes. Do not invent Herdr interfaces, reconstruct conversations from terminal output, or add a Shepherdr conversation store.

## Outside this product

- Project planning and architecture management.
- A replacement for Herdr.
- Public hosting.
- Lost-device recovery in the current design.
- Team accounts, roles, and organizations.
- Provider-specific handling for blocked prompts.
- A replacement or second agent runtime beside Herdr.
- A live, parsed, scraped, or reconstructed Chat view of terminal output. Any future Chat requires a deliberately designed agent-used channel.

## How we will know it works

Use production Shepherdr with a real Herdr server and a real phone. Compare flat and worktree-nested Home with Herdr, check every terminal and exact agent total, exercise expansion and Blocked place restoration, and verify connection and coherent-update states without flicker or stale actions. In Terminal, verify phone observation, single-batch transient control and release, occupied no-send and confirmed takeover retry, reading and selection, and the absence of persistent mobile ownership controls. For each later management slice, perform its real named, blank, clean, dirty, interrupted, and hostile-input cases. Tests and emulator checks support confidence, but the real phone workflow is the gate.

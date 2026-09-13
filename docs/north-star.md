# Shepherdr product direction

Status: approved

Scope: the current product

## The promise

Shepherdr is a simple way to check and operate Herdr from a phone. A person can see Herdr's real workspaces and terminals, understand what agents are doing when present, reach the exact work that needs attention, and take the workspace or Terminal actions that exist now.

Shepherdr manages Herdr. It does not replace Herdr or create another organization or agent runtime.

## The complete phone loop

1. On first protected setup, open the setup link or scan its QR code, then choose **Trust this device**.
2. On later visits, sign in with a passkey, or deliberately run with sign-in off.
3. See Herdr's current workspaces and terminals, with agent identity and status when an agent is present.
4. See how many agents are working and which are blocked.
5. Open the intended exact terminal in Reader; choose **Message** or switch to the full terminal for direct typing.
6. Send text or shortcuts; while a recognized agent is present, send files with optional text from Reader or insert file references in the full terminal for deliberate submission.
7. Choose notifications for this browser or installed app.
8. Open an exact current terminal from a status notification, or return to Home when that target is gone.

The daily return path stays short: open Shepherdr, see who needs attention, and reach that work.

## Home and attention

Home shows every real Herdr workspace and terminal exactly once. Agent identity and status are optional terminal information; an ordinary terminal still exists and has no invented status. Shepherdr uses Herdr's words unchanged: **working**, **blocked**, **idle**, **done**, and **unknown**.

Home has one filter over the names already shown. Filtering leaves the global attention counts unchanged and does not overwrite the person's expansion choices.

When Herdr establishes a worktree relationship, Home mirrors it as one group. It does not create groups from names, paths, branches, or guesswork. The group shows only nonzero real agent totals. A single real top-level terminal remains an openable row with a separate disclosure action; other shapes use a neutral group heading and show every real terminal when expanded.

Groups with working or blocked agents start expanded. Later expansion choices win for that visit. **Expand all** and **Collapse all** appear only when useful.

The attention area shows the working count. When agents are blocked, **N blocked** opens a temporary **Blocked** view with the minimum workspace context. **Show all terminals** restores Home's prior expansion, scroll, and focus.

## Truthful connection and content

The stable connection badge says **Live**, **Reconnecting**, **Offline**, **Herdr is not running**, or **Cannot use this Herdr**. It reflects whether Shepherdr can reach Herdr.

Shepherdr keeps the last complete Home in place until a complete replacement is ready. It shows no internal refresh state. If no Home has loaded, the list area has one short loading or unavailable message. **No terminals** means Herdr is running with nothing open.

## Workspace actions

Home shows only actions that can be targeted from current Herdr state. A person can:

- create a workspace at a freely entered directory;
- create a worktree from a confirmed repository workspace or a current directory that Herdr accepts as a Git source;
- close a workspace after seeing additional linked workspaces and nonzero agent counts that will be affected; and
- delete a clean linked-worktree checkout without force or branch deletion.

Each action checks fresh Herdr state and runs one at a time. Shepherdr reports an interrupted or unconfirmed result as unknown. It does not fabricate previews or success, retry automatically, force removal, or run a second Git workflow beside Herdr.

## Terminal and file sending

Home opens only an exact current terminal. On a phone, Terminal opens in Reader with browser text selection, older available output, **Message**, and terminal shortcuts. A terminal icon beside Settings opens the full terminal for direct keyboard input; a phone icon returns to Reader. Both views share one retained controller for the same Herdr terminal, with no separate runtime or conversation. Switching preserves Reader's place, selection, draft, and pending files. Both views reach history still retained by Herdr, with independent navigation rather than a shared scroll position.

Opening the terminal attempts ordinary control without input or keyboard focus. If another controller is present, Shepherdr observes without stealing control. **Control**, confirmed **Take over**, and **Release** are explicit actions; Reader conflict recovery can submit the one confirmed **Take over and send** action. Switching views and successful sends retain control. Release leaves observation until explicit Control, and losing control never triggers a fight to regain it. Input is never retried automatically. The terminal uses its actual available dimensions while controlling; observation never resizes another controller's terminal. Desktop retains its existing interactive control until release.

While the exact current terminal has a Herdr-recognized agent, Reader's composer can select arbitrary files, remove pending files, and send files with or without text. The full terminal has a compact files-only panel: **Insert files** inserts local file references at the application's cursor without Enter, leaving the Reader draft separate. Reader's **Send** submits its text and path list. Shepherdr stores the selected files temporarily on its own machine and sends their absolute local paths through Terminal. It does not claim the files were delivered to a model or read by an agent.

This behavior was accepted by the human on 2026-09-13; [the accepted mobile Terminal direction](mobile-terminal-shared-trial-proposal.md) records its scope and evidence. Herdr 0.9.0 is the minimum and the validated version. Newer or unknown versions retain a best-effort warning, not a validation claim.

Files stay with their workspace when an agent exits or changes and when Shepherdr restarts. After the workspace is gone, Shepherdr tries to delete only the staging folder it created for that workspace. Cleanup can fail, and a crash may leave files behind. This temporary operator-owned storage is not durable file history.

## Notifications

Notification settings belong to each browser or installed app. `blocked`, `done`, trusted sign-in added, and trusted sign-in removed start on; the other agent statuses and workspace opened or closed notices start off. A status notice opens only the exact current terminal, otherwise **Terminal unavailable**. Workspace notices open Home.

Notifications are best effort. There is no history, unread state, replay, delivery claim, or visible-page suppression. Push services and operating systems may delay, duplicate, or miss an alert.

## Sign-in and trusted devices

Protected access with passkeys is the default. First setup and later invitations use a short-lived link or QR code to reach **Trust this device** and create a passkey. A new browser cannot trust itself. Ordinary returns use **Sign in with a passkey** and ask for no account name or email address.

**Devices** lists trusted sign-ins. A passkey may sync, so its copies share one entry and are revoked together. A trusted browser can create an invitation, revoke a sign-in while another remains, or sign out. Stopped-service local commands can also invite, list, revoke, or destructively reset all trust. There is no remote or self-service lost-device recovery.

Sign-in can be deliberately turned off for a start. In that mode the interface keeps **Sign-in is off** visible and every browser that can reach Shepherdr has operator authority.

## Private network

Shepherdr gives access to real work. Keep it reachable only through a trusted private network, including when passkeys are on. Such a network contains only users and devices the operator is willing to give access to the machine running Herdr. Tailscale Serve is one practical example. Tailscale Funnel and other public exposure are unsupported.

Passkeys are an extra lock, not permission to publish Shepherdr publicly.

## Honest state and untrusted content

Show only actions and state that exist. Never claim success before Herdr or the responsible service confirms it. Never invent a more precise agent status than Herdr provides. Reconnects, restarts, offline devices, and interrupted actions leave a clear account of what is known.

Treat Herdr output, names, agents, terminal identities and content, repository files, uploaded files, paths, attachments, pasted content, device labels, and notification data as untrusted. Displaying them must never grant Shepherdr authority, redirect an action, choose another target, or create trust.

Use only capabilities Herdr already exposes. Do not invent Herdr interfaces.

## Outside the current product

Project planning, architecture management, public hosting, team accounts, roles, organizations, remote recovery, provider-specific prompt handling, and a second agent runtime are outside the product.

## How we know it works

Acceptance uses the production Shepherdr path with real Herdr and a real phone. It compares flat and grouped Home with Herdr, checks every terminal and agent total, exercises attention and place restoration, and confirms ordinary updates do not blink, move the page, or change connection state incorrectly. It covers exact Terminal reading and input, the phone's actually offered file sources and exact staged bytes and paths, workspace actions, protected and sign-in-off access, trusted-device changes, notification delivery behavior, cleanup, restart, interruption, and hostile content.

Automated and emulator checks support confidence but do not replace a required real-phone workflow. Acceptance reports only what was actually exercised.

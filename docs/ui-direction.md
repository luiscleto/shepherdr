# Shepherdr interface direction

Status: proposed

This is a voice and interface guide, not a component library.

The current runnable product includes Home, Terminal, workspace management, and per-browser notifications. Sign-in and device trust have approved direction but are not implemented. Chat remains later work.

## Voice

Write for a person checking Herdr workspaces, terminals, and agents from a phone, possibly while tired or interrupted.

- Headings state what is happening.
- Body text is short and useful.
- Buttons say what they do.
- Errors say what happened, what is known, and what to try next.
- Security language is quiet and direct, not dramatic.
- Internal names stay off the screen unless the person needs an exact value to act.

Use familiar words: **workspace**, **terminal**, **agent**, **worktree**, **device**, and **notification**. Keep internal words such as source, group, and pane off the screen. **Linked workspace** may appear only when a person needs to understand which additional workspaces an action will affect.

Use Herdr's status words unchanged: **working**, **blocked**, **idle**, **done**, and **unknown**. Supporting text may say a blocked agent “needs you,” but do not turn that into another status.

Exact paths and raw errors appear only when they help the person act. IDs and internal trust values stay off the screen.

## Screens

The current product has two main screens:

- **Home** — see every current workspace and terminal, with agent status and attention when an agent is present.
- **Terminal** — read one exact current terminal and send text or shortcuts when needed.

Workspace management uses small sheets from Home rather than another main screen. Notification settings are reached from Home or Terminal.

Do not add **Sign in**, **Trust this device**, **Devices**, or related controls before protected access is implemented.

## Planned access experience

When sign-in is off, the first screen is Home and it quietly keeps **Sign-in is off** visible. Do not add a mode picker or a red warning page.

When passkeys are on, an ordinary visit starts at **Sign in** and asks the person to use their passkey. It asks for no name, email address, or account.

First setup prints a link and QR code. Open the link on that computer, or scan the QR code on a phone. A valid setup or invitation link, whether opened directly or through a scanned QR code, starts at **Trust this device** and then asks the person to create a passkey. A setup link or QR code is only for trusting that browser and making a passkey; a new browser cannot trust itself.

Every unusable setup or invitation uses the same explanation, whatever the reason: **This invitation can't be used. Create a new invitation on the machine running Shepherdr or from a trusted device.** If someone who has a passkey loses browser data, say **Sign in again.** Do not combine these recovery paths or expose internal access or protocol terms when the person does not need them to act.

**Devices** exists only when passkeys are on. It lists trusted sign-ins without claiming that a row is one physical device. Explain that a passkey may sync and its copies share one entry and are revoked together. If copy or backup state is shown, label it **last reported by this passkey** and include the observation time; never imply a provider or live physical-device state.

From **Devices**, a person can use their passkey before creating a short-lived invitation as a link or QR code or revoking a trusted sign-in. Offer **Revoke** only when another trusted sign-in remains; do not show a disabled final **Revoke**. Clearing all access requires stopping Shepherdr and resetting it on the machine. A person can also sign out.

Keep this work within the current small phone interface. Preserve Home, Terminal, workspace actions, and the existing placement and behavior of **Notifications** settings. Add no placeholder access controls before the complete passkey flow exists.

## Visual character

Think of a well-used drafting table made comfortable for a small screen:

- warm off-white paper surfaces with a slightly raised workspace surface, without making everything beige;
- charcoal text and fine, confident dividing rules;
- shallow shadow only where it separates a major workspace surface, with flat internal rows and little corner rounding;
- one restrained accent for focus and the main action;
- status colors used sparingly and never as the only signal;
- typography, spacing, and alignment doing most of the work;
- no gradients, glass panels, glowing controls, hacker decoration, or stacks of generic rounded cards.

Use a strong sans-serif face for the product name, workspace names, and terminal names. Use a monospaced face only for exact technical values, statuses, and compact counts. Keep the normal Home masthead to **Shepherdr**; do not spend a second line repeating **Home**. A temporary view such as **Blocked** keeps its own visible heading.

## Home and attention

Home first answers:

1. Who is working?
2. Who is blocked?
3. What needs my attention?
4. What workspaces and terminals are here?

Home shows every real workspace and every real terminal exactly once. Agent identity and one of Herdr's exact status words are optional terminal information. An ordinary terminal has no status badge and does not contribute to attention.

Home includes one client-side text filter over visible workspace, tab, terminal, and agent names. Its label is **Filter workspaces and terminals**. It filters only the last complete Home already in the browser; it does not trigger another read, change global working or blocked counts, or renumber displayed titles. A workspace or tab name match keeps its current rows together. A terminal or agent match shows that row with the minimum workspace context. While filtering, reveal matching nested content without changing remembered expansion choices. Show **No matches** and “Try another filter.” when nothing matches.

Keep ordinary flat Home compact. Terminal rows use a strong human title, optional agent identity and exact status below it, and a small terminal glyph at the aligned trailing edge. The whole live row remains the **Open** action; the glyph is not a second button. Do not add an info action until there is useful approved metadata and a real flow for showing it.

When Herdr establishes a valid worktree nest, replace its top-level workspace's ordinary position with one nested section. Show only nonzero actual agent totals under the top-level name, in this order: **working**, **blocked**, **idle**, **done**, **unknown**. Each compact badge retains both its number and status word; never communicate it by color alone. Do not show a workspace-count badge. Counts do not open or filter anything.

When a nest's top-level workspace has exactly one terminal, keep that real terminal row visible as the parent row whether the nest is expanded or collapsed. Give it a separate leading disclosure action and keep its trailing terminal glyph aligned with other terminal rows. Expanding reveals only the worktree rows beneath it. If the top-level workspace has zero or multiple terminals, use a neutral disclosure heading and show all of its ordinary terminal rows when expanded. Never fall back to the first terminal. Accessible terminal names use **Open**, followed by the terminal title and its place. Do not expose internal terminology.

On the first loaded visit, expand nests containing working or blocked agents and collapse the others. After that, the person's manual expand or collapse choice wins for the visit even when statuses change. Show compact **Expand all** when any nest is collapsed and **Collapse all** when all are expanded. Omit both when there are no nests and in **Blocked**.

The persistent attention area shows the working count and no zero-blocked copy. When agents are blocked, **N blocked** opens **Blocked**. This view temporarily shows only blocked terminal rows plus the minimum workspace context. It omits nest disclosure, totals, **Expand all**, **Collapse all**, **New worktree**, and **Actions**. **Show all terminals** returns to Home and restores its prior scroll, focus, and expansion state.

Every terminal **Open** action uses the same human title and place on Home and in its accessible name. Internal IDs do not appear as titles or disambiguation. Hide terminal actions whenever no complete usable Home exists.

## Connection and Home updates

Reserve a stable top-right badge slot. Its labels are exactly:

- **Live**
- **Reconnecting**
- **Offline**
- **Herdr is not running**
- **Cannot use this Herdr**

Valid current data or a liveness heartbeat means **Live**. Reading Home again never changes or animates a healthy badge. Keep the existing transport behavior and 45-second threshold before **Offline**. **Herdr is not running** and **Cannot use this Herdr** suppress **Live** and do not open the reconnect sheet. The stable slot prevents layout shift.

Keep the last complete Home in place until its complete replacement is ready. Do not show a Home refresh banner, insert a temporary row, or move the page. If no complete Home has loaded, replace the list with one short loading or unavailable state. Never show a partial Home.

When a live Home has nothing open, show **No terminals** and “Herdr is running, but nothing is open.” An ordinary terminal prevents this empty state.

## Home actions

The Home tools row includes **New space**. A workspace row keeps its terminal action first and places a vertical three-dot menu immediately after it when actions are available. Do not add empty menus, disabled future controls, or placeholder actions.

## Terminal

Terminal uses the human terminal title and a short connection or input state. Those states describe the terminal, not the agent, and never replace or embellish an agent's Herdr status.

Terminal header, command bar, text composer, and feedback controls use the same paper, charcoal, dividing-rule, and accent palette as Home. The terminal rendering surface keeps its own high-contrast colors. Do not recolor terminal output or derive terminal colors from the surrounding application chrome.

On a phone, use Reader for stable reading, browser selection, text entry, and terminal shortcuts. Do not add persistent control buttons to Reader. On a desktop, use the full terminal renderer and the desktop controls recorded in `terminal-direction.md`.

During a temporary disconnect, an already-open Reader text composer stays open and locally editable, preserving its draft, selection, and focus. **Send** and remote shortcuts are unavailable until recovery. Reconnecting does not reopen or refocus a composer the person closed or unfocused.

The exact phone sending and takeover rules, desktop behavior, and current acceptance record live in `terminal-direction.md`. Do not duplicate them in interface copy.

## Notifications

Home and Terminal each offer **Notifications** settings for that browser or installed app. These settings do not trust a device or create an inbox or unread state. Keep the invitation quiet and request browser permission only after **Turn on notifications**. Do not add notification history, banners, replay, or delivery claims.

Notification text names the relevant Herdr workspace when possible. A status notification opens only its exact current terminal or **Terminal unavailable**; a workspace notification opens Home.

## Workspace management

**New space** opens a short sheet. Its directory field starts with `~`, suggests paths from open top-level repository workspaces, filters suggestions while typing, and accepts the exact entered value. Its label is optional.

**New worktree** appears in the three-dot menu for a known repository workspace and for an ordinary workspace whose current directory Shepherdr can resolve truthfully. Herdr decides whether an ordinary directory is a valid Git source. Its short sheet says:

> This adds a workspace in a new folder.

Branch help is exactly:

> Branch is optional. Leave it blank to let Herdr choose.

A blank branch is omitted so Herdr chooses. Do not show a fabricated preview. Preserve the person's current focus when the request is sent. If Shepherdr cannot confirm the result exactly, show:

> Result unknown. Check Home before starting another worktree.

Do not retry automatically or infer success.

Every visible close action says **Close workspace**. The confirmation names that workspace. If additional linked workspaces will close, say how many. Mention affected agents only when the total is nonzero, and call out exact nonzero working, blocked, and unknown counts when those agents may be interrupted. Do not say group or show a zero-agent sentence.

Offer **Delete checkout** only for a linked-worktree checkout. Never force deletion and never delete its branch. Show the exact freshly validated path as inert text. If Herdr refuses because the checkout has changes, say plainly that it was not deleted and that the person must resolve the changes before trying again.

Creation, close, and deletion change Home only after Herdr confirms the result. If Shepherdr cannot confirm a mutation, say the result is unknown and do not retry automatically.

All interactive targets are at least 44 by 44 CSS pixels. While an action is running, remove its management actions rather than leaving a stale control.

## Mobile behavior

- Design at a narrow phone width first.
- Keep the main actions within thumb reach without covering content.
- Make interactive targets at least 44 by 44 CSS pixels.
- Respect safe areas, browser controls, and device rotation.
- Preserve Home reading, scroll, focus, and expansion state when temporarily filtering or leaving and returning.
- Use wider screens to reveal useful context, not merely stretch the phone layout.

## Honesty and access

- Show only actions and state that exist. No disabled future controls or placeholder chrome.
- Do not add disabled or placeholder Chat controls.
- Never claim success until Herdr or the service confirms it.
- Never invent a more precise status than Herdr provides.
- Make reconnecting, unavailable information, and interrupted actions visible.
- Treat all displayed Herdr content as untrusted text. It must never grant application authority.
- Do not rely on color, motion, or tiny dots to communicate status.
- Support reduced motion, keyboard use, and screen readers.
- Confirm actions that terminate work or remove a folder.

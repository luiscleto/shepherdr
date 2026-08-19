# Shepherdr interface direction

Status: proposed

This is a voice and interface guide, not a component library.

## Voice

Write for a person checking Herdr terminals and agents from a phone, possibly while tired or interrupted.

- Headings state what is happening.
- Body text is short and useful.
- Buttons say what they do.
- Errors say what happened, what is known, and what to try next.
- Security language is quiet and direct, not dramatic.
- Internal names stay off the screen unless the person needs the exact value to act.

Use familiar words: **workspace**, **agent**, **terminal**, **device**, and **notification**.

Use Herdr's status words unchanged: **working**, **blocked**, **idle**, **done**, and **unknown**. Supporting text may say a blocked agent “needs you,” but do not turn that into another status.

Exact paths, IDs, commands, and raw errors belong in details or the terminal when they help the person act.

## Screens

The first screen depends on how Shepherdr was started:

- Without sign-in, open **Home**. Keep a quiet persistent note such as “Sign-in is off.” Do not add a mode picker, warning page, or red banner.
- With passkeys, open **Sign in**. If the device is new, continue to **Trust this device**.

The main screens are:

- **Sign in** — sign in with a passkey.
- **Trust this device** — begin adding a new device. Reaching this screen does not make the device trusted.
- **Home** — see every current terminal under its workspace and tab, with agent status and attention when an agent is present.
- **Terminal** — take full control of the same Herdr work.
- **This device's notifications** — choose blocked-agent notifications.
- **Devices** — view trusted devices and revoke one.

Show **Sign in**, **Trust this device**, and **Devices** only when passkeys are on.

A new device cannot trust itself. It must wait for an already trusted authority or an explicit action on the machine running Herdr. The exact flow comes later.

Do not add screens, tabs, or controls for work that does not exist.

## Visual character

Think of a well-used drafting table made comfortable for a small screen:

- warm off-white or lightly tinted paper surfaces, without making everything beige;
- charcoal text and fine, confident dividing rules;
- almost no shadow and very little corner rounding;
- one restrained accent for focus and the main action;
- status colors used sparingly and never as the only signal;
- typography, spacing, and alignment doing most of the work;
- no gradients, glass panels, glowing controls, hacker decoration, or stacks of generic rounded cards.

The terminal may be a darker, high-contrast working surface inside the same product.

Choose display and body typefaces with a clear character. Use a monospaced face for the terminal and exact technical values only. Final fonts and colors come later.

## Home and attention

Home first answers:

1. Who is working?
2. Who is blocked?
3. What needs my attention?
4. Can I open any current terminal now?

Home lists every real Herdr terminal exactly once in its ordinary all-terminals view. Agent identity and one of Herdr's exact status words are optional terminal metadata. An ordinary terminal has no status badge and does not contribute to attention.

A single-terminal workspace is one full-row destination while remaining a real workspace heading for assistive navigation. Multi-terminal and multi-tab headings appear only when they add place. Omit a lone default tab; with several tabs, mark only Herdr's current tab with **current**. Do not add agent counts, repeated cards, repeated **Open terminal** buttons, or dead headings for workspaces with no terminal. The whole terminal row opens and has a target of at least 44 by 44 CSS pixels; a chevron is decoration, not a collapse control.

Use one human terminal title on Home, the Terminal header, the terminal accessible label, and blocked-only reuse. Use the workspace title for a flattened row; otherwise prefer a distinct Herdr label or terminal title and fall back to **Terminal**. Add neutral ` 1`, ` 2` suffixes only within these collision sets, taken from the complete all-terminals order:

- same-named flattened workspaces across Home; and
- same-titled terminal rows within one workspace and, when a tab heading is shown, that tab.

Blocked filtering reuses titles computed from the complete all-terminals view and never renumbers them. Do not number unrelated rows elsewhere, use opaque IDs or Herdr workspace numbers, or add ordinals to secondary text. Accessible names begin with **Open**, include the same disambiguated title and place, and retain visible secondary text and agent status so screen-reader output does not hide useful information.

The persistent attention area shows the working count. Show no zero-blocked copy. When agents are blocked, **N blocked** opens a **Blocked** view that filters and reuses the same terminal rows with workspace/tab context. It does not duplicate an attention list or invent a “needs you” status. **Show all terminals** restores the all-terminals scroll position and focused row.

When Herdr is live with nothing open, show **No terminals** and “Herdr is running, but nothing is open.” An ordinary terminal prevents this empty state. Offline, reconnecting, “Herdr is not running,” incompatible, and last-known states need useful treatment of their own. One connection panel labels stale rows; do not repeat a last-known badge on every row. Last-known rows are visibly not openable and have no open affordance.

## Moving through the product

Home leads to the selected real terminal. Moving between Home and Terminal must preserve the person's scroll position and focused row.

A blocked-agent notification opens that agent's terminal, not generic Home.

## Terminal

The terminal favors fidelity over decoration. Preserve selection, keyboard input, control keys, scrolling, resize behavior, reconnect state, and text paste. Use only capabilities Herdr already exposes. Opening observes first and never steals control. When control is occupied, keep **Controlled elsewhere · observing**. **Take over** stays explicit and confirms with “Take control? The current controller will lose input.”

## Mobile behavior

- Design at a narrow phone width first.
- Keep the main actions within thumb reach without covering content.
- Make interactive targets at least 44 by 44 CSS pixels.
- Respect safe areas, the on-screen keyboard, browser controls, and device rotation.
- Preserve reading, scroll, and focused-row position between Home and Terminal.
- Use wider screens to reveal useful context, not merely stretch the phone layout.

## Honesty and access

- Show only actions and state that exist. No disabled future controls or placeholder chrome.
- Do not add disabled or placeholder Chat controls.
- Never claim success until Herdr or the service confirms it.
- Never invent a more precise status than Herdr provides.
- Make reconnecting, stale information, and interrupted actions visible.
- Treat Herdr output, names, IDs, agents, terminal content, repository files, attachments, and pasted content as untrusted. Displaying it must never give it Shepherdr application authority. Opaque IDs do not appear as titles or disambiguation.
- Do not rely on color, motion, or tiny dots to communicate status.
- Support reduced motion, keyboard use, and screen readers.
- Confirm actions that terminate work, revoke a device, or reset sign-in.

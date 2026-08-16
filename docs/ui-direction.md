# Shepherdr interface direction

Status: proposed

This is a voice and interface guide, not a component library.

## Voice

Write for a person checking agents from a phone, possibly while tired or interrupted.

- Headings state what is happening.
- Body text is short and useful.
- Buttons say what they do.
- Errors say what happened, what is known, and what to try next.
- Security language is quiet and direct, not dramatic.
- Internal names stay off the screen unless the person needs the exact value to act.

Use familiar words: **workspace**, **agent**, **chat**, **terminal**, **device**, and **notification**.

Use Herdr's status words unchanged: **working**, **blocked**, **idle**, **done**, and **unknown**. Supporting text may say a blocked agent “needs you,” but do not turn that into another status.

Exact paths, IDs, commands, and raw errors belong in details or the terminal when they help the person act.

## Screens

The first screen depends on how Shepherdr was started:

- Without sign-in, open **Home**. Keep a quiet persistent note such as “Sign-in is off.” Do not add a mode picker, warning page, or red banner.
- With passkeys, open **Sign in**. If the device is new, continue to **Trust this device**.

The main screens are:

- **Sign in** — sign in with a passkey.
- **Trust this device** — begin adding a new device. Reaching this screen does not make the device trusted.
- **Home** — see workspaces, agents, status, and attention.
- **Chat** — talk with one agent at a time.
- **Terminal** — take full control of the same Herdr work.
- **This device's notifications** — choose messages and blocked-agent notifications.
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

Chat stays in this light, focused shell. The terminal may be a darker, high-contrast working surface inside the same product.

Choose display and body typefaces with a clear character. Use a monospaced face for the terminal and exact technical values only. Final fonts and colors come later.

## Home and attention

Home first answers:

1. Who is working?
2. Who is blocked?
3. What needs my attention?
4. Can I open chat or the terminal now?

A persistent attention control shows the working count. When any agent is blocked, it clearly calls for attention and opens a direct list or path to those agents.

Empty, offline, and “Herdr is not running” need useful screens of their own. Do not hide them behind a spinner or blank page.

## Moving through the product

Chat is the usual phone view. Terminal is the full-control escape hatch. Moving between home, chat, and terminal must preserve the person's place.

A notification opens the relevant agent, not generic home. Open chat when it can handle the next step. Open the terminal when Herdr does not provide a general chat input for what the blocked agent needs.

## Chat and terminal

Chat should read like a focused exchange, not terminal output placed in speech bubbles. Clearly separate the person's messages, agent messages, sending state, service notices, and attachments.

Chat and terminal show the same Herdr work. Do not suggest that chat starts a second conversation or another agent.

Pasted images in chat need a preview and a remove action before sending. Show clear progress and failure text. Give the image an accessible name.

The terminal favors fidelity over decoration. Preserve selection, keyboard input, control keys, scrolling, resize behavior, reconnect state, and text paste. Image paste belongs there only when the result remains clear and reliable.

## Mobile behavior

- Design at a narrow phone width first.
- Keep the main actions within thumb reach without covering content.
- Make interactive targets at least 44 by 44 CSS pixels.
- Respect safe areas, the on-screen keyboard, browser controls, and device rotation.
- Preserve reading and scroll position between home, chat, and terminal.
- Use wider screens to reveal useful context, not merely stretch the phone layout.

## Honesty and access

- Show only actions and state that exist. No disabled future controls or placeholder chrome.
- Never claim success until Herdr or the service confirms it.
- Never invent a more precise status than Herdr provides.
- Make reconnecting, stale information, and interrupted actions visible.
- Treat Herdr output, agents, terminal content, repository files, attachments, and pasted content as untrusted. Displaying it must never give it Shepherdr application authority.
- Do not rely on color, motion, or tiny dots to communicate status.
- Support reduced motion, keyboard use, and screen readers.
- Confirm actions that terminate work, revoke a device, or reset sign-in.

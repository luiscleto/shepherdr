# Shepherdr interface direction

Status: approved

This is a voice and interface guide, not a component library.

## Voice

Write for a person checking Herdr workspaces, terminals, and agents from a phone, possibly while tired or interrupted.

- Headings state what is happening.
- Body text is short and useful.
- Buttons say what they do.
- Errors say what happened, what is known, and what to try next.
- Security language is quiet and direct.
- Internal names stay off the screen unless the person needs an exact value to act.

Use familiar words: **workspace**, **terminal**, **agent**, **worktree**, **device**, and **notification**. Keep internal words such as source, group, pane, route, and session off the screen. **Linked workspace** may appear only when someone needs to understand the additional work an action affects.

Use Herdr's status words unchanged: **working**, **blocked**, **idle**, **done**, and **unknown**. Supporting text may say a blocked agent “needs you,” but that is not another status. Show paths and raw errors only when they help someone act. Do not show internal IDs.

## Screens and settings

The product has two main screens:

- **Home** shows every current workspace and terminal, with agent status and attention when an agent is present.
- **Terminal** reads one exact current terminal and sends text, shortcuts, and, for a recognized agent, local file paths.

Workspace actions use small sheets from Home. Home and Terminal both open **Settings**, which contains **Notifications** and, in protected mode, **Devices**. Do not add another main screen or placeholder controls.

## Access and devices

When sign-in is off, Home opens directly and quietly keeps **Sign-in is off** visible. There is no mode picker or warning page.

Protected visits start at **Sign in** with **Sign in with a passkey**. They ask for no name, email address, or account. A valid setup or invitation link opens **Trust this device**, asks for a short device name, and creates a passkey. A new browser cannot trust itself.

Every unusable invitation says: **This invitation can't be used. Create a new invitation on the machine running Shepherdr or from a trusted device.** A person who already has a passkey but has lost browser state sees **Sign in again.** Keep these recovery paths distinct.

**Devices** exists only in protected mode. It lists trusted sign-ins without claiming that each row is one physical device. Explain that a passkey may sync and that its copies share one entry and are revoked together. Backup state is **last reported by this passkey** and includes its observation time; it is not live provider or physical-device state.

A trusted browser can choose **Trust another device**, copy or scan a ten-minute invitation, **Revoke** a trusted sign-in while another remains, or **Sign out**. Do not show a disabled final **Revoke**. Clearing all access requires stopping Shepherdr and resetting it on the machine.

## Visual character

Think of a well-used drafting table made comfortable for a small screen:

- warm off-white paper surfaces with a slightly raised workspace surface;
- charcoal text and fine, confident dividing rules;
- shallow shadow only between major surfaces, with flat internal rows and little rounding;
- one restrained accent for focus and the main action;
- sparse status colors that never replace text;
- typography, spacing, and alignment doing most of the work; and
- no gradients, glass panels, glowing controls, hacker decoration, or stacks of generic rounded cards.

Use a strong sans-serif face for the product, workspace, and terminal names. Use monospace only for exact technical values, statuses, and compact counts. The normal masthead says **Shepherdr**; a temporary view such as **Blocked** has its own visible heading.

## Home and attention

Home first answers who is working, who is blocked, what needs attention, and which workspaces and terminals exist. Every real terminal appears exactly once. An agent identity and exact Herdr status are optional information; an ordinary terminal has no status badge.

The client-side field is labeled **Filter workspaces and terminals**. It filters only the last complete Home already in the browser. It does not trigger a read, change global counts, renumber titles, or overwrite expansion choices. When nothing matches, show **No matches** and “Try another filter.”

Keep ordinary Home compact. A terminal row has a strong human title, optional agent identity and status, and a small aligned terminal glyph. The whole current row is **Open**; the glyph is not another button.

When Herdr confirms a worktree nest, show one nested section with only nonzero agent totals in the order **working**, **blocked**, **idle**, **done**, **unknown**. Counts are information, not controls. A single top-level terminal remains its own openable parent row with a separate leading disclosure. Other shapes use a neutral disclosure heading and show every terminal when expanded. Never substitute a representative terminal.

Working or blocked nests start expanded. Later choices win for the visit. Show **Expand all** or **Collapse all** only when useful. The attention area shows the working count and no zero-blocked text. **N blocked** opens **Blocked**; **Show all terminals** restores the prior Home expansion, scroll, and focus.

## Connection and Home updates

Reserve a stable top-right badge with exactly **Live**, **Reconnecting**, **Offline**, **Herdr is not running**, or **Cannot use this Herdr**. Reading Home never changes or animates a healthy badge. Keep the last complete Home until its complete replacement is ready, with no refresh banner or temporary row.

If no complete Home exists, use one short loading or unavailable state in the list's place. When Herdr is live with nothing open, show **No terminals** and “Herdr is running, but nothing is open.”

## Home actions

The tools row includes **New space**. A workspace row keeps its terminal action first and places a vertical three-dot menu immediately after it when current actions exist. Do not add empty menus, disabled future controls, or placeholders.

**New space** starts its directory field with `~`, suggests paths from open top-level repository workspaces, and accepts the exact entered value. The label is optional.

**New worktree** is available for a known repository workspace or an ordinary workspace whose current directory can be resolved. Its sheet says “This adds a workspace in a new folder.” Branch help says “Branch is optional. Leave it blank to let Herdr choose.” Do not fabricate a preview or infer success.

Every close action says **Close workspace**. The confirmation names that workspace and any nonzero additional linked-workspace or agent effects. **Delete checkout** appears only for a linked worktree, never forces deletion, never deletes its branch, and shows the freshly checked path as inert text. If a checkout has changes, say it was not deleted and that the changes must be resolved.

Creation, close, and deletion change Home only after Herdr confirms them. An unconfirmed mutation says the result is unknown and is not retried automatically.

## Terminal

Terminal uses the human terminal title and a short connection or input state. These states describe the terminal, not the agent. The surrounding controls use the Home palette; the terminal surface keeps its own high-contrast colors.

On a phone, the accepted Reader is the stable reading surface. It supports browser selection, older available output, continued live output, **Message**, and horizontally scrollable shortcuts. **Message** opens the text composer; there are no persistent control buttons. During a temporary disconnect, an open composer keeps its draft, selection, pending files, and focus, but send and remote shortcuts are unavailable.

The composer shows **Add files** only while the exact terminal has a Herdr-recognized agent. Its compact paperclip action opens **Photos** and **Files** choices. The platform decides the actual picker sources, multi-selection, and focus return; Shepherdr does not force camera capture or promise that every source exists.

While selected files are read, show **Preparing files…** and keep send unavailable. Pending attachments are compact cards in a contained horizontal scrolling strip. Each card has a recognizable filename and size, an optional browser-local image thumbnail, and a compact remove icon with an accessible **Remove _filename_** label. The add, send, close, and remove controls may be icon-only, but they keep clear accessible names and 44-by-44 targets.

Picker cancel, temporary disconnect, definite not-sent results, and unknown results preserve text and pending files. Files can be sent without text. A confirmed send clears the submitted draft. An unknown result tells the person to check the terminal before deliberately sending again. There is no automatic retry.

If another controller is present, offer only the confirmed **Take over and send** path. Desktop Terminal uses the full renderer and keeps interactive control until **Release**.

File language stays local and truthful: Shepherdr sends local filesystem paths through Terminal. Do not say files were attached to a provider, delivered to a model, or read by an agent.

## Notifications

**Notifications** settings apply only to this browser or installed app. Opening settings does not request permission. A quiet invitation offers **Turn on notifications** and **Not now**; only the enable action asks the browser.

Settings can turn notifications on or off and select agent-status, workspace, and trusted-sign-in events. A status notification opens only its exact current terminal or **Terminal unavailable**. Workspace notices open Home. Do not add history, unread state, replay, banners, or delivery claims.

## Mobile behavior and honesty

- Design for a narrow phone first and keep main actions within thumb reach without covering content.
- Make interactive targets at least 44 by 44 CSS pixels.
- Respect safe areas, browser controls, rotation, reduced motion, keyboard use, and screen readers.
- Preserve Home reading, scroll, focus, and expansion state when filtering or leaving and returning.
- Use wider screens to reveal useful context, not merely stretch the phone layout.
- Show only actions and state that exist.
- Never claim success until Herdr or the responsible service confirms it.
- Make reconnecting, unavailable information, and interrupted actions visible.
- Treat all displayed Herdr and user content as untrusted text; it cannot grant authority.
- Do not rely on color, motion, or tiny dots to communicate status.
- Confirm actions that terminate work or remove a folder.

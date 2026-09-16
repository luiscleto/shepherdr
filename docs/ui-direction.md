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

Use familiar words: **workspace**, **terminal**, **agent**, **worktree**, **group**, **device**, and **notification**. Keep internal words such as source, pane, route, and session off the screen. **Linked workspace** may appear only when someone needs to understand the additional work an action affects.

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

The filter is labeled **Filter workspaces and terminals**. It uses only the names already shown on Home. It does not change global counts, renumber titles, or overwrite expansion choices. When nothing matches, show **No matches** and “Try another filter.”

Keep ordinary Home compact. A terminal row has a strong human title, optional agent identity and status, and a small aligned terminal glyph. The whole current row is **Open**; the glyph is not another button.

When Herdr confirms a worktree relationship, show one group with only nonzero agent totals in the order **working**, **blocked**, **idle**, **done**, **unknown**. Counts are information, not controls. A single top-level terminal remains its own openable parent row with a separate leading disclosure. Other shapes use a neutral group heading and show every terminal when expanded. Never substitute a representative terminal.

Groups with working or blocked agents start expanded. Later choices win for the visit. Show **Expand all** or **Collapse all** only when useful. The attention area shows the working count and no zero-blocked text. **N blocked** opens **Blocked**; **Show all terminals** restores the prior Home expansion, scroll, and focus.

Keep one row per workspace on Home. One terminal opens directly. When a workspace has several terminals, the same terminal control carries a small count badge and opens a terminal picker: a bottom sheet on a phone and a compact dialog on a larger screen. List every terminal there with its status, open action, and three-dot menu. Do not add another inline expand/collapse level for terminals.

## Connection and Home updates

Reserve a stable top-right badge with exactly **Live**, **Reconnecting**, **Offline**, **Herdr is not running**, or **Cannot use this Herdr**. Reading Home never changes or animates a healthy badge. Keep the last complete Home until its complete replacement is ready, with no refresh banner or temporary row.

If no complete Home exists, use one short loading or unavailable state in the list's place. When Herdr is live with nothing open, show **No terminals** and “Herdr is running, but nothing is open.”

## Home actions

The tools row includes **New space**. A workspace row keeps its terminal action first and places a vertical three-dot menu immediately after it when current actions exist. Do not add empty menus, disabled future controls, or placeholders.

**New space** starts its directory field with `~`, suggests paths from open top-level repository workspaces, and accepts the exact entered value. The label is optional.

**New worktree** is available for a known repository workspace or an ordinary workspace whose current directory can be resolved. Its sheet says “This adds a workspace in a new folder.” Branch help says “Branch is optional. Leave it blank to let Herdr choose.” Do not fabricate a preview or infer success.

Every close action says **Close workspace**. The confirmation names that workspace and any nonzero additional linked-workspace or agent effects. **Delete checkout** appears only for a linked worktree, never forces deletion, never deletes its branch, and shows the freshly checked path as inert text. If a checkout has changes, say it was not deleted and that the changes must be resolved.

Creation, close, and deletion change Home only after Herdr confirms them. If Shepherdr cannot confirm what happened, it says the result is unknown and does not retry automatically.

## Terminal

Terminal uses the human terminal title and a short connection or input state. These states describe the terminal, not the agent. The surrounding controls use the Home palette; the terminal surface keeps its own high-contrast colors.

On a phone, Terminal opens in the last Reader or Terminal view chosen in this browser, defaulting to Reader: a stable reading surface with browser selection, older available output, continued live output, **Message**, and horizontally scrollable shortcuts. After a deliberate upward history attempt near the top and a successful history read with no additional output, Reader may show “More history may be available in Terminal.” in a dismissible paper-colored banner below the header, with **Open Terminal** and an accessible close button. An attempt also checks older output when Reader has no native overflow or scroll event; an earlier successful unchanged history read can supply the evidence. Initial output, live updates, downward movement, errors, and the read-depth limit alone never show it. The banner overlays the reading area without changing terminal dimensions or covering the composer. Dismissal lasts for the current terminal visit, and the banner stays hidden in full Terminal. **Open Terminal** uses the existing view toggle and remembers that choice. Beside Settings, an icon-only terminal button opens the full terminal and an icon-only phone button returns to Reader. Keep accessible names **Full terminal** and **Reader** and 44-by-44 targets. Use narrow side gutters while respecting safe areas. Full Terminal uses native keyboard input through its real input area, focused only by an explicit tap.

Both views share retained control of the same terminal. Switching preserves Reader's place, selection, draft, and pending files; history navigation remains independent in each view. Fit the terminal to the actual available content area, including keyboard and composer changes. Never resize another controller's terminal while observing.

**Message** opens Reader's text composer. Keep the applicable **Control**, **Take over**, or **Release** action compact within terminal actions, without a persistent ownership button row. Opening attempts ordinary control without focus; an occupied terminal stays under its existing controller until confirmed takeover. Release leaves observation until explicit Control. Switching views and sending retain control. During a temporary disconnect, an open composer keeps its draft, selection, pending files, and focus, but send and remote shortcuts are unavailable.

### Send keys

Human-approved UX: **Send keys** is a text-only bottom-bar button, with no icon, immediately before **Release** when Release is shown. Preserve the existing lifecycle-dependent **Control**, confirmed **Take over**, and **Release** behavior. **Message** (or the existing **Attach files** action in full Terminal), **Enter**, **Send keys**, and actions that cannot sensibly be represented by a key picker remain fixed and non-removable, with their existing availability rules.

**Send keys** opens a paper bottom sheet with **Ctrl**, **Alt / Option**, and **Shift** toggles and **Common**, **F keys**, and **Character** choices. Select one base key plus modifiers, show a readable preview, and send only through an explicit send action. **Tab** and **Backspace** have explicit picker keys; neither depends on the mobile keyboard. Preserve access to all existing shortcuts, including Esc, all four arrows, and Ctrl C/D/Z. Alt / Option means terminal Alt; it does not promise universal macOS Option behavior or arbitrary operating-system keys.

Keys act on the open terminal's current state, independently of any unsent Reader message or pending files. Sending a key never submits or clears that draft. Keep the existing controller; while observing, use **Control** or confirmed **Take over** first, then deliberately send the key. The key action itself never takes over. Let the terminal determine the effect of a combination, including ordinary aliases.

Include every confirmed public Herdr key capability, beyond the examples in the study: supported named keys, function-key range, printable characters and modifiers, including Super/Command and Hyper where their encodings are confirmed. Use the existing categories and minimal layout adjustments. Parser acceptance alone is not support; do not show keys that have no actual key encoding. Natural aliases remain usable, and the terminal application determines their effect.

A person can pin a shortcut without sending it, then edit, remove, or reorder optional saved shortcuts in this browser, or **Restore defaults**. Start with the study's small optional set, **Esc** and **Tab**; other existing keys remain reachable through the picker without all starting pinned. There is no arbitrary shortcut-count cap. Selecting a key, changing modifiers, pinning, and editing never send input. Modifiers belong to the selected combination, not to ordinary typing or other actions. Keep Message, files, native keyboard input, and the rest of Terminal intact. Do not add macros, profiles, global sticky modifiers, or provider-specific behavior. The [approved Send keys brief](send-keys-plan.md) records the Herdr encoding route, server safeguards, capability evidence and human-accepted limitations.

### Message and files

The composer shows **Add files** only while the exact terminal has a Herdr-recognized agent. Its compact paperclip action opens **Photos** and **Files** choices. The platform decides the actual picker sources, multi-selection, and focus return; Shepherdr does not force camera capture or promise that every source exists.

While selected files are read, show **Preparing files…** and keep send unavailable. Pending attachments are compact cards in a contained horizontal scrolling strip. Each card has a recognizable filename and size, an optional browser-local image thumbnail, and a compact remove icon with an accessible **Remove _filename_** label. The add, send, close, and remove controls may be icon-only, but they keep clear accessible names and 44-by-44 targets.

Full Terminal replaces **Message** with an accessible **Attach files** paperclip. Its compact files-only panel contains selection, preparation feedback, a horizontal file strip, cancel, and explicit **Insert files**. It has no message textarea. Insertion adds file references at the application's current cursor without Enter; the person submits them separately. Cancel inserts nothing and preserves pending files and the separate Reader draft. Reader's **Send** still submits text, files, or both.

Picker cancel, temporary disconnect, definite not-sent results, and unknown results preserve text and pending files. A confirmed Reader send clears the submitted draft; full-terminal file insertion leaves that draft alone. An unknown result tells the person to check the terminal before deliberately sending again. There is no automatic retry.

If another controller is present, takeover requires confirmation that the current controller will lose input. Reader's **Take over and send** submits only the action explicitly confirmed. Losing control leaves observation without fighting back or replaying input. Explicit paste inserts text once without appending Enter. Desktop Terminal uses the full renderer and keeps interactive control until **Release**.

File language stays local and truthful: Shepherdr sends local filesystem paths through Terminal. Do not say the files were delivered to a model or read by an agent.

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

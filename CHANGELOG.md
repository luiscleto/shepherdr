# Changelog

All notable changes to Shepherdr are recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.6.0] - 2026-11-03

### Added

- In Herdr Core, an agent that finished shows **done** until you look at it, in the Herdr app or on a Shepherdr terminal page you have open, then **idle**, as it does in the app. Notifications are unchanged.
- In Herdr Core, pins are shared with the Herdr app: a pinned agent's row shows a pin, its menu offers **Pin** or **Unpin**, and **N pinned** opens a **Pinned** view with only those agents.

### Changed

- An agent's notification leads with the name you gave its tab or terminal, such as "Review PR17 CI is done", and its text is the space or workspace name alone. Without a given name it still says "Agent is done".

### Fixed

- The full terminal gives characters the same widths as Herdr Core 0.1.17 and the Herdr TUI, so emoji such as ✍️, ❤️, 👍, 🇵🇹 and 👩‍💻 take two columns and no longer cover the next character or push a full-screen program's layout a column off.

### Compatibility

- Support Herdr Core 0.1.17 and the Herdr app 0.1.12.

## [0.5.2] - 2026-11-02

### Changed

- In Herdr Core, a space with several tabs lists them as rows under the space, which opens and closes like a worktree group, instead of one row with a picker of all its terminals. A tab with one terminal opens it from its row; a split tab opens the picker for its own terminals.

## [0.5.1] - 2026-11-01

### Added

- Shepherdr also follows herdr-core, the runtime behind the Herdr desktop app, whenever it runs. When the Herdr TUI and Herdr Core are both running, a switch on Home picks which one to show, and each browser remembers its choice. For Herdr Core, Home lists the app's spaces and tabs with the app's names, and terminals that no space holds appear once under **Other terminals**. Home keeps working while the app window is closed.
- Notifications come from both the Herdr TUI and Herdr Core, whatever Home shows, and an agent's notification opens that Herdr's terminal.
- **New space** suggests the repository folders open in Herdr TUI and Herdr Core, whichever one Home shows.
- With the Herdr app, an agent's **error** status shows on Home, **N error** opens an **Error** view, and a new **error** notification choice starts on.
- Herdr Core terminals open from Home, with Reader, the full terminal, **Message**, **Send keys** and files. They open in control, so you can type at once, and take your screen's size. While you control a terminal and the page is in view, Shepherdr takes your screen's size back when the app window takes it, such as when the app window is focused or a program in it replies. If the app window takes the size again before you next use the page, it keeps it until then. **Release** goes back to observing and gives the size to the app window, as leaving the terminal does; out of view, the app window can take the size when it's next used. **Control** takes control again.
- With the Herdr app, **Close terminal** and **Close space** work from Home, whether or not the app window is open. The confirmation counts the agents that may be interrupted, including any reporting **error**, and the terminals a closing terminal started that will close with it. Closing a space's last terminal closes the space. Terminals under **Other terminals** can be closed one at a time.
- While the Herdr app window is open, **New space**, **New terminal** and **Rename space** work from Home too. The app window shows the new space, and moves to a new terminal when its tab is showing. If the window is closed, Shepherdr says so and sends nothing. If it closes or stops answering after the action is sent, Shepherdr says the result is unknown.
- While the Herdr app window is closed, **New space** and **New terminal** still work, with a warning: Shepherdr starts them in Herdr Core, and the app adds them when it next opens. Home shows them at once where the app will put them. New space has no label then, and the app names the space after its folder; New terminal starts in the terminal's current folder and opens, and the app adds it at the end of the tab. **Rename space** still needs the window open.
- `-herdr-core-socket` names herdr-core's socket when it isn't at `$XDG_RUNTIME_DIR/herdr-core/default.sock`. Without either, Shepherdr follows only the Herdr TUI. With another socket, the Herdr app actions are off.

### Changed

- `herdr` on `PATH` is needed only to open Herdr TUI terminals; Shepherdr starts without it.
- The masthead names the Herdr it shows: **Herdr TUI 0.9.5** or **Herdr Core 0.1.16**.

### Fixed

- Leaving a terminal page for another page no longer keeps control of the terminal, or its size, while the browser holds the page for **Back**. Going back opens the terminal again as usual. As with any other leave, unsent text and files in **Message** are not kept.
- **Delete checkout** no longer says "and 0 agents" when a checkout has no agents.
- When nothing is open, **No terminals** now shows below the filter, where the list goes.

### Compatibility

- Support herdr-core 0.1.15–0.1.16 with the Herdr app 0.1.10–0.1.11. Newer versions run best effort with a warning. For Herdr Core there is no **Take over**, and **Send keys** offers only keys it can send. **New space**, **New terminal** and **Rename space** need `herdr-app` on `PATH`, and **Rename space** needs the app window open; there are no worktree actions and no terminal rename.
- Herdr TUI support is unchanged: 0.9.0–0.9.5, with 0.9.0 the minimum.

## [0.4.3] - 2026-10-16

### Changed

- Links in the full terminal ask before opening. The sheet shows the full address, and **Open** uses a new tab. Only web addresses open.
- **Send keys** no longer offers Hyper, which recent Herdr versions do not send. Saved shortcuts that use Hyper are removed.
- **Send keys** explains that Super / Command reaches only apps that turn on enhanced keyboard input.

### Compatibility

- Support Herdr 0.9.0–0.9.5, keeping the 0.9.0 minimum. Newer or unknown versions still run best effort with a warning.
- Keep Split, Rename, and Close available on every supported Herdr version.

## [0.4.2] - 2026-09-17

### Compatibility

- Support Herdr 0.9.0–0.9.1, retaining the 0.9.0 minimum. Newer or unknown versions still run best effort with a warning.
- Keep Split, Rename, and Close available on both validated Herdr versions.

## [0.4.1] - 2026-09-16

### Added

- **Send keys** for common keys, F1–F12, and character/modifier combinations. Save and reorder your own shortcuts in this browser.
- Reader offers a dismissible **Open Terminal** hint when no more history is available. This helps with Grok and other agents that require scrolling inside the terminal app to reach older output.

### Changed

- Remember your last Reader or Terminal view in this browser.
- Update the default bar with Esc, Enter, Up, Down, Backspace, and Ctrl+C. Saved shortcuts stay unchanged.
- **Send keys** closes after a successful send and leaves message drafts and attachments untouched.

### Compatibility

- Requires Herdr 0.9.0 or later (0.9.0 validated).

## [0.3.1] - 2026-09-13

### Changed

- Bundle IBM Plex Mono regular and bold for consistent text, tables, and separators in Reader and the full terminal.

### Fixed

- Wait for fonts to load before measuring the terminal for the first time.

## [0.3.0] - 2026-09-13

### Added

- Switch from Reader to a full terminal on your phone for direct typing, then return without losing your reading place, message draft, or selected files.
- Insert file paths at the full terminal's cursor from a compact files-only panel, without pressing Enter. Reader's **Send** still submits messages and files together.

### Changed

- Reader and the full terminal retain the same control and use the terminal's actual available size, without taking control from someone else automatically.
- Smaller side gutters leave more room for terminal output. Terminal and phone icons beside Settings switch views.

### Fixed

- Explicit paste preserves multiline text and inserts it once without adding Enter.

### Compatibility

- **Breaking:** Herdr 0.9.0 is now the minimum. Update Herdr before upgrading Shepherdr. Herdr 0.9.0 is validated; newer or unknown versions run best effort with a warning.

## [0.2.2] - 2026-09-09

### Fixed

- Use Herdr's default socket path on macOS instead of Application Support.

## [0.2.1] - 2026-09-08

### Compatibility

- Support Herdr 0.9.0 while retaining support for 0.8.0–0.8.2.

### Fixed

- Fix closing a workspace group on Herdr 0.9.0 after confirmation.

## [0.2.0] - 2026-09-02

### Added

- Split, rename, or close terminals from Home. When a workspace has several terminals, a terminal picker lists every one.
- On a phone, terminal actions stay on screen even when a workspace has many terminals.

### Changed

- Routine terminal connection and notification details now appear in the log only when debug logging is enabled.

### Fixed

- Recovered Home after returning from the background so it no longer reconnects repeatedly.
- Restored wheel and trackpad scrolling in the desktop terminal.
- Improved terminal line layout on phones so output fits the screen, without taking over a terminal someone else is using.

## [0.1.1]

### Fixed

- Corrected release validation portability for macOS and Linux ARM64 candidates.
- Corrected draft-release lookup so API failures cannot be mistaken for a missing draft.

## [0.1.0]

### Added

- See Herdr workspaces, worktrees, terminals, agents, and attention from a phone.
- Read terminals, send input, and send local file paths to recognized agents.
- Create and close workspaces, create worktrees, and delete clean linked checkouts.
- Protect access with passkeys or deliberately run with sign-in off, manage trusted sign-ins, and choose notifications for each browser.
- Downloadable builds for supported Linux and macOS computers; the release page lists the available systems.

### Security

- Shepherdr listens on loopback and is intended only for a trusted private network; passkeys add protection but do not make public exposure supported.
- Shepherdr, Herdr, and Herdr agents must run as the same operating-system account so the owner-only local socket and staged files remain usable.

### Compatibility

- Works with Herdr 0.8.0 through 0.8.2. Older versions do not start; newer versions start with a warning that they have not been tested.
- macOS downloads are unsigned and not notarized, so macOS may show a system warning before the first run.

[Unreleased]: https://github.com/luiscleto/shepherdr/compare/v0.6.0...HEAD
[0.6.0]: https://github.com/luiscleto/shepherdr/compare/v0.5.2...v0.6.0
[0.5.2]: https://github.com/luiscleto/shepherdr/compare/v0.5.1...v0.5.2
[0.5.1]: https://github.com/luiscleto/shepherdr/compare/v0.4.3...v0.5.1
[0.4.3]: https://github.com/luiscleto/shepherdr/compare/v0.4.2...v0.4.3
[0.4.2]: https://github.com/luiscleto/shepherdr/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/luiscleto/shepherdr/compare/v0.3.1...v0.4.1
[0.3.1]: https://github.com/luiscleto/shepherdr/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/luiscleto/shepherdr/compare/v0.2.2...v0.3.0
[0.2.2]: https://github.com/luiscleto/shepherdr/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/luiscleto/shepherdr/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/luiscleto/shepherdr/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/luiscleto/shepherdr/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/luiscleto/shepherdr/releases/tag/v0.1.0

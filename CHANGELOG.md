# Changelog

All notable changes to Shepherdr are recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Send keys** offers common keys, F1–F12, and printable characters with Ctrl, Alt / Option, Shift, Super / Command, and Hyper. Pin, edit, remove, and reorder shortcuts in this browser.
- Reader offers **Open Terminal** when you try to scroll further back after a history read returns no additional output.

### Changed

- Order the default terminal bar as Message, Send keys, Release, Esc, Enter, Up, Down, Backspace, Ctrl+C. Fixed Enter follows the first optional shortcut. Existing saved choices stay unchanged; Restore defaults applies the new list.
- Terminal shortcuts and fixed Enter use Herdr's key encoding while retaining control. Keys leave unsent Reader messages and files alone; observing requires explicit Control or Take over first.
- Remember your last Reader or Terminal view choice in this browser for subsequent terminal opens.

### Fixed

- Keep bottom-bar separators consistent between fixed actions and saved shortcuts.
- Close Send keys only after Herdr confirms the send, without sticky success text. Failed and unknown sends keep the sheet open with their notice and recovery.
- Detect upward Reader history attempts even when the output fits without scrolling. Show the history hint as a dismissible banner below the header without moving the reading area.

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

[Unreleased]: https://github.com/luiscleto/shepherdr/compare/v0.3.1...HEAD
[0.3.1]: https://github.com/luiscleto/shepherdr/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/luiscleto/shepherdr/compare/v0.2.2...v0.3.0
[0.2.2]: https://github.com/luiscleto/shepherdr/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/luiscleto/shepherdr/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/luiscleto/shepherdr/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/luiscleto/shepherdr/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/luiscleto/shepherdr/releases/tag/v0.1.0

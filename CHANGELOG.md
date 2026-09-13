# Changelog

All notable changes to Shepherdr are recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

- **TRIAL, isolated branch only:** mobile Reader and full Terminal share retained control and require Herdr 0.9.0. Explicit paste forwards one protected envelope without adding Enter. Validation remains incomplete; this is not an accepted or released product change.
- **TRIAL:** recover mobile content width, use terminal/phone view icons, and give full Terminal a compact files-only insertion panel. Human acceptance of this correction is pending.

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

[Unreleased]: https://github.com/luiscleto/shepherdr/compare/v0.2.2...HEAD
[0.2.2]: https://github.com/luiscleto/shepherdr/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/luiscleto/shepherdr/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/luiscleto/shepherdr/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/luiscleto/shepherdr/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/luiscleto/shepherdr/releases/tag/v0.1.0

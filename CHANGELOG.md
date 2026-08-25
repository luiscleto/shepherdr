# Changelog

All notable changes to Shepherdr are recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/luiscleto/shepherdr/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/luiscleto/shepherdr/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/luiscleto/shepherdr/releases/tag/v0.1.0

# Changelog

All notable changes to Shepherdr are recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0]

### Added

- A mobile-first Home view of Herdr workspaces, worktrees, terminals, agents, and attention state.
- Exact terminal reading and input, including temporary local file staging for recognized agents.
- Workspace creation, worktree creation, workspace close, and clean linked-checkout deletion through Herdr.
- Passkey-protected access, explicit sign-in-off operation, trusted sign-in management, and per-browser notifications.
- Linux amd64, Linux arm64, macOS amd64, and macOS arm64 release candidates gated on their native systems.

### Security

- Shepherdr listens on loopback and is intended only for a trusted private network; passkeys add protection but do not make public exposure supported.
- Shepherdr, Herdr, and Herdr agents must run as the same operating-system account so the owner-only local socket and staged files remain usable.

### Compatibility

- Supports stable Herdr 0.8.0 through 0.8.2. Newer versions continue best-effort with a warning, while older versions are refused.
- Release binaries are unsigned and are not notarized.

[Unreleased]: https://github.com/luiscleto/shepherdr/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/luiscleto/shepherdr/releases/tag/v0.1.0

# Release strategy

Status: approved

## Release shape

Historical first-release decision: the immutable `v0.1.0` candidate remained unpublished; the compatible fix-forward `v0.1.1` became the first published release. The original [release setup wave](wave-release-setup.md) records that work, not the version for later releases.

Current preparation, authorized 2026-11-01 by the human ("can we ship a new version now"): `v0.5.0`, a minor release adding Herdr Core and Herdr app support. Shepherdr also follows herdr-core whenever it runs, with a Home switch between Herdr TUI and Herdr Core, the app's spaces and tabs, Herdr Core terminals with Reader, the full terminal, Message, Send keys and files, an agent's error status and its notifications, and Close terminal, Close space, New space, New terminal and Rename space through the Herdr app. `herdr` on `PATH` is needed only to open Herdr TUI terminals. The Herdr TUI range is unchanged: 0.9.0 remains the minimum and 0.9.0–0.9.5 are validated. herdr-core 0.1.15–0.1.16 with the Herdr app 0.1.10–0.1.11 are validated, and Herdr Core and the app are optional; newer or unknown versions run best effort with a warning. The notes cover these changes since `v0.4.3`. Existing tags remain immutable, including the unpublished `v0.4.0` tag.

Downloadable GitHub Release archives are the primary installation path. A plain, manually dispatched GitHub Actions workflow builds candidates from an existing approved tag and exact commit, attaches only candidates that pass their gates to a draft release, and stops. It never creates a tag or publishes a release; a human reviews and publishes the draft. A rerun for the same tag updates that tag's existing draft, replacing its notes and assets, and never creates a second one. GitHub's by-tag release lookup returns published releases only, so the workflow finds the draft in the repository's release list instead. It refuses to touch a published release, stops if it finds more than one draft for the tag so a human can delete the extras, and never updates drafts for one tag from two runs at once. It replaces the draft's assets by that release's ID, not by its tag, and checks that the release is still a draft before each asset change.

Candidate targets are Linux amd64, Linux arm64, macOS amd64, and macOS arm64. Each binary must be built and exercised on a native GitHub-hosted runner with `CGO_ENABLED=0`. A target is omitted if its native build, tests, packaged-binary checks, or real Herdr TUI or herdr-core smoke check fails. Cross-compilation alone is not support evidence. Windows is later work: Herdr supports Windows, but Shepherdr's current Unix-specific storage and locking primitives do not compile there.

Archives use predictable names such as `shepherdr_0.5.0_linux_amd64.tar.gz`. Each contains one top-level directory with the executable, `README.md`, `CHANGELOG.md`, and `LICENSE`; the release also provides a SHA-256 checksum file.

The current workflow allows only an existing `v0.5.0` tag and verifies that it, the checkout, and `origin/master` all identify the exact full `commit_sha` input. Native smoke checks start one packaged Shepherdr against both a matching Herdr 0.9.5 CLI/server and herdr-core 0.1.16, each isolated under the runner's temporary folder, and require Home to report the Herdr TUI side Live and the Herdr Core side Live at version 0.1.16. Herdr comes from its GitHub release. herdr-core publishes no GitHub releases, so it comes from the URL and SHA-256 in its 0.1.16 release manifest at `releases.herdr.dev`. Both downloads have their SHA-256 pinned for each target. herdr-core 0.1.16 has builds for all four targets; a target without a pinned herdr-core build fails its smoke check rather than skipping it. The changes are independently reviewed and merged, and validated with Herdr 0.9.0–0.9.5, herdr-core 0.1.15–0.1.16 and the Herdr app 0.1.10–0.1.11. This preparation starts from exact commit `883e0f3d573e269ada8b222724bbc3307f7d0ad7` in the native Herdr `chore-release-0-5-0` worktree. It leaves product code, browser assets, tests, and `web/dist` unchanged. An independent reviewer must review the exact preparation commit. The orchestrator then handles integration, push, tag, and manual workflow dispatch, and assigns validation of the draft artifacts. The human reviews the release changelog before publication. Preparation alone does not authorize publication.

After the approved result is on `master` and the existing `v0.5.0` tag points to that same full commit, invoke:

```sh
gh workflow run release.yml --repo luiscleto/shepherdr --ref master \
  -f tag=v0.5.0 -f commit_sha="<full-approved-master-SHA>"
```

Replace the SHA placeholder with the exact approved integrated commit. The workflow stops at a draft; artifact validation and human changelog review precede publication.

## Browser assets and installation

The Go executable embeds `web/dist`, which is about 1.4 MiB and contains the built application, terminal bundle, WASM, and static assets. Production builds must run `npm ci` and the web build before `go build`. GitHub's generated source archives and `go install` cannot run npm as part of the Go module build.

To make `go install` an optional convenience, the deterministic `web/dist` output is tracked in ordinary source and release tags, marked as generated, and verified by CI against a clean rebuild. This is a narrow exception to `AGENTS.md`; it does not permit other generated output. Release-only asset commits and a separate asset module are unnecessary. With the repository module corrected to its canonical identity, installation is:

```sh
go install github.com/luiscleto/shepherdr@latest
```

The README recommends release archives, explains checksum verification and replacement upgrades, and retains a source-build fallback. It does not offer a pipe-to-shell installer, container, package-manager repository, updater, or automatic migration. Shepherdr and Herdr must run as the same operating-system account so Shepherdr can use Herdr's local socket and Herdr agents can read uploaded files.

## Versions and changes

`shepherdr --version` reports the release version injected with linker flags, a Go module version for `go install`, or `devel` for an unversioned local build. Release builds do not rewrite source files.

`CHANGELOG.md` follows Keep a Changelog with an `[Unreleased]` section and curated version entries. User-visible behavior, compatibility, security, configuration, installation, and upgrade changes require entries; internal refactors, tests, and documentation corrections may omit them when they do not change operator behavior. Release notes start from the changelog entry and add only release-specific platform or validation details. Raw conventional-commit dumps are not release notes, and the workflow is not a second changelog.

Before 1.0, breaking changes increment the minor version and carry explicit upgrade or migration notes; compatible fixes increment the patch version. Tags are never moved or reused. A bad release is marked as unsuitable and replaced by a new version after the problem is fixed.

## Deliberate limits

There is no automatic tagging or publishing, signing or notarization system, installer, package ecosystem, container release, or self-updater. Those are reconsidered only after a demonstrated need. macOS candidates are therefore unsigned, and their ordinary download-and-run behavior must be checked before publication.

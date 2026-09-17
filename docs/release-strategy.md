# Release strategy

Status: approved

## Release shape

Historical first-release decision: the immutable `v0.1.0` candidate remained unpublished; the compatible fix-forward `v0.1.1` became the first published release. The original [release setup wave](wave-release-setup.md) records that work, not the version for later releases.

Current preparation, authorized 2026-09-17: `v0.4.2`, a compatibility patch supporting Herdr 0.9.1 while retaining Herdr 0.9.0 and Split, Rename, and Close on both validated versions. Herdr 0.9.0 remains the minimum; newer or unknown versions run best effort with a warning. The notes cover this compatibility change since `v0.4.1`. Existing tags remain immutable, including the unpublished `v0.4.0` tag.

Downloadable GitHub Release archives are the primary installation path. A plain, manually dispatched GitHub Actions workflow builds candidates from an existing approved tag and exact commit, attaches only candidates that pass their gates to a draft release, and stops. It never creates a tag or publishes a release; a human reviews and publishes the draft.

Candidate targets are Linux amd64, Linux arm64, macOS amd64, and macOS arm64. Each binary must be built and exercised on a native GitHub-hosted runner with `CGO_ENABLED=0`. A target is omitted if its native build, tests, packaged-binary checks, or real Herdr smoke check fails. Cross-compilation alone is not support evidence. Windows is later work: Herdr supports Windows, but Shepherdr's current Unix-specific storage and locking primitives do not compile there.

Archives use predictable names such as `shepherdr_0.4.2_linux_amd64.tar.gz`. Each contains one top-level directory with the executable, `README.md`, `CHANGELOG.md`, and `LICENSE`; the release also provides a SHA-256 checksum file.

The current workflow allows only an existing `v0.4.2` tag and verifies that it, the checkout, and `origin/master` all identify the exact full `commit_sha` input. Native smoke checks use matching Herdr 0.9.1 CLI/server, with the download SHA-256 pinned for each target. The compatibility fix is independently approved and merged, with human real-phone acceptance on running Herdr 0.9.1. This preparation starts from exact commit `b1601ff3dd6757ebbadf67c316fe76b4e4cdc9ec` in the native Herdr `release-v0-4-2` worktree. It leaves product code, browser assets, and the macOS short-socket test-fixture fix unchanged. An independent reviewer must review the exact preparation commit. The orchestrator then handles integration, push, tag, and manual workflow dispatch, and assigns validation of the draft artifacts. The human reviews the release changelog before publication. Preparation alone does not authorize publication.

After the approved result is on `master` and the existing `v0.4.2` tag points to that same full commit, invoke:

```sh
gh workflow run release.yml --repo luiscleto/shepherdr --ref master \
  -f tag=v0.4.2 -f commit_sha="<full-approved-master-SHA>"
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

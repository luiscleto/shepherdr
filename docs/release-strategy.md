# Release strategy

Status: approved

## Release shape

Historical first-release decision: the immutable `v0.1.0` candidate remained unpublished; the compatible fix-forward `v0.1.1` became the first published release. The original [release setup wave](wave-release-setup.md) records that work, not the version for later releases.

Current preparation, authorized 2026-11-08 by the human ("can release a patch"): `v0.7.2`, a patch release. It adds support for herdr-core 0.1.19 and the Herdr app 0.1.14, so neither logs the best-effort warning, and the native smoke check now pins herdr-core 0.1.19. The Herdr TUI range is unchanged: 0.9.0 remains the minimum and 0.9.0–0.9.5 are validated. herdr-core 0.1.15–0.1.19 with the Herdr app 0.1.10–0.1.14 are validated, and Herdr Core and the app are optional; newer or unknown versions run best effort with a warning. The notes cover these changes since `v0.7.1`. Existing tags remain immutable, including the unpublished `v0.4.0` tag. The `v0.5.0` tag remains immutable and unpublished after a macOS test failure; `v0.5.1` replaced it as the release adding Herdr Core and Herdr app support, and `v0.5.2` listed a Herdr Core space's tabs as rows under the space. The `v0.6.0` tag remains immutable and unpublished: its workflow hung on a test-only browser test issue under Node 20 and was cancelled. `v0.6.1` replaced it and was published 2026-11-03, adding seen and pinned agents in Herdr Core, named notifications, Herdr's emoji widths, streamed file sends, Go 1.27.3, Node 26, build provenance attestations, and security fixes. `v0.7.0` was published 2026-11-04, adding Shared files. `v0.7.1` was published 2026-11-06, adding scrollback to the full terminal for Herdr app terminals.

Downloadable GitHub Release archives are the primary installation path. A plain, manually dispatched GitHub Actions workflow builds candidates from an existing approved tag and exact commit, attaches only candidates that pass their gates to a draft release, and stops. It never creates a tag or publishes a release; a human reviews and publishes the draft. A rerun for the same tag updates that tag's existing draft, replacing its notes and assets, and never creates a second one. GitHub's by-tag release lookup returns published releases only, so the workflow finds the draft in the repository's release list instead. A draft edited without its tag reports an `untagged-…` tag name there, so the workflow also takes an untagged draft named for the release or targeting its commit as the tag's draft, always sends the tag and target when it updates the draft, and confirms both before changing assets. It refuses to touch a published release, stops if it finds more than one draft for the tag so a human can delete the extras, and never updates drafts for one tag from two runs at once. It replaces the draft's assets by that release's ID, not by its tag, and checks that the release is still a draft before each asset change.

Candidate targets are Linux amd64, Linux arm64, macOS amd64, and macOS arm64. Each binary must be built and exercised on a native GitHub-hosted runner with `CGO_ENABLED=0`. A target is omitted if its native build, tests, packaged-binary checks, or real Herdr TUI or herdr-core smoke check fails. It is also omitted if a pinned `govulncheck` finds a vulnerability the code reaches on that target, or if the binary's Go version is not the toolchain `go.mod` names. Findings only in required modules the code never calls don't fail a target. Cross-compilation alone is not support evidence. Windows is later work: Herdr supports Windows, but Shepherdr's current Unix-specific storage and locking primitives do not compile there.

Archives use predictable names such as `shepherdr_0.7.2_linux_amd64.tar.gz`. Each contains one top-level directory with the executable, `README.md`, `CHANGELOG.md`, and `LICENSE`; the release also provides a SHA-256 checksum file. The workflow attests build provenance for every archive and the checksum file with GitHub artifact attestations (`actions/attest-build-provenance`), so `gh attestation verify <archive> --repo luiscleto/shepherdr --signer-workflow luiscleto/shepherdr/.github/workflows/release.yml` shows that this repository's release workflow built them. Only the draft job gets the `id-token: write` and `attestations: write` permissions this needs.

The current workflow allows only an existing `v0.7.2` tag and verifies that it, the checkout, and `origin/master` all identify the exact full `commit_sha` input. Native smoke checks start one packaged Shepherdr against both a matching Herdr 0.9.5 CLI/server and herdr-core 0.1.19, each isolated under the runner's temporary folder, and require Home to report the Herdr TUI side Live and the Herdr Core side Live at version 0.1.19. Herdr comes from its GitHub release. herdr-core publishes no GitHub releases, so it comes from the URL and SHA-256 in its 0.1.19 release manifest at `releases.herdr.dev`. Both downloads have their SHA-256 pinned for each target. herdr-core 0.1.19 has builds for all four targets; a target without a pinned herdr-core build fails its smoke check rather than skipping it. The changes are independently reviewed and merged. The compatibility change was validated against isolated herdr-core 0.1.19 and Herdr app 0.1.14 stacks. Release preparation starts from exact commit `e208eedd15205f921363162be8df69e2fbad5537` on `master` in the native Herdr `release-072` worktree and leaves product code, browser assets, tests, and `web/dist` unchanged. An independent reviewer must review the exact preparation commit. The orchestrator then handles integration, push, tag, and manual workflow dispatch, and assigns validation of the draft artifacts. The human reviews the release changelog before publication. Preparation alone does not authorize publication.

After the approved result is on `master` and the existing `v0.7.2` tag points to that same full commit, invoke:

```sh
gh workflow run release.yml --repo luiscleto/shepherdr --ref master \
  -f tag=v0.7.2 -f commit_sha="<full-approved-master-SHA>"
```

Replace the SHA placeholder with the exact approved integrated commit. The workflow stops at a draft; artifact validation and human changelog review precede publication.

The workflow's summary names the draft's release ID. After artifact validation and the human's changelog review, the human publishes that draft by its ID and names the tag again, so the release lands on the real tag even if an edit untagged the draft:

```sh
gh api --method PATCH repos/luiscleto/shepherdr/releases/<draft-release-id> \
  -f tag_name=v0.7.2 -F draft=false -f make_latest=true
```

## Browser assets and installation

The Go executable embeds `web/dist`, which is about 0.75 MiB and contains the built application, terminal bundle, and static assets; the terminal lab's WASM was removed with the lab on 2026-11-04 by human decision. Production builds must run `npm ci` and the web build before `go build`. GitHub's generated source archives and `go install` cannot run npm as part of the Go module build.

To make `go install` an optional convenience, the deterministic `web/dist` output is tracked in ordinary source and release tags, marked as generated, and verified by CI against a clean rebuild. This is a narrow exception to `AGENTS.md`; it does not permit other generated output. Release-only asset commits and a separate asset module are unnecessary. With the repository module corrected to its canonical identity, installation is:

```sh
go install github.com/luiscleto/shepherdr@latest
```

The README recommends release archives, explains checksum and attestation verification and replacement upgrades, and retains a source-build fallback. It does not offer a pipe-to-shell installer, container, package-manager repository, updater, or automatic migration. Shepherdr and Herdr must run as the same operating-system account so Shepherdr can use Herdr's local socket and Herdr agents can read uploaded files.

## Versions and changes

`shepherdr --version` reports the release version injected with linker flags, a Go module version for `go install`, or `devel` for an unversioned local build. Release builds do not rewrite source files.

`CHANGELOG.md` follows Keep a Changelog with an `[Unreleased]` section and curated version entries. User-visible behavior, compatibility, security, configuration, installation, and upgrade changes require entries; internal refactors, tests, and documentation corrections may omit them when they do not change operator behavior. Release notes start from the changelog entry and add only release-specific platform or validation details. Raw conventional-commit dumps are not release notes, and the workflow is not a second changelog.

Before 1.0, breaking changes increment the minor version and carry explicit upgrade or migration notes; compatible fixes increment the patch version. Tags are never moved or reused. A bad release is marked as unsuitable and replaced by a new version after the problem is fixed.

## Deliberate limits

There is no automatic tagging or publishing, project signing key, code signing or notarization, installer, package ecosystem, container release, or self-updater. Those are reconsidered only after a demonstrated need. Release authenticity comes from GitHub artifact attestations, which need no key custody. macOS candidates are therefore unsigned, and their ordinary download-and-run behavior must be checked before publication.

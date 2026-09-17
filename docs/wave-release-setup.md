# First release setup wave

Status: approved implementation brief

Historical scope: this is the original v0.1.0 setup brief. Its version restrictions and Herdr 0.8.2 smoke requirement are not current release inputs. Follow [the current release strategy](release-strategy.md) and workflow for the active version guard and matching Herdr CLI/server smoke checks. The original wave record below is retained unchanged.

Preparation base: `c4ce5e8f6e2d96cf08f1f0258e5f0ad76e69ca89`

Worker starting commit: the exact commit containing this approved brief. The orchestrator must pin that commit in the worker dispatch after this document is committed; the worker must verify it before making changes.

## Outcome and scope

Prepare, but do not publish, the `v0.1.0` release path described in `docs/release-strategy.md`:

- Correct the module identity and all internal imports from `github.com/luisc/shepherdr` to `github.com/luiscleto/shepherdr`.
- Add stable README installation and upgrade guidance. Recommend binary archives and link to <https://github.com/luiscleto/shepherdr/releases/latest>, not a workflow-rewritten version URL. Document archive names, checksum verification, the `npm ci`/web build/Go build source fallback, `go install github.com/luiscleto/shepherdr@latest`, and a pinned `go install github.com/luiscleto/shepherdr@v0.1.0` example. Upgrades stop Shepherdr, preserve its state and configuration, replace the verified binary, check `--version`, and restart it. Retain the same-account requirement for Shepherdr, Herdr's local socket, and uploaded files.
- Add `CHANGELOG.md` with `[Unreleased]` and a curated `v0.1.0` entry. Release notes derive from that entry, not from a conventional-commit dump or duplicate workflow-maintained text.
- Amend `AGENTS.md` only to permit deterministic, CI-verified `web/dist` as Go embed input. Do not broaden the generated-output rule.
- Track a clean deterministic `web/dist`, mark it generated in `.gitattributes`, and make a clean rebuild fail verification if it changes tracked bytes or leaves unexpected files.
- Add `shepherdr --version`: release linker flags take precedence, Go module build information supplies the `go install` version, and an unversioned local build reports `devel`. Do not dirty or rewrite source files.
- Add a plain `workflow_dispatch` GitHub Actions workflow. It accepts and verifies an existing `v0.1.0` tag and expected full commit SHA, checks that they identify the approved clean `master` commit, builds native candidates with `CGO_ENABLED=0`, packages and verifies them, writes SHA-256 checksums, and creates or updates a normal **draft** GitHub Release. It must have no push or tag trigger and no path that creates a tag or publishes a release.
- Attempt `linux/amd64` on `ubuntu-24.04`, `linux/arm64` on `ubuntu-24.04-arm`, `darwin/amd64` on `macos-15-intel`, and `darwin/arm64` on `macos-15`. Each target must build on its matching runner. A failed target produces no archive; the workflow summary and draft make every omitted target explicit.

Do not implement Windows support, pipe-to-shell installation, package-manager distribution, Docker, an updater, signing or notarization, automatic tagging or publishing, or a version beyond `v0.1.0`. Do not create the `v0.1.0` tag, dispatch the release workflow, or create a GitHub Release in this wave.

## Ownership and overlap

One release-setup worker owns the module declaration and affected Go imports; version implementation and tests; `README.md`; new `CHANGELOG.md`; the narrow `AGENTS.md` rule; `.gitignore`, `.gitattributes`, the web build script and `web/dist`; and the new release workflow and any small packaging script it directly needs.

The likely overlaps are `main.go` for embedding and version handling, repository-wide import-path changes, the web build and tracked output, and release-facing documentation. Keep this as one worker brief. Do not edit approved product or architecture documents.

## Acceptance checks

The worker reports commands and exact results for all checks that can run before a tag exists:

1. `git` starts at the orchestrator-pinned brief commit and ends with no unrelated changes. `go list -m` reports `github.com/luiscleto/shepherdr`, and the old module path is absent from `go.mod` and Go imports.
2. From locked dependencies, the memory-capped web tests and typecheck pass. Two clean web builds produce the tracked manifest and identical bytes; `git diff --exit-code -- web/dist` passes and `git status --short --untracked-files=all -- web/dist` is empty after each rebuild.
3. `CGO_ENABLED=0 go test ./...` passes. Tests cover release-linker, Go-module, and `devel` version selection. A release-style build prints exactly `shepherdr v0.1.0` for `--version`; a local unversioned build reports `shepherdr devel`.
4. A local module install into a temporary `GOBIN`, without running npm during the Go build, succeeds from the tracked assets and reports a non-empty version. The public `@latest` and `@v0.1.0` commands remain publication-time checks because neither exists during this wave.
5. The workflow has only a manual trigger and refuses a missing tag, a tag/SHA mismatch, a non-`master` commit, or a version other than `v0.1.0`. Its release operation can create or update only a draft and cannot tag or publish.
6. Every candidate job proves its runner's native OS and architecture, runs the Go suite and build with `CGO_ENABLED=0`, verifies build metadata, extracts the archive, checks executable mode and contents, verifies its SHA-256 entry, and exercises the packaged binary's `--version`.
7. Before a target may be attached, its native runner starts matching Herdr 0.8.2 and the packaged Shepherdr as the same account, confirms health and embedded Home assets rather than the missing-assets response, exercises protected startup, and confirms clean process shutdown. Cross-compiled output cannot substitute for this check.
8. Each successful archive is named `shepherdr_0.1.0_<goos>_<goarch>.tar.gz`, contains a single same-named top directory with `shepherdr`, `README.md`, `CHANGELOG.md`, and `LICENSE`, and is listed in `shepherdr_0.1.0_checksums.txt`. A failed native gate omits that target and is visible in the workflow summary and draft notes.
9. Documentation commands, links, filenames, same-account constraint, and upgrade steps agree with the workflow and extracted archives. The repository's ordinary Go and memory-capped browser suites remain green.

The implementation wave stops before tagging or dispatching. After integration, the orchestrator pins a clean approved `master` commit and assigns the release version and tag. A human then creates the tag, manually dispatches the workflow with the tag and exact SHA, downloads and verifies every draft artifact, completes the accepted real-phone production workflow, checks the unsigned macOS experience for included macOS targets, and either publishes the draft or rejects it. No failed target is added by hand.

## Review and integration

An independent reviewer verifies the exact worker commit against this brief, including the workflow's negative guards, generated-asset reproducibility, version precedence, archive contents, native-gate enforcement, documentation, and prohibited scope. The reviewer reports findings and does not fix them.

An orchestrator-assigned integrator, distinct from the worker, integrates only an approved result from the pinned brief commit and reruns the pre-tag acceptance checks. Integration success means the release path is prepared; only the later human draft-release gate establishes release readiness and authorizes publication.

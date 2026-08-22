# Protected-access dependency review

Reviewed: 2026-08-22

The protected-access implementation pins `github.com/go-webauthn/webauthn` at `v0.17.4`.

Primary upstream evidence reviewed before pinning:

- The upstream `v0.17.4` release is marked **Latest**, immutable, and signed. It was released on 2026-05-22 from commit `bc5e90d19d2f5d9463438b4800d00312fc107bc5`: <https://github.com/go-webauthn/webauthn/releases/tag/v0.17.4>
- The release is a dependency-only maintenance release. Its module requires Go 1.25 and records the Go 1.26.3 toolchain: <https://raw.githubusercontent.com/go-webauthn/webauthn/v0.17.4/go.mod>
- Upstream's security policy lists the current line as supported and GitHub reports no published repository security advisories at review time: <https://github.com/go-webauthn/webauthn/security>

The repository baseline is Go 1.26.0. The worker checks used Go 1.26.5. Shepherdr delegates WebAuthn option construction and verification to this library and does not implement CBOR, COSE, attestation, or signature verification itself.

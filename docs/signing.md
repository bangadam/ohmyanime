# Signing and notarization

Release artifacts are signed **only** when credentials are configured as repository secrets. When they are not, builds are produced and published as **unsigned**, and must never be described as signed.

## Per-platform requirements

### macOS (codesign + notarization)
- An Apple Developer ID Application certificate (Developer ID Application: Name (TEAMID)), exported as `.p12`.
- An app-specific password for `notarytool` (created at appleid.apple.com).
- Required GitHub repository secrets (environment-variable references only — never hardcoded):

| Secret | Purpose |
|---|---|
| `MACOS_SIGNING_ID` | Certificate/common name, e.g. `Developer ID Application: Bangadam (TEAMID)` |
| `MACOS_P12_BASE64` | Base64 of the `.p12` certificate |
| `MACOS_P12_PASSWORD` | Password for the `.p12` |
| `MACOS_NOTARY_APPLE_ID` | Apple ID used for notarization |
| `MACOS_NOTARY_PASSWORD` | App-specific password for notarytool |
| `MACOS_NOTARY_TEAM_ID` | Apple team ID |

Pipeline (`.github/workflows/release.yml`, job `sign-macos`):
1. Import the `.p12` into a temporary keychain.
2. Build, codesign with the Developer ID, notarize, and staple (the build tool's `--sign` flags; see `go run ./cmd/desktop build --help`).
3. Verify: `codesign --verify --strict`, `spctl -a -t exec`, `xcrun stapler validate`.

The job runs only when all six secrets are non-empty (`if: secrets.MACOS_SIGNING_ID != '' && …`). Otherwise it is skipped and the unsigned artifact from the `build` job is the only output.

### Windows
Signing is optional. If a code-signing certificate is available later, add an `ev`/OV certificate as a secret and a `signtool sign /fd SHA256` + timestamp step. Until then Windows builds ship unsigned and are labeled as such.

### Linux
No signing. Packages are checksummed only (SHA-256 sums published alongside artifacts is planned).

## What remains manual (honest limits)
- Signing itself is non-deterministic (timestamps, notarization service) — no CI test can assert a signed result without credentials; verification steps only run in the signing job.
- First-time certificate creation, Apple Developer Program membership, and uploading the six secrets to GitHub are manual steps done by the repository owner.
- If the `cmd/desktop build --sign` flags are not yet implemented in the build tool, the signing job prints a pointer to this document and stops; the acceptance criteria of issue #18 cover documentation + CI configuration only, with no fake signing.
- Manual smoke: download the signed `.app` on a clean Mac, confirm `codesign --verify --strict` and Gatekeeper open without warnings.

## CI safety
- Secrets are referenced exclusively as `secrets.*` mapped into step `env:` variables; no step echoes them, and the decoded certificate is removed after import and never uploaded.
- The `signing-status` job records in every release run whether signing ran, so unsigned artifacts are never mislabeled.

## mygo.json build note
`mygo.json` carries only packaging metadata (identifier, icons, Linux deps). Signing identity is intentionally not stored there — it is supplied at build time from the environment/CI secrets so no credential can land in the repo.

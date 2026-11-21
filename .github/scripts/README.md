# Repository checks

`validate-commit-message.sh` reads a complete Conventional Commit message on
stdin. Types, lowercase descriptions, scopes from `commit_scopes.txt`, and
attribution restrictions are shared by commit and PR-title validation.
`validate-commits.sh` reads complete messages in a supplied range (or CI's
`BASE_SHA` and `HEAD_SHA`); it does not fetch missing history.

`test-commit-validation.sh` checks accepted and rejected messages through that
same validator. It creates no commits and changes no git configuration.

`coverage-gate.sh coverage.out` reads the package list and 90% threshold from
`.coverage.yml`. Coverage is weighted by statements, with exact comparison
before rounding for display. Repeated source blocks from `-coverpkg=./...` count
once, covered if any test binary ran them. `make coverage-gate` also enforces
the 93% total floor inherited from rmctl.

The workflow actions retain rmctl's commit-SHA pins. A macOS CI check builds and
tests with cgo for the Keychain binding, using injected APIs rather than the
login Keychain. Cross-compilation checks Linux, Windows and macOS without cgo.
Release setup, signing and package-manager scripts are described below.

`dupl.sh` scans production sources at rmctl's 60-token threshold. Tests and
generated files are excluded before scanning. `make dupl-check` fails on either
clones or scanner diagnostics. The local commit hook delegates to the shared
validator; no hook configuration was changed by this task.

## Release packaging

The release scripts, supported targets, local dry runs, runtime and licence
requirements, signing credentials and package-manager destinations are documented
in [builds and releases](../../docs/releases.md). `test-release.sh` tests both
formula and manifest variants using a fake release, plus the local macOS upload
flow with fixture tools; it never publishes. CI releases Linux and Windows;
`release-macos.sh` adds signed macOS archives locally and dispatches Homebrew.

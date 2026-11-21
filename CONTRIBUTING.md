# Contributing

Use Go 1.27 with toolchain go1.27.4. Native builds honor Go’s cgo setting; macOS Keychain support and the race
detector need a supported native compiler. `make build-nocgo` checks the
portable build, which lacks Keychain support. Tools are pinned
in `devtools` and built into `bin/tools`, not installed into the user's bin.

```sh
make lint
make vet
make test
make test-integration
make coverage-gate
make security-scan
```

`make lint` tidies both modules, formats imports and code, and runs golangci-lint.
CI uses `make golangci` to avoid rewriting files. `make vet` also checks Go's
modernizers with `go fix -diff`. Tests use the race detector; integration and
coverage runs shuffle the order. Coverage retains rmctl's 90% per-package
floor and 93% total floor. Add negative cases alongside successful paths;
do not lower the thresholds to accommodate untested changes.

Follow [the architecture](docs/architecture.md). Use cases ask for ports;
only wiring assembles adapters. Exported identifiers have doc comments.
Comments explain choices and invariants. Keep errors contextual and values
out of diagnostics. Avoid new dependencies unless the design calls for them.

Commands build `output.Result` values and use `env.Emit`; `cliutil` selects
the renderer. Golden files pin text, table and JSON answers. To update them
intentionally, run `SNCTL_UPDATE_GOLDEN=1 go test ./cmd/snctl/commands/...`,
then inspect every diff and rerun without the variable.

All account data in tests is synthetic. Use temporary directories for config,
secrets and cache. Inject `KeychainItems` through wiring or the main test seam.
Never test against the login Keychain, and never let an inherited environment
choose a user's real configuration or backend. The separately tagged [Private Cloud test](#opt-in-private-cloud-test) requires explicit credentials.

Commit messages use Conventional Commits, lowercase descriptions and optional
scopes from `commit_scopes.txt`. Describe the concrete change; omit test
summaries and generation signatures. `root` is not a scope. The same validator
checks complete commit messages and PR titles:

```sh
bash .github/scripts/test-commit-validation.sh
```

## Release steps

Use gitflow: prepare `release/VERSION` from `develop`, set the release version
and finish the release into `main` and back into `develop`. The owner creates
the `vVERSION` tag on the release commit and pushes the branches and tag.
Automation never creates or moves a tag. An existing tag can also be selected
through the release workflow's manual dispatch input.

Before the owner pushes, run the Go checks above, `actionlint`,
`shellcheck .github/scripts/*.sh`, and `bash .github/scripts/test-release.sh`.
Run `.github/scripts/release-macos.sh --dry-run vVERSION` with a fresh
`RELEASE_DIR` to build and package both macOS architectures without credentials;
`RELEASE_ARCHS` can restrict a local check to the host architecture. Run
`smoke-archive.sh` for each native archive. Cross-build all six lite targets
with `release-build.sh --dry-run VERSION lite`, `GOOS` and `GOARCH`.
Full Linux/Windows builds use the corresponding setup script in disposable CI
runners; these setup scripts install system development packages.

The tag workflow runs CI, builds and smoke-tests all twelve archives, verifies
licences, writes checksums, and publishes with GoReleaser without rebuilding.
Only the publication job has repository write permission. Stable releases then
update `altessa-s/homebrew-tap` and `altessa-s/scoop-bucket` when their scoped
tokens are configured. No external update occurs during local validation.
See [builds and releases](docs/releases.md) for secret names, signing and
notarization, dry-run commands, artifact contents and current binding limits.

### Opt-in Private Cloud test

`make test-bench` builds and runs the real-server suite with the `bench` tag.
It is excluded from default tests and CI. Set `SNCTL_BENCH_ENDPOINT`,
`SNCTL_BENCH_ACCOUNT`, and `SNCTL_BENCH_PASSWORD_FILE`; the password file must
contain a line `password:<value>`. Protect that file with owner-only permissions.
The suite uses temporary configuration and file secrets, creates a fresh
`NOTE/Note/snctl-bench-<timestamp>-<random>` folder, and exercises uploads,
replacement, parallel and incremental transfers, moves, rename, recycle/restore,
usage and duplicate detection. Cleanup recycles only its created folder; it
never empties the recycle bin. A failed cleanup reports the retained folder.

```sh
export SNCTL_BENCH_ENDPOINT=http://127.0.0.1:19072
export SNCTL_BENCH_ACCOUNT=synthetic-account
export SNCTL_BENCH_PASSWORD_FILE=/path/to/private-password-file
make test-bench
```

To compile and check the suite without logging in, run
`go test -tags=bench -run '^$' ./tests/bench`.

# Builds and releases

On pushes to git-flow `release/*` and `hotfix/*` branches, which are pushed
before their tag, on pull requests to `main`, and on manual dispatch,
`packaging-ci.yml` builds full and lite archives natively for Linux and Windows,
for amd64 and arm64. It calls the same `release-package.yml` reusable workflow
as the tag release, using `release-build.sh --dry-run` and a synthetic
`v0.0.0-ci` version. Both callers run the same setup, build, licence checks,
archive extraction/runtime smoke and checksum generation/verification. Packaging
CI uses read-only repository access, skips artifact uploads and never publishes a release.
Full jobs have a 60-minute timeout; lite jobs have 30 minutes.

Tag releases upload the eight archives after these checks.
The signed macOS archives for both architectures are added locally after CI
publishes the release. Both contain an executable named `snctl` (`snctl.exe` on
Windows). Archive names follow rmctl:

| Build | macOS | Linux | Windows |
|---|---|---|---|
| Full | `snctl_VERSION_darwin_ARCH.zip` | `snctl_VERSION_linux_ARCH.tar.gz` | `snctl_VERSION_windows_ARCH.zip` |
| Lite | `snctl-lite_VERSION_darwin_ARCH.zip` | `snctl-lite_VERSION_linux_ARCH.tar.gz` | `snctl-lite_VERSION_windows_ARCH.zip` |

Full builds enable cgo and compile the pinned snnote-go and snrender-go libraries.
macOS uses Apple's CoreGraphics, CoreText and ImageIO frameworks. Linux uses
Cairo >= 1.18, cairo-ft, FreeType, GdkPixbuf and zlib from the system. On Debian
trixie, install `libcairo2 libfreetype6 libgdk-pixbuf-2.0-0 zlib1g` to run it.
Windows builds use matching MSYS2 CLANG64 or CLANGARM64 packages and carry their
runtime DLLs beside the executable. Keep the archive's files together.

snrender-go v1.0.0 requires PDFium for all Cairo builds. Setup fetches the
binding's pinned chromium/8125 SDK using its vendored, checksum-verified
`fetch-pdfium.py`; callers supply `CGO_CFLAGS` and `CGO_LDFLAGS`.
Linux full archives ship `libpdfium.so` beside `snctl`, found through a literal
`$ORIGIN` run path. Windows full archives ship `pdfium.dll` beside `snctl.exe`,
with the Cairo DLL closure. The complete SDK `LICENSE` and `licenses/` directory
are retained under `licenses/pdfium/`, with a separate required notice manifest;
the packer rejects missing runtime files or notices. macOS Apple archives need
no PDFium and the local macOS publisher is unchanged. Lite never fetches, links
or ships PDFium. `.mark` source overlays are described in [rendering](rendering.md).

The current binding embeds no external font bundle. Its Cairo recognition font
is generated internally and requires no installed font. Full packages include
the licences of both Go bindings and both vendored C libraries. All vendored
licence/copyright/OFL files are retained; if a later binding adds `fonts/`, its
OFL notices are required and copied too. Packaging fails for missing required
licences, empty notices, or a Windows DLL package without licence text; a package
metadata record alone is insufficient. Windows dependency discovery checks the
whole DLL closure with `ntldd -R`, and smoke tests run the extracted executable
with only the Windows system directories on PATH.

Lite uses cgo with `-tags norender` on every platform, matching rmctl. macOS
retains notebook reading and Keychain through system frameworks. Linux retains
notebook reading and needs only the system C library. Windows retains notebook
reading with `-linkmode=external -extldflags=-static` and imports only Windows
system DLLs. Lite needs no graphics libraries or bundled DLLs. Drawing, `render`,
`open` and annotated notebook mirror output are unavailable. Cloud file commands,
profiles, file secrets, PDF/EPUB mirrors, backups, cache, tasks and digests remain
available. `version --full` reports both `render` and `notes`. A separate
`CGO_ENABLED=0` source build also omits notebook reading and macOS Keychain.

Every archive has a `licenses/go` directory containing LICENSE, COPYING and
NOTICE files from exactly the external modules selected by `go list -deps -json`
for that target, cgo setting and build tags. A linked module without licence
text stops packaging. Lite includes the reader's vendored native licence too.
The packer checks the notice checksum manifest for both variants.

## Local validation

These commands build real archives from the current checkout, including local
changes, and never sign or publish. The macOS `--dry-run` does not fetch a tag
or create a worktree; its version argument only labels the build. It disables
Go module, checksum database, VCS and toolchain downloads, so the required Go
toolchain and dependencies must already be cached for offline validation.
Choose a new `RELEASE_DIR` for each run; occupied staging directories fail
rather than being silently reused.

```sh
mkdir -p bin
export RELEASE_DIR=$(mktemp -d "$PWD/bin/release-local.XXXXXX")
RELEASE_ARCHS=arm64 bash .github/scripts/release-macos.sh --dry-run v9.8.7
bash .github/scripts/smoke-archive.sh "$RELEASE_DIR/snctl_9.8.7_darwin_arm64.zip" v9.8.7
bash .github/scripts/smoke-archive.sh "$RELEASE_DIR/snctl-lite_9.8.7_darwin_arm64.zip" v9.8.7
bash .github/scripts/test-release.sh
GOOS=linux GOARCH=arm64 RELEASE_DIR="$PWD/bin/lite-cross" bash .github/scripts/release-build.sh --dry-run v9.8.7 lite
```

`release-build.sh [--dry-run] VERSION full|lite` builds and packs a target;
`release-pack.sh` checks notices again before archiving. Linux and Windows full
builds need the setup script's C23 compiler and graphics stack. The setup
scripts run on their respective platforms after Go module download and write
compiler and PDFium settings to `GITHUB_ENV`. Release builds also fetch the SDK
when `PDFIUM_DIR` is not supplied; when supplied, it must be the setup script's
verified SDK for the target architecture. Linux release setup passes a literal
`$ORIGIN` rather than the build-tree run path used for CI tests. Windows test
steps put `PDFIUM_BIN` on PATH. Linux lite needs clang 19; Windows lite uses
MSYS2 clang with static external linking. Cross-building lite requires a C23
compiler and C runtime targeting the destination architecture. Run system setup
in disposable Linux containers or Windows CI runners.

`smoke-archive.sh ARCHIVE VERSION` extracts into a new temporary directory,
verifies the licence manifest, checks version and help, and renders the committed
blank notebook and a source-PDF overlay to PDF and PNG for full builds. The PNG
preview exercises PDFium on Cairo. Linux smoke checks the exact `$ORIGIN` run
path and resolves PDFium from the extracted directory with loader overrides
removed. Linux and Windows smoke also hide the bundled PDFium temporarily and
require the binary to fail to start, proving no system/build-tree copy can
satisfy the dependency. Lite reads the fixture with `snctl text`.
Both builds execute the file-command workflow against a loopback
fake cloud using synthetic file secrets; no cloud login or Keychain is used.
`test-windows-tools.sh` exercises DLL discovery, package licences and static
imports with fixture tools in Bash 5; it runs in Linux CI and does not replace
native Windows execution. Cross-built archives must be smoked on their target
machine. CI smoke-tests all eight Linux and Windows archives before publishing.
The local macOS publisher smoke-tests the extracted full and lite archives for
its Mac's native architecture before uploading; it also builds and signs the
other architecture, as rmctl does.

`checksums.sh DIRECTORY` writes `checksums.txt` for all archives in that directory.
`homebrew-formula.sh [--lite] VERSION CHECKSUMS` and
`scoop-manifest.sh [--lite] VERSION CHECKSUMS` render to stdout without changing
external repositories. The test renders both variants from a fake release,
checks Ruby syntax and every formula/manifest hash, and rejects missing hashes
and licences. It also exercises the local publisher with fixture builds and fake
signing/notary/GitHub tools: fetched-tag worktree builds, missing tags on origin,
worktree cleanup after failures, offline current-checkout dry runs, Darwin-only
checksum replacement, partial upload/dispatch retries and prerelease handling,
all without network or credentials. An optional checksums argument
incorporates real locally built archive hashes into that fixture; `RELEASE_TEST_OUTPUT` retains the rendered
files. Missing full targets in that fixture retain synthetic hashes and are
never claimed as built artifacts.

## Workflow credentials

All workflows default to `contents: read`. Only the publishing job grants
`contents: write`; build checkouts disable persisted credentials. Go downloads modules from the public proxy.

Signing credentials stay on the release Mac; no Apple signing or notarization
secrets are stored in GitHub. macOS uses the same hardened runtime, secure
timestamp, codesign verification and single notarization submission as rmctl.

| Credential or setting | Location and purpose |
|---|---|
| Developer ID Application certificate and private key | Installed in the Mac's keychain; never exported to CI |
| `MACOS_SIGN_IDENTITY` | Local override; defaults to `Developer ID Application: Altessa Solutions Inc (SZA4GU6AL2)` |
| `NOTARY_PROFILE` | Local notarytool keychain profile; defaults to `rmctl-notary` |
| `gh` authentication | Local access to upload release assets and dispatch workflows (contents and actions write) |
| `HOMEBREW_TAP_TOKEN` | GitHub secret: contents write access only to `altessa-s/homebrew-tap` |
| `SCOOP_BUCKET_TOKEN` | GitHub secret: contents write access only to `altessa-s/scoop-bucket` |

snctl shares rmctl's notarytool profile. If it is missing, create it once on
the Mac with `xcrun notarytool store-credentials rmctl-notary` and follow its
prompts. The
Apple account credentials remain in that keychain profile. Missing signing
identity or unavailable notary credentials fail a normal run. `--dry-run`
skips all credential access, signing, notarization, Git fetches and GitHub
commands, even with inherited credentials; it builds the current checkout
offline without requiring a real tag or a clean tree. Normal runs build the
tag fetched from origin in a temporary detached worktree. Only dry runs permit
unsigned archives. A bare signed binary cannot be stapled; Gatekeeper retrieves
its ticket online. Windows binaries are unsigned.

## Release procedure

1. Publish the version tag through the normal release process, or dispatch
   `release.yml` for an existing tag. CI runs checks, builds and smoke-tests
   the eight Linux/Windows full and lite archives, then publishes those archives
   and `checksums.txt`. Stable releases can update Scoop immediately: its two
   manifests only need the Windows archives.
2. Wait for the GitHub release to be published. On a Mac with Go, Apple's
   command-line tools, `gh`, `jq`, Python and Ruby,
   use any checkout of the repository, including `develop` with tracked or
   untracked changes. The local publisher fetches the requested tag from
   `origin` and creates a temporary detached worktree at `refs/tags/<tag>`.
   It builds the tagged source and uses that tag's build, packaging, licence,
   checksum and smoke-test scripts; the caller's HEAD and local changes do
   not enter the release. A tag missing on origin fails before any build or
   signing, even if that tag exists locally.
3. Run `make release-macos TAG=v1.2.3`. The installed Developer ID identity and
   `rmctl-notary` profile are used by default; override `MACOS_SIGN_IDENTITY`
   and `NOTARY_PROFILE` locally if needed. Normal uploads require both
   architectures (`RELEASE_ARCHS="arm64 amd64"`, the default). `RELEASE_DIR`
   defaults to `bin/release` in the caller's checkout. An exit trap removes the
   temporary worktree and staging files on success or failure.
4. The script stages fresh full and lite builds for arm64 and amd64, signs
   every binary with hardened runtime and a secure timestamp, verifies signatures,
   and notarizes all four in one submission. It packs with the existing licence
   checks and smoke-tests both extracted native archives before publication.
5. The script downloads the current `checksums.txt`, preserves every non-macOS
   row, replaces the Darwin archive rows, and uploads all four archives and the
   merged checksums using `--clobber`. It leaves local archives and the merged
   checksums in `RELEASE_DIR`. After a network failure, rerun the same command:
   fresh temporary staging and a fresh checksum download make partial uploads
   safe to replace. A failed Homebrew dispatch can also be retried this way.
6. For stable releases, the script dispatches `homebrew.yml` at the release tag.
   The workflow downloads the published macOS and Linux archives, computes their
   checksums and generates both formulas before updating `altessa-s/homebrew-tap`.
   Homebrew is never invoked by `release.yml`. A prerelease tag or a GitHub
   release marked prerelease skips Homebrew. The workflow must exist on the
   repository's default branch to accept dispatches.

To retry only formula publication, dispatch `homebrew.yml` with the existing
release tag after its macOS upload, for example
`gh workflow run homebrew.yml --ref v1.2.3 -f tag=v1.2.3`.
Formula generation fails if required archives are missing. Missing package-manager
tokens warn and skip publication; prereleases skip their publishing jobs.
Winget is not configured for snctl: it first needs registered package identifiers
and an initial manual submission.

Manual release dispatch validates the version syntax and resolves
`refs/tags/<tag>` from fetched tags before CI or builds start. All manually
selected checkouts use that explicit tag ref, so a version-shaped branch cannot
be released. A tag-push release continues to use its event ref.


### Windows TIFF and LGPL sources

On the disposable Windows full-package runner, `.github/scripts/setup-release-tiff.sh`
rebuilds the installed MSYS2 libtiff release with JBIG disabled before snctl
is linked. Use the same step before a standalone Windows full release build.
The bundler checks the recursive import list again on the extracted executable.

The ZIP carries each dynamically linked LGPL package’s signed MSYS2 source
archive, including its build recipe and patches, under `licenses/msys2/`
beside the package record and licence. This includes GLib and GdkPixbuf.

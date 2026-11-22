# snctl

A command-line client for the Supernote Cloud, built the way
[rmctl](https://github.com/altessa-s/rmctl) is built.

snctl connects to the official Supernote Cloud and Ratta private clouds through
[sncloud-go](https://github.com/altessa-s/sncloud-go).

## Features

- [Browse and measure the library](docs/library.md), find duplicates and download folder trees.
- [Upload files and folders](docs/uploads.md) with a preview and explicit replacement.
- [Read notebooks and highlights](docs/notebooks.md), search stored handwriting recognition and export text.
- [Render notebooks, annotated PDFs and Atelier drawings](docs/rendering.md) or open a preview.
- [Mirror PDFs and EPUBs](docs/mirror.md) between the cloud and a local folder, including annotated output.
- [Back up original files and restore them](docs/backups.md) to the same account or another profile.
- [React to cloud changes](docs/watch.md) with a program that receives event batches.
- [Work offline](docs/configuration.md#cache) with a listing and file bodies cached per profile.
- Manage [to-do lists and tasks](#to-do-lists-and-tasks) and [digests](#digests) from the command line.
- Schedule jobs with [Bash](docs/automation.md), [AppleScript](docs/applescript.md) or [PowerShell](docs/powershell.md).

## Install

The first public release is v1.0.0. Releases provide full and lite builds for macOS, Linux and Windows, each for
amd64 and arm64. Both install the `snctl` command; choose one.

With Homebrew on macOS or Linux:

```sh
brew install altessa-s/tap/snctl
# Or install the build without rendering.
brew install altessa-s/tap/snctl-lite
```

With Scoop on Windows:

```powershell
scoop bucket add altessa-s https://github.com/altessa-s/scoop-bucket
scoop install snctl
# Or: scoop install snctl-lite
```

The Homebrew formulas conflict because both install `snctl`. Scoop cannot
express that conflict; the most recently installed shim wins. Homebrew installs
bash, zsh and fish completions and supports `brew services start snctl` (or
`snctl-lite`) for [mirrors](docs/mirror.md).

You can also download [release archives](https://github.com/altessa-s/snctl/releases):
`snctl_<version>_<os>_<arch>` for full and
`snctl-lite_<version>_<os>_<arch>` for lite, with `.zip` on macOS/Windows and
`.tar.gz` on Linux. Verify their SHA-256 against `checksums.txt`. Keep full
archives' runtime libraries beside the executable. Linux full builds need the
system Cairo, FreeType, GdkPixbuf and zlib packages and bundle `libpdfium.so`
beside `snctl`; Windows full archives include `pdfium.dll` and
the runtime DLLs and their licences. macOS binaries are signed and notarized
on the release Mac and added after CI publishes Linux and Windows archives.
See the [release procedure](docs/releases.md#release-procedure).

Full builds support notebook reading and drawing. Lite uses cgo and `norender`:

| Lite platform | Included | Runtime |
|---|---|---|
| macOS | `text`, `highlights`, `note`, Keychain, cloud files, PDF/EPUB mirrors, backups, profiles, cache, tasks and digests | Apple system frameworks |
| Linux | Notebook reading and all file commands | System C library; no graphics stack |
| Windows | Notebook reading and all file commands | Statically linked C runtime; Windows system DLLs only |

Lite omits drawing, `render`, `open` and annotated notebook mirror output on
every platform. `snctl version --full` reports `render` and `notes`;
`snctl --version` reports the release version. Every archive includes licences
for its linked Go modules and native libraries. See [builds and releases](docs/releases.md).

## Build from source

Go 1.27, with toolchain go1.27.4. macOS Keychain support needs cgo and Apple’s command-line tools. Notebook reading needs a C23 compiler; rendering uses Apple frameworks on macOS and the Cairo stack plus PDFium on Linux and Windows. See [rendering setup](docs/rendering.md#limits-and-build-requirements).

```sh
git clone https://github.com/altessa-s/snctl.git
cd snctl
make snctl
./bin/snctl version --full
```

`make snctl` builds
`bin/snctl` with native reading and drawing when cgo is enabled.
`make snctl-lite` builds with `norender` there, retaining notebook reading
when cgo is enabled. `CGO_ENABLED=0` source builds omit both native libraries.
`version --full` reports the capabilities actually compiled in.

## Help groups

`snctl --help` groups commands by what they do:

| Group | Commands |
|---|---|
| Account | `login`, `logout`, `account`, `profile`, `secrets`, `completion`, `help` |
| Reading — these change nothing | `ls`, `tree`, `stat`, `find`, `du`, `dupes`, `get`, `mget`, `backup`, `sync`, `watch`, `text`, `highlights`, `note`, `render`, `open` |
| Writing — these change the cloud and ask first | `put`, `mput`, `mkdir`, `mv`, `rename`, `rm`, `restore`, `trash`, `mirror`, `tasks`, `digests` |
| Diagnostics | `check`, `debug` |

Short descriptions start with a capital letter, including subcommands.
`completion` keeps Cobra's "Generate the autocompletion script for the specified shell"
and `help` keeps "Help about any command". `version` uses "Display version information",
as in rmctl, and stays in Cobra's Additional Commands group. Each command and
subcommand has its own description and examples: `snctl get --help`,
`snctl render --help`, or `snctl help tasks lists add`. Mixed commands explain
their readers and writers: `trash` alone lists, task and digest readers change
nothing, and `backup restore` uploads. `check` reads unless `--write` authorizes
its scoped self-test. A saved mirror definition authorizes future passes;
ad-hoc mirror runs ask first. Service installation starts the saved mirrors.

## Local commands

```sh
snctl profile add work --endpoint official --yes
snctl profile add private --endpoint http://private.example:8080 --yes
snctl profile ls
snctl profile info work
snctl -P work secrets status
snctl -P work secrets migrate --to keychain --set-default --yes
snctl profile del private --yes
snctl debug paths
snctl version --full --format json
snctl completion zsh
```

A profile is a name, an endpoint and a choice of secret store. Adding one does
not log in. `--secrets file|keychain` on `profile add` records its backend;
without it the profile inherits the configuration. Migration requires secrets
already present; a fresh profile has none. macOS builds with cgo support the Keychain; every
platform supports a file of mode 0600.

Cloud file, task, digest and profile changes ask first; in a pipe they require
`--yes`. Login and logout act on the session directly; saved mirrors have
consent from their definitions. Answers go to stdout; questions and commentary go to stderr.
Use `--format table|plain|json`, `SNCTL_FORMAT` or `--json` to choose an answer
format (`text` remains an alias for `plain`).
`-o`/`--output` names a destination file or directory on commands that write files.
Without a choice, a terminal gets a table and a pipe gets tab-separated plain
lines. `--quiet` hides progress lines only; JSON suppresses them too. Prompts,
warnings, errors and verbose commentary still go to stderr.
`completion` accepts `bash`, `zsh`, `fish` and `powershell`.

## Cloud commands

Create a profile, then authenticate. Passwords are read without echo on a
terminal or from one line on stdin with `--password-stdin`; there is no password
argument or environment variable. The account from the last successful login
is reused when `--account` is omitted. Phone accounts also use `--country-code`.
An emailed challenge is sent automatically; enter the code at the prompt or
supply `--code` in a noninteractive run.

```sh
snctl -P work login --account reader@example.com
snctl -P work account --json
snctl -P work logout
```

Commands use the selected profile (`-P` or `SNCTL_PROFILE`). Examples below
assume it is already authenticated. Remote paths follow the tablet, with or
without a leading slash. A private cloud may prohibit folders in the virtual
root. The web's `Note` folder is `NOTE/Note` in tablet paths; use that exact
spelling. `Note/x` is refused with top-level candidates. Writes under `NOTE/x`
are also refused because the private server mirrors them into `NOTE/Note/x`.
`NOTE` is a virtual category, not an alias for `NOTE/Note`. Use `DOCUMENT/Document`
for the web's Document folder. `ls --ids` includes identifiers; `trash` always does.

| Command | Example |
|---|---|
| `ls` | `snctl ls NOTE/Note` |
| `tree` | `snctl tree NOTE/Note` |
| `stat` | `snctl stat NOTE/Note/Journal.note --json` |
| `find` | `snctl find Journal NOTE/Note` |
| `find --text` | `snctl find --text "release notes" NOTE/Note` |
| `du` | `snctl du / -d 2` |
| `dupes` | `snctl dupes DOCUMENT/Document` |
| `sync` | `snctl sync NOTE/Note --files` |
| `debug cache` | `snctl debug cache status --all` |
| `get` | `snctl get NOTE/Note/Journal.note -o ./downloads/` |
| `mget` | `snctl mget / -o ~/supernote --incremental --delete -j 8` |
| `put` | `snctl put journal.txt NOTE/Note/journal.txt --yes` |
| `put` (several) | `snctl put a.pdf b.epub --to DOCUMENT/Document --yes` |
| `mput` | `snctl mput ~/Papers DOCUMENT/Document/Papers --incremental --yes` |
| `mkdir` | `snctl mkdir -p NOTE/Note/Reading/2026 --yes` |
| `mv` | `snctl mv NOTE/Note/journal.txt NOTE/Note/Reading --yes` |
| `rename` | `snctl rename NOTE/Note/Reading/journal.txt notes.txt --yes` |
| `rm` | `snctl rm NOTE/Note/Reading/notes.txt --yes` |
| `trash ls` | `snctl trash ls` |
| `trash empty` | `snctl trash empty --yes` |
| `restore` | `snctl restore 123456789 --yes` |
| `mirror add / ls / rm` | `snctl mirror add papers ~/Papers /DOCUMENT/Document/Papers --pull-new` |
| `mirror run` | `snctl mirror run --all --watch` |
| `mirror service install / uninstall / status / logs` | `snctl mirror service logs -n 50 -f` |
| `backup` | `snctl backup ~/Supernote-backup NOTE/Note` |
| `backup restore` | `snctl backup restore ~/Supernote-backup NOTE/Note/Recovered --dry-run` |
| `watch` | `snctl watch --json` |
| `watch --exec` | `snctl watch --exec ~/bin/sn-on-change.sh --only file --under /NOTE/Note` |
| `check` | `snctl check` |
| `check --write` | `snctl check --write` |

`get <path> -o -` writes raw bytes to stdout and cannot be combined with JSON.
Existing destination directories remain directories; files are replaced through
an atomic sibling rename. `put - NOTE/Note/input.txt --yes` reads stdin. `put`
takes several files into a folder (`--to`, or the last argument), `--name` for
one file, and requires `--replace` to replace an existing file; `--dry-run`
checks and sends nothing. `mkdir` refuses an existing folder; `mkdir -p` creates
the missing folders above it and accepts one already there. `mv` moves into an existing folder, keeping the filename, or takes a new full
path whose parent exists. It refuses an existing file and a folder moved into itself.
`rename` changes only the name within the same folder. Documents carry their
full-name `.mark` sidecars through `mv`, `rename` and `rm`; stderr names both
files before the single confirmation. `--dry-run` lists both without writing.
An occupied target sidecar is a conflict; partial success fails with both outcomes.
`rm` recycles by default; `--permanent` bypasses
the recycle bin. Restore takes an identifier from `trash ls`. Each of these
commands starts with one planning listing. File writes re-check affected parents
fresh after confirmation and verify created or moved names afterward. A changed
plan is exit 4; a post-write twin or failed verification listing is exit 6, with
no rollback. `rm`, `mv` and `rename` refuse NFC twins unless `--exact` selects the
byte-exact source name; document/sidecar pairing must still be unambiguous.
See [cloud paths](docs/library.md#cloud-paths).

Listings fold `Paper.pdf.mark` into `Paper.pdf` (and likewise for EPUBs);
orphans remain visible, and `--marks` shows raw sidecars in `ls`, `tree`,
`find` and `dupes`. Human tables and trees place FLAGS before the name:
`R` recognized handwriting, `H` highlights.
The legend goes to stderr only for nonempty human tables.
Plain output keeps its stable fields and has no flags; scripts use `--json`
to read flags.
JSON retains `recognition` and adds `flags` plus sibling `mark` metadata;
`ls`, `tree` and `find` accept `--recognition` to inspect unknown notebooks.
`text -o FILE` saves cleaned text, and `text FOLDER -o DIR` keeps the tree.
See [notebooks](docs/notebooks.md) for recognition, search, raw JSON and cleaning.

### Folders and space

`mget` downloads a cloud folder into a local directory, keeping the tree, with
safe local names and a record that makes `--incremental` and a guarded
`--delete` possible. `mput` uploads a local tree, `.note`, `.mark`, `.pdf` and
`.epub` files, creating folders, under the account sync lock. Existing files require `--replace`; concurrent
collisions are reported according to the server result. `--incremental` skips files with the same
size and MD5. `du` sums sizes per folder with the account capacity; `dupes`
groups nonempty files with identical bytes. See [library](docs/library.md),
[uploads](docs/uploads.md) and [examples](docs/examples.md).

### To-do lists and tasks

```sh
snctl tasks lists ls
snctl tasks lists add Reading --yes
snctl tasks lists rename LIST_ID Books --yes
snctl tasks lists rm LIST_ID --yes
snctl tasks ls LIST_ID --json
snctl tasks add 'Read chapter' --list LIST_ID --detail 'Take notes' --yes
snctl tasks edit TASK_ID --title 'Read chapter two' --detail 'Review notes' --yes
snctl tasks done TASK_ID --yes
snctl tasks undone TASK_ID --yes
snctl tasks rm TASK_ID --yes
```

`tasks ls` without an identifier lists all tasks. `tasks add --due` accepts
RFC3339. Edits preserve recurrence, reminders, links and ordering already held
by the service; this CLI does not expose editing those fields or occurrences.
Deleting a list also deletes its tasks.

### Digests

```sh
snctl digests groups ls
snctl digests groups show GROUP_ID
snctl digests groups add --name Articles --comment 'Reading notes' --yes
snctl digests groups edit GROUP_ID --name Papers --yes
snctl digests groups rm GROUP_ID --yes
snctl digests digests ls
snctl digests digests show DIGEST_ID --json
snctl digests digests add --content 'A passage' --group GROUP_IDENTIFIER --tag reference --yes
snctl digests digests edit DIGEST_ID --comment 'My comment' --yes
snctl digests digests rm DIGEST_ID --yes
snctl digests tags ls
snctl digests tags show TAG_ID
snctl digests tags add --name reference --yes
snctl digests tags edit TAG_ID --name reading --yes
snctl digests tags rm TAG_ID --yes
snctl digests handwriting get DIGEST_ID comment.mark
snctl digests handwriting put DIGEST_ID comment.mark --yes
```

Group membership uses the group's `identifier`, shown by `groups ls`, while
show/edit/rm take numeric IDs. `--tag` is repeatable. Unspecified digest fields
are preserved. Group removal leaves its digests intact. Handwriting upload
attaches the uploaded `.mark` reference to the existing digest.

### Live self-test

`check` reads session/user/capacity, device and web listings, downloads a file
if one exists (the library verifies a supplied MD5), lists tasks and digests,
and waits up to 30 seconds for the feed's first event. `check --write` explicitly
authorizes a new random `NOTE/Note/snctl-check-*` folder, small and multipart uploads,
download comparisons, a move into a subfolder, a rename and cleanup. It never
empties the recycle bin. Cleanup gets a separate two-minute context on failure
or interruption and refuses to delete a folder containing unexpected entries.
Failures return a nonzero status and report retained folders. Results include
one measured step per line; JSON emits one result object per step.

See also [contributing](CONTRIBUTING.md) and [security](SECURITY.md).

One rule, as in rmctl: every command that changes the cloud — creating,
uploading, moving, renaming, recycling, restoring, deleting, emptying the
recycle bin, task and digest changes, handwriting upload — asks on a terminal
and requires `--yes` in a pipe. A bulk change shows its plan before the
question. `--dry-run` sends nothing and needs no answer. Reading and local
output (`get`, `mget`, `render`) never ask: downloads atomically replace local
files without a prompt, as rmctl does, and `mget --delete` removes only files
it recorded, behind its own guard. `login`, `logout` and `check --write` are
their own authorization. Saved mirror definitions authorize later passes;
ad-hoc mirror runs ask before changing files.

## Notebook reading and rendering

Inputs follow the [notebook path rule](docs/notebooks.md); local notebooks read
directly and cloud notebooks use canonical paths. Local reading needs no login. All commands preserve the source.

```sh
snctl render Journal.note -o journal.pdf
snctl render Journal.note --page 2 -o page.png --scale 2
snctl render Journal.note --format jpg --quality 90 -o pages/
snctl render NOTE/Note -o notebooks-pdf --incremental -j 4
snctl open Journal.note
snctl render DOCUMENT/Document/Report.pdf -o annotated.pdf
snctl open DOCUMENT/Document/Report.pdf --no-marks
snctl render sketch.spd --drawing-page canvas -o sketch.png
snctl open EXPORT/sketch.spd
snctl note sketch.spd --json
snctl text Journal.note --format markdown
snctl text Journal.note --page 1,2 --json
snctl highlights Paper.pdf --format markdown
snctl note Journal.note --raw --json
```

`render` writes a whole notebook as PDF, a selected page as PNG/JPEG/SVG,
or every page as numbered images. `open` draws a private temporary preview
and hands it to the system viewer. `.mark` rendering overlays a sibling PDF (or `--source PDF`); without a source it produces annotation
sheets without the source PDF or EPUB; stderr reports this limitation.
Pass a PDF/EPUB document directly to use its unique full-name `.mark` sibling,
with the same pages, output names and options as passing the sidecar. EPUBs
retain standalone annotation sheets because EPUB overlay is unsupported.
Without annotations, `open` opens the original document in a private preview;
`open --no-marks` skips annotations. Both report the reason on stderr. `render`
without annotations fails with guidance to use `snctl get`; `render --no-marks`
is an argument error. Cloud inputs follow cache/`--offline` rules, and folder
exports remain limited to `.note`/`.mark`.
`--no-template`, `--no-text` and `--strict-images` control the background,
stored recognition layer and damaged image handling.

`text` prints typed text boxes and stored recognized handwriting. Recognition
text exists only in real-time-recognition notebooks; no OCR runs. `highlights`
accepts a PDF/EPUB document or its `.mark` and prints highlighted PDF text, color codes, comments, page numbers
and rectangles. Passage text is extracted from its sibling source PDF; EPUBs,
missing sources and unavailable extraction warn and report null text in JSON. `note` reports kind, version, device,
page geometry, templates, layers and content counts; `--raw` adds ordered
header/footer metadata. Atelier `.spd` drawings render to one PNG, PDF or SVG;
`note` reports their canvas dimensions, layers and tile counts. Painted bounds
are the default; `--drawing-page canvas` uses the saved viewport, whose centre
semantics are inferred from one real sample. See the [drawing caveats](docs/rendering.md#atelier-drawings).

Page numbers are the notebook's own numbers listed by `note`: sequential from 1
for `.note`, with gaps where damaged pages were skipped, and source-document
numbers for `.mark`. Page selectors, numbered image names and JSON fields use
those numbers; missing numbers report available ranges.
`render -o -` streams a PDF or one selected image, with notices on stderr.
Input selection follows the [notebook path rule](docs/notebooks.md).

See [Rendering](docs/rendering.md) for build requirements and limitations and
[Notebooks](docs/notebooks.md) for reading formats and page selection.

## Scripts

Without a terminal, commands that change the cloud require `--yes`: `put`, `mput`, `backup restore`, `mkdir`, `mv`, `rename`, `rm`, `restore`, `trash empty`, `tasks add/edit/done/undone/rm`, `tasks lists add/rename/rm`, `digests groups/tags/digests add/edit/rm`, `digests handwriting put`. Ad-hoc `mirror run --dir ... --folder ...` also requires `--yes`.
`login`, `logout` and `check --write` authorize their own operations; saved
mirror definitions authorize their later runs. `--dry-run` sends no changes and needs no confirmation.

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

## Built on

snctl uses sncloud-go for cloud access, snnote-go for notebooks and marks,
snspd-go for Atelier drawings and snrender-go for rendering.

## Acknowledgements

Brandon Philips’s supernote-typescript project contributed a test notebook.
The [fixture notes](testdata/notebooks/README.md) document its origin and licence.

## Documentation

| Guide | What it covers |
|---|---|
| [Examples](docs/examples.md) | Commands and their answers on a sample library. |
| [Configuration](docs/configuration.md) | Profiles, endpoints, secret stores and the offline cache. |
| [Completion](docs/completion.md) | Shell setup and cached cloud path completion. |
| [Output](docs/output.md) | Table, plain and JSON formats, confirmation and exit status. |
| [Library](docs/library.md) | Tablet paths, folder downloads, space and duplicate reports. |
| [Uploads](docs/uploads.md) | Upload plans, replacement, collisions and sync locking. |
| [Notebooks](docs/notebooks.md) | Recognition, text search, inspection and highlights. |
| [Rendering](docs/rendering.md) | PDF and image output, overlays, drawings and build requirements. |
| [Mirrors](docs/mirror.md) | Two-way file sync, annotated copies and service installation. |
| [Backups](docs/backups.md) | Incremental backups, retention and verified restoration. |
| [Watch](docs/watch.md) | Change announcements, filters, batching and handler input. |
| [Automation](docs/automation.md) | Bash jobs for backups, offline work, exports and reports. |
| [AppleScript](docs/applescript.md) | Finder, Mail, Notes, Preview and scheduled jobs on macOS. |
| [PowerShell](docs/powershell.md) | Windows scripts, Explorer integration and Task Scheduler. |
| [Releases](docs/releases.md) | Full and lite archives, runtime libraries and release procedure. |
| [Architecture](docs/architecture.md) | Package boundaries and implementation responsibilities. |

## License

snctl is distributed under the [MIT License](LICENSE). Release archives include
third-party notices under `licenses/`, including those for PDFium and the
MSYS2 runtime libraries on platforms that ship them.

## Trademark

Supernote is a trademark of Ratta Software. This project is not affiliated with,
endorsed by or sponsored by Ratta Software.

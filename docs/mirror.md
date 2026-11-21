# Mirrors

Back to the [README](../README.md).

A mirror keeps a local directory and a Supernote cloud folder in step, both ways.
PDFs and EPUBs keep their full names and extensions. New files, changes, renames,
moves and deletions follow the other way. With `--pull-new`, cloud notebooks are
also drawn as PDFs under `_annotated/`.

```sh
snctl mirror add papers ~/Papers /DOCUMENT/Document/Papers --pull-new
snctl mirror run papers --dry-run
snctl mirror run papers
snctl mirror service install
```

```text
~/Papers/
├── Report.pdf                 ⇄ DOCUMENT/Document/Papers/Report.pdf
├── Books/Novel.epub            ⇄ DOCUMENT/Document/Papers/Books/Novel.epub
├── _annotated/Journal.pdf      ← Journal.note, with pull_new = true
├── .snctl-removed/             ← retained local originals and drawings
├── .snctl-mirror.json          ← the journal of the last known state
└── .snctl-mirror.lock
```

## Defining mirrors

`mirror add <name> <dir> <cloud-folder> [--pull-new] [--no-delete]` appends a
`[[mirror]]` table to config.toml, preserving every other line. The local
directory must exist; the first real pass creates a missing cloud folder.
The cloud path accepts an optional leading `/`; definitions added by snctl store
a cleaned path with one leading `/`. Existing tables accept either form. The
path must be under `DOCUMENT/Document` or another
[writable root](uploads.md), including NOTE/Note and NOTE/MyStyle. The existing
cloud boundary has no file-type restriction by root; any server refusal stays
pending and is reported by the upload path. Mirrors sync PDF and EPUB
originals; `.note` files have the optional drawing path, and `.mark` sidecars
stay solely in the cloud. The existing upload type rule has no additional
round-trippable document formats.

Names are 1–64 lower-case letters, digits, underscores or hyphens, starting with
a letter or digit. `--profile/-P`, or `SNCTL_PROFILE` when supplied at add time,
binds the definition to that profile; otherwise it uses the run's profile.
Overlapping local directories are refused, as are overlapping cloud folders of
the same profile. An unbound profile is treated conservatively as potentially
overlapping any profile. Hand-edited overlapping definitions leave the later
one unrun. Adding a definition is consent for its future passes.

`mirror ls [--json]` reads only this machine. It shows tracked documents, the
last pass, pending work, the last error, and additions, changes or deletions
since the journal by local size and modification time.

`mirror rm <name>` removes that mirror's table, preserving other lines. Files,
cloud entries and the journal stay. Inline TOML arrays cannot be removed without
rewriting their surroundings and require a manual edit.

## A pass

`mirror run <name>...`, or `mirror run --all`, runs once and prints each action
and a final `pass` line. `--dry-run` plans without changing content or journal;
the advisory lock file may be created. `--dir D --folder F` selects an ad-hoc
mirror and requires confirmation, or `--yes`, for a real pass. Defined mirrors
need no further confirmation.

Cloud writes share one fresh re-check snapshot at the first write phase and one
verification snapshot after the pass's writes, outside the sync lock. Later
operations plan against that re-check view updated with the pass's own changes.
Before every move or trash, the affected source and destination parents are
listed afresh (one nonrecursive listing per distinct parent per operation).
Changes to ID, path, hash, size, modification time or NFC twins refuse that
operation; the next pass will re-plan. Creates/uploads retain the shared bound.
A post-write Unicode twin is reported as a warning: the write stays applied,
its journal entry is updated, and the pass continues. Like completed-drawing
warnings, these warnings add no pending work and do not fail an otherwise
successful pass (exit 0). A failed verification listing or another cause fails
the pass with a nonzero exit, while retaining the applied write and its journal
entry. Cancellation
retains its usual exit status. Separate-copy replacements still check
the uploaded copy before recycling the old one and may need extra safety reads.
If a cloud original changes before trashing, the report names the changed entry
and explains that the next pass will re-plan; the changed original is retained.

The journal is the truth: each entry follows a cloud identifier, its path and
content hash, and a local relative path, SHA-256, size and modification time.
Each pass reads a fresh cloud listing. A damaged journal stops work rather than
starting over silently. The journal binds the cloud folder's identity and path
and the resolved profile, endpoint and account. A folder renamed, moved, recycled or replaced in the
cloud is refused; when still present, the message names its current path.
Move the journal aside only when deliberately starting over.
The configured folder matches cloud folders using case folding and Unicode NFC;
ambiguous matches are refused. Transfers use the cloud folder's actual spelling,
while config.toml keeps the configured spelling.

| Action | Behavior |
|---|---|
| `upload` | Send a new local PDF or EPUB through the normal upload planner and sync lock. |
| `download` | Verify and bring down a cloud original; retain an older local version in `.snctl-removed/`. |
| `adopt` | Pair originals with equal bytes and names without transferring them, or resume a completed upload. |
| `update` | Replace the cloud original in place; use the normal verified replacement fallback if the server allocates another copy. |
| `conflict` | Keep local bytes under `name (local conflict <SHA-256 prefix>).ext` on both sides, then bring the cloud bytes down to the original name. |
| `move-local` | Follow a cloud rename or move inside the mirrored folder. |
| `move-cloud` | Follow an unambiguous local rename or move identified by SHA-256. |
| `trash` | Put an original deleted locally in the cloud recycle bin. |
| `remove` | Move a local original or notebook drawing into `.snctl-removed/` after its cloud entry disappears or moves out. |
| `draw` | Render a cloud notebook or PDF with its `.mark` into the corresponding `_annotated/` namespace with `pull_new = true`. |
| `detach` | Under `no_delete = true`, keep the other copy and stop following the entry. |
| `skip` | Leave unsettled files, ambiguous simultaneous renames and interrupted work needing intervention pending. |
| `refuse` | Leave a guarded deletion pending. |

Nothing is deleted outright. Original local files are never overwritten: a
verified download first retains the old version, then publishes without
replacing an occupied name. Occupied retained names get a number. The mirror
may replace only its own journal and notebook drawings locally. Cloud updates
use the normal `upload --replace` path: a server that overwrites in place keeps
the document's identifier and adds nothing to the recycle bin. The line says
“replaced in place”. If the server instead allocates another copy, the normal
fallback verifies its bytes, recycles the prior original after unlocking, and
moves the verified copy to the intended name. That line says “server allocated
another copy; recycled prior cloud copy”; its identifier changes. An interrupted
replacement records its intent before uploading and resumes from local bytes
or adopts the verified completed upload.
Names ending in ` (replacement <md5> N)` before the extension, where `<md5>`
is 32 hexadecimal digits and `N` is positive, are reserved for upload recovery.
The mirror skips these names both locally and in cloud listings, including
subdirectories. Ordinary names such as ` (replacement draft 1)` still sync.

Folder creation and uploads hold the account sync lock. After a refused
replacement upload, the fallback also lists the destination folder under that
lock to find and reuse an existing copy before uploading again. Some servers
hide newly committed copies until unlock, so a copy absent from that listing
can still lead to another reserved name; verified recycling waits until unlock. A rename first
creates missing destination folders under a short lock, releases it, then moves
the original before any new files are uploaded into the renamed directory.
Trash and fallback replacement recycling run without a sync session; deleting a folder’s
last original leaves the empty cloud folder in place. Successful cloud moves
are saved in the journal before later transfers. If a pass stops before that
save, the next listing and the original’s identifier recover its new path.
A failed upload is verified after the sync lock is released, as `put` is.
Supernote has no create-only upload primitive, so clients writing without the
sync lock can still race an upload; destination and duplicate checks report
such discrepancies and retain uploaded copies.

If both sides changed, both versions survive. If names were independently
changed to different paths on both sides, the mirror leaves the entry pending
and asks for the names to be reconciled. Case and Unicode NFC forms pair
without case; ambiguous duplicate names are refused rather than guessed.
Moves never replace a file that appeared meanwhile. Symbolic links are not
followed; directory access is confined to the opened root.

If an original was deleted locally while its cloud copy changed since the last
pass, the changed cloud copy comes down again under its current name. The line
reports “deleted here, changed there: restored”; nothing is recycled. Changed
content wins over deletion, just as a local edit survives a cloud deletion in
`.snctl-removed/`.

The mirror ignores dot names in any component, `_annotated/`, `.DS_Store`,
`Thumbs.db`, `desktop.ini`, `~$…`, `.part`, `.partial`, `.crdownload`, `.download`,
`.tmp`, `.temp`, `.swp` and names ending in `~`. Files written less than two
seconds ago wait for a later pass; the final line says “still being written;
run again in a few seconds” when such files are pending. A one-shot run returns
without waiting for a file that may still be changing. A local file renamed and edited at the same
time cannot be identified by its old hash; it is a deletion plus a new file.
Empty local directories have no separate cloud deletion semantics.

The persistent `.snctl-mirror.lock` takes the system's nonblocking advisory
lock. One process owns a directory at a time; watch retries contention.
The journal records completed changes at least every ten seconds during a long
pass and at its end, including cancellation. Replacement intent is written
before upload, and incoming-download intent before local retention. Its own actions
are journaled so a second pass has nothing to do; uploads need no notification
from the cloud, which does not announce the client's own changes.

### Annotations and notebooks

A document's tablet annotations live in a neighboring `.mark` sidecar. The
preferred match retains the full original name: `Report.pdf.mark` belongs to
`Report.pdf`. Stem-only names such as `Report.mark` (used by older exports) are
also accepted if exactly one PDF/EPUB original has that stem. When both
`Report.pdf` and `Report.epub` exist, a stem-only sidecar is ambiguous: the pass
warns and skips it. Full-name sidecars take precedence. Matching follows the
mirror's existing case and Unicode normalization rules.

The mirror never uploads, deletes or synchronizes sidecars as local files. With
`--pull-new`, it reads sidecars privately for drawing; without it, sidecar changes
do not touch originals. Only originals follow local renames; corresponding
sidecar renames belong on the tablet or in the cloud.

`--pull-new` draws each `.note` into `_annotated/notebooks/<path>.pdf` and each
PDF with a matching sidecar into `_annotated/documents/<original path>`.
For example, `Report.note` becomes `_annotated/notebooks/Report.pdf`, while
`Report.pdf` + `Report.pdf.mark` becomes `_annotated/documents/Report.pdf`.
Recognized `.note` notebooks also produce cleaned Markdown beside that PDF:
`_annotated/notebooks/Report.md`. It includes typed boxes and recognized text,
with page/title headings, using the [text cleaning rules](notebooks.md#text).
Recognition requires nonblank cleaned page recognition; header flags alone and
typed-only notebooks produce no Markdown. Each notebook revision updates both
outputs. Removing recognition retains the old Markdown under `.snctl-removed/`.
Renaming or removing a notebook retains its Markdown by the same rules as its
PDF, including `--no-delete` and the mass-deletion guard. `_annotated/` Markdown
is never uploaded. Both files are staged before publication, and their durable
journal intent resumes interrupted publication without rerendering. Older
journals whose PDFs are already current receive a one-time text-only inspection,
using a cached body first, without rendering the PDF again. Standard error reports
how many current notebooks received text. Legacy drawing paths still migrate by
rendering into the namespaced layout.

If both staged and published Markdown are missing during recovery, the intent is
abandoned and the notebook is redrawn. A persistent publication failure (for
example, a directory at the `.md` target) retains the intent and stages for retry,
just as a failed PDF publication does. Only that owner and its reserved output
names wait; unrelated drawings continue. Remove the obstruction and rerun to
publish the staged bytes. An existing untracked regular `.md` is retained under
`.snctl-removed/` before replacement. Dry runs report a conditional Markdown
retirement when a current companion would be removed if recognition disappeared;
they do not download bodies to decide that condition.

The journal remains version 2. Downgrading to a binary without text support can
drop the text fields when it rewrites the journal. Returning to this version
inspects current PDFs again to restore companion tracking; older drawing layouts
may require a redraw. Existing Markdown is retained before an untracked replacement.

The separate namespaces cannot collide, even for notebooks and PDFs of the same
stem. Existing pre-release notebook drawings recorded at the old path are
retained in `.snctl-removed/` after their replacement has been produced. Journal
version 2 records the namespaced layout; version 1 journals migrate automatically
when `--pull-new` is enabled. This includes notes in cloud folders named
`notebooks/` or `documents/`. A failed render or a renderless build leaves the
previous drawing live and pending for a later pass. Publication first stages the
completed replacement inside the confined mirror root and journals its intent.
Distinct old paths are retired after publication; colliding owners and case-only
aliases are retired immediately before the atomic replacement, with restoration
on publication failure. An interrupted publication resumes from its staged bytes.
Drawing failures remain pending for watch-mode retries while unrelated originals
and drawings continue. Each pending publication keeps its own durable intent;
only a journal write failure stops the pass. Failed restorations retain their
intent and report the affected path. If both the stage and a regular output are
missing, recovery restores available prior drawings, warns, and discards the
intent so rendering can retry. Each non-dry-run pass removes unreferenced regular
`.snctl-drawing-*` files immediately inside the mirror root.
Dry-run reports both the drawing and its pending move-aside.

Drawings download by each listed entry's identifier and MD5 through the cache,
without resolving every cloud path again. Only selected drawing inputs are
retained from mirror downloads; ordinary mirrors do not populate the body cache. PDF drawings are journaled against
both the original and sidecar identities and hashes; changing either redraws
them. Rename, original removal and sidecar removal retain old drawings under
`.snctl-removed/`. `_annotated/` is never uploaded. Dry runs report drawing and
retention actions without downloading or writing them. A build without rendering
leaves drawings pending with the existing capability error while still mirroring
originals. EPUB annotation overlays are unsupported: the pass warns and skips
those drawings while continuing to mirror EPUB originals. Incomplete or
case-duplicate sidecars skip only the affected drawing with a warning. Ambiguous
legacy sidecars warn once per change of the ambiguity, including after a restart.
A failed or skipped document reconciliation defers its drawing until a later pass;
drawing never changes the original document's reconciliation markers.

Dropped annotation sheets/highlights and mismatched source geometry are warnings
on the completed draw action, in both text and the optional JSON `warnings`
array. They do not fail the pass. Signature-bearing sources also produce a
warning; snctl does not validate signatures. A matching filename cannot prove
that a sidecar belongs to that revision of the PDF.

### The mass-deletion guard

Deletions on either side are refused when more than 50 would run, when at least
two exceed half the tracked documents, or when their source side is empty.
This catches unmounted drives and incomplete synchronization. One deletion out
of one or two normally goes through, subject to the empty-source guard.
`--force-delete` deliberately overrides these checks. `--no-delete` instead
keeps the surviving copy and detaches the entry. A missing local directory
fails before comparison.

## Keeping in step

`mirror run --all --watch` runs until interruption (exit 130).

- fsnotify recursively watches existing and newly created directories. When
  watching fails, exceeds the descriptor budget or loses events, a full pass
  follows and a names/sizes/times scan runs every `--poll` (3s). The native
  watcher is retried every `--rescan` (15m).
- A pass follows changes after `--debounce` (2s), capped at 30s of continuous
  changes. Unsettled files receive another pass after two seconds.
- The cloud change feed triggers passes. Repeated identical announcements
  within five minutes are suppressed without another listing. Connection and
  reconnection trigger a full pass. Full rescans also catch missed events,
  stopped feeds and a machine waking from sleep.
- Failures of a whole pass retry after 5s, doubling up to 5m. Individual failed
  steps wait for the next pass. One mirror's failure does not stop another.
- With `--all`, config.toml is reread every 30s: additions start, removals stop,
  and changed definitions restart.

## The service

`mirror service install` installs and starts the current snctl executable as
`snctl mirror run --service` for the current user. Install again after moving
or upgrading the executable or changing configuration paths. A Homebrew Cellar
path uses an existing stable `opt` link.

| System | Definition |
|---|---|
| macOS | launchd login job, `~/Library/LaunchAgents/io.altessa.snctl.mirror.plist`, restarted after failure. |
| Linux | systemd user unit, `$XDG_CONFIG_HOME/systemd/user/snctl-mirror.service`, or `~/.config/systemd/user/…`. Installation refuses without an answering user session. |
| Windows | Task Scheduler logon task `snctl-mirror`, with its XML beside the log; runs through headless conhost, hidden, restarted after failure. |

The definition saves the resolved configuration paths and selected profile, and keeps `SNCTL_CONFIG_DIR`, `SNCTL_CACHE_DIR`, `SNCTL_CONFIG_FILE`,
`SNCTL_PROFILE`, `SNCTL_SECRETS`, `SNCTL_KEYCHAIN_SERVICE`, `SNCTL_CACHE_TTL` and
`SNCTL_CACHE_BUDGET` when set. Windows passes these as explicit saved service
arguments because tasks have no private environment block. These settings
contain backend names and paths, never login credentials.

`mirror service status` reports installation and running state.
`mirror service uninstall` stops the job and removes its definition; mirrored
files and logs stay. On Linux the unit normally stops at logout; the owner can
choose `loginctl enable-linger $USER` to keep a user manager after logout.

The service writes plain tab-separated actions to `<config dir>/logs/mirror.log`,
rotated at 5 MiB with three older logs retained.
`mirror service logs [-n N] [-f]` prints the last 50 lines by default and follows
new lines with `-f`. On macOS early process output goes to
`mirror-service.out` beside the log; on Linux it goes to the user journal.

## JSON

`mirror run --json` streams one object per action and final pass: `time`,
`mirror`, `action`, `path`, `to`, `document`, `detail`, `error`, `dry_run` and,
for a pass with pending work, `pending`. `mirror ls --json` includes the
specification, `tracked`, `last_pass`, `last_error`, `pending`, `local_changes`
and any local `error`.

On filesystems without hard links, moves stage a hidden temporary sibling,
sync its bytes, then install the complete file before removing the source.
Linux and macOS use an exclusive atomic rename; other platforms check that
the destination is absent before renaming under the mirror lock (uncoordinated
external writers can race that check). A crash can leave a hidden temporary
or two complete copies, but never publishes a partial copy at the final name.
Special files (including FIFOs, sockets and devices) are refused in mirror
and backup output paths.

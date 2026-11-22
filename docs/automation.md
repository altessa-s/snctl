# Automation

Back to the [README](../README.md).

These Bash jobs use an authenticated profile. The commands were exercised with
a full build and synthetic files on a local Supernote Private Cloud. Each
Bash script was extracted, run against that account and checked with ShellCheck. Scheduler
definitions are installation examples. Save each script under the indicated name,
make it executable, and run it once from the account that will schedule it.

## Contents

- [Ground rules for scripts](#ground-rules-for-scripts)
- [Working offline](#working-offline)
- [Nightly backup](#nightly-backup) and [restoring](#restoring)
- [A hot folder](#a-hot-folder)
- [Notebooks to Markdown](#notebooks-to-markdown)
- [Highlights to Markdown](#highlights-to-markdown)
- [Reacting to changes](#reacting-to-changes) and [keeping watch running](#keeping-watch-running)
- [Rendering a folder](#rendering-a-folder)
- [Mirroring annotated PDFs](#mirroring-annotated-pdfs)
- [Library reports](#library-reports)
- [Task and digest snapshot](#task-and-digest-snapshot)
- [A second account](#a-second-account)
- [CI and retries](#ci-and-retries)

## Ground rules for scripts

- Ask for `--json` when selecting fields with jq. Most lists return one array.
  Watch streams one object per line. Plain tables use tab-separated fields
  without headers. Keep diagnostics on stderr.
- Address live files by tablet path, for example `NOTE/Note/Work/Journal.note`.
  `ls --ids` shows file IDs, but live file commands do not accept ID prefixes.
  Recycle-bin restore takes the ID from `trash ls`. Task and digest commands
  have their own identifiers.
- Preview uploads and restores with `--dry-run`. Use `--yes` for an
  unattended cloud write. Downloads, renders and backups can write local files
  without asking. A saved mirror definition authorizes its later passes.
- Log in before scheduling. Select the account with `SNCTL_PROFILE` or override
  it with `-P name`. Use absolute binary and output paths in a scheduler.
  Set `SNCTL_CONFIG_DIR` and `SNCTL_CACHE_DIR` if the job needs non-default
  roots. Its user must have access to the selected secret store.
- Refresh the listing online. Use `sync FOLDER --files` to retain the bodies
  as well, then read with `--offline`. Allow enough cache space for the scope.

| Exit | Meaning |
|---|---|
| 0 | Success |
| 1 | Operation failed or confirmation declined |
| 2 | Invalid arguments or missing confirmation in a pipe |
| 3 | Missing or expired session |
| 4 | Conflict, changed write plan, busy sync lock or guarded deletion |
| 5 | Required native notebook or rendering capability unavailable |
| 6 | Write completed but destination verification failed. Inspect before retrying. |
| 130 | Interrupted (SIGINT) |
| 143 | Terminated (SIGTERM) |

See [output](output.md) for confirmation rules and mirror verification warnings.

## Working offline

Run `sn-offline.sh prepare NOTE/Note/Work` before disconnecting. Later,
`sn-offline.sh read NOTE/Note/Work /absolute/path/to/downloads` downloads from
that profile's cache. A missing or evicted body makes the read fail.

```bash
#!/usr/bin/env bash
set -euo pipefail
case ${1:?prepare or read} in
  prepare) exec snctl sync "${2:?cloud folder}" --files ;;
  read) exec snctl --offline mget "${2:?cloud folder}" -o "${3:?directory}" --incremental ;;
  *) exit 2 ;;
esac
```

## Nightly backup

`~/bin/sn-nightly.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
export PATH="${SN_JOB_PATH:-/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin}"
export SNCTL_CACHE_TTL=0
exec snctl -P "${SNCTL_PROFILE:-work}" backup "${1:-$HOME/Backups/Supernote}" "${2:-/}" --jobs 4 --format plain
```

Set `SN_JOB_PATH` if the executable is elsewhere. Replace the sample home
and binary paths below before loading a job. The first run copies every live file. Later runs verify local bytes and download
only changes. Deleted files accumulate in `removed/`; add `--prune` only when you
want deleted cloud files removed from the active backup without retention.
Choose a separate directory for each profile and scope. See [backups](backups.md)
for bindings and interruption behavior.

On macOS, save `~/Library/LaunchAgents/io.altessa.snctl.backup.plist` and load it
with `launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/io.altessa.snctl.backup.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>io.altessa.snctl.backup</string>
  <key>ProgramArguments</key><array><string>/Users/you/bin/sn-nightly.sh</string></array>
  <key>StartCalendarInterval</key><dict><key>Hour</key><integer>2</integer><key>Minute</key><integer>15</integer></dict>
  <key>StandardOutPath</key><string>/Users/you/Library/Logs/snctl-backup.log</string>
  <key>StandardErrorPath</key><string>/Users/you/Library/Logs/snctl-backup.log</string>
</dict></plist>
```

On Linux, `~/.config/systemd/user/snctl-backup.service`:

```ini
[Unit]
Description=Back up Supernote files
[Service]
Type=oneshot
ExecStart=%h/bin/sn-nightly.sh
```

`~/.config/systemd/user/snctl-backup.timer`:

```ini
[Unit]
Description=Nightly Supernote backup
[Timer]
OnCalendar=*-*-* 02:15:00
Persistent=true
[Install]
WantedBy=timers.target
```

Enable with `systemctl --user enable --now snctl-backup.timer`. Run
`loginctl enable-linger "$USER"` once to keep the timer and other user units
running while logged out. Cron is another
option (logging goes to the named file):

```cron
PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin
15 2 * * * /home/you/bin/sn-nightly.sh >> /home/you/snctl-backup.log 2>&1
```

## Restoring

Inspect the plan before a restore. This recreates the recorded tree below a new
writable folder, leaves existing different files alone, and skips identical bytes:

Run `sn-restore.sh ~/Backups/Supernote NOTE/Note/Recovered` for the plan.
Set `APPLY=1` to apply it.

```bash
#!/usr/bin/env bash
set -euo pipefail
mode=--dry-run
if [[ ${APPLY:-0} == 1 ]]; then mode=--yes; fi
exec snctl backup restore "${1:?backup directory}" "${2:?destination folder}" "$mode"
```

With the CLI, omit the cloud folder to use original locations. `--replace` explicitly permits
replacement at existing destinations. Restoration stages and checks every
recorded file before writing, then uses the account sync lock; partial successes
remain if a later upload fails. Repeat the restore to skip completed matching
files. A whole library may include read-only roots: restore those files below a
new writable folder instead. Backups are portable across profiles; identifiers
in their manifest are historical and the server allocates uploaded identities.

## A hot folder

Run `sn-hot.sh /absolute/inbox DOCUMENT/Document/Reading` periodically with
the same scheduler as the backup. Have the producer rename finished files
into the inbox after closing them. Keep one job running at a time.
`DRY_RUN=1` leaves the inbox untouched. Existing names require attention.

```bash
#!/usr/bin/env bash
set -euo pipefail
export PATH="${SN_JOB_PATH:-/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin}"
in=${1:?absolute inbox}
dest=${2:-DOCUMENT/Document}
shopt -s nullglob nocaseglob
for f in "$in"/*.pdf "$in"/*.epub; do
  [[ -f $f && ! -L $f ]] || continue
  if [[ ${DRY_RUN:-0} == 1 ]]; then
    snctl put --to "$dest" --dry-run -- "$f"
  else
    mkdir -p "$in/Sent"
    [[ ! -e "$in/Sent/${f##*/}" ]] || { echo "Already archived: $f" >&2; exit 1; }
    snctl put --to "$dest" --yes -- "$f"
    mv "$f" "$in/Sent/"
  fi
done
```

## Notebooks to Markdown

Run `sn-vault.sh NOTE/Note/Work ~/Supernote-originals ~/Vault/Work`.
Reserve the originals directory for this source. The vault keeps the same
relative names, including subfolders. Each note contains typed text and any
handwriting recognition stored on the tablet. Folder-mode `text` selects
recognized notebooks only. This script reads each local notebook individually
so typed-only notebooks are included. A checksum beside each export records
the last successful conversion. Removing a cloud notebook leaves its export.

```bash
#!/usr/bin/env bash
set -euo pipefail
src=${1:?cloud folder}
raw=${2:?local originals}
vault=${3:?vault folder}
status=0
report=$(snctl mget "$src" -o "$raw" --incremental --json) || status=$?
# Use the safe local names assigned by mget, including collision suffixes.
jq -j '.items[] | select(.status != "failed" and .local and (.local | ascii_downcase | endswith(".note"))) |
  .local, "\u0000"' <<<"$report" |
while IFS= read -r -d '' file; do
  rel=${file#"$raw"/}
  md="$vault/${rel%.*}.md"
  sum=$(shasum -a 256 < "$file")
  if [[ -f $md && -f $md.sha256 && $(cat "$md.sha256") == "$sum" ]]; then continue; fi
  mkdir -p "$(dirname "$md")"
  snctl text "$file" --format markdown -o "$md"
  printf '%s\n' "$sum" > "$md.sha256"
done
exit "$status"
```

If a download fails, the script still exports the notebooks that mget read
successfully and returns the mget failure status.

Use absolute paths without trailing slashes. For a new source folder, choose
a new originals directory. Delete a checksum to force an export.

## Highlights to Markdown

Run `sn-highlights.sh DOCUMENT/Document/Reading/Paper.pdf ~/Vault/Paper.md`.
The sibling `Paper.pdf.mark` supplies page numbers, comments and rectangles.
The full build also reads passages from the source PDF. EPUBs retain the
geometry and comments. Warnings about unavailable passages go to stderr.

```bash
#!/usr/bin/env bash
set -euo pipefail
dest=${2:?Markdown file}
mkdir -p "$(dirname "$dest")"
tmp=$(mktemp "$dest.XXXXXX")
trap 'rm -f "$tmp"' EXIT
snctl highlights "${1:?document path}" --format markdown > "$tmp"
mv "$tmp" "$dest"
```

## Reacting to changes

`watch --exec` passes batches on stdin and event metadata through environment
variables. It does not interpolate cloud names into commands. This handler reconciles a folder of PDFs whenever the batch contains a
notebook change or reports dropped events. Folder rendering preserves safe
local names and its incremental record skips completed, unchanged notebooks.

`~/bin/sn-on-change.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
if jq -e '.dropped > 0 or any(.events[]; .path | ascii_downcase | endswith(".note"))' >/dev/null; then
  snctl -P "${SNCTL_PROFILE:?}" render "${SN_RENDER_SCOPE:-NOTE/Note}" \
    -o "${SN_RENDER_DIR:-$HOME/Supernote PDFs}" --incremental
fi
```

Start it with `snctl -P work watch --exec ~/bin/sn-on-change.sh --only file --under /NOTE/Note`.
The handler needs jq and a full build. Deleted items have no current path.
Old PDFs remain. A paired PDF supplies the background for a rendered mark;
EPUB marks produce standalone annotation sheets.

Announcements can repeat or go missing during disconnects. Schedule the
same folder render periodically. The dropped count covers local truncation
only. Filters exclude connection events. An unfiltered handler can also
reconcile on `connected`. See [watch](watch.md) for queue limits and timeouts.

### Keeping watch running

Watch reconnects on retryable socket failures. A service manager can restart it
after a fatal error or start it at login. On macOS,
`~/Library/LaunchAgents/io.altessa.snctl.watch.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>io.altessa.snctl.watch</string>
  <key>ProgramArguments</key><array>
    <string>/opt/homebrew/bin/snctl</string><string>-P</string><string>work</string>
    <string>watch</string><string>--exec</string><string>/Users/you/bin/sn-on-change.sh</string>
    <string>--only</string><string>file</string><string>--under</string><string>/NOTE/Note</string>
  </array>
  <key>EnvironmentVariables</key><dict><key>PATH</key><string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string></dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>60</integer>
  <key>StandardOutPath</key><string>/Users/you/Library/Logs/snctl-watch.log</string>
  <key>StandardErrorPath</key><string>/Users/you/Library/Logs/snctl-watch.log</string>
</dict></plist>
```

On Linux, `~/.config/systemd/user/snctl-watch.service`:

```ini
[Unit]
Description=Render Supernote notebooks as they change
After=network-online.target
[Service]
Environment=PATH=/usr/local/bin:/usr/bin:/bin
ExecStart=/usr/local/bin/snctl -P work watch --exec %h/bin/sn-on-change.sh --only file --under /NOTE/Note
Restart=always
RestartSec=60
TimeoutStopSec=10
[Install]
WantedBy=default.target
```

Enable with `systemctl --user enable --now snctl-watch.service`. To keep the
watcher running while logged out, run `loginctl enable-linger "$USER"` once.
SIGTERM permits
the same child grace period and exits 143; Ctrl-C exits 130. Set service stop
budgets above the five-second grace period.

## Rendering a folder

Run `sn-render.sh NOTE/Note/Work ~/Archive/Work` after a sync, or nightly.
The full build exports a PDF per notebook and mark. Serve the PDFs from the
output tree. For an individual PNG see [examples](examples.md#rendering).

```bash
#!/usr/bin/env bash
set -euo pipefail
exec snctl render "${1:?cloud folder}" -o "${2:?PDF directory}" --incremental
```

## Mirroring annotated PDFs

Create an empty local directory, then run
`sn-papers.sh ~/Papers DOCUMENT/Document/Reading` from cron.
Review the plan with `DRY_RUN=1`. The mirror syncs original PDFs and EPUBs
in both directions. With the full build it also draws PDF and mark pairs
under `_annotated/documents/`. Send readers that annotated copy to include
the tablet ink. Local changes to originals can upload. This job leaves
deletions on the side where they occurred. See [mirrors](mirror.md).

```bash
#!/usr/bin/env bash
set -euo pipefail
export PATH="${SN_JOB_PATH:-/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin}"
mode=--yes
if [[ ${DRY_RUN:-0} == 1 ]]; then mode=--dry-run; fi
exec snctl mirror run --dir "${1:?absolute local directory}" --folder "${2:?cloud folder}" \
  --pull-new --no-delete "$mode"
```

## Library reports

Run `sn-report.sh DOCUMENT/Document 2026-09-01` to report files modified
before that calendar date (using the date in each listing timestamp).
Capacity and usage are account-wide bytes. Duplicate groups concern the scope.

```bash
#!/usr/bin/env bash
set -euo pipefail
scope=${1:-/}
cutoff=${2:?YYYY-MM-DD}
snctl du "$scope" -d 2 --format table
snctl dupes "$scope" --format table
snctl tree "$scope" --json | jq -r --arg cutoff "$cutoff" '
  .[] | select(.folder == false and .modified and .modified[:10] < $cutoff) | .path'
snctl account --json | jq '{used,total}'
```

## Task and digest snapshot

Run `sn-snapshot.sh ~/Backups/reading.json` after the nightly file backup.
This separate report keeps the task and digest records returned by the two
list commands. They are read sequentially. Handwritten digest attachments
require a separate download. File backup restore does not import these records.

```bash
#!/usr/bin/env bash
set -euo pipefail
dest=${1:?output JSON}
mkdir -p "$(dirname "$dest")"
tmp=$(mktemp -d "$dest.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
snctl tasks ls --json > "$tmp/tasks"
snctl digests digests ls --json > "$tmp/digests"
jq -n --slurpfile t "$tmp/tasks" --slurpfile d "$tmp/digests" \
  '{tasks:$t[0],digests:$d[0]}' > "$tmp/report"
mv "$tmp/report" "$dest"
```

## A second account

Log in to both profiles first. Run
`sn-copy.sh work NOTE/Note/Work personal NOTE/Note/Imported /absolute/staging`.
Keep a dedicated backup for the source. Backup preserves original bytes,
including document sidecars. Restore places paths relative to the source
scope under the destination. Preview first with `DRY_RUN=1`.

```bash
#!/usr/bin/env bash
set -euo pipefail
stage=${5:?dedicated backup directory}
snctl -P "${1:?source profile}" backup "$stage" "${2:?source folder}"
mode=--yes
if [[ ${DRY_RUN:-0} == 1 ]]; then mode=--dry-run; fi
snctl -P "${3:?destination profile}" backup restore "$stage" "${4:?destination folder}" "$mode"
```

## CI and retries

Run jobs as the user that owns the profile, or provision its configuration
and file-backed credentials through the runner's secret store. Keep the cache
private. Avoid printing `account --json` or credential files to the job log.
For a bounded retry of a repeatable, incremental upload, run
`sn-upload.sh /absolute/ready DOCUMENT/Document/Reading`. It replans on
exit 4, including busy locks. A persistent name conflict still fails. Exit
6 requires inspection of the destination. Preview with `DRY_RUN=1`.

```bash
#!/usr/bin/env bash
set -euo pipefail
mode=--yes
if [[ ${DRY_RUN:-0} == 1 ]]; then mode=--dry-run; fi
for delay in 1 2 4; do
  if snctl mput "${1:?local tree}" "${2:?cloud folder}" --incremental "$mode"; then
    exit 0
  else
    status=$?
  fi
  if [[ $status != 4 || $delay == 4 ]]; then exit "$status"; fi
  sleep "$delay"
done
```

# Automation

Back to the [README](../README.md).

Configure a profile with file credentials or an already available secret store
before scheduling work. Jobs do not log in interactively. Set absolute binary,
configuration, cache and output paths; a service manager has a smaller environment
than your terminal. Every cloud write below uses `--yes` or `--dry-run`.

## Nightly backup

`~/bin/sn-nightly.sh`:

```sh
#!/bin/sh
set -eu
export PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin
export SNCTL_CONFIG_DIR="$HOME/.config/snctl"
export SNCTL_CACHE_DIR="$HOME/.cache/snctl"
export SNCTL_CACHE_TTL=0
exec snctl -P work backup "$HOME/Backups/Supernote" --jobs 4 --format plain
```

The first run copies every live file. Later runs verify local bytes and download
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

Enable with `systemctl --user enable --now snctl-backup.timer`. Cron is another
option (logging goes to the named file):

```cron
15 2 * * * /home/you/bin/sn-nightly.sh >> /home/you/snctl-backup.log 2>&1
```

## Restoring

Inspect the plan before a restore. This recreates the recorded tree below a new
writable folder, leaves existing different files alone, and skips identical bytes:

```sh
snctl -P work backup restore ~/Backups/Supernote NOTE/Note/Recovered --dry-run
snctl -P work backup restore ~/Backups/Supernote NOTE/Note/Recovered --yes
```

Omit the cloud folder to use original locations. `--replace` explicitly permits
replacement at existing destinations. Restoration stages and checks every
recorded file before writing, then uses the account sync lock; partial successes
remain if a later upload fails. Repeat the restore to skip completed matching
files. A whole library may include read-only roots: restore those files below a
new writable folder instead. Backups are portable across profiles; identifiers
in their manifest are historical and the server allocates uploaded identities.

## Reacting to changes

`watch --exec` passes batches on stdin and event metadata through environment
variables. It does not interpolate cloud names into commands. This handler
renders every changed `.note` still present in the listing to a PDF under the
same folder tree. It selects notebook paths rather than numeric identifiers,
because snctl addresses live files by canonical tablet path.

`~/bin/sn-on-change.sh`:

```bash
#!/usr/bin/env bash
set -uo pipefail
out="${SN_RENDER_DIR:-$HOME/Supernote PDFs}"
batch=$(cat)
if [[ ${SNCTL_DROPPED:-0} -gt 0 ]]; then
  echo "missed $SNCTL_DROPPED local events; schedule a full folder render" >&2
fi
jq -r '.events[] | select(.kind == "file" and (.path | endswith(".note"))) | .path' <<<"$batch" |
  sort -u |
  while IFS= read -r path; do
    pdf="$out${path%.note}.pdf"
    mkdir -p "$(dirname "$pdf")"
    snctl -P "$SNCTL_PROFILE" render "$path" -o "$pdf" --json </dev/null >/dev/null
  done
```

```sh
snctl -P work watch --exec ~/bin/sn-on-change.sh --only file --under /NOTE/Note
```

The handler needs a build with rendering support and `jq`. It receives names
and paths unmasked. Deleted items have no current path, so this example leaves
previous PDFs in place. It draws only original notebooks; `.mark` annotation
sheets need separate handling because rendering does not include their source
PDF. See [watch](watch.md) for queue limits, timeouts and environment fields.

Announcements can repeat or be missed during disconnects. In addition to the
handler, schedule a periodic reconciliation such as
`snctl -P work render NOTE/Note -o ~/Supernote-PDFs --incremental`.
The dropped count covers local truncation only. With filters, connection events
do not invoke the handler. An unfiltered handler can react to `connected` by
scanning the complete library.

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

Enable with `systemctl --user enable --now snctl-watch.service`. SIGTERM permits
the same child grace period and exits 143; Ctrl-C exits 130. Set service stop
budgets above the five-second grace period.

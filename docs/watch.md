# Running a program on changes

Back to the [README](../README.md).

`snctl watch --exec PROGRAM [-- ARG...]` follows Supernote announcements, prints
its usual event stream, and runs a program for each batch.

- **Batches.** `--debounce` defaults to 2s from the first event in a batch.
  Runs happen one at a time in order. Up to 16 batches wait; another batch joins
  the last waiting one. Each pending or merged batch keeps at most the newest
  1000 events. Older events are counted in `SNCTL_DROPPED` and JSON `dropped`,
  and noted once per batch on stderr. A batch with a nonzero dropped count
  always runs, even if filters leave it empty. Rescan the library when this
  count is nonzero.
- **Repeats.** Identical announcements within a batch collapse by raw type,
  kind, ID and path. Across batches, `--repeat-window` defaults to 5m and
  suppresses the same announcement after its batch has been resolved, including
  announcements excluded by `--only` or `--under`. A failed listing remains
  retryable.
  Suppressed repeats do not extend that window; `--repeat-window 0` disables
  suppression across batches. This can also suppress a genuine change with
  identical announcement fields during the window, so handlers should reconcile
  current state. An entirely suppressed batch makes no run and no listing.
  `connected` events and dropped counts are unaffected. Plain `watch` and the
  printed stream with `--exec` remain raw protocol views; the window applies
  only to execution and requires `--exec`.
- **Arguments.** No shell or interpolation is used. Arguments after `--` go
  directly to the executable. `--shell` explicitly selects `sh -c`; arguments
  after `--` then become `$0`, `$1`, and so on. Cloud names never enter argv.
- **Input.** The child receives the last kept event in `SNCTL_EVENT`,
  `SNCTL_ID`, `SNCTL_NAME`, `SNCTL_PATH`, `SNCTL_KIND`, `SNCTL_TIME`, plus
  `SNCTL_COUNT`, `SNCTL_DROPPED` and `SNCTL_PROFILE`. Each variable replaces
  inherited entries of the same name, case-insensitively on Windows. The batch
  is JSON on stdin:

  ```json
  {"profile":"default","dropped":0,"events":[{"time":"2026-11-14T12:00:00Z","event":"MODIFYFILE","id":"123","name":"Journal.note","path":"/NOTE/Note/Journal.note","kind":"file"}]}
  ```

  Time is local receipt time in UTC. Paths have a leading `/`. Names are
  unmasked. An item absent from the current file listing has empty name, path
  and kind. An empty filtered batch has empty last-event variables.
- **Filters.** `--only file|folder` and `--under /NOTE/Note/Work` select events
  from one fresh device listing per batch, saved through the profile cache.
  `--under` includes the named folder itself and its descendants, using Unicode NFC equivalence and case-sensitive path components.
  Event paths retain the device’s original spelling. An event whose item is missing passes only with neither
  filter. `SNCTL_COUNT` counts kept events; dropped counts are before filtering.
  If listing fails, an unfiltered batch still runs without descriptions; a
  filtered batch runs empty only if it lost events.
- **Runs.** `--exec-timeout` defaults to 5m. Child stdout and stderr go to
  watch's stderr; child failures and timeouts are noted and watch continues.
  Ctrl-C gives a current run 5 seconds to finish, then stops it, discards waiting
  batches and exits 130. Unix runs use a process group; Windows runs use a
  Job Object, so cancellation also stops descendants. On Unix cancellation
  sends SIGTERM to the group, then forcibly stops remaining processes after
  the command's wait delay. Commands run outside the terminal foreground
  process group: interactive reads from /dev/tty (such as ssh or git prompts)
  can stop with SIGTTIN until timeout; use noninteractive commands. Background descendants
  are also stopped when the command finishes. A Unix program that
  deliberately creates a new session or process group escapes that group. Feed errors still stop watch. Watch requires online
  access; backup and render can be used separately offline.

```sh
snctl watch --exec ~/bin/sn-on-change.sh --only file --under /NOTE/Note/Work
snctl watch --shell --exec 'jq -r ".events[].path" >> ~/sn-changes.log'
```

## What Supernote announces

The unchanged printed event fields are `type`, `kind`, `id`, `path`. The adapter
expands each feed announcement into individual changes. For execution,
`event`/`SNCTL_EVENT` is the raw announcement kind, or the type when no kind
exists. `kind`/`SNCTL_KIND` instead describes the current REST item (`file` or
`folder`). The supported announcement kinds are:

| Type | Event kinds |
|---|---|
| `files` | `DOWNLOADFILE`, `ADDFOLDER`, `MODIFYFILE`, `MODIFYFOLDER`, `DELETEFILE`, `DELETEFOLDER`, `COPYFILE`, `COPYFOLDER`, `MOVEFILE`, `MOVEFOLDER`, `WAITING`, `STARTSYNC` |
| `tasks` | `QUERY`, `SORT` |
| `digests` | `ADD_DIGEST`, `UPDATE_DIGEST`, `DELETE_DIGEST` |
| `connected` | `connected` (new connection, including reconnect) |

Task and digest IDs do not establish file paths; unfiltered handlers can inspect
these raw announcements. File filters exclude them; their IDs are not resolved against the file namespace.
On Private Cloud 26.10.09, changes are not announced to the client that made
them. Use a second profile to watch your own writes. Other clients receive
identical announcements about every 7 seconds, observed roughly 40 times for
the `files STARTSYNC` and `files DOWNLOADFILE` pair even after acknowledgement.
The execution repeat window limits these repeated runs and listings. The
adapter drops device identity. There is no reliable portable `this_device`
claim for these events: JSON and environment deliberately omit it. Do not use
the feed as an audit log of this process's own writes.

The library reconnects after clean disconnects and retryable transport/HTTP
failures with backoff. Authentication failures, rejected announcements and
malformed protocol responses end the stream. Announcements are hints, can
repeat, and have no replay cursor or completeness guarantee. `connected` means
reconcile REST state after a possible gap. It runs an unfiltered handler, but
file/path filters exclude it; filtered automation needs a periodic full scan.
`SNCTL_DROPPED` accounts only for local queue truncation, not missed server
notifications. See [automation](automation.md) for handlers and service setup.

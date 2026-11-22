# Output

Answers go to stdout. Prompts, errors and verbose commentary go to stderr.
Commands construct one typed result with a JSON view and a document for people;
they do not lay out tables themselves.

## Formats

`--format table|plain|json` chooses the answer format.
`-o`/`--output` is a destination path on file-producing commands.
`text` remains an alias for `plain`. `--json` chooses JSON and must
agree with an explicit answer format. Explicit flags win over `SNCTL_FORMAT`; otherwise a terminal gets
a table and a pipe gets plain lines. Invalid output or color flags are rejected before
any local mutation.

Plain output uses tab-separated fields without headings for tables, `key<TAB>value`
lines for records, and a terminating newline. A TAB, line break or backslash
inside a value is written as `\t`, `\n`, `\r` or `\\`, so each field stays on its
line; a Windows path appears with doubled backslashes. JSON answers are indented and use stable
snake_case field names, preserves integer precision and has a terminating
newline. Empty collections are `[]`, not `null`. Table output uses display
widths, so combining characters and wide names align.

`--color auto|always|never` selects ANSI styling. Auto needs an output terminal
that supports color and neither `NO_COLOR` nor `TERM=dumb`. Color never changes
JSON values. `--verbose` reports configuration loading on stderr. `--quiet` hides progress
lines only, including transfer, notebook reading and drawing progress. JSON
answers suppress progress too, whether selected by `--json`, `--format json`
or `SNCTL_FORMAT=json`. Prompts, warnings, errors and verbose commentary are
still written to stderr; `--quiet` does not confirm a change.

Secret values never appear in status, migration or profile output. JSON masking
helpers redact `accessToken`, `account`, `clientID`, passwords and authorization
fields at any depth; name masking is separate from secret redaction. These
helpers are not a license to print raw secret-store data.

## Confirmation and exit status

One rule, rmctl's: a command that changes the cloud asks first — `mkdir`,
`put`, `mput`, `mv`, `rename`, `rm`, `restore`, `trash empty`, every task and
digest change, handwriting upload — and so do `profile add`, `profile del` and
`secrets migrate`. A bulk change (`mput`, `put` of several files) shows its plan
on a terminal before the question, and the question counts the uploads,
replacements and folders. Only `y` or `yes` confirms. `--yes` bypasses the
prompt; outside a terminal a change without it fails with exit 2 before any
request that writes. `--dry-run` sends nothing and needs no answer. Reading, and
writing local files (`get`, `mget`, `backup`, `render`), never asks; `mget --delete`
removes only files snctl recorded and is held back by its own guard instead.
`login` and `logout` act on the session, and `check --write` is itself the
authorization of its scoped self-test. A saved mirror definition authorizes
its future passes; ad-hoc mirror runs ask before changing files.
The first Ctrl-C cancels the command and reports the interruption. A second
Ctrl-C forces exit. Services are released when a normal command returns.

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Operation failed or confirmation declined |
| 2 | Usage error, including an unconfirmed nonterminal write |
| 3 | Missing or expired cloud session |
| 4 | Destination conflict (replacement requires `--replace`), a refused `mget --delete`, or the account sync lock held by another device |
| 5 | Notebook reading or rendering is not compiled in |
| 6 | A file write happened, but post-write verification found an NFC twin or could not list the destination; no rollback. Mirror reports verification twins as warnings with exit 0 (see [mirror](mirror.md#a-pass)) |
| 130 | Interrupted by SIGINT |
| 143 | Terminated by SIGTERM |

## Cloud output

File listings carry `id`, `name`, `path`, `folder`, `size`, `hash`, `modified` and the
additive nullable boolean `recognition`. Existing fields keep their types and
meaning. `flags` is an ordered array of names (`recognition`, `highlights`),
empty as `[]`. A paired document gains `mark`, an object with the sidecar
`id`, `name`, `path`, `folder`, `size`, `hash`, `modified` and `recognition`;
size and modified time remain null when unavailable. No body is downloaded
for pairing. `mark` is omitted when there is no unambiguous full-name sibling.
`--marks` retains raw sidecar entries alongside the decorated document.
Identifiers are strings to preserve their precision in scripts. Account output
contains the account name, user name, cloud, used/total byte counts and expiry;
opaque private-cloud tokens have no expiry claim, represented as JSON null.
No token, password or device identity is included.

`get` reports `cached` when its body came from the cache and `downloaded` when
it came from the cloud, in table, plain and JSON output.

`get PATH -o -` and `digests handwriting get ID -` write raw bytes, so JSON is
rejected for those destinations. Transfer progress is written only on a terminal,
to stderr. `watch --json` uses compact JSON lines, one announcement per line,
with `type`, `kind`, `id` and `path`. Watch exits successfully on cancellation.
`check --json` emits measured step objects with `name`, `status`, `duration_ns`
and `detail`; statuses are `pass`, `fail` or `skipped`. Any failed step causes
exit 1, and cleanup failures remain visible as their own steps. For a person
`check` prints three columns, the status, the time taken and the step: the time
is rounded (`282ms`, `2.1s`, `1m23s`; `<1ms` for less, `-` for a step not run),
and the reason a step failed or was skipped follows its name after a colon. The
folder made by `check --write` is named in full once, in the step that creates
it; later steps name files and folders relative to it.

Questions name the change, including off-terminal refusals. Cloud errors intentionally discard arbitrary server messages to prevent credential
reflection; failures outside recognized classifications print `cloud operation failed`.

`ls` table columns are type (`d` or `-`), size, modified time,
FLAGS and name relative to the listed folder. `--ids` adds identifiers;
trash always shows IDs. FLAGS appears before the name in trees too.
Flags use fixed order: `R` for recognized handwriting and `H` for
highlights/annotations (a full-name `.mark` sibling); empty cells render `-`.
A one-line `Flags: R recognized handwriting, H highlights` legend goes to stderr
before a nonempty human listing table/tree. JSON, plain output, empty results
and `stat` records have no legend. The ordered vocabulary lives in one place.
The existing nullable `recognition` field still distinguishes unknown notebooks
from inspected unrecognized ones; neither gets `R`. `--recognition` discovers
unknown recognition without changing the human table layout.
Plain output has no flags field. `ls`, name-based `find` and upload/result
listings keep type, size, modified time and name/path, followed by ID when
requested (always for trash). `--recognition` appends the existing yes/no/?/-
recognition field to plain `ls` and name-based `find`. Plain `tree` keeps its
indented name lines, with ID when requested and recognition only with
`--recognition`. Scripts that need flags use `--json`.
Name searches match either the document or its folded sidecar name, showing the
document with `H` in human output. `--marks` also shows raw sidecars. Orphans remain visible.
`tree` renders hierarchy for people and flat complete entries in JSON.
Table `stat` uses labelled fields, including `flags`, and adds `mark_path`, `mark_size`
and `mark_modified` when supplied for a paired document. Duplicate sidecars
produce a warning on stderr and a document record without mark metadata.
Plain `stat` retains its existing fields, without recognition, flags or mark metadata.
Account capacity uses binary human-readable units;
JSON retains exact byte counts. Removal reports recycle-bin versus permanent
deletion explicitly. Conflicts never suggest an unsupported overwrite flag.

## Transfer reports

`mget`, `mput` and `put --dry-run` answer with one row per file or folder and
then the count of each status. The columns are status, cloud path, local path,
size and reason; `-` stands for an empty cell in text. JSON is one object:
`items` (each with `status`, `cloud`, `local`, `size`, `folder` for a folder and
`reason` when there is one), `counts` by status, and `dry_run` for a dry run.

| Status | Meaning |
|---|---|
| `downloaded`, `uploaded`, `replaced`, `created` | Done: a file written locally or in the cloud, a cloud file replaced with its old version recycled, a cloud folder made |
| `cached` | A verified cached body was written locally by get or mget, online or offline |
| `uploaded_as`, `duplicate` | The server chose a different path, or an NFC-equivalent twin exists (exit 6); stderr reports both paths and IDs for twins |
| `upload`, `replace`, `create` | The plan of a dry run, or of a run stopped before writing |
| `unchanged` | `--incremental` found it identical; nothing was sent |
| `deleted` | `mget --delete` removed a file snctl had written |
| `kept` | `mget --delete` left a file changed since snctl wrote it, and stopped recording it |
| `skipped` | A transfer leaves it out; unsafe local output paths make the command fail |
| `exists` | A cloud file of that name exists and `--replace` was not given (exit 4) |
| `failed` | This file failed; the others went on (exit 1). `interrupted` after Ctrl-C or SIGTERM |

A run where some files fail still prints the whole report, then the error.
`put` without `--dry-run` answers with the entries it created, like `ls`, and
reports each failed file on stderr. If an upload is renamed or duplicated, put instead emits the transfer report so the status is preserved in JSON. `du` prints folders with their size and
file count, then the account's used and total capacity and the recycled bytes
counted with `--all`; JSON carries `folders`, `used`, `capacity` and
`recycled` in bytes. `dupes` prints hash, size and path, one row per file, the
files of a group together; JSON is a list of groups with `hash`, `size` and
`files`.

Progress for `mget`, `mput` and `put` is one line on stderr, on a terminal only:
a bar, bytes and files done of their totals, the rate and the time left.

## Notebook output

`render` and `open` use their own `--format pdf|png|jpg|svg` for the file.
`--json` or `SNCTL_FORMAT` selects their answer format independently. `text`
and `highlights` have `--format text|markdown|json`; `note` uses the ordinary
answer formats. A page number is the notebook's own number listed by `note`:
sequential from 1 for `.note`, with gaps where damaged pages were skipped, and
source-document numbers for `.mark`. Selectors,
numbered image names and JSON fields share it. Missing numbers return status 2
with available ranges;
damaged notebooks return 1; missing native capabilities return 5.

Drawing and transfer progress bars appear on stderr only when input and stderr
are terminals. JSON suppresses progress; annotation-sheet notices and warnings remain on stderr.

`render -o -` streams a whole-notebook PDF or one page selected with `--page`.
JSON and directory outputs are refused for stdout. Download bytes and drawn
pages are separate phases; a folder run reports completed documents out of total.

`text -o FILE` writes content in its selected text/Markdown/JSON format and
reports completed paths (`{"paths":[...]}` in JSON). `find --text` reports
FLAGS, path and comma-separated page numbers in table output. Plain output
retains path, comma-separated page numbers and recognition; JSON
uses `[{"entry":{...},"pages":[...]}]`. See [notebooks](notebooks.md).

## Scripts

Without a terminal, commands that change the cloud require `--yes`: `put`, `mput`, `mkdir`, `mv`, `rename`, `rm`, `restore`, `trash empty`, `tasks add/edit/done/undone/rm`, `tasks lists add/rename/rm`, `digests groups/tags/digests add/edit/rm`, `digests handwriting put`. `backup restore` and ad-hoc `mirror run --dir ... --folder ...` also require
`--yes`. `login`, `logout` and `check --write` authorize their own operations;
saved mirror definitions authorize their later runs. `--dry-run` sends no changes and needs no confirmation.

Document moves, renames and removals plan both the document and its full-name
sidecar from one invocation snapshot. The plan goes to stderr before one
confirmation, including with `--yes`. `--dry-run` prints path/destination rows,
or `{"items":[{"entry":{...},"destination":"..."}],"dry_run":true}`; removals
omit destination. Dry runs never confirm or write. A sidecar destination conflict
is exit 4 before either write. If the document changes and the sidecar fails,
the error names both outcomes and exits nonzero; no rollback is claimed.

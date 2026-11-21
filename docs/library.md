# Space, duplicates and folder downloads

Back to the [README](../README.md).

## Cloud paths

Cloud path segments are case-sensitive and compared in Unicode NFC. An exact
byte match wins; otherwise a unique NFC-equivalent name is accepted. Multiple
matches are a conflict. Listings, JSON, cache keys and downloads retain the
cloud's original spelling. New destination names retain the user's bytes;
`mv`, `rename`, `mkdir` and `put` refuse an existing NFC-equivalent name like
an exact duplicate. Full-name `.mark` pairing also compares in NFC and refuses
ambiguous documents or sidecars before writes. `rm`, `mv` and `rename` refuse NFC-equivalent source siblings, including files
and folders, before confirmation. Errors list every exact spelling in quotes and
its ID. `--exact` selects only the byte-exact source name; if that name is absent,
it fails with not-found. Parent segments still follow normal NFC resolution.
Document and `.mark` pairing must remain unambiguous even with `--exact`.
These commands accept paths, not IDs. A name can be changed to its own NFC/NFD spelling when no other
equivalent entry exists. `find` names and `--text`
substrings compare in NFC as well as ignoring case.

The server treats NFC and NFD as different names. After confirmation, immediately
before the first file write, snctl takes one fresh full snapshot from the
server, bypassing the cache and planning snapshot, and derives all affected
parents from it. It checks planned source
IDs and paths, existing targets, NFC twins and sidecar changes again. A changed
plan aborts with a conflict (exit 4) before any file changes. Dry runs make no
extra fresh-listing requests; offline writes remain refused.
Before each individual move or deletion, snctl also reads just its affected
parents afresh, deduplicated within that operation. It compares source ID,
path, hash, size and modification time, destination occupancy and NFC twins.
A change refuses the operation with exit 4; earlier applied writes remain.

After successful creates, uploads, moves and renames, snctl freshly lists each
destination parent using one fresh full snapshot per command or batch, and
checks every written name (including folders and `.mark` files). A twin reports both quoted paths and IDs
on stderr. A failed verification listing warns that the write happened. Both
cases return the distinct `post-write verification failed` error (wrapping
`port.ErrConflict`) and exit **6**. Written entries stay in place; snctl does not
roll back or retry. Inspect the reported paths before retrying. Upload checks
run after releasing the sync lock, when committed entries become visible.
Deletes have no post-write verification. Mirror records completed writes with
verification twins as warnings in its journal and continues the pass. Failed
verification listings fail the pass while retaining applied writes; see
[mirror pass warnings](mirror.md#a-pass).

`restore <id>` (also `trash restore <id>`) resolves the recycle-bin entry's
original parent ID through the location API and maps it to the tablet tree.
If that parent cannot be established, restore refuses rather than guessing a
folder. It refuses exact and NFC-equivalent destination siblings before
confirmation, listing quoted spellings and IDs. A document carries its unique
trashed full-name `.mark` sibling from the same original parent; ambiguous
documents or sidecars conflict. An occupied target sidecar, including an orphan,
also conflicts. A direct restore of a `.mark` restores only that entry.
Two trashed versions of a document with two marks from the same original folder
conflict because their pairing is ambiguous. Restoring a mark directly cannot
bypass an occupied sidecar: if a live sidecar already occupies that name (or an
NFC-equivalent name), the direct mark restore also conflicts.

Restore rechecks the recycle-bin selection and destinations after confirmation,
then freshly checks the affected destination parent immediately before each
restore using the same helper as moves and deletions. Successful restores share
one fresh verification snapshot: twins, a restored ID missing at its planned
path, or failed verification listings return exit **6**, with no rollback.
A missing-ID diagnostic names the expected path. A later carry-along failure reports the completed
restore and the entry left in the bin. `trash empty` only permanently deletes
recycled IDs; it creates no names and needs no Unicode destination checks.

Another client can still write in the instant between a fresh listing and the
write; the server provides no atomic create-only guard. Post-write verification
detects twins visible in its listing, not later changes.

Local path lookup is unchanged and follows the filesystem's rules. Mirror's
separate legacy companion matching is unchanged. Folder downloads keep their
existing local filename safety and collision rules described below.

## Space and duplicates

- **`du [path] [-d N] [--all]`** adds up the sizes the cloud lists for the files below a folder (`/` by default): the total, then each folder down to `-d`/`--depth` levels (1; 0 is the total alone), in path order, with its file count, and after them the account's used and total capacity. `--all` also counts the recycle bin, as a separate line; it needs `/`, because recycled entries carry no original path that would place them in a folder. A file listed without a size counts as zero bytes rather than a guess.
- **`dupes [path]`** groups the files below a folder whose content hash and size are equal: identical bytes. This is not rmctl's `dupes`, which groups documents made from the same source PDF or EPUB; a Supernote file has no such source, and two copies of one notebook edited apart are different files here. Files the cloud lists without a hash are left out.

Both read one listing and download nothing.

## Downloading a folder

**`mget <folder> -o <dir> [--incremental] [--delete [--force]] [-j N]`** writes every file below a cloud folder into a local directory, keeping the tree; `/` takes the whole account, laid out as `NOTE/Note/...`, `DOCUMENT/Document/...`.

- **Names.** Cloud path components such as `..` are neutralised to `notebook`, so `H/../escape.note` lands at `H/notebook/escape.note` beneath the selected output directory (subject to collision suffixes). Every component is made safe for the local file system (`/ \ : * ? " < > |` and control characters become `-`; Windows device names get a `_`; overlong names are shortened with a hash). Two entries whose local paths are equal without regard to case, or a file named like a folder, both keep a place: each gets its identifier before the extension, `Journal (40).note`. The suffix depends only on paths and identifiers, never on listing order, so a rerun writes the same names.
- **Integrity.** Each file is checked against the cloud's MD5 by the library, written to a temporary sibling and renamed into place with the cloud's modification time: a file is whole or absent. A file that fails is reported and the others still come down; the exit status is then 1.
- **The record.** `.snctl-mget.json` in the directory lists, by relative path, each file snctl wrote: the cloud identifier, content hash and modification time it had, and its size. It is written once, at the end, through the same atomic replacement, also after Ctrl-C or SIGTERM, and lists only files already in place. Folder rendering keeps the same kind of record in `.snctl-render.json`; both are written by one shared piece of code.
- **`--incremental`** skips a file when its record matches the cloud entry (identifier, hash and time) and the file on disk still has the recorded size and modification time (to the second). Anything else downloads again.
- **`--delete`** removes recorded files the folder no longer holds, and nothing else: a file snctl did not write is never touched, and a recorded file whose size or time changed since is kept and dropped from the record (`kept` in the report). Only paths inside the directory are considered, whatever the record says. A guard, ported from rmctl's mirror, refuses the whole run before anything changes when the cloud folder is empty while the record lists files (a wrong folder or account looks like this), when there are more than 50 deletions, or when they are more than half of the recorded files (two at least); `--force` carries them out. The refusal is exit 4.
- **`-j N`** downloads N files at a time (4 by default).

The report and its statuses are described in [output](output.md#transfer-reports).

## Finding and listing

`find` keeps filtering the full listing, case-insensitively by name. The web view's server-side search (sncloud-go's `Files.Search`) was not adopted: nothing establishes that its case rules match, sncloud-go's own emulator matches case-sensitively, it cannot be limited to a folder without the listing anyway, and its entries carry no tablet path, only the parent folder's name, so `find` would need the listing to print paths in any case. Results would change without notice.

`ls` has no `-a`. rmctl's `ls -a` shows trashed items in their folders, which works because the reMarkable trash keeps an item's parent. The Supernote recycle bin keeps no original path, so a recycled entry has no folder to be listed in; `trash ls` shows the recycle bin with identifiers that `restore` takes.

`mget --incremental` is a cloud-to-local mirror: when the cloud changes, it replaces the local copy even if that copy was edited. `--delete` keeps local files whose size or timestamp changed since snctl wrote them. It cannot detect edits that preserve both size and timestamp.

Both mget and folder rendering use root-confined output handles and reject links in every output component, including their record files. Rejected entries are reported and make the invocation fail while other entries continue. Names are written in NFC and compared using Unicode case folding and NFC normalization.

Output records include the endpoint, saved account identifier, and cloud folder identifier and path. A different source requires `--force`, which starts a new record without using the earlier record to delete files. Legacy or damaged records also require `--force`.

`dupes` excludes empty files.

## Moving entries

`mv <path> <dst>` moves into an existing folder, retaining the source name. A
missing destination names the new full path and requires an existing parent
folder. Existing files and moves of a folder into itself or its descendants
are refused with exit 4. `rename <path> <name>` keeps the parent folder and
refuses an occupied name, including an existing folder. Both require the usual
confirmation for cloud changes.

## Document annotations

`ls`, `tree`, `find` and `dupes` fold NFC-equal full-name `.mark` siblings into PDF/EPUB
documents. `--marks` shows raw entries; orphan marks remain visible. Pairing happens
before name/duplicate filtering and needs no downloads. Name searches also match
a folded sidecar name and show its document with H in human output. `stat` reports the sibling;
duplicate sidecars produce a warning and no mark metadata.
Human tables and trees use FLAGS (`R` recognized handwriting, `H` highlights),
with a legend on stderr only for nonempty human listing tables. Plain output
keeps its existing fields without flags; scripts use `--json` for flags.
See [output](output.md).

`mv`, `rename` and `rm` carry a document's full-name `.mark` with it. Renaming
`Paper.pdf` to `New.pdf` also renames `Paper.pdf.mark` to `New.pdf.mark`; moving into
a folder preserves both names. Renaming to a non-document name also carries the
sidecar: `Paper.pdf` to `Paper.txt` moves its mark to `Paper.txt.mark`, which
then appears as an orphan in listings. `rm --permanent` permanently deletes both; ordinary
`rm` recycles both. A direct operation on a `.mark` changes only that file.
Stderr lists both changes before one confirmation; pipes still need `--yes`.
`--dry-run` lists both files and sends no writes. Moves refuse an occupied document
or target `.mark` before changing either, including a target orphan beside an
unannotated source or a non-document renamed to a document. Ambiguous source
document or sidecar names are refused before confirmation or writes. A partial failure names what changed and what did not,
and returns nonzero. There is no `cp` command.

Display folding never changes `get`, `mget`, backup, sync or mirror transfers.
`mget` and mirror retain both files on disk; mirror's existing legacy companion
matching and annotated output remain unchanged. `du` still counts physical bytes
and files. Recycle-bin listings carry no original paths, so `trash ls` does not
pair entries; restore resolves their original parents with extra location lookups.

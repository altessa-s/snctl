# Uploads

Back to the [README](../README.md).

## Where files may go

snctl permits new entries below `NOTE/Note`, `NOTE/MyStyle` and `DOCUMENT/Document`. `NOTE` and `DOCUMENT` are virtual categories: an entry written directly below them, or below another subfolder of them, can appear twice on the tablet, so snctl refuses those places. Below other top-level folders (`EXPORT`, `INBOX`, `SCREENSHOT`) the cloud's own rules apply; nothing is created at the top level itself. Neither the protocol nor sncloud-go has a rule about file types.

## put

**`put <file|->... [destination] [--to folder] [--name name] [--replace] [--dry-run]`**

- The destination is `--to`, or else the last argument: a folder, or, for one file, a full path including its new name. `--name` renames one file put into a folder.
- `-` uploads standard input, staged in a private temporary file first so that the transfer can be retried; because standard input carries the file it cannot answer a question, so it needs `--yes` (or `--dry-run`).
- Each file is its own change, in the order given: a file that fails is reported on stderr and the others still go up. The exit status is 1 when one failed, 4 when one met a cloud file of the same name; `--replace` uploads directly to that path. A folder of that name is never replaced.
- `--dry-run` checks every file and the destination against one listing and sends nothing; it needs no `--yes`.
- `put` sends any file a person names: naming it is the decision.

rmctl's put flags not carried over: `--tag` (the cloud has no document tags), `--cover` and `--current-page` (reMarkable document settings; a Supernote file has none to set), `--notebook` and `--device` (typed-text notebooks are a reMarkable format), `--content-only` and `--drop-annotations` (they rewrite a reMarkable document's page list around its annotations; a Supernote `.note` or a PDF with its `.mark` is replaced whole).

## mput

**`mput <dir> [folder] [--incremental] [--replace] [--dry-run] [-j N]`** uploads the files below a local directory into a cloud folder, `/` by default, creating the subfolders the directory has. With `/`, a directory laid out like the cloud, as `mget / -o dir` writes it, goes back to the same places.

- **What goes up:** `.note`, `.mark`, `.pdf` and `.epub` files, the files the tablet opens from its folders. Skipped, each with its reason in the report: hidden files and folders (snctl's `.snctl-*.json` records among them), empty files, links and other non-regular files, other types, and anything bound for a place the cloud does not take (above). Nothing is sent for a skipped file.
- **Empty folders.** As in rmctl, empty local folders are not uploaded. Only folders needed for uploadable files are created, with the existing mkdir semantics and confirmation.
- **Existing entries.** A cloud file of the same name is reported `exists` (exit 4) unless `--replace`. `--incremental` first compares it with the local file: the same size and the same content hash mean `unchanged`. The local hash is the lowercase hexadecimal MD5 of the whole file, which is exactly what sncloud-go computes when it uploads (`content_hash`) and what the listing reports back, so the comparison needs no record of earlier uploads.
- **Plan, question, writes.** The plan is made from one listing and printed on a terminal before the question, which counts the uploads, replacements and folders and their size. Folders are created first, outermost first; then the files, `-j` at a time (4 by default). A file below a folder that could not be created fails; the others go on.
- **Replacement.** On Private Cloud 26.10.09, `--replace` overwrites in place: the identifier is kept, size and bytes change, one entry remains, and nothing is recycled. The official cloud is unverified. The double defaults to this behavior and also models duplicate entries and autorename. On a server that allocates a new identifier for replacement, snctl downloads and verifies the new bytes after releasing the sync lock, then recycles the old entry and moves the new entry to the requested path if needed. Failure retains the uploaded copy and reports its path; a failed final move also reports that the old version is in the recycle bin.
- **The sync lock.** Both put and mput hold the account sync lock during folder creation and uploads. Web recycling and final replacement moves run after successful release. A tablet holding it causes exit 4 before any file changes. After a successful acquisition, sncloud-go attempts release even after cancellation, with a five-second timeout.
- **Concurrent uploads.** Private Cloud 26.10.09 has been observed returning HTTP 200 with `success:false`, `errorCode:"500"` and a temporary-processing-error message from upload finish after storing files intact under concurrent uploads. The server log showed an identifier collision (`DuplicateKeyException`) inserting into `f_user_file`. snctl keeps the default parallelism of four and verifies ambiguous upload failures after the batch and sync lock end, by refreshing just the destination in its invocation-local listing. An exact path, size and MD5 match is reported as `uploaded`, with a warning naming the file. Verification is allowed only when the path was absent before uploading or `--replace` changes its content. Mapped refusals are not verified; missing or differing content remains a failure. This does not retry the upload. Another client can still create a file between the fresh pre-write listing and upload because the lock does not bind clients that ignore it. A returned different path is reported as `uploaded_as`, and NFC-equivalent live twins as `duplicate`, with both paths and IDs on stderr and exit 6. For a newly planned upload, neither is reported as replaced.
- **`--dry-run`** prints the plan and sends no request that writes, not even the lock.

The report and its statuses are described in [output](output.md#transfer-reports).

## Recovering an uncertain sync lock

The profile directory contains a `sync-lock` timestamp while ownership may be outstanding. A successful scope and release clears it. If a later acquisition is refused and this marker exists, snctl reports that a previous run may still hold the lock.

The linked sncloud-go v1.0.0 exposes only `Device.WithSyncLock`. It releases after acknowledged acquisition, but does not attempt release if the start response is lost, and exposes no public acquire, release, or force-unlock method. Consequently snctl cannot implement `debug sync-lock release` or release an uncertain acquisition through that API. No direct protocol request is substituted for the library. A library change is required for automatic recovery.

For a private cloud, have its administrator inspect the account's synchronization state and clear the stale lock using that server's supported administration procedure, or wait for its configured expiry. For the official service, contact Supernote support if the lock does not clear. There is no verified universal timeout or public manual-unlock API to prescribe. Deleting the local timestamp alone does not release the remote lock. After remote recovery, a successful upload clears the timestamp.

With `-v` or `--verbose`, cloud failures log the operation, cloud path, library error kind, and available code or HTTP status on stderr. Rejected envelopes include `errorCode` and the server’s fixed `errorMsg`, truncated to 200 characters. Transport failures include their error class. Credentials, cookies, signed URLs and request bodies are excluded. Unknown envelope codes, including numeric server-error codes such as `500`, and unmapped HTTP errors remain ambiguous; only library-mapped refusals bypass verification.

On Private Cloud 26.10.09, files stored by finishes returning `errorCode "500"` remained invisible to exact-path lookups throughout the sync lock, including 20 seconds of polling, and appeared with matching size and MD5 immediately after release. Verification therefore runs after the batch and lock end, with at most two retries (after 200 ms and 800 ms) only when the lookup itself fails ambiguously.

Put, mput and backup restore use one planning listing, then one fresh full
snapshot after confirmation and under the sync lock, before any file write.
A changed target, source identity, or NFC twin aborts the batch with exit 4.
After releasing the lock they verify every written file and folder using one
fresh full snapshot covering all destination parents. A twin or a failed verification listing
reports that the write happened and returns exit 6; no rollback is attempted.
On servers that allocate a separate replacement copy, that copy is verified
before recycling the old entry, and a final rename is verified afterward. The
approved old ID is the only excluded entry during temporary-copy verification.
Every replacement recycle and final move checks the affected parents afresh
immediately before that destructive operation, including copies at the same
path. Creates/uploads otherwise retain the batched listing bound.
Dry runs add no fresh listings. See [cloud paths](library.md#cloud-paths) for
exact selection and the remaining race window.

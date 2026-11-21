# Backups

Back to the [README](../README.md).

`snctl backup <dir> [path]` keeps the whole library, or a file or folder below
`path`, as original Supernote bytes. Reading and local backup output need no
confirmation. Bodies are read through the profile cache first; an online miss
is downloaded and checked. `sync --files` followed by `--offline backup` works
without requests when the listing and all selected bodies remain cached.
Backup does not populate the body cache on a miss. Configured listing TTL also
applies to online backup; set `SNCTL_CACHE_TTL=0` to insist on a live listing.

```sh
snctl backup ~/Supernote-backup
snctl backup ~/Work-backup NOTE/Note/Work --jobs 8
snctl sync NOTE/Note/Work --files
snctl --offline backup ~/Work-backup NOTE/Note/Work
```

- **Whole files.** A `.note` or `.mark` already contains its notebook or ink.
  Nothing is repacked. Files are stored as `files/<id>-<md5><ext>`. The MD5 in
  the stored name lets a changed file appear atomically while the previous
  manifest still points to its complete previous bytes. The extension is
  lowercase; the original name is preserved in the manifest. These files can
  also be uploaded individually with `put --name <original-name>`.
- **Manifest.** `manifest.json` records version 1, profile, credential-free
  endpoint, UTC creation time and canonical scope, then each file's identifier,
  name, original tablet path, size, MD5, update time and stored relative name.
  Tablet paths in entries have snctl's usual spelling without a leading slash;
  the scope starts with `/`. Empty folders, tasks and digests are not archived.
- **Incremental.** Matching MD5 and size skip a download only when the recorded
  local file also verifies. A rename updates metadata without downloading the
  same bytes. `--jobs N` bounds downloads (default four; zero selects four).
  Each file is staged, synchronized and renamed. The manifest is published
  after all files and retained removals are complete. Cancellation preserves
  the previous manifest and its files. Whole, unrecorded files can remain after
  an interruption; they do not become restore inputs.
- **Removals.** Files deleted or recycled since the previous backup are copied
  whole to `removed/` and their active copies removed after manifest publication.
  `--prune` deletes these former active files instead. A file still in the
  library but moved outside the scope always goes to `removed/`, even with
  `--prune`. Superseded content generations are discarded after publication.
  The manifest lists active files only; `removed/` is retained independently.
- **Directory binding.** Another profile or endpoint needs `--force`; another
  path, including a renamed folder, needs `--rescope`. These overrides are
  separate. Changing scope never prunes former files. The directory is held
  under an exclusive advisory `.snctl-backup.lock` for backup and restore;
  another operation fails as busy. The lock file stays in place. Platforms
  without advisory locking have the same limitation as the profile cache.
- **Output.** Table, plain and JSON report `documents`, `written`, `skipped`,
  `moved`, `pruned` and `outside`. `outside` is the subset of moved files still
  in the library. Progress appears on stderr only with terminal input and stderr.

The live device listing excludes the recycle bin. The separate recycle-bin API
has neither original paths nor complete MD5 metadata, so `--all` explicitly
refuses instead of claiming a complete backup of recycled files.

## Restoring

`snctl backup restore <dir> [cloud-folder]` verifies every recorded file before
planning any cloud change. With no destination it restores recorded paths.
With a destination it restores paths relative to the manifest's bound `path`.
A backup bound to `NOTE/Note/Work` places `NOTE/Note/Work/a.note` at
`NOTE/Note/Recovered/a.note` when restored into `NOTE/Note/Recovered`;
`Work/sub/b.note` keeps its `sub/b.note` suffix. A backup bound to a single file
places that file's original name in the destination. `--to` is an equivalent
destination flag. A directory itself named `restore` is backed up as `./restore`.

A whole-library backup bound to `/` keeps category roots below the destination:
`NOTE/Note/a.note` becomes `NOTE/Note/Recovered/NOTE/Note/a.note`. This preserves
the distinction between categories and avoids collisions between equal names.
Unlike rmctl, which recreates the full recorded folder path under `--to`, snctl
uses the backup scope as the base. Supernote paths always include category roots
such as `NOTE/Note` and `DOCUMENT/Document`; nesting those roots and the backed-up
folder again for a scoped restore would obscure the intended destination.

```sh
snctl backup restore ~/Work-backup --dry-run
snctl backup restore ~/Work-backup NOTE/Note/Recovered --dry-run
snctl backup restore ~/Work-backup NOTE/Note/Recovered --yes
```

The plan shows missing folders and uploads before asking for confirmation.
The local column names the backup's `files/<stored-name>` body, including its
backup directory, so the source remains identifiable after staging is removed.
Noninteractive writes require `--yes`. `--dry-run` verifies bytes and reads the
cloud but sends no write request, including no sync-lock request. Identical
MD5 and size at a destination are always skipped. Different existing files are
reported as `exists` (exit 4) unless `--replace`; a folder occupying a file's
path fails. Other eligible files can still be uploaded. Backup identifiers are
historical metadata, not identities the upload API can restore.

Unlike rmctl's archive import, Supernote cannot import an old identifier as a
new copy in one library commit. Restoration uses the [upload rules](uploads.md):
missing folders are created, writes stay inside writable roots, and each file
is its own change under the account sync lock. Use a new destination to keep
existing files. `--replace` follows the server's overwrite, duplicate or
renaming behavior, verifies a replacement before recycling an old entry when
needed, and reports collisions. Ambiguous finishes are verified only after the
sync lock ends and are never blindly resent. The operation is not a transaction;
partial successful files remain and a later restore skips matching bytes.

Restore also supports `--jobs N`, is unavailable offline, and never restores
`removed/` automatically. Keep backup directories private: their bytes and
unmasked names are account data, although no token is stored in the manifest.

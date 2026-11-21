# Configuration

Settings are local; cloud commands authenticate and contact the endpoint of
the selected profile through sncloud-go.

## Paths

snctl uses Go's per-user configuration and cache directories, then appends
`snctl`. `debug paths` prints the resolved locations. Opening a cache creates its private directory and applies its budget.

| Platform | Configuration root | Cache root |
|---|---|---|
| macOS | `~/Library/Application Support/snctl` | `~/Library/Caches/snctl` |
| Linux | `$XDG_CONFIG_HOME/snctl`, otherwise `~/.config/snctl` | `$XDG_CACHE_HOME/snctl`, otherwise `~/.cache/snctl` |
| Windows | `%AppData%/snctl` | `%LocalAppData%/snctl` |

`config.toml` sits in the configuration root. A profile's token file is
`<config root>/<name>/token.json`; its cache is `<cache root>/<name>`.
Profiles share no listings or bodies, even when two files have the same MD5.

## Profiles

Names are 1–64 characters of lowercase ASCII letters, digits, `_` and `-`,
starting with a letter or digit. `--profile` (`-P`) wins over `SNCTL_PROFILE`;
otherwise the selected name is `default`. Selection alone does not save a
profile. `profile info` can describe those defaults with `exists: false`;
`profile ls` also includes profiles with cached listings or bodies. Both profile commands show `cache_bytes`, `cache_budget` and `cache_budget_source`.

```toml
[secrets]
backend = "file"

[profiles.work]
endpoint = "official"

[profiles.private]
endpoint = "http://private.example:8080"

[profiles.private.secrets]
backend = "keychain"
```

The endpoint is `official` (also the default) or an absolute HTTP(S) URL.
Private clouds may use plain HTTP. Credentials, query strings and fragments
are rejected; store no passwords or tokens in an endpoint or config.toml.
Unknown TOML keys are errors.

`profile add <name> --endpoint … [--secrets file|keychain]` appends settings;
it refuses an existing profile. `profile del <name>` clears both known vaults,
removes snctl's token files, clears its cache and removes the profile's TOML tables. Files snctl
did not create remain in place. An inaccessible selected Keychain stops the
operation. On builds without Keychain support, deleting a file-backed profile cannot
remove Keychain items. Profile deletion removes local state; run logout first to end the remote session.

## Environment

| Variable | Meaning |
|---|---|
| `SNCTL_PROFILE` | Selected profile, unless `--profile` is supplied |
| `SNCTL_CONFIG_DIR` | Replace the entire configuration root |
| `SNCTL_CONFIG_FILE` | Read and edit this TOML file instead |
| `SNCTL_CACHE_DIR` | Replace the entire cache root |
| `SNCTL_CACHE_TTL` | Read TTL as a nonnegative Go duration; `0` disables reuse |
| `SNCTL_CACHE_BUDGET` | Every profile’s budget; default `512MiB`, `0` means unlimited |
| `SNCTL_SECRETS` | Override the backend with `file` or `keychain` |
| `SNCTL_KEYCHAIN_SERVICE` | Override the default `io.altessa.snctl` service |
| `SNCTL_FORMAT` | Default output format: `table`, `text`, `plain` or `json` |
| `NO_COLOR`, `TERM` | Disable automatic color when set, or when `TERM=dumb` |

The backend precedence is environment, profile, global `[secrets]`, then file.
`SNCTL_SOCKET_URL` optionally overrides only the change-feed origin, for private
cloud installations exposing a separate port. The library validates all origins
and permits cleartext for private clouds as documented by its PrivateCloud API.
Private profiles disable XSRF, as Ratta Private Cloud service 26.10.09 requires.
Official profiles use the library's official HTTPS endpoints and XSRF policy.

For an isolated local bench, set `SNCTL_CONFIG_DIR` and `SNCTL_CACHE_DIR` to new
directories, and `TMPDIR` to a private temporary directory. The library uses the
platform temporary directory for downloaded files; the CLI removes successful
transfer staging files after use. Stdin uploads also stage there. No password
environment variable is supported. `login --account` selects the account; later
logins reuse the selected profile's stored account name. `--country-code` supplies
a numeric telephone prefix. `--password-stdin` reads one line and never saves it.
`--code` completes an emailed challenge after requesting its delivery.

## Secrets and migration

Only `accessToken`, `account` and `clientID` pass through the secret-store port.
A password is never stored. Status prints names, not values. The file backend
writes through a sibling temporary file, syncs it, and replaces the old file;
it requires mode 0600 on Unix and creates directories with mode 0700. Windows
uses the user's filesystem ACLs; Unix mode checks do not describe those ACLs.

On macOS, generic-password items use service `io.altessa.snctl` and accounts
`<profile>/<secret name>`. Like rmctl, the adapter uses `go-keychain` v0.0.1
through Security.framework in builds with cgo. Items explicitly disable
synchronization and are accessible after first unlock on this device only.
An existing item is replaced whole so foreign accessibility attributes cannot
survive; a failed replacement attempts to restore its old value with the same
policy. The Keychain may ask for access or fail while locked. Other systems
and macOS builds without cgo return an unsupported error for Keychain calls;
file storage remains available.

`secrets migrate --to file|keychain` copies each value, reads it back, and only
then deletes the source. An occupied target requires `--replace`. Failure before
source deletion restores overwritten target values; source-deletion failure
leaves the copied values in the target and reports the partial cleanup.

`--set-default` records the backend before clearing the source. As in rmctl,
the config writer appends a new backend table; it refuses to overwrite an
existing, different profile backend, preserving comments and hand edits.
Without `--set-default`, migration prints the configuration lines to apply.
An environment override of the old backend must be unset or changed as well.
A fresh profile has nothing to migrate.

## Notebook files

Notebook commands use the same profile for cloud paths. An existing local file
is preferred over a cloud path with the same spelling. Local files need no
cloud session and remain usable under `--offline`. Choose an
unambiguous canonical cloud path if a local name collides.

`-o` names a local destination and does not select an answer format.
`SNCTL_FORMAT` still controls answer formatting, including render reports;
render's own `--format` selects the generated file format. `open` leaves its
private `snctl-preview-*` directory in the system temporary directory because
the viewer may continue reading after snctl exits. Delete it after closing the
viewer. Preview directories use mode 0700 on Unix. On Windows, they are created
with a protected ACL granting the current user access, inherited by preview
files, even when the temporary parent directory is shared.
Folder rendering keeps `.snctl-render.json` and `mget` keeps
`.snctl-mget.json` in the output directory, independently of the cloud
cache location; `mput` never uploads them. Deleting a record makes the next
`--incremental` run download everything again and `--delete` forget what it
wrote.


## Cache

Each profile stores its last complete device listing and its cloud-confirmation
time under `<cache root>/<profile>/`. Directories are created with mode 0700,
files with mode 0600. Atomic replacement and an operating-system lock serialize
changes across processes. Windows uses the user's filesystem ACLs. On platforms
other than Unix and Windows, cross-process locking is unavailable; do not run
concurrent commands against the same profile cache.

Body copying, hashing and fsync run outside the lock. Lock acquisition waits up
to 30 seconds for bookkeeping and honours command cancellation. On Windows,
eviction skips open bodies and retries on a later cache operation; usage may
briefly exceed a lowered budget until readers close their files. Online cache
failures fall back to the cloud, with diagnostics under `-v`; offline commands
exit 1 and name the cause.

Supernote exposes no root hash or generation in sncloud-go v1.0.0. An online
library read takes one recursive listing and records it. A positive TTL trusts a
listing confirmed less than that duration ago; its timestamp is not extended by
cache hits. Writes and `sync` always list live and ignore TTL. Cloud file writes
invalidate the stored listing before mutation, including ambiguous failures.
The next read obtains a fresh listing. After successful uploads, the duplicate
check records a complete fresh listing for subsequent offline reads. If
invalidation fails, that invocation stops trusting its cached listing and
retries discarding it when it next obtains the cache lock.

File bodies are addressed by their cloud MD5, checked against both MD5 and size
on every cache read, and dropped if damaged. Every library-file download checks
the cache first, online too. Plain downloads do not populate it; `sync --files`, mirror snapshots
and successful uploads retain bodies. Mirrors reuse verified originals and
sidecars between drawing passes; the same budget and online fallback apply. Uploads use the local file, without an
extra download, and count as recently used. Bodies larger than the budget are
skipped, with a `-v` note. Folders have no bodies. Tasks, digests and digest handwriting have no
corresponding device-listing cache.

Each profile's budget covers retained bodies. The listing, sync record and
removal count are durable state outside eviction. The first budget set wins:

1. `SNCTL_CACHE_BUDGET` (all profiles).
2. `[profiles.<name>.cache]` `budget`.
3. `[cache]` `budget`.
4. `512MiB`.

`"0"` means unlimited, not disabled. Sizes between zero and `1MiB` are refused,
with the setting named. Binary KiB/MiB/GiB/TiB, decimal KB/MB/GB/TB, fractions and
bare byte counts are accepted. Least recently used bodies leave first. A lowered
budget applies on cache open, including `debug cache status`. Each removal
invalidates the complete sync and advances the removal count. Completion reads
without opening the cache for mutation and does not evict.

`--no-cache` neither reads nor writes the cache. `sync` refuses it (exit 2);
cache diagnostics refuse access rather than silently ignoring the flag.
The listing records the endpoint URL without credentials or query and the saved
account identifier. A different endpoint or account discards it for online
reuse; offline reads exit 1 with `cached listing belongs to another
endpoint/account; run sync`. Old listings without endpoint metadata also need
an online refresh.

`logout`, and login when the endpoint or supplied account differs from the saved listing or account,
clear the profile's cache before changing session state. Login also clears it
conservatively when the prior listing is unavailable and its endpoint cannot
be established. `login` and `logout` refuse `--no-cache` (exit 2), because a session change
must be able to clear the cache without violating that flag.

The `recognition` file in the profile cache is a versioned JSON index:
`{"version":1,"bodies":{"<lowercase MD5>":true}}`. It stores yes/no booleans
only, never recognized text or filenames. Writes merge under the same process
lock and use atomic replacement; lookups do not create or touch files. Missing,
damaged or unsupported-version state is unknown, never false. The next successful
merge replaces a damaged index; filesystem permission failures still need to be
corrected. Each command loads one snapshot. Recognition scans collect updates and
merge once under the cache lock on completion, partial failure or Ctrl-C, preserving
other processes' writes. Recognition metadata survives body eviction
and counts as bookkeeping, outside the body budget. `debug cache clear` and
session cache clearing remove it. `--no-cache` bypasses it completely.
Readable `.note` downloads also retain their verified bodies under the ordinary
per-body budget and LRU rules. Recognition scans also retain verified invalid
`.note` bodies for later parsing attempts; ordinary invalid downloads keep their
existing transfer behavior.

### config.toml

Cache tables are read strictly, like endpoint and secrets settings. A profile's
values override global values key by key. `SNCTL_CACHE_TTL` overrides either
TTL; `--offline` and `--offline=false` override the configured boolean.

```toml
[cache]
ttl = "5m"          # default is "0": ask on every read
offline = false
budget = "2GiB"

[profiles.work.cache]
budget = "8GiB"

[profiles.archive.cache]
budget = "0"

[profiles.travel.cache]
offline = true
```

## Offline

`--offline` makes no cloud request. A missing or damaged listing, absent path or
missing body is an error naming what is missing (exit 1). A listing confirmed
more than a day ago produces a warning. Offline does not authenticate or refresh
a session. The following is the complete command classification:

| Commands | Offline behavior |
|---|---|
| `ls`, `tree`, `stat`, `find`, `dupes` | Read the last listing |
| `du` | Sum the last listing; account used/capacity are unavailable and reported as zero |
| `du --all` | Exit 1: the separate recycle-bin listing is not cached |
| `get`, `mget` | Read the listing and verified cached bodies |
| `text`, `highlights`, `note`, `render`, `open` | Local files work as usual; cloud paths require cached listing and bodies; native build capabilities still apply |
| `profile ls`, `profile info`, `profile del`, `secrets status`, `debug paths`, `debug cache status`, `debug cache clear` | Local operations; normal confirmations apply |
| `login`, `logout`, `account`, `sync`, `put`, `mput`, `mkdir`, `mv`, `rename`, `rm`, `restore`, `trash` and its subcommands | Refuse offline, exit 2 |
| Every `tasks` or `digests` subcommand, `watch`, `check` (including `--write`), `profile add`, `secrets migrate` | Refuse offline, exit 2 |
| Help, `version`, completion script generation | Local informational commands; no cloud access |
| Cloud-path completion | Cached listing and saved account identifier, even without `--offline`; no request |

There are no `cat` or `export` commands in this version. Recycled items are
absent from the device listing. They cannot be completed or synced; no `sync
--all` flag pretends to include them.

## sync

`snctl sync [path]` retains the listing. Supernote has no separate page files:
`--files` retains every file body below the path (`/` by default), including
whole `.note` and `.mark` files needed by notebook commands. A single file path
is accepted too. `--jobs N` defaults to 4 (0 selects the default).

Before any body download, sync adds up unique MD5 bodies in the scope, including
those already held. An over-budget sync exits 1 naming bytes needed, budget and
the setting to raise, without downloading any body. The listing can remain.
Bodies already held count as recently used, so unrelated old bodies leave first.
A budget of zero imposes no limit.

A complete sync records its listing time, path, `--files` and removal count.
`current=true` means nothing in the selected scope needed fetching, including
when no previous sync record exists. An identical sync answers `current` after **one live listing**, no downloads;
it still verifies retained bodies. `--force` bypasses the previous record.
After fetching, every planned body must still be held and the removal count
must still match before completion is recorded. Cancellation or failure leaves
completed bodies available for the next sync; it records no new completion.

Terminal stderr shows transfer progress. Pipes receive no progress. Table,
plain and JSON answers expose `current`, `path`, `documents`, `fetched`,
`cached`, `bytes_fetched`, `bytes_needed`, `cache_bytes`, `budget` and
`budget_source`. Budget zero is `unlimited` in human output and `0` in JSON.
No fabricated root hash or cloud generation is reported.

```sh
snctl sync NOTE/Note --files
snctl --offline get NOTE/Note/Journal.note -o journal.note
snctl --offline render NOTE/Note/Journal.note -o journal.pdf
snctl debug cache status --all --json
snctl debug cache clear --all --yes
```

`debug cache status [--all]` reports each profile's directory, bytes, body count,
budget, source, confirmation time and removal count. `debug cache clear [--all]`
names profiles in its confirmation and clears cache-owned data, retaining the
lock and removal counter. `profile ls` and `profile info` show cache size and
budget without a cloud request.

Cache opening removes regular `.part-` and `.read-` temporaries older than one
hour under the profile lock. `debug cache status` reports remaining temporaries.
A damaged removal counter prevents sync completion from being recorded until a
body removal or `debug cache clear` rewrites a valid count. Local backup output
is also available offline; `backup restore` and `watch` require online access.

## Mirrors

Each `[[mirror]]` table defines a [two-way mirror](mirror.md). `mirror add`
appends a table and `mirror rm` removes it, preserving unrelated lines.

```toml
[[mirror]]
name = "papers"
dir = "/Users/me/Papers"
folder = "/DOCUMENT/Document/Papers"

[[mirror]]
name = "contracts"
dir = "/Users/me/Work/Contracts"
folder = "/DOCUMENT/Document/Contracts"
profile = "work"
pull_new = true
no_delete = true
```

`name` is unique, 1–64 lower-case letters, digits, underscores or hyphens,
starting with a letter or digit. `dir` is an absolute existing directory;
`folder` is a cloud path from `/`, under NOTE/Note, NOTE/MyStyle, DOCUMENT/Document or another writable
root. Server-specific type refusals remain pending upload failures. Missing cloud folders are created on the first real pass.
`profile` binds the definition to that profile; without it the run's profile
applies. `pull_new` renders cloud notebooks to `_annotated/`; `no_delete`
detaches entries deleted on one side while keeping their surviving copies.
Both booleans default to false.

Duplicate names, relative directories, invalid profiles and unknown keys are
errors. Inline arrays can be read, but `mirror rm` refuses to rewrite them.
`mirror add` refuses overlapping directories and cloud folders; unbound
profiles are conservatively treated as potentially overlapping any profile.
The service rereads the file every 30 seconds. Its log is
`<config dir>/logs/mirror.log`, rotated at 5 MiB with three older logs.
The service retains SNCTL_CONFIG_DIR, SNCTL_CACHE_DIR, SNCTL_CONFIG_FILE,
SNCTL_PROFILE, SNCTL_SECRETS, SNCTL_KEYCHAIN_SERVICE, SNCTL_CACHE_TTL and
SNCTL_CACHE_BUDGET when set during installation.

Token-file updates are serialized across snctl processes with a private sibling
`.lock` file. That lock file remains after logout; it contains no credentials.
Logout with no token file is a no-op and creates neither directory nor lock.
Waiting for another process’s lock respects command cancellation; an uncontended
update can complete even when its context is already canceled.

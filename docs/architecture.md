# Architecture

The command line drives input ports. Use cases implement them over output
ports. Adapters handle the machine. Only wiring assembles both sides.

| Directory | Responsibility |
|---|---|
| `cmd/snctl` | Process streams, terminal detection, signals, build seams |
| `cmd/snctl/commands` | Cobra tree, local and cloud commands |
| `commands/cliutil` | Shared output selection, confirmation, masking, exit codes |
| `commands/output` | Typed results and table, plain, JSON renderers |
| `commands/clitest` | Fake input ports, command runner and golden comparison |
| `internal/domain` | Pure endpoint validation, safe local names, the writable-folder and upload-type rules |
| `internal/app/mirror` | Journal reconciliation, deletion guards, watch supervision, mirror definitions and service use cases |
| `internal/adapter/fswatch` | Recursive native directory watching and descriptor budgeting; sole fsnotify owner |
| `internal/adapter/autostart` | Per-user service definitions, runner invocation and rotating logs |
| `cmd/snctl/commands/mirror` | Mirror and mirror-service command input/output |
| `internal/app/archives` | Incremental whole-file backup generations, manifest validation and verified restore staging |
| `internal/app/files` | Restore upload plans, path resolution, verified downloads, mkdir -p, mv, du, dupes, mget, mput and put |
| `internal/app/outdir` | Shared by mget and folder render: the record file, collision-free local paths, the deletion guard |
| `internal/port` | Profile, session, files, organizer, feed and storage contracts |
| `internal/app/profile` | Add, list, describe and remove local profiles |
| `internal/app/check` | Live read checks, scoped writes and bounded cleanup |
| `internal/app/secrets` | Status and migration, including read-back and rollback |
| `internal/adapter/cloud` | The only sncloud-go importer; translates public library views |
| `internal/adapter/localfs` | Private staging, scoped reads and atomic local replacement |
| `internal/adapter/config` | Paths, strict TOML, locking and guarded file replacement |
| `internal/adapter/secretfile` | Atomic private token file with cross-process locking |
| `internal/adapter/keychain` | Profile namespacing over an injectable item store |
| `internal/adapter/keychain/system` | macOS Security.framework binding with cgo; unsupported stub otherwise |
| `internal/adapter/vault` | Adapt a secret store for migration |
| `internal/adapter/flock` | Platform advisory locks |
| `internal/adapter/fsdir` | Directory listings that refuse a regular file on every platform |
| `internal/wiring` | Resolve profile settings and compose local and cloud services |
| `internal/porttest` | Synthetic, isolated port doubles, an in-memory disk and tablet tree |
| `internal/clouddouble` | A loopback private cloud the real sncloud-go client talks to, for end-to-end tests |

`architecture_test.go` checks each package's imports and rejects packages
without a layer rule. The domain has no I/O. Ports import only domain; use
cases import domain and ports; adapters import neither use cases nor commands.
Commands see input ports, not adapters. The local filesystem adapter also uses
the existing advisory-lock adapter for pinned backup directories; the token-file
adapter uses it to serialize updates across processes. Test doubles stay out of production.
`.golangci.yml` enforces the same boundaries during linting.

The only package importing `sncloud-go` is `internal/adapter/cloud`, enforced
for every production package by `architecture_test.go`. The library's public
views are behind small adapter-local interfaces so adapter tests use synthetic
doubles without importing its internal emulator. No CLI code performs HTTP.
The library is required at a released version; `go.mod` carries no replacement.

`port.CloudSession` extends `port.Session` with a safe account summary and
lifecycle. `port.Files`, `port.Tasks`, `port.Digests`, `port.Feed` and `port.Probe`
separate cloud capabilities. The adapter translates their models to public
library types; no protocol type crosses those ports. `port.LocalFiles` owns
staging and atomic local replacement through `internal/adapter/localfs`.
`internal/app/check` orchestrates the independent ports and bounded cleanup.
`cmd/snctl/commands/cloud` handles arguments, confirmations and typed output.

| Operation | Library view and reason |
|---|---|
| `ls`, `tree`, `stat`, `find`, completion | Device: tablet paths, recursive listings and path lookup |
| `get`, `put`, `mkdir`, `mv`, `rename` | Device: full tablet paths after resolving folder destinations, and library-checked transfers |
| `rm` | Resolve path with Device, recycle the identifier through Files; device deletion would be permanent |
| `rm --permanent`, check cleanup | Device: permanently remove only explicitly selected or created IDs |
| `trash`, `restore` | Files: the web view owns the recycle bin and restore operations |
| `account` | Session plus Files.User and Files.Capacity; no credential field is exposed |
| `tasks`, `digests`, `watch` | Tasks, Digests and Feed views respectively |
| `check` | Both tree views independently, plus the session and organizer/feed views |
| `du`, `dupes` | Device listing (sizes and content hashes), Files.Capacity; Files.RecycleBin for `du --all` |
| `mget`, `mput` | Device listing, downloads and uploads; `mput` holds Device.WithSyncLock around its writes |

One invocation-local listing for planning. The device API answers a path lookup only from a
full recursive listing (its path query accepts aliases snctl must not follow),
so each lookup used to cost a listing: `mv` and `rename` made three, `rm` two,
`restore` relisted everything to echo one entry. `files.Service` now takes one
`port.FileSnapshot` view per command; the adapter answers its `Stat` and `List`
from that listing and applies the command's own creations, moves and deletions
to it, so `mkdir -p` and `mput` see the folders they just made. Uploads refresh the view after completion to detect concurrent collisions.
The mutable view lives only as long as the command; a profile cache keeps the last confirmed listing across commands. `restore` now confirms with its
identifier and makes no listing.

The profile's selected vault implements the library SecretStore through a
translation adapter. The library generates and retains `clientID` per profile;
logout clears account/token but retains that identity. Its constructor performs
no network requests and reads no secrets. Passwords pass only through the login
call; no password value is saved, included in errors, or logged. Library logging
is discarded by default. `--verbose` supplies the diagnostic stream explicitly;
wire diagnostics in the environment alone do not enable it. Arbitrary library/server/store errors are replaced
with fixed CLI-safe messages; classified session/conflict/not-found errors survive.

Tasks updates fetch the complete library row first, preserving fields outside
this CLI's editing surface. Digest edits pass only explicitly selected fields.
The library offers no conditional create-only upload or compare-and-swap task
update: preflight overwrite checks and read-modify-write edits can race another
client. Handwriting upload followed by attachment is not transactional; an
attachment failure can leave an unattached uploaded object. There is no public
library operation to remove such an object separately.

Remote completion reads the profile's cached listing alone, without credentials,
network access or cache mutation. Errors produce
no candidates or diagnostic output. Transfers report bytes on stderr only when
stdin and stderr are terminals. Feed batches expand to one announcement per
line, omit device identities, and reconnect through the library's Watch iterator.
Connected events mean callers should reconcile REST state; missed events are
not replayed. An interrupt stops watch with its signal exit status after the child grace period.

The self-test uses a random folder under `NOTE/Note` and tracks returned identifiers.
Its cleanup uses an uncanceled two-minute context, deletes created files first,
and deletes folders only after fresh listings establish that they are empty.
It never calls EmptyRecycleBin or deletes an unknown identifier. A mutation whose
response is lost may leave an unidentified entry; cleanup reports a nonempty
folder rather than guessing ownership. The multipart sample is 9 MiB + 123 bytes,
above the library's 8 MiB threshold, and both sample names end in `.txt` for
private-cloud compatibility.

Command Short descriptions start with a capital letter throughout the tree.
The built-in help and completion commands retain Cobra's default Shorts
("Help about any command" and "Generate the autocompletion script for the specified shell").
Version retains rmctl's "Display version information" in Additional Commands.

Service construction is lazy: help, version and completion need no settings,
secret store or filesystem. Profiles are settings, so listing or describing
them does not unlock the Keychain. Secret status and migration open only the
stores their work requires. Every command that changes the cloud uses CLI
confirmation ([output](output.md#confirmation-and-exit-status)); validation of
output flags happens before services mutate state.

`internal/clouddouble` is an `httptest` server speaking the private cloud's
device and web protocol as sncloud-go sends it: listing (by path, recursive or
not), path and identifier lookup, download with an MD5 and byte ranges, the
private-cloud upload (apply, form upload, finish), folder creation, move, copy,
recycle, restore, permanent deletion, emptying the recycle bin, capacity, the
user query and the sync lock. Faults, by URL path or tablet path and count,
answer an HTTP status, stall, refuse the session, announce a wrong checksum or
cut a download in half. Its request log lets a test assert that a dry run or a
refused command sent no request that writes. sncloud-go's own emulator is
internal to that module and cannot be imported. The double does not sign
official-cloud storage URLs, speak multipart uploads, log in, or serve tasks,
digests, the feed or search; those stay covered by adapter tests with doubles
of the library's views.

Tests use temporary directories, fake Keychain items, injected environments
and streams. Binary integration tests inherit only execution-related variables
and explicit temporary paths. No test accesses the real login Keychain, even
on macOS. Race detection and shuffled tests run in CI. Coverage includes calls
across package boundaries, with duplicate instrumented source blocks counted
once, and retains rmctl's 90% package and 93% total floors.

### Canonical tablet paths

The adapter resolves paths against a recursive real device tree, using exact
case and spelling. It never uses query/by/path's permissive aliases. Existing
entries supply identifiers; new destinations require an existing canonical parent.
Unknown top-level spellings report the listed candidates. A leading slash is
optional. The web's Note is `NOTE/Note`, Document is `DOCUMENT/Document`.

Private Cloud 26.10.09 accepts both `Note/x` and `NOTE/x`, but upload/move
causes additional entries under `NOTE/Note/x`. Wire traces show consistent
caller spelling through the library and successful server responses: this is
server path/mirroring behavior, exposed by snctl's former pass-through policy.
Creation/upload/move below NOTE outside Note/MyStyle, and below DOCUMENT
outside Document, are refused. No raw HTTP or library patch is used. Existing
entries there remain readable and can be removed by their exact listed IDs.

`check --write` scans the entire device tree after bounded cleanup, including
when creation failed, looking for its unique folder name and every known created
ID. Unexpected copies fail the check; the scan never deletes unidentified data.

### Notebook capabilities

`internal/app/files` resolves canonical paths and owns the lifetime of verified
cloud downloads. `get` calls that input port. `internal/app/notebooks` shares it
with local reading, page selection, drawing, previews and recursive PDF export.
The untouched cloud commands continue to use their existing ports.

`port.NoteReader` and `port.Renderer` contain no native library types.
`internal/adapter/notes` alone imports snnote-go; `internal/adapter/renderer`
alone imports snrender-go. Depguard and architecture tests enforce those
boundaries. Both have portable stubs; `norender` excludes only drawing.
The rendering port accepts notebook bytes and optional source-document bytes,
and the adapter uses snrender-go v1.0.0 overlay methods when the source is present.
Overlay diagnostics cross the port as portable omission counts and warnings.
The source-PDF indexes address the original pages, including pages without ink;
standalone marks retain stored-sheet selection.

snctl links only snrender-go as a PDFium consumer. It does not call
`SetPDFiumLock`: the library serializes its own Cairo previews internally; Apple
uses CoreGraphics. If another PDFium binding is introduced, wiring must install
one shared process-lifetime mutex before the first preview and every external
PDFium operation (including initialization and cleanup) must use it, as required
by snrender-go. PDF export does not acquire that lock.

`port.NotebookFiles` owns atomic output, timestamps and private preview
folders. A drawing is written to a temporary sibling, synchronized, closed,
checked for cancellation and renamed. Recursive export runs bounded workers,
keeps report order deterministic and records only completed PDFs. The system
opener receives a completed file as one argument, without shell evaluation.


### Per-profile caching

`port.FileCache` separates persistence from cloud policy. `internal/adapter/cache`
owns atomic listing, sync and removal state, an advisory process lock, streamed
MD5/size verification, temporary copies for callers and LRU body eviction.
`internal/adapter/cloud` selects live, TTL or offline listing access and checks
this port before downloading. It alone translates sncloud-go entries; neither
the disk cache nor the use case imports the cloud library.

`internal/app/prefetch` implements `port.Syncer`: live listing, scope resolution,
budget planning before downloads, bounded concurrent transfers and verification
before recording completion. `port.CacheAdmin` supplies diagnostics and clearing.
Wiring selects per-profile budgets and modes after command flags are parsed.
The command layer classifies offline access explicitly; new commands default
to refusal. Completion has a separate cache-only file view.

The listing is Supernote's index. There is no root generation endpoint; the
sync record stores the confirmed listing and scope rather than inventing one.
An eviction removes the record before removing a body, advances the durable
counter under the lock, and completion checks the counter again when committing.
Bodies count toward the budget; durable state does not. Architecture tests add
explicit boundaries for both new packages without relaxing existing ones.

### Backups and watch execution

`port.Backups` is implemented by `internal/app/archives`. `port.BackupStorage`
opens a confined, exclusively locked `port.BackupDirectory` in `localfs`, using
its existing atomic output implementation and the platform flock adapter.
`port.BackupLocal` adds caller-owned staging; no use case imports the filesystem.
Versioned, MD5-bearing stored names preserve the previous manifest during an
interrupted backup. Removed files are copied aside before publication; obsolete
active files are cleaned only after the new manifest is durable. The manifest
has profile, endpoint and scope binding and contains no credential fields.

`port.BackupUploader` reuses `files.Service` planning, writable roots, confirmation,
sync locking, upload-mode reconciliation and post-lock ambiguous verification.
Every backup input is verified and staged before that boundary is called.
A restore can be partial because Supernote has no atomic multi-file import.
The new use-case package has the same architecture rule and coverage floor as
other use cases; the filesystem lock dependency is explicit in its layer rule.

The cloud command's watch dispatcher adapts rmctl's bounded debounce, queue and
process runner to `port.Change`. Process execution is a command-line facility:
argv comes from explicit flags, never cloud data. A batch gets one fresh file
listing through the cloud adapter, which persists it through the cache. File
announcements alone resolve file identities; organizer namespaces remain separate.
The child receives JSON stdin and replaced environment variables. One worker
serializes runs, applies timeouts and allows five seconds after cancellation;
remaining batches are discarded. Connected announcements permit unfiltered
handlers to reconcile notification gaps. Device identity remains omitted.
Socket.IO integration fixtures use a websocket dependency only in tests, with
a production-import prohibition in depguard.

Opening a profile cache sweeps regular `.part-` and `.read-` staging files older
than one hour under its existing lock. Diagnostics count remaining temporaries.
A damaged removal generation invalidates every sync record until the next
removal or clear rewrites it; no damaged sentinel can become a trusted record.

## Two-way mirrors

The mirror use case compares a confined local scan and a fresh remote view with
its journal. `MirrorDisk` opens a pinned `MirrorDirectory` with an advisory
lock. The directory rejects links and escaping names, publishes originals
without replacing occupied files and retains prior local originals under
`.snctl-removed/`. Drawings and the journal are the mirror's own replaceable
outputs. Journals validate version, local paths, cloud scope, endpoint/account identity and output paths.
Replacement and incoming-download intents precede retention so interrupted
passes resume instead of interpreting partial work as user deletion.

`MirrorConnections` creates a separate live cloud listing and transfer services
for every pass; concurrent watches never share mutable planning snapshots.
Uploads use the existing phase-2 verified staging and post-lock verification.
Its internal `RecycleExisting` option retains prior cloud bytes before a mirror
replacement, even on servers with in-place overwrite behavior. Other upload
callers retain their existing replacement semantics. Cloud deletes always use
the recycle bin. Content conflicts retain both versions under distinct names.

`DirWatcher` owns native directory events, while the use case owns debounce,
rescans, polling fallback, cloud-announcement suppression and retry scheduling.
The timer is injected. Profile engines are opened through an input port and
config reload starts or stops individual watches without stopping others.
Notebook and PDF/.mark drawings share the notebook input port, private staging
and confined publication through the pinned root. The unused `DocumentOverlay`
placeholder was removed. Originals and sidecars download by listed identifier
and MD5 through the cloud cache; the journal records both input versions for a
PDF overlay. The two output namespaces (`_annotated/notebooks` and
`_annotated/documents`) prevent collisions. Sidecars are read only for drawing,
never synchronized or uploaded. Dropped sheets/highlights become action warnings,
without making a successfully drawn pass fail. EPUB overlays report a warning
and are skipped. Removing a sidecar retains its prior PDF drawing just as removal
of an original does.

`ServiceRunner` is the service-manager execution port. The autostart adapter
builds launchd, systemd-user and scheduled-task definitions; tests inject a fake
runner and temporary directories. Production composition resolves the installed
executable and allowed environment values. Windows transports those values as
saved service arguments, validated before configuration is loaded. Log rotation
and tail/follow operations remain local adapter responsibilities.

The closed architecture and dependency rules add mirror, fswatch and autostart
packages and restrict fsnotify to its adapter. New behavioral packages use the
unchanged 90% package floor and 93% total coverage gate.

### Recognition metadata and text

`domain.CleanText` owns the explicit search/copy cleaning policy. Portable
notebook methods copy text before cleaning and share text/Markdown formatting
between CLI exports and mirror companions. `port.NotebookText` extends the
reader with discovery, all-words search and atomic export; the ordinary reader
and renderer remain separate capabilities, so lite builds retain text support.

`port.RecognitionIndex` stores boolean facts by MD5. The cache adapter merges
its versioned `recognition` JSON under its existing lock. Each command uses a
memoised snapshot; discovery batches observer and reader updates into one
merge-write on completion, error or cancellation. Notebook parsing
records the complete body before page selection. A wired verified-body observer
lets get/sync/mirror downloads discover recognition without coupling the cloud
adapter to the native parser. Reads of previously cached sync bodies fill missing
metadata too. The observer is best effort, matching optional cache behavior;
discovery retains unknown entries and reports partial failures while continuing;
search propagates parser failures. Discovery can retain damaged verified bodies
under the existing body budget for later local parsing attempts. Listing commands
decorate answers through the input port, without downloading unless requested.

Mirror drawing intents include an optional staged Markdown companion. Both
outputs share the notebook's version, retirement decisions and recovery path;
the journal contains paths and booleans, never recognized text. Existing version
2 journals remain readable and inspect current PDFs for text without rendering again.

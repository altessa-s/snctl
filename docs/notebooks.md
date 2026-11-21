# Notebooks

Back to the [README](../README.md).

`text`, `highlights` and `note` read local files or canonical cloud paths through
snnote-go v1.0.0. Atelier `.spd` inspection uses snspd-go v1.0.0. They do not edit notebooks.
Local files need no cloud login.
The native parser requires cgo and a C23 compiler; a portable build retains the
commands and returns status 5 explaining the missing capability.

Notebook reading and drawing use the same input rule: an existing regular `.note`,
`.mark` or `.spd` file (case-insensitive extension) is local; `highlights`,
`render` and `open` also accept existing PDF/EPUB documents. Other names,
including existing directories, are cloud paths. A colliding local file wins. Use `./` or `../` to
force local input and a leading `/` to select a cloud path. An existing absolute
OS file path is local; for a colliding absolute name, use the cloud's full canonical
path. Explicit local paths that are missing fail as files, as do notebook filenames
whose parent directory exists. Local directories never hide cloud folders for these readers. `find --text` is
the exception: an existing local directory is searched recursively without
following symbolic links; `/` always means the cloud root. A
path forced local with `./` or `../` that names a directory is a usage error: a
notebook file is needed. When a missing relative notebook starts with a cloud
top-level name (`NOTE`, `DOCUMENT`, `Note`, `Document`, `MyStyle`) or lies in a
directory that `render` or `mget` wrote, as `text Note/n02.note` after
`render Note` does, the message says that a leading `/` addresses the cloud.

A page number is the notebook's own number, listed by `note`: `.note` pages run
from 1, with gaps where damaged pages were skipped; `.mark` pages use
source-document numbers and can have gaps.
Selectors, numbered image names and JSON page fields all use those numbers.
A missing number is a usage error listing available numbers as ranges.
Highlights can select document pages with no handwritten sheet.

## Text

```sh
snctl text journal.note
snctl text NOTE/Note/Journal.note --page 1,3 --format markdown
snctl text journal.note --page 1 --page 2 --json
```

Text contains live typed text boxes and stored recognized handwriting, in page
order. Recognition text is present only for real-time-recognition notebooks.

`text` and `note` also accept a PDF or EPUB wherever they accept its full-name
`.mark` sibling. They use the same annotation resolver as `highlights`, `render`
and `open`: cloud siblings match exact spelling or NFC, and ambiguous document
or sidecar names conflict. A document without annotations reports
`<doc> has no annotations` (`ErrNotFound`). Local lookup follows the filesystem's
rules and never falls back to a cloud sidecar for a local document.

`text Paper.pdf` prints the annotation text with `Paper.pdf` as the document
and `Paper.pdf.mark` noted as its source; JSON uses `name` and optional `mark`
fields. Text exports retain this attribution. `note` reports the document name
and mark too. These commands read typed boxes and stored recognition from the
mark; they do not extract EPUB source text. `highlights` retains its existing
EPUB warning and null passage text. Notebook completion offers documents with
unambiguous full-name marks, including NFC-equivalent cloud sidecars.
No OCR or new recognition is performed. `--page` accepts repeated or
comma-separated stored page numbers. Missing pages are errors, and duplicates
do not repeat a page.

`--format text` prints page labels when several pages are selected. Markdown
adds page headings and title headings. Supernote titles are generally bitmap
regions without text labels: these appear as `Title` headings rather than
invented recognized words. JSON retains page geometry, text-box strings,
recognition text, title locations and notebook inspection fields.

`-o`/`--output FILE` saves the text instead of printing it. Unless `--format`
is explicit (or JSON is selected), `.md`, `.txt` and `.json` infer Markdown,
text and JSON respectively; other extensions keep the default text format.
An existing destination directory receives `<notebook stem>.<extension>`.
Like get/render, a completed local file atomically replaces an existing file;
there is no prompt. `-o -` keeps the ordinary stdout text/JSON behavior.

For a cloud folder, `-o DIR` is required. Export visits its descendants and
writes one file per recognized `.note`, keeping the tree with the same safe,
collision-resistant names as render/mget. Typed-only notebooks are skipped.
Folder export rejects `--page`; it has no incremental or deletion mode, so it
replaces matching files and leaves other local files alone. Outputs are confined
to the opened destination root. Stdout reports a path per completed output;
JSON answers are `{"paths":[...]}`. File contents use the selected text format.

```sh
snctl text journal.note -o journal.md
snctl text NOTE/Note -o ./recognized --format markdown
snctl text journal.note --raw --json
snctl find --text 'release notes' NOTE/Note
snctl find --text 'release notes' ./notes --json
```

Text output, saved text, search and mirror Markdown normalize CRLF and bare CR
to LF and remove U+00AD (soft hyphen), U+200B (zero width space), U+200C
(zero width non-joiner), U+200D (zero width joiner), U+2060 (word joiner), and
U+FEFF (BOM/zero width no-break space). Other characters and spacing are
preserved. snnote-go's parsed data and the notebook bytes are never changed.
`text --raw` requires JSON and preserves stored strings and line endings;
`note --raw` continues to expose stored metadata and uncleaned text.

`find --text WORDS [path]` searches recognized handwriting and typed text boxes
in `.note` and `.mark` files. It splits the cleaned query at whitespace and
requires every word as a substring somewhere in the notebook, ignoring case.
Words may be on different pages; results report the stored numbers of every
page containing at least one query word. There is no phrase, regex or word-boundary
syntax. Table output shows FLAGS, path and comma-separated pages. Plain output
retains path, comma-separated pages and recognition, without a flags field.
A matched full-name sidecar is reported as its document, with sidecar metadata, unless `--marks` is selected.
The path may name a single PDF/EPUB document as well as a mark or folder;
a single document resolves its annotations. If its `.mark` is absent,
`find --text WORDS <document>` exits **1** with `<document> has no annotations`.
JSON is an array of `{"entry":{...},"pages":[...]}`. Missing or unreadable bodies
fail the search rather than silently losing matches. Like rmctl, text search
reads bodies through the cache; snctl's explicitly chosen all-words matching
also supports recognition text and cleaning.

## Recognition marks

A notebook has recognition when **any readable page contains nonblank recognition
text after cleaning**. Typed text boxes, `FILE_RECOGN_TYPE`, language and status
flags alone do not qualify. A whitespace-only or invisible-only recognition
string is unrecognized. The cloud listing has no recognition information.

`ls`, `tree`, `stat` and name-based `find` look up content MD5 in a private
per-profile index without downloading. JSON retains `"recognition": true|false|null`;
non-notebooks use null. Human tables and trees use FLAGS: `R` recognized
handwriting, `H` highlights/annotations. Letters use that order; empty cells are
`-`. Unknown recognition and known false both omit `R`, with the distinction
preserved in JSON and the table `stat` recognition field. FLAGS precedes the name
in human tables and trees. Plain output retains its existing fields and tree
indentation without flags; `--recognition` appends its existing recognition
field to plain `ls`, name-based `find` and `tree`. Scripts use `--json` for
flags. `--recognition` discovers unknown notebooks.
A nonempty human listing prints `Flags: R recognized handwriting, H highlights`
on stderr above the answer; plain/JSON and empty results omit the legend.
JSON adds `"flags":["recognition"]` or `["highlights"]` (both in that order
when applicable), and `[]` for none. Existing JSON fields retain their types.

Full-name sidecars fold into their document: `Paper.pdf.mark` belongs to
`Paper.pdf`, `Book.epub.mark` to `Book.epub`, in the exact same folder and spelling.
The document's JSON gains `mark` with the sidecar's path, ID, name, hash, size and
modified time, all from the listing; no body is fetched. A missing document leaves
its orphan `.mark` visible. Legacy stem-only `.mark` files remain separate, as do
ambiguous duplicate paths. `--marks` on `ls`, `tree`, `find` or `dupes` shows raw
sidecars. `stat DOCUMENT` reports its sidecar; `stat SIDECAR` remains direct.
The recycle bin has no original folders and cannot establish sibling pairs, so
`trash ls` retains its raw entries. Downloads, cache, backup and mirror records
keep both physical files. Local `find --text` follows the same folding rule.

`ls --recognition`, `tree --recognition` and `find NAME --recognition` inspect
only unknown `.note` bodies in their selected results. Discovery plans the
unique unknown bodies against the cache budget before any download, then uses
the usual verified cache and progress policy. `--offline` requires retained
bodies; `--no-cache` discovers marks for that answer without reading or writing
the index or body cache. Successful notebook reads and drawing fill the index,
as do get, `sync --files` (including previously cached bodies), mirror and
`find --text`. A changed MD5 is unknown until inspected; renames and identical
copies reuse the same boolean. Invalid or unreadable bodies remain `?`: discovery
warns for each failed entry, continues inspecting the remaining notebooks, prints
the partial listing and exits 1, following bulk transfer conventions. Successful
metadata is saved even when interrupted. Scans also retain damaged verified bodies
under the normal cache budget, so repeated scans can retry parsing locally instead
of downloading the same bytes. They may be downloaded again after eviction, with
`--no-cache`, or if caching failed. The boolean index has no durable parse-failure
state, so a parser upgrade can retry the body. A parse failure is never recorded
as unrecognized.

## Highlights

```sh
snctl highlights paper.pdf --format text
snctl highlights paper.mark --page 2 --format markdown
snctl highlights paper.mark --json
```

Pass a PDF or EPUB document to resolve its full-name `.mark` automatically,
or pass the `.mark` directly as before. Without a sidecar the command fails
with `<document> has no annotations`. Completion offers documents with marks
and raw `.mark` files, using cached remote listings only.
Only `.mark` notebooks carry document highlights. snrender-go v1.0.0 extracts
passage text from the sibling source PDF using document rectangles and stored
page numbers, in the PDF engine's reading order. It performs no OCR. Local
`Report.pdf.mark` uses `Report.pdf`; legacy `Report.mark` uses `Report.pdf` only
when there is no same-stem EPUB. `Report.epub.mark` has no PDF source. These are
the same pairing rules as render and mirror. Cloud reads use the canonical
sidecar path and the ordinary verified download cache: `--offline` requires
retained `.mark` and PDF bodies (prepare them with `sync DOCUMENT/Document --files`);
`--no-cache` bypasses that cache. Local inputs
never need a login or contact the cloud.

Text appears in text/plain, table, Markdown and JSON answers (`--format` or
`--json`). JSON adds `"text": string|null` to each highlight, retaining the
existing fields. An empty extracted string is successful; null means unavailable.
Text/plain and table escape backslashes, tabs and line breaks as `\\`, `\t`,
`\n` and `\r`, keeping each passage on one `text:` line. Markdown puts the same
escaped text in an indented literal block, so passage markup cannot become headings
or geometry lines. JSON retains the original string.
Any failure to obtain the source PDF, including not found, ambiguous pairing,
network or authentication errors, and offline operation without a cached PDF,
leaves text null and warns on stderr without failing the command. EPUBs, lite
builds and extraction failures degrade the same way. Cancellation still fails.
Source PDFs are limited to 512 MiB (the renderer's PDF text limit), or the
configured cache body budget if smaller. Oversized PDFs also warn and leave text
null; local reads stop after the limit plus one byte, and cloud sizes are checked
before download.
`--page` extracts only selected highlights, including pages without ink sheets.
The PDF is fetched once. snrender-go v1.0.0 reparses it once per highlight and
returns one string for a rectangle union; it has no reusable document or batch
API that preserves separate passages. Extraction is sequential with one call per
selected highlight, so many highlights on a large PDF can be slow. Use `--page`
to limit that work.
PDFKit/PDFium may differ in ligatures and multi-column reading order; text is
cleaned by the renderer. Comments remain separate user annotations.

`text` and `find --text` continue to use typed boxes and recognition, which have
never included highlight comments or passages in their text stream. `text`
notebook JSON metadata also exposes the additive nullable highlight `text` field;
neither command loads a source PDF for it.

Every highlight preserves its raw color code, optional comment, stored page
number, screen rectangles in page pixels and document rectangles in the source document's coordinate space.
Unknown color codes are preserved rather than guessed. The `.mark` bytes
themselves carry no quoted passage text. Markdown
uses page headings and labeled geometry. JSON preserves missing comments and
rectangles as null where the original format does. Obsidian output is outside this phase.

## Inspection

`note <file|path>` reports kind, format version, device spelling, readable page
count, dimensions, templates, layers per page, strokes, links, titles, keywords,
highlights and skipped pages. `--raw` includes header and footer metadata in
stored order, preserving duplicate keys. `--format table|text|json` and `--json`
select the ordinary answer format. Raw metadata may include notebook names and
identifiers; it is requested explicitly.

The parser may skip optional damaged content while retaining readable pages.
Inspection exposes skipped page and stroke counts. Inputs with no readable
notebook structure fail with status 1; out-of-range page requests fail with 2.
For `.spd`, `note` reports kind `drawing`, canvas width/height (unknown values
are `-`, or null in JSON), layers with stored IDs/names and per-layer tile counts,
and total tiles. JSON adds a `drawing` object with `width`, `height`,
`format_version`, `layers` and `tiles`; notebook fields retain their existing
types. `--raw` does not add SQLite config or tile bytes. `text`, `highlights` and
notebook page selectors do not apply to drawings.
See [Rendering](rendering.md) for drawing, temporary previews and native limits.

Folder rendering records its endpoint, saved account identifier, and cloud folder identifier and path in `.snctl-render.json`. A different source or an old record without identity requires `--force` to start a new record. Output paths and the record use one root-confined handle; symbolic links and Windows junctions below the output directory are rejected.

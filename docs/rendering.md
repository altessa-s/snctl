# Rendering

Back to the [README](../README.md).

`render <path|file> [-o file|dir]` draws a local or cloud `.note`, `.mark`,
PDF/EPUB with annotations, or Atelier `.spd`, or a cloud notebook folder.
Input follows the [notebook path rule](notebooks.md). Cloud input
uses canonical paths and checksum-verified downloads when the server supplies
a checksum. No source is edited.

| Choice | Result |
|---|---|
| No page, PDF | Every readable page in one PDF |
| `--page N` | One notebook page number, PNG by default |
| `--format png\|jpg\|svg` without `--page` | Numbered images in a directory |
| `-o` | File or existing directory for one output; existing directory or trailing separator for multiple outputs |
| `--scale` | PNG/JPEG/SVG pixels per tablet unit; native range 1/64–8, default 1 |
| `--quality` | JPEG quality 1–100; 0 selects Go's default 75 |
| `--no-template` | Hide the stored background template |
| `--no-text` | Omit the existing invisible recognition layer |
| `--source PDF` | Source PDF (local file or cloud path) for a single `.mark` overlay |
| `--strict-images` | Refuse damaged image layers instead of skipping them |

Without an explicit format, a recognized `-o` extension decides; otherwise a
selected page becomes PNG and a whole notebook PDF. Default names are
`<name>-rendered.pdf` and `<name>-pNNNN.<format>`. An image directory defaults to
`<name>-rendered`, containing `<name>-p0001.<format>` and so on.

Without a source PDF, page numbers are the notebook's own numbers listed by `note`: sequential from 1
for `.note`, source-document numbers for `.mark`. Selectors, JSON page fields and
image names such as `p0022` use these numbers. Missing numbers return status 2
with the available ranges.

An image directory is selected by an existing directory, a trailing path separator,
or an omitted `-o` with an image format. An image filename without `--page` is a
usage error. `-o -` streams a whole-notebook PDF or one selected image to stdout;
it rejects directory/folder outputs and JSON answers. Notices remain on stderr.

The command queries scale bounds from the linked renderer at runtime. A page
that would exceed its default pixel limit returns status 2 with the largest
scale that fits. SVG embeds a raster and therefore accepts the same scale option.

```sh
snctl render journal.note -o journal.pdf
snctl render journal.note --page 1 -o first.svg
snctl render journal.note --format png -o journal-pages/
snctl render NOTE/Note -o notebooks --incremental -j 4
snctl open journal.note --page 1
```

Each file is written into a private temporary sibling and renamed only after
success. SIGINT returns 130; SIGTERM returns 143. Both cancel and remove the unfinished file. In a
multi-file export, already completed files remain; the directory is not a
transaction. Download bytes and drawn pages have separate progress phases on terminal stderr. Concurrent folder runs show one aggregate count of completed documents.

## PDF and EPUB documents

Pass the document shown by `ls` directly to `render` or `open`. A unique sibling
with its full name plus `.mark` is used exactly as if that sidecar were passed:
`Report.pdf` selects `Report.pdf.mark`, and `Book.epub` selects `Book.epub.mark`.
Output names, page numbers, `--page`, `--format`, `--source` and `-o` follow the
sidecar's existing rules. Ambiguous sidecars fail with a conflict. Legacy
stem-only `Report.mark` files must still be passed directly.

PDF annotations overlay the sibling PDF. EPUB overlay remains unsupported:
passing an EPUB with marks produces the same standalone annotation sheets and
notice as passing its `.epub.mark` directly.

Without a sidecar, `open` copies the original PDF/EPUB into its private preview
directory and opens the viewer. `open --no-marks` skips sidecar lookup and opens
the original even when annotations exist. Stderr reports the missing or skipped
annotations. Rendering options apply only when drawing annotations; original
previews retain the document's bytes and report zero rendered pages.

`render` without annotations fails with a not-found error explaining that there
is nothing to render; use `snctl get DOCUMENT -o FILE` to download the original.
`render --no-marks` is an argument error. `open --no-marks` requires a PDF/EPUB.
Folder exports remain limited to `.note`/`.mark` files. Completion for `render`
and `open` offers documents with full-name marks, notebooks and drawings.

```sh
snctl render DOCUMENT/Document/Report.pdf -o annotated.pdf
snctl open ./Book.epub
snctl open DOCUMENT/Document/Report.pdf --no-marks
```

Cloud documents and sidecars use the same verified download cache as direct
`.mark` input. `--offline` needs a cached listing and retained bodies (prepare
with `sync DOCUMENT/Document --files`); overlays need both `.mark` and PDF
bodies, while original previews need only the document body. `--no-cache`
bypasses the cache. Local documents and their sidecars stay local and need no
login. Failed previews are cleaned up; successful original previews have the
same lifetime as rendered previews below.

## Atelier drawings

```sh
snctl render sketch.spd -o sketch.png
snctl render sketch.spd --drawing-page canvas --scale 2 -o sketch.svg
snctl render EXPORT/sketch.spd -o sketch.pdf
snctl open ./sketch.spd
snctl note ./sketch.spd --json
```

A single `.spd` produces one PNG, PDF or SVG, without a notebook `--page` number.
Without `-o`, the name is `<stem>-rendered.<format>` (PDF by default); an existing
directory receives that name. `-o - --format png|pdf|svg` streams bytes, using
the same atomic output, cache and offline rules as notebooks. `open` uses the
same private preview lifetime. Folder exports remain limited to `.note`/`.mark`.

`--drawing-page painted` (default) frames the union of stored tiles, including
transparent/damaged tiles; an empty drawing is 1×1. `--drawing-page canvas`
clips to saved canvas dimensions centred on the viewport. **Only one real
Atelier sample has been studied; viewport centre anchoring and scale semantics
are inferred, not established format guarantees.** Layers composite in stored
order, including the reference layer; unknown visibility flags are not guessed.

`--scale` changes raster resolution for all three drawing formats, including PDF;
PDF physical size uses canvas PPI and stays fixed. SVG and PDF embed raster
images. `--no-template` selects a transparent canvas instead of white;
`--strict-images` refuses damaged/unsupported tiles instead of skipping them.
Drawings reject JPEG, notebook `--page`, `--source`, `--quality` and `--no-text`.
There is no separate DPI flag because snctl already uses scale.

The format is a SQLite snapshot with PNG tiles. Supply a complete saved export;
uncheckpointed WAL sidecars cannot be read or detected. Tile/render limits apply,
and parsing is synchronous but bounded. Drawing inspection requires cgo and is
available in lite builds; rendering requires a full build. Listings keep their
existing file/folder classification. No drawing is modified.

## Folder PDFs

A folder is exported recursively as PDFs, retaining its folder hierarchy with
filesystem-safe names. Every member of a colliding PDF-name group gains an identifier suffix, independent of listing order. Windows device names and long components are made portable. `-j` /
`--jobs` bounds concurrent notebooks, default 4. Failures are reported per
document while other documents continue; any failure makes the command fail.
`-a` / `--all` includes recycled notebooks. sncloud-go v1.0.0 does not expose
their original paths, so `render / --all` places them under `.trash`.
A narrower folder with ambiguous recycled notebooks is refused with guidance
to use the root. Recycled folders cannot be traversed through this library;
only notebook entries returned by the recycle-bin listing can be exported.

`.snctl-render.json` records source identifier, content hash, modification time
and render options for each completed output. `--incremental` skips only when
the record agrees and the output retains the source modification time. Missing
hashes or timestamps, a missing/damaged record, missing outputs, changed options
or edited output timestamps cause drawing again. Exports with skipped pages
are not recorded. Completed draws merge atomically into the existing record; an
interrupted run retains entries it did not reach. Runs that draw nothing do not
rewrite the record. Missing outputs are discarded from the next saved record. Removed or renamed source files leave previous PDFs in place;
there is no pruning in this phase. Folder exports reject page and image options.

## Preview lifetime

`open` draws a PDF or selected-page image, or copies an original document,
in a private temporary directory and opens the system viewer through `open`,
`xdg-open` or Windows ShellExecute.
The directory remains after success because the viewer outlives snctl. Remove
it after closing the viewer. Failed previews are removed when possible.

## Limits and build requirements

snrender-go v1.0.0 overlays `.mark` annotations on a source PDF. A single-file
`render` or `open` automatically looks for the sibling PDF: `Report.pdf.mark`
uses `Report.pdf`; a stem-only `Report.mark` looks for `Report.pdf`. If a PDF
and EPUB share that stem, the legacy sidecar is ambiguous: render warns and
draws standalone sheets. Rename it to `Report.pdf.mark` or pass `--source`
to select the PDF. Cloud lookup
uses the sidecar's canonical path. Missing
sources retain transparent-sheet behavior. Other lookup/download failures are
reported, so an inaccessible PDF is not silently omitted.

Use `--source <local file|cloud path>` to supply a different source PDF explicitly:

```sh
snctl render Report.pdf.mark -o annotated.pdf
snctl render annotations.mark --source original.pdf --page 3 -o page.png
snctl render DOCUMENT/Document/Report.pdf.mark -o annotated.pdf --json
```

Existing local files take precedence for `--source`, including bare relative
names such as `Report.pdf` and files without an extension. Siblings of local
inputs are always local. Use a leading `/` and the full cloud path (for example,
`/DOCUMENT/Document/Report.pdf`) to bypass a relative local namesake. Existing
absolute local files remain accepted.

An explicit source need not have a `.pdf` filename; its contents must be PDF.
Explicit missing sources fail. EPUB overlay is unsupported. Overlays support
whole-document PDF and PNG with `--page`; page numbers refer to the source PDF,
including pages with no annotations. JPEG, SVG and image-directory overlays are
refused with an options error. Without a source, stored-sheet selection and
transparent PNG/SVG output remain available; JPEG uses a white background.
Folder exports continue to draw standalone sheets and reject `--source`.

Overlay PDF preserves the source's page objects and appends ink; standalone
notebook PDF/SVG embeds page images. Diagnostics about dropped sheets/highlights,
geometry disagreement and signatures appear on stderr and in an optional
`warnings` array in JSON. Existing JSON fields remain unchanged; `sheets_only`
is false for overlays. Signatures are not validated. Geometry and filenames
cannot prove the source's identity.
PDF and SVG embed page images rather than vector stroke contours. PDF can
carry stored recognition text, supported links and title outlines. There is no
OCR. Recognition layout and extraction follow the native backend's limitations.
Damaged optional images are skipped unless `--strict-images`; native resource,
allocation and cancellation limits still apply.

Full builds require cgo and a C23 compiler. macOS uses Apple frameworks from
Xcode command-line tools. Linux uses Cairo >= 1.18, cairo-ft, FreeType,
GdkPixbuf and zlib through pkg-config; Debian trixie provides this stack with
clang. Windows uses MSYS2 UCRT64 gcc, pkgconf, cairo, freetype, gdk-pixbuf2 and
zlib, with the runtime DLLs on PATH. Every Cairo build (including macOS with
`snrender_cairo`) also requires the pinned PDFium SDK at build and run time.
Apple's default macOS backend needs no PDFium. See [go.mod](../go.mod) for the reader and renderer versions.

For a Linux source build, fetch the checksum-verified SDK from the binding and
pass its paths explicitly (choose `linux-arm64` for ARM64):

```sh
go mod download github.com/altessa-s/snrender-go
renderer=$(go list -m -f '{{.Dir}}' github.com/altessa-s/snrender-go)
python3 "$renderer/internal/clib/snrender/scripts/fetch-pdfium.py" --platform linux-x64 --output "$PWD/.cache/pdfium"
export CGO_CFLAGS="-I$PWD/.cache/pdfium/include"
export CGO_LDFLAGS="-L$PWD/.cache/pdfium/lib -Wl,-rpath,$PWD/.cache/pdfium/lib"
CC=clang-19 make snctl
```

Windows setup uses `win-x64`/`win-arm64`, Windows-form include/library paths
(`cygpath -m`), and PDFium's `bin` on PATH. Linux and Windows release scripts
bundle the runtime and all its notices; keep the archive files together.
Lite (`norender`) requires neither the PDFium SDK nor its runtime.

`make snctl` builds the full native binary when cgo is enabled.
`make snctl-lite` uses `norender` and retains notebook reading. Distributed lite
builds enable cgo on macOS, Linux and Windows. See [builds and releases](releases.md)
for runtimes and licences. `CGO_ENABLED=0` source builds omit both native
libraries. Commands remain visible and explain missing capabilities with status
5. `version --full` reports `render` and `notes`.

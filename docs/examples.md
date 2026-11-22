# Examples

Back to the [README](../README.md).

These transcripts come from a local Supernote Private Cloud with sanitized
notebook and PDF fixtures. Commands with jq select a few fields from the full
answer. Times and account totals depend on the library.

## Browsing

```console
$ snctl ls DOCUMENT/Document/Reading --format table
TYPE     SIZE  MODIFIED          FLAGS  NAME
────  ───────  ────────────────  ─────  ─────────
-     1.6 KiB  2026-11-22 11:11  H      Paper.pdf
d           -  2026-11-22 11:13  -      Copy
```

```console
$ snctl tree NOTE/Note/Work --format plain
Journal.note
```

The H flag means the document has a full-name mark beside it. The legend
goes to stderr. Use JSON to inspect the pair.

```console
$ snctl stat DOCUMENT/Document/Reading/Paper.pdf --json | jq '{path,flags,sidecar:.mark.path}'
{
  "path": "DOCUMENT/Document/Reading/Paper.pdf",
  "flags": [
    "highlights"
  ],
  "sidecar": "DOCUMENT/Document/Reading/Paper.pdf.mark"
}
```

## Finding files and notebook text

```console
$ snctl find Journal NOTE/Note/Work --format table
TYPE       SIZE  MODIFIED          FLAGS  NAME
────  ─────────  ────────────────  ─────  ───────────────────────────
-     372.3 KiB  2026-11-22 11:11  -      NOTE/Note/Work/Journal.note
```

The search reads stored recognition and typed text boxes. This notebook
has typed text on page 1. The last plain field reports handwriting recognition.

```console
$ snctl find --text Проверка NOTE/Note/Work --format plain
NOTE/Note/Work/Journal.note	1	no
```

```console
$ snctl note NOTE/Note/Work/Journal.note --json | jq '{kind,device,format_version,pages:(.pages|length)}'
{
  "kind": "note",
  "device": "N6",
  "format_version": 20260016,
  "pages": 2
}
```

```console
$ snctl text NOTE/Note/Work/Journal.note --page 1
Проверка текстового блока
Проверка текстового блока 2, другой размер
Еще один текстовый блок
```

## Highlights

```console
$ snctl highlights DOCUMENT/Document/Reading/Paper.pdf --json | jq '[.highlights[] | {page,comment,text}]'
[
  {
    "page": 1,
    "comment": null,
    "text": ""
  },
  {
    "page": 1,
    "comment": "Test comment",
    "text": ""
  },
  {
    "page": 1,
    "comment": null,
    "text": ""
  }
]
```

The PDF has no text under these rectangles. An empty string means the
engine read the PDF. A null passage means text was unavailable. Markdown
also retains the geometry and the comment.

## Rendering

```console
$ snctl render NOTE/Note/Work/Journal.note -o Journal.pdf --json
{
  "path": "Journal.pdf",
  "pages": 2,
  "sheets_only": false,
  "skipped_pages": 0
}
```

```console
$ snctl render NOTE/Note/Work/Journal.note --page 1 -o page.png --json
{
  "path": "page.png",
  "pages": 1,
  "sheets_only": false,
  "skipped_pages": 0
}
```

## Measuring

```console
$ snctl du NOTE/Note/Work -d 1 --format table
PATH                  SIZE  FILES
───────────────  ─────────  ─────
/NOTE/Note/Work  372.3 KiB  1

used      413.4 KiB
capacity  24.3 GiB
recycled  0 B
```

```console
$ snctl dupes DOCUMENT/Document/Reading --format table
HASH                                 SIZE  PATH
────────────────────────────────  ───────  ────────────────────────────────────────
3ba5cf7160e33fc89440df2326325ca7  1.6 KiB  DOCUMENT/Document/Reading/Copy/Paper.pdf
3ba5cf7160e33fc89440df2326325ca7  1.6 KiB  DOCUMENT/Document/Reading/Paper.pdf
```

## Downloading and uploading trees

```console
$ snctl mget NOTE/Note/Work -o downloads --format table
STATUS  CLOUD                        LOCAL                        SIZE  REASON
──────  ───────────────────────────  ──────────────────────  ─────────  ──────
cached  NOTE/Note/Work/Journal.note  downloads/Journal.note  372.3 KiB  -

cached  1
```

```console
$ snctl mget NOTE/Note/Work -o downloads --incremental --format plain
unchanged	NOTE/Note/Work/Journal.note	downloads/Journal.note	381225	-

unchanged	1
```

With `--delete`, mget removes previously recorded files that disappeared
upstream, subject to its deletion guard. Locally edited or unrecorded files
stay. Names that collide on a case-insensitive disk receive ID suffixes.

```console
$ snctl mput downloads NOTE/Note/Work --incremental --yes --json | jq .counts
{
  "skipped": 1,
  "unchanged": 1
}
```

The skipped item is the hidden download record. To preview additions,
point mput at another folder. A terminal displays the plan before asking
for confirmation. A pipe needs `--yes` to apply it.

```console
$ snctl mput downloads NOTE/Note/Work/Imported --dry-run --format plain
create	NOTE/Note/Work/Imported	-	-	-
upload	NOTE/Note/Work/Imported/Journal.note	downloads/Journal.note	381225	-
skipped	-	downloads/.snctl-mget.json	0	hidden file

upload	1
create	1
skipped	1
```

```console
$ snctl mput downloads NOTE/Note/Work/Imported --yes --json | jq .counts
{
  "created": 1,
  "skipped": 1,
  "uploaded": 1
}
```

## Single files and folders

Start with three local PDFs. Upload two to `Filed` and fetch that folder
once so mget records the local copies.

```console
$ snctl mkdir -p DOCUMENT/Document/Reading/Filed --yes --format plain
d	-	-	DOCUMENT/Document/Reading/Filed

$ snctl put Review.pdf --to DOCUMENT/Document/Reading --yes --format plain
-	1614	-	DOCUMENT/Document/Reading/Review.pdf

$ snctl put Draft.pdf Report.pdf --to DOCUMENT/Document/Reading/Filed --dry-run --format plain
upload	DOCUMENT/Document/Reading/Filed/Draft.pdf	Draft.pdf	431	-
upload	DOCUMENT/Document/Reading/Filed/Report.pdf	Report.pdf	827	-

upload	2

$ snctl put Draft.pdf Report.pdf --to DOCUMENT/Document/Reading/Filed --yes --format plain
-	431	-	DOCUMENT/Document/Reading/Filed/Draft.pdf
-	827	-	DOCUMENT/Document/Reading/Filed/Report.pdf

$ snctl mget DOCUMENT/Document/Reading/Filed -o filed --format plain
cached	DOCUMENT/Document/Reading/Filed/Draft.pdf	filed/Draft.pdf	431	-
cached	DOCUMENT/Document/Reading/Filed/Report.pdf	filed/Report.pdf	827	-

cached	2
```

Move the review into `Filed`, rename it, then move the draft out. The next
download uses the record to remove the local draft and keep the report.

```console
$ snctl mv DOCUMENT/Document/Reading/Review.pdf DOCUMENT/Document/Reading/Filed --yes --format plain
moved to DOCUMENT/Document/Reading/Filed

$ snctl rename DOCUMENT/Document/Reading/Filed/Review.pdf "First review.pdf" --yes --format plain
moved to DOCUMENT/Document/Reading/Filed/First review.pdf

$ snctl mv DOCUMENT/Document/Reading/Filed/Draft.pdf DOCUMENT/Document/Reading --yes --format plain
moved to DOCUMENT/Document/Reading

$ snctl mget DOCUMENT/Document/Reading/Filed -o filed --incremental --delete --format plain
cached	DOCUMENT/Document/Reading/Filed/First review.pdf	filed/First review.pdf	1614	-
unchanged	DOCUMENT/Document/Reading/Filed/Report.pdf	filed/Report.pdf	827	-
deleted	-	filed/Draft.pdf	431	-

cached	1
unchanged	1
deleted	1
```

## For scripts

```console
$ snctl ls DOCUMENT/Document/Reading --json | jq -r '.[] | select(.mark) | .path'
DOCUMENT/Document/Reading/Paper.pdf
```

Use the returned tablet path as one quoted argument. Bulk uploads also
accept individual files with `put --to`. For scheduling, exporting Markdown
and reacting to changes, see [automation](automation.md).

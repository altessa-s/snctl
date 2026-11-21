# Examples

Back to the [README](../README.md).

The measuring, downloading and uploading answers below come from the built
binary run against the fake private cloud the tests use
(`internal/clouddouble`), in the `table` and `plain` formats; long reports are
shortened with `…`. The terminal question and the last section show the shape
of the answer with made-up files. A real account answers the same way with its
own names, sizes and times.

## Measuring

```console
$ snctl du / -d 2
PATH                     SIZE  FILES
──────────────────  ─────────  ─────
/                   303.6 KiB  58
/DOCUMENT           192.8 KiB  32
/DOCUMENT/Document  192.8 KiB  32
/EXPORT                   0 B  0
/INBOX                    0 B  0
/NOTE               110.8 KiB  26
/NOTE/MyStyle             0 B  0
/NOTE/Note          110.8 KiB  26
/SCREENSHOT               0 B  0

used      303.6 KiB
capacity  1.0 GiB
recycled  0 B

$ snctl dupes DOCUMENT/Document/Papers
HASH                                  SIZE  PATH
────────────────────────────────  ────────  ──────────────────────────────────────────
7a0be5e9faf9eb9518def1802ba3a2e1  11.7 KiB  DOCUMENT/Document/Papers/attention (1).pdf
7a0be5e9faf9eb9518def1802ba3a2e1  11.7 KiB  DOCUMENT/Document/Papers/attention.pdf
```

## Downloading everything, then keeping it current

```console
$ snctl mget / -o supernote -j 8
STATUS      CLOUD                                       LOCAL                                                     SIZE  REASON
──────────  ──────────────────────────────────────────  ────────────────────────────────────────────────────  ────────  ──────
downloaded  DOCUMENT/Document/Books/novel.epub          supernote/DOCUMENT/Document/Books/novel.epub          17.6 KiB  -
downloaded  DOCUMENT/Document/Papers/attention (1).pdf  supernote/DOCUMENT/Document/Papers/attention (1).pdf  11.7 KiB  -
downloaded  DOCUMENT/Document/Papers/attention.pdf      supernote/DOCUMENT/Document/Papers/attention.pdf      11.7 KiB  -
downloaded  NOTE/Note/Home/Ideas 14.note                supernote/NOTE/Note/Home/Ideas 14.note                  1000 B  -
downloaded  NOTE/Note/Home/Journal 03.note              supernote/NOTE/Note/Home/Journal 03 (40).note             14 B  -
…
downloaded  NOTE/Note/Home/journal 03.note              supernote/NOTE/Note/Home/journal 03 (39).note             14 B  -
…
downloaded  NOTE/Note/Work/Sketch 08.note               supernote/NOTE/Note/Work/Sketch 08.note                2.3 KiB  -

downloaded  30
```

`Journal 03.note` and `journal 03.note` would be one file on a file system
that ignores case, so both carry their identifiers.

```console
$ snctl mget / -o supernote --incremental --format plain
unchanged	DOCUMENT/Document/Books/novel.epub	supernote/DOCUMENT/Document/Books/novel.epub	18000	-
…
unchanged	NOTE/Note/Work/Sketch 08.note	supernote/NOTE/Note/Work/Sketch 08.note	2400	-

unchanged	30
```

After `NOTE/Note/Work/Reading 07.note` was removed in the cloud:

```console
$ snctl mget / -o supernote --incremental --delete
STATUS     CLOUD                                       LOCAL                                                     SIZE  REASON
─────────  ──────────────────────────────────────────  ────────────────────────────────────────────────────  ────────  ──────
unchanged  DOCUMENT/Document/Books/novel.epub          supernote/DOCUMENT/Document/Books/novel.epub          17.6 KiB  -
…
unchanged  NOTE/Note/Work/Sketch 08.note               supernote/NOTE/Note/Work/Sketch 08.note                2.3 KiB  -
deleted    -                                           supernote/NOTE/Note/Work/Reading 07.note               1.4 KiB  -

unchanged  29
deleted    1
```

## Uploading a tree

```console
$ snctl mput supernote DOCUMENT/Document/Restore -j 4 --yes
STATUS    CLOUD                                                                 LOCAL                                                     SIZE  REASON
────────  ────────────────────────────────────────────────────────────────────  ────────────────────────────────────────────────────  ────────  ───────────
created   DOCUMENT/Document/Restore/DOCUMENT                                    -                                                            -  -
created   DOCUMENT/Document/Restore/DOCUMENT/Document                           -                                                            -  -
…
created   DOCUMENT/Document/Restore/NOTE/Note/Work/2026                         -                                                            -  -
uploaded  DOCUMENT/Document/Restore/DOCUMENT/Document/Books/novel.epub          supernote/DOCUMENT/Document/Books/novel.epub          17.6 KiB  -
…
uploaded  DOCUMENT/Document/Restore/NOTE/Note/Work/Sketch 08.note               supernote/NOTE/Note/Work/Sketch 08.note                2.3 KiB  -
skipped   -                                                                     supernote/.snctl-mget.json                                 0 B  hidden file

uploaded  29
created   12
skipped   1

$ snctl mput supernote DOCUMENT/Document/Restore --incremental --yes --json
{
  "items": [
    …
  ],
  "counts": {
    "skipped": 1,
    "unchanged": 29
  }
}
```

Without `--yes` on a terminal, the plan is listed before the question:

```console
$ snctl mput ~/Papers DOCUMENT/Document/Papers
  create   DOCUMENT/Document/Papers/2026
  upload   DOCUMENT/Document/Papers/2026/attention.pdf
  upload   DOCUMENT/Document/Papers/2026/bert.pdf
Below DOCUMENT/Document/Papers: upload 2 files, create 1 folder (1.2 MiB)? [y/N]
```

## Single files and folders

```console
$ snctl put a.pdf b.epub --to DOCUMENT/Document --dry-run --format plain
upload	DOCUMENT/Document/a.pdf	a.pdf	3	-
upload	DOCUMENT/Document/b.epub	b.epub	3	-

upload	2

$ snctl mkdir -p DOCUMENT/Document/Papers/2026/Q1 --yes
$ snctl mv NOTE/Note/Plan.note NOTE/Note/Work --yes
$ snctl rename NOTE/Note/Work/Plan.note "Plan 2026.note" --yes
```

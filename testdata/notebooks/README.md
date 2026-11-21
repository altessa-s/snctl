# Notebook fixtures

The project owner contributed three anonymised Supernote Nomad (N6) captures,
format 20260016, under the [MIT licence](LICENSE). These are byte-for-byte
copies of the sanitized [snnote-lib v1.0.0 samples](https://github.com/altessa-s/snnote-lib/tree/v1.0.0/tests/fixtures/samples).

| Local file | Upstream sample | Contents |
|---|---|---|
| highlights.mark | pdf-highlights-n6-20260016.mark | Generated wave, two highlights and an underline with paired sheet and PDF coordinates |
| text.note | textbox-links-n6-20260016.note | Three Russian text boxes, a heading, an internal page link and a web link |
| sparse.mark | epub-n6-20260016.mark | Cleared layer on document page 22, the only sheet in the file |

The samples have replacement file and page identifiers. The text notebook
has generated link targets and template. The wave replaces both the original
stroke record and its raster. The [overlay fixture](../overlay/README.md)
uses the same sanitized mark.

`blank.note` is a test-only copy of Brandon Philips’s supernote-typescript
fixture, distributed under the [Apache License 2.0](LICENSE-supernote-typescript).
Upstream renamed `nomad-3.26.40-blank-2p.note` to
`blank-a6x-3.26.40-two-pages.note`. snctl shortened the filename and kept the
bytes intact, matching the [snrender-go v1.0.0 copy](https://github.com/altessa-s/snrender-go/blob/v1.0.0/testdata/supernote-typescript/blank-a6x-3.26.40-two-pages.note). Release archives omit the fixture.
[Upstream fixture at the imported revision](https://github.com/philips/supernote-typescript/blob/52dde1ecb0735d76df56e33226e80045339feb06/tests/input/blank-a6x-3.26.40-two-pages.note).

The synthetic fixtures are byte-for-byte copies from [snrender-go v1.0.0](https://github.com/altessa-s/snrender-go/tree/v1.0.0/testdata).

| Local file | Upstream fixture | Generator |
|---|---|---|
| `drawing.spd` | `spd/valid.spd` | `tests/generate_spd.py` |
| `drawing-bad.spd` | `spd/bad.spd` | `tests/generate_spd.py` |
| `source-text.pdf` | `pdf-text/r0-offset0.pdf` | `tests/check_pdf_text.py` |

All three use the accompanying MIT licence and contain synthetic data. The
drawing has a 256×256 canvas, three layers and six stored tiles. Its painted
extent is 512×256.

# Overlay fixtures

`Report.pdf.mark` is a byte-for-byte copy of the sanitized
[highlights.mark](../notebooks/README.md) device fixture under the MIT licence.

The two source PDFs are snrender-lib v1.0.0 fixtures distributed with
[snrender-go v1.0.0](https://github.com/altessa-s/snrender-go/tree/v1.0.0/testdata/overlay)
under the accompanying [MIT licence](LICENSE).

- `Report.pdf` is `overlay/pdf-highlights-n6-20260016.mark.pdf`.
  The upstream geometry test draws five blue rectangles at highlight
  coordinates on a blank page. The marker positions survive sanitization.
- `short.pdf` is `overlay/source.pdf`, from upstream `tests/source_pdf.h`.
  Annotations outside this shorter source exercise omission diagnostics.

//go:build (darwin || linux || windows) && cgo && !norender

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package renderer

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/altessa-s/snctl/internal/port"

	snrender "github.com/altessa-s/snrender-go"
)

func renderOverlay(ctx context.Context, n *snrender.Notebook, source []byte, opts port.RenderOptions, w io.Writer) (int, error) {
	var result snrender.OverlayResult
	o := &snrender.OverlayOptions{Render: snrender.Options{Scale: opts.Scale, HideBackground: opts.NoTemplate,
		DisableRecognition: opts.NoText, StrictImages: opts.StrictImages}, Result: &result}
	defer func() {
		if opts.Diagnostics != nil {
			*opts.Diagnostics = port.OverlayDiagnostics{SheetsDropped: result.SheetsDropped, HighlightsDropped: result.HighlightsDropped,
				AllAnnotationsDropped: result.AllAnnotationsDropped, NoPairsAgree: result.NoPairsAgree, SignatureFields: result.SignatureFields}
		}
	}()
	if opts.Format == formatPNG && opts.Page > 0 {
		return 1, overlayError(n.WriteOverlayPNG(ctx, w, source, opts.Page-1, o))
	}
	if opts.Format != formatPDF || opts.Page != 0 {
		return 0, port.ErrOptions
	}
	pages := 0
	o.Render.Progress = func(done, total int) {
		pages = total
		if opts.Progress != nil {
			opts.Progress(int64(done), int64(total))
		}
	}
	err := n.WriteOverlayPDF(ctx, w, source, 0, snrender.AllPages, o)
	return pages, overlayError(err)
}

func overlayError(err error) error {
	if errors.Is(err, snrender.ErrInvalidArgument) {
		return fmt.Errorf("%w: %w", port.ErrOptions, err)
	}
	return err
}

//go:build (darwin || linux || windows) && cgo && !norender

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package renderer

import (
	"context"
	"io"

	"github.com/altessa-s/snctl/internal/port"

	snrender "github.com/altessa-s/snrender-go"
)

func renderDrawing(ctx context.Context, data []byte, opts port.RenderOptions, w io.Writer) (int, error) {
	o := &snrender.DrawingOptions{Scale: opts.Scale, StrictTiles: opts.StrictImages}
	if opts.DrawingPage == "canvas" {
		o.Page = snrender.DrawingCanvas
	}
	if opts.NoTemplate {
		o.Background = &snrender.Color{}
	}
	var err error
	switch opts.Format {
	case formatPNG:
		err = snrender.WriteDrawingPNG(ctx, w, data, o)
	case formatPDF:
		err = snrender.WriteDrawingPDF(ctx, w, data, o)
	case "svg":
		err = snrender.WriteDrawingSVG(ctx, w, data, o)
	default:
		return 0, port.ErrOptions
	}
	if err != nil {
		return 0, err
	}
	if opts.Progress != nil {
		opts.Progress(1, 1)
	}
	return 1, nil
}

// PageText extracts document rectangles from a stored, one-based highlight page.
func (Renderer) PageText(ctx context.Context, pdf []byte, page int, rects []*port.Rect) (string, error) {
	rs := make([]snrender.Rect, len(rects))
	for i, r := range rects {
		if r == nil {
			rs[i].IsNull = true
			continue
		}
		rs[i] = snrender.Rect{X: r.X, Y: r.Y, Width: r.Width, Height: r.Height}
	}
	return snrender.PageText(ctx, pdf, page-1, rs, nil)
}

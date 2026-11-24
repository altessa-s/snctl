//go:build (darwin || linux || windows) && cgo && !norender

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package renderer owns native rendering and its handle lifetime.
package renderer

import (
	"context"
	"errors"
	"fmt"
	"image/jpeg"
	"io"

	"github.com/altessa-s/snctl/internal/port"

	snrender "github.com/altessa-s/snrender-go"
)

const (
	formatPNG = "png"
	formatPDF = "pdf"
)

// LibraryVersion identifies the linked library for mirror rejection records.
const LibraryVersion = snrender.LibraryVersion

// Renderer streams completed drawing data to an atomic output writer.
type Renderer struct{}

// Available reports that this build includes rendering.
func (Renderer) Available() error { return nil }

// Render draws annotation sheets when no source document is supplied.
func (Renderer) Render(ctx context.Context, data, source []byte, opts port.RenderOptions, w io.Writer) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if opts.Drawing {
		return renderDrawing(ctx, data, opts, w)
	}
	n, err := snrender.Open(data)
	if err != nil {
		return 0, notebookRenderError(err)
	}
	defer func() { _ = n.Close() }()
	if len(source) != 0 {
		return renderOverlay(ctx, n, source, opts, w)
	}
	count, err := n.PageCount()
	if err != nil {
		return 0, err
	}
	if count == 0 || opts.Page < 0 || opts.Page > count {
		return 0, port.ErrPage
	}
	return renderPage(ctx, n, count, opts, w)
}

// Every Open failure belongs to ErrNotebook. Only reader content statuses
// identify an input revision that the mirror can skip on a later pass.
func notebookRenderError(err error) error {
	class := port.ErrNotebook
	if failure, ok := errors.AsType[*snrender.ParseError](err); ok {
		switch failure.Status {
		case snrender.ParseOK, snrender.ParseInvalidArgument, snrender.ParseOutOfMemory:
			// These statuses do not identify damaged content.
		case snrender.ParseUnsupportedFormat, snrender.ParseUnexpectedEOF, snrender.ParseMalformed,
			snrender.ParseImageEncoding, snrender.ParseUnsupportedEncoding, snrender.ParseLimit:
			class = port.ErrNotebookContent
		}
	}
	return fmt.Errorf("%w: %w", class, err)
}

func renderPage(ctx context.Context, n *snrender.Notebook, count int, opts port.RenderOptions, w io.Writer) (int, error) {
	var err error
	o := &snrender.Options{Scale: opts.Scale, HideBackground: opts.NoTemplate,
		DisableRecognition: opts.NoText, StrictImages: opts.StrictImages}
	if opts.Progress != nil {
		o.Progress = func(done, total int) { opts.Progress(int64(done), int64(total)) }
	}
	if opts.Format == formatPDF {
		return count, n.WritePDF(ctx, w, 0, snrender.AllPages, o)
	}
	if opts.Page == 0 {
		return 0, port.ErrPage
	}
	switch opts.Format {
	case formatPNG:
		err = n.WritePNG(ctx, w, opts.Page-1, o)
	case "svg":
		err = n.WriteSVG(ctx, w, opts.Page-1, o)
	case "jpg":
		// JPEG has no alpha channel; a white canvas prevents transparent marks turning black.
		o.Background = &snrender.Color{Red: 1, Green: 1, Blue: 1, Alpha: 1}
		bitmap, berr := n.Bitmap(ctx, opts.Page-1, o)
		if berr != nil {
			return 0, berr
		}
		quality := opts.Quality
		if quality == 0 {
			quality = jpeg.DefaultQuality
		}
		err = jpeg.Encode(w, bitmap, &jpeg.Options{Quality: quality})
	default:
		return 0, port.ErrOptions
	}
	return 1, err
}

// Limits queries the native defaults so command validation follows the linked library.
func (Renderer) Limits() port.RenderLimits {
	l := snrender.DefaultLimits()
	return port.RenderLimits{MinimumScale: l.MinimumScale, MaximumScale: l.MaximumScale, MaximumPixels: l.MaximumPixels}
}

// RenderPages retains one native notebook across an image directory export.
func (Renderer) RenderPages(ctx context.Context, data []byte, opts port.RenderOptions, write func(int, func(io.Writer) error) error) error {
	n, err := snrender.Open(data)
	if err != nil {
		return notebookRenderError(err)
	}
	defer func() { _ = n.Close() }()
	count, err := n.PageCount()
	if err != nil {
		return err
	}
	progress := opts.Progress
	opts.Progress = nil
	for i := range count {
		if err := ctx.Err(); err != nil {
			return err
		}
		opts.Page = i + 1
		if err := write(i, func(w io.Writer) error { _, err := renderPage(ctx, n, count, opts, w); return err }); err != nil {
			return err
		}
		if progress != nil {
			progress(int64(i+1), int64(count))
		}
	}
	return nil
}

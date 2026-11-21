// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package notebooks reads and draws local or cloud notebooks through ports.
package notebooks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"path/filepath"
	"slices"
	"strings"

	"github.com/altessa-s/snctl/internal/app/outdir"
	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
)

const (
	noteExtension = ".note"
	kindMark      = "mark"
	formatPDF     = "pdf"
	formatJPEG    = "jpg"
	formatPNG     = "png"
)

// Service shares input selection and atomic output among notebook commands.
type Service struct {
	Fetch    port.Fetch
	Cloud    port.Files
	Files    port.NotebookFiles
	Reader   port.NoteReader
	Index    port.RecognitionIndex
	Renderer port.Renderer
	Opener   port.Opener
}

func (s Service) load(ctx context.Context, query string, progress port.Progress) (string, []byte, error) {
	return s.loadLimited(ctx, query, progress, 0)
}

// loadLimited bounds local reads and checks verified cloud metadata before download.
// A zero limit preserves the ordinary notebook input policy.
func (s Service) loadLimited(ctx context.Context, query string, progress port.Progress, limit int64) (string, []byte, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	local, err := s.Files.LocalInput(query)
	if err != nil {
		return "", nil, err
	}
	if local {
		r, openErr := s.Files.Open(query)
		if openErr != nil {
			return "", nil, openErr
		}
		var input io.Reader = r
		if limit > 0 {
			input = io.LimitReader(r, limit+1)
		}
		data, readErr := io.ReadAll(input)
		if limit > 0 && int64(len(data)) > limit {
			data, readErr = nil, sourceSizeError(limit)
		}
		return filepath.Base(query), data, errors.Join(readErr, r.Close(), ctx.Err())
	}
	if s.Fetch == nil {
		return "", nil, fs.ErrNotExist
	}
	if limit > 0 {
		e, resolveErr := s.Fetch.Resolve(ctx, query)
		if resolveErr != nil {
			return "", nil, resolveErr
		}
		if e.Size != nil && *e.Size > limit {
			return "", nil, sourceSizeError(limit)
		}
	}
	e, data, err := s.Fetch.Read(ctx, query, progress)
	if limit > 0 && int64(len(data)) > limit {
		return e.Name, nil, errors.Join(err, sourceSizeError(limit), ctx.Err())
	}
	return e.Name, data, err
}

// Read selects stored page numbers, preserving notebook order and rejecting missing pages.
func (s Service) Read(ctx context.Context, query string, pages []int, progress port.Progress) (port.Notebook, error) {
	if !domain.DocumentName(query) {
		return s.read(ctx, query, pages, progress, false)
	}
	if err := s.Reader.Available(); err != nil {
		return port.Notebook{}, err
	}
	mark, err := s.annotationPath(ctx, query)
	if err != nil {
		return port.Notebook{}, err
	}
	n, err := s.read(ctx, mark, pages, progress, false)
	if err != nil {
		return n, err
	}
	n.Mark = n.Name
	n.Name = filepath.Base(query)
	local, err := s.Files.LocalInput(query)
	if err == nil && !local {
		var doc port.Entry
		doc, err = s.Fetch.Resolve(ctx, query)
		n.Name = doc.Name
	}
	return n, err
}

// ReadHighlights includes document pages that carry highlights without a handwritten sheet.
func (s Service) ReadHighlights(ctx context.Context, query string, pages []int, progress port.Progress) (port.Notebook, error) {
	if domain.DocumentName(query) {
		if err := s.Reader.Available(); err != nil {
			return port.Notebook{}, err
		}
		mark, err := s.annotationPath(ctx, query)
		if err != nil {
			return port.Notebook{}, err
		}
		query = mark
	}
	return s.read(ctx, query, pages, progress, true)
}

func (s Service) read(ctx context.Context, query string, pages []int, progress port.Progress, highlights bool) (port.Notebook, error) {
	if err := s.Reader.Available(); err != nil {
		return port.Notebook{}, err
	}
	name, data, err := s.load(ctx, query, progress)
	if err != nil {
		return port.Notebook{}, err
	}
	var n port.Notebook
	if strings.EqualFold(filepath.Ext(name), ".spd") {
		reader, ok := s.Reader.(port.DrawingReader)
		if !ok {
			return n, port.ErrNotebookUnavailable
		}
		n, err = reader.ParseDrawing(data)
	} else {
		n, err = s.parse(ctx, data)
	}
	if err != nil {
		return n, err
	}
	n.Name = name
	if len(pages) == 0 {
		if highlights {
			err = s.highlightText(ctx, query, &n, progress)
		}
		return n, errors.Join(err, ctx.Err())
	}
	for _, want := range pages {
		if !slices.Contains(pageNumbers(n, highlights), want) {
			return port.Notebook{}, missingPage(n, want, highlights)
		}
	}
	n.Pages = slices.DeleteFunc(slices.Clone(n.Pages), func(p port.NotePage) bool { return !slices.Contains(pages, p.Number) })
	n.Titles = slices.DeleteFunc(slices.Clone(n.Titles), func(t port.NoteTitle) bool { return !slices.Contains(pages, t.Page) })
	n.Highlights = slices.DeleteFunc(slices.Clone(n.Highlights), func(h port.NoteHighlight) bool { return !slices.Contains(pages, h.Page) })
	if highlights {
		err = s.highlightText(ctx, query, &n, progress)
	}
	return n, errors.Join(err, ctx.Err())
}

// Folder determines whether a cloud path names a folder; existing local files win.
func (s Service) Folder(ctx context.Context, query string) (bool, error) {
	if err := s.Renderer.Available(); err != nil {
		return false, err
	}
	if query == "/" {
		return true, nil
	}
	local, err := s.Files.LocalInput(query)
	if err != nil || local {
		return false, err
	}
	if s.Fetch == nil {
		return false, fs.ErrNotExist
	}
	e, err := s.Fetch.Resolve(ctx, query)
	return e.Folder, err
}

// Normalize validates options before input is fetched or output created.
func Normalize(o port.RenderOptions) (port.RenderOptions, error) {
	if o.Format == "" {
		o.Format = formatPDF
		if o.Page > 0 {
			o.Format = formatPNG
		}
	}
	if o.Format == "jpeg" {
		o.Format = formatJPEG
	}
	if !slices.Contains([]string{formatPDF, formatPNG, formatJPEG, "svg"}, o.Format) || o.Page < 0 ||
		o.Quality < 0 || o.Quality > 100 || (o.Quality != 0 && o.Format != formatJPEG) ||
		(o.Page != 0 && o.Format == formatPDF) || math.IsNaN(o.Scale) || math.IsInf(o.Scale, 0) || o.Scale < 0 {
		return o, port.ErrOptions
	}
	return o, nil
}

// Render writes a PDF, one image, or numbered images in a directory.
func (s Service) Render(ctx context.Context, query, dest string, o port.RenderOptions) (port.RenderReport, error) {
	o, err := Normalize(o)
	if err != nil {
		return port.RenderReport{}, err
	}
	if o.NoMarks {
		return port.RenderReport{}, fmt.Errorf("%w: --no-marks is only supported by open for PDF/EPUB documents", port.ErrOptions)
	}
	if err = s.Renderer.Available(); err != nil {
		return port.RenderReport{}, err
	}
	if domain.DocumentName(query) {
		mark, markErr := s.optionalAnnotation(ctx, query)
		if markErr != nil {
			return port.RenderReport{}, markErr
		}
		if mark == "" {
			return port.RenderReport{}, fmt.Errorf("%s has no annotations; nothing to render (use snctl get to download the document): %w", query, port.ErrNotFound)
		}
		query = mark
	}
	name, data, err := s.load(ctx, query, o.DownloadProgress)
	if err != nil {
		return port.RenderReport{}, err
	}
	if strings.EqualFold(filepath.Ext(name), ".spd") {
		o.Drawing = true
		return s.renderDrawingData(ctx, name, data, dest, o)
	}
	if o.DrawingPage != "" {
		return port.RenderReport{}, fmt.Errorf("%w: --drawing-page requires an .spd drawing", port.ErrOptions)
	}
	if o.Page == 0 && !s.Files.IsDir(dest) && slices.Contains([]string{".png", ".jpg", ".jpeg", ".svg"}, strings.ToLower(filepath.Ext(dest))) {
		return port.RenderReport{}, fmt.Errorf("%w: give --page or an output directory for an image filename", port.ErrOptions)
	}
	source, warnings, err := s.sourcePDF(ctx, query, name, o, true)
	if err != nil {
		return port.RenderReport{}, err
	}
	report, err := s.renderSourceData(ctx, name, data, source, dest, o)
	report.Warnings = append(warnings, report.Warnings...)
	return report, err
}

func (s Service) renderData(ctx context.Context, name string, data []byte, dest string, o port.RenderOptions) (port.RenderReport, error) {
	return s.renderSourceData(ctx, name, data, nil, dest, o)
}

func (s Service) renderSourceData(ctx context.Context, name string, data, source []byte, dest string, o port.RenderOptions) (port.RenderReport, error) {
	n, err := s.parse(ctx, data)
	if err != nil {
		return port.RenderReport{}, err
	}
	overlay := len(source) != 0
	if overlay && (n.Kind != kindMark || o.Format != formatPDF && (o.Format != formatPNG || o.Page == 0)) {
		return port.RenderReport{}, fmt.Errorf("%w: source PDF overlays require a .mark and PDF or PNG with --page", port.ErrOptions)
	}

	if !overlay && (len(n.Pages) == 0 || o.Page != 0 && !slices.Contains(pageNumbers(n, false), o.Page)) {
		return port.RenderReport{}, missingPage(n, o.Page, false)
	}
	geometry := n
	if overlay {
		geometry.Pages = nil
	} // Overlay pixels use the source CropBox, checked by the renderer.
	if err = validateScale(s.Renderer.Limits(), geometry, o); err != nil {
		return port.RenderReport{}, err
	}
	base := safeName(strings.TrimSuffix(name, filepath.Ext(name)))
	report := port.RenderReport{SheetsOnly: n.Kind == kindMark && !overlay, SkippedPages: n.SkippedPages}
	if o.Format != formatPDF && o.Page == 0 {
		return s.renderImages(ctx, n, data, base, dest, o, report)
	}

	filename := base + "-rendered.pdf"
	if o.Page > 0 {
		filename = fmt.Sprintf("%s-p%04d.%s", base, o.Page, o.Format)
	}
	if o.Page > 0 && !overlay {
		o.Page = slices.Index(pageNumbers(n, false), o.Page) + 1
	}
	var diagnostics port.OverlayDiagnostics
	o.Diagnostics = &diagnostics
	if dest == "-" {
		if o.Writer == nil {
			return report, port.ErrOptions
		}
		report.Path = "-"
		report.Pages, err = s.Renderer.Render(ctx, data, source, o, o.Writer)
		report.Warnings = diagnostics.Warnings()
		return report, err
	}
	report.Path, err = s.Files.Destination(dest, filename)
	if err != nil {
		return report, err
	}
	report.Pages, err = s.draw(ctx, data, source, report.Path, o)
	report.Warnings = diagnostics.Warnings()
	return report, err
}

func (s Service) renderImages(
	ctx context.Context, n port.Notebook, data []byte, base, dest string, o port.RenderOptions, report port.RenderReport,
) (port.RenderReport, error) {
	var err error
	if dest == "-" {
		return report, fmt.Errorf("%w: standard output requires PDF or --page", port.ErrOptions)
	}
	if dest != "" && !s.Files.IsDir(dest) && !strings.HasSuffix(dest, "/") && !strings.HasSuffix(dest, string(filepath.Separator)) {
		return report, fmt.Errorf("%w: give --page or an output directory ending in a path separator", port.ErrOptions)
	}
	if dest == "" {
		dest = base + "-rendered"
	}
	disk, openErr := outdir.Open(s.Files, dest)
	if openErr != nil {
		return report, openErr
	}
	defer func() { _ = disk.Close() }()
	s.Files = disk
	report.Path = dest
	err = s.Renderer.RenderPages(ctx, data, o, func(index int, draw func(io.Writer) error) error {
		path := filepath.Join(dest, fmt.Sprintf("%s-p%04d.%s", base, n.Pages[index].Number, o.Format))
		if produceErr := s.Files.Produce(ctx, path, draw); produceErr != nil {
			return produceErr
		}
		report.Pages++
		return nil
	})
	return report, err
}

func (s Service) draw(ctx context.Context, data, source []byte, path string, o port.RenderOptions) (int, error) {
	var pages int
	err := s.Files.Produce(ctx, path, func(w io.Writer) error {
		var err error
		pages, err = s.Renderer.Render(ctx, data, source, o, w)
		return err
	})
	return pages, err
}

// Open leaves a private preview for the viewer, whose lifetime exceeds the command's.
func (s Service) Open(ctx context.Context, query string, o port.RenderOptions) (port.RenderReport, error) {
	if err := s.Renderer.Available(); err != nil {
		return port.RenderReport{}, err
	}
	o, err := Normalize(o)
	if err != nil {
		return port.RenderReport{}, err
	}
	if domain.DocumentName(query) {
		var mark string
		if !o.NoMarks {
			mark, err = s.optionalAnnotation(ctx, query)
			if err != nil {
				return port.RenderReport{}, err
			}
		}
		if mark == "" {
			return s.openDocument(ctx, query, o)
		}
		query = mark
	} else if o.NoMarks {
		return port.RenderReport{}, fmt.Errorf("%w: --no-marks requires a PDF/EPUB document", port.ErrOptions)
	}
	if o.Format != formatPDF && o.Page == 0 && !strings.EqualFold(filepath.Ext(query), ".spd") {
		return port.RenderReport{}, port.ErrOptions
	}
	dir, err := s.Files.PreviewDir()
	if err != nil {
		return port.RenderReport{}, err
	}
	report, err := s.Render(ctx, query, dir, o)
	if err != nil {
		_ = s.Files.Remove(dir)
		return report, err
	}
	return report, s.openPreview(ctx, dir, report.Path)
}

func (s Service) openPreview(ctx context.Context, dir, preview string) error {
	err := s.Opener.Open(ctx, preview)
	if err != nil {
		_ = s.Files.Remove(preview)
		_ = s.Files.Remove(dir)
	}
	return err
}

func safeName(name string) string { return domain.SafeName(name) }

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest

import (
	"context"
	"io"

	"github.com/altessa-s/snctl/internal/port"
)

// NoteReader supplies portable notebook models and classified failures.
type NoteReader struct {
	Notebook         port.Notebook
	Err, Unavailable error
}

// Available reports the configured build capability.
func (r NoteReader) Available() error { return r.Unavailable }

// Parse returns the synthetic notebook.
func (r NoteReader) Parse([]byte) (port.Notebook, error) { return r.Notebook, r.Err }

// Renderer makes native failures and writer failures independently testable.
type Renderer struct {
	Text             string
	TextErr          error
	Err, Unavailable error
	Pages            int
}

// Available reports the configured rendering capability.
func (r Renderer) Available() error { return r.Unavailable }

// Render exercises progress and streams synthetic output.
func (r Renderer) Render(_ context.Context, _, _ []byte, o port.RenderOptions, w io.Writer) (int, error) {
	if o.Progress != nil {
		o.Progress(1, 1)
	}
	if r.Err != nil {
		return 0, r.Err
	}
	_, err := io.WriteString(w, "%PDF-synthetic")
	return r.Pages, err
}

// Opener records previews without launching desktop applications.
type Opener struct {
	Path string
	Err  error
}

// Open records the completed file path.
func (o *Opener) Open(_ context.Context, path string) error { o.Path = path; return o.Err }

// Notebooks is a callback-free input double for command output and errors.
type Notebooks struct {
	Notebook    port.Notebook
	Report      port.RenderReport
	Tree        port.TreeReport
	IsFolder    bool
	Err         error
	Unavailable error
}

// Read returns a synthetic reading view.
func (n Notebooks) Read(context.Context, string, []int, port.Progress) (port.Notebook, error) {
	return n.Notebook, n.Err
}

// Render returns a synthetic output report.
func (n Notebooks) Render(context.Context, string, string, port.RenderOptions) (port.RenderReport, error) {
	return n.Report, n.Err
}

// Open returns a synthetic preview report.
func (n Notebooks) Open(context.Context, string, port.RenderOptions) (port.RenderReport, error) {
	return n.Report, n.Err
}

// Folder reports the configured source kind.
func (n Notebooks) Folder(context.Context, string) (bool, error) { return n.IsFolder, n.Err }

// RenderTree returns a synthetic recursive report.
func (n Notebooks) RenderTree(context.Context, string, string, port.RenderOptions, port.TreeOptions) (port.TreeReport, error) {
	return n.Tree, n.Err
}

// Limits supplies deterministic native bounds to command tests.
func (Renderer) Limits() port.RenderLimits {
	const minScale, maxScale, maxPixels = 1.0 / 64, 8, 16000000
	return port.RenderLimits{MinimumScale: minScale, MaximumScale: maxScale, MaximumPixels: maxPixels}
}

// Limits supplies deterministic command bounds without native code.
func (Notebooks) Limits() port.RenderLimits { return Renderer{}.Limits() }

// RenderPages exercises each output callback independently.
func (r Renderer) RenderPages(ctx context.Context, data []byte, o port.RenderOptions, write func(int, func(io.Writer) error) error) error {
	if r.Err != nil {
		return r.Err
	}
	for i := range r.Pages {
		if err := write(i, func(w io.Writer) error { _, err := r.Render(ctx, data, nil, o, w); return err }); err != nil {
			return err
		}
	}
	return nil
}

// ReadHighlights returns the same synthetic view through the highlight selection boundary.
func (n Notebooks) ReadHighlights(ctx context.Context, query string, pages []int, progress port.Progress) (port.Notebook, error) {
	return n.Read(ctx, query, pages, progress)
}

// Available supplies the build capability independently of operation failures.
func (n Notebooks) Available() error { return n.Unavailable }

// PageText returns synthetic extraction, with independently configured failures.
func (r Renderer) PageText(context.Context, []byte, int, []*port.Rect) (string, error) {
	return r.Text, r.TextErr
}

// ParseDrawing supplies synthetic canvas inspection.
func (r NoteReader) ParseDrawing(data []byte) (port.Notebook, error) { return r.Parse(data) }

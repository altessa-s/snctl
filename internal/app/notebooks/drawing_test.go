// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package notebooks_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"testing"

	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

type drawingRenderer struct {
	porttest.Renderer
	options port.RenderOptions
}

func (r *drawingRenderer) Render(ctx context.Context, data, source []byte, o port.RenderOptions, w io.Writer) (int, error) {
	r.options = o
	if len(source) != 0 {
		return 0, errors.New("drawing received a source")
	}
	return r.Renderer.Render(ctx, data, source, o, w)
}

func TestDrawingOutputs(t *testing.T) {
	for _, format := range []string{"pdf", "png", "svg"} {
		for _, dest := range []string{"out." + format, "-", ""} {
			s, d := setup()
			d.Data["local.spd"] = []byte("drawing")
			r := &drawingRenderer{Pages: 1}
			s.Renderer = r
			var out bytes.Buffer
			o := port.RenderOptions{Format: format, DrawingPage: "canvas", Scale: .5, Writer: &out}
			report, err := s.Render(t.Context(), "local.spd", dest, o)
			if err != nil || report.Pages != 1 || report.SheetsOnly || !r.options.Drawing || r.options.DrawingPage != "canvas" {
				t.Fatal(report, err, r)
			}
			if dest == "-" && out.Len() == 0 {
				t.Fatal("no stdout")
			}
			if dest == "" && report.Path != "local-rendered."+format {
				t.Fatal(report.Path)
			}
		}
	}
	s, _ := setup()
	s.Fetch = drawingFetch{}
	r := &drawingRenderer{Pages: 1}
	s.Renderer = r
	if _, err := s.Render(t.Context(), "/EXPORT/remote.spd", "out.png", port.RenderOptions{Format: "png"}); err != nil || !r.options.Drawing {
		t.Fatal(err, r)
	}
}

type drawingFetch struct{ fetch }

func (drawingFetch) Read(context.Context, string, port.Progress) (port.Entry, []byte, error) {
	return port.Entry{Name: "remote.spd"}, []byte("drawing"), nil
}

func TestDrawingOptionsAndFailures(t *testing.T) {
	for i, o := range []port.RenderOptions{{Page: 1}, {Source: "source.pdf"}, {NoText: true}, {Format: "jpg"},
		{DrawingPage: "unknown"}, {Scale: 9}, {Writer: nil}} {
		s, d := setup()
		d.Data["local.spd"] = []byte("drawing")
		dest := "out.pdf"
		if i == 6 {
			dest = "-"
		}
		if _, err := s.Render(t.Context(), "local.spd", dest, o); !errors.Is(err, port.ErrOptions) {
			t.Fatal(o, err)
		}
	}
	for _, fail := range []string{"destination", "produce", "renderer"} {
		s, d := setup()
		d.Data["local.spd"] = []byte("drawing")
		if fail == "renderer" {
			s.Renderer = porttest.Renderer{Err: io.ErrClosedPipe}
		} else {
			d.FailAt = fail
			d.Err = io.ErrClosedPipe
		}
		if _, err := s.Render(t.Context(), "local.spd", "", port.RenderOptions{}); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatal(fail, err)
		}
	}
	s, _ := setup()
	if _, err := s.Render(t.Context(), "local.note", "", port.RenderOptions{DrawingPage: "canvas"}); !errors.Is(err, port.ErrOptions) {
		t.Fatal(err)
	}
}

func TestDrawingReadAndPreview(t *testing.T) {
	s, d := setup()
	d.Data["local.spd"] = []byte("drawing")
	s.Reader = porttest.NoteReader{Notebook: port.Notebook{Kind: "drawing", Drawing: &port.DrawingInfo{Tiles: 6}}}
	n, err := s.Read(t.Context(), "local.spd", nil, nil)
	if err != nil || n.Drawing == nil || n.Drawing.Tiles != 6 {
		t.Fatal(n, err)
	}
	if _, err := s.Read(t.Context(), "local.spd", []int{1}, nil); !errors.Is(err, port.ErrPage) {
		t.Fatal(err)
	}
	if _, err := s.ExportText(t.Context(), "local.spd", "out.txt", port.TextOptions{Extension: "txt", Encode: func(port.Notebook) ([]byte, error) { t.Error("encoded drawing as text"); return nil, nil }}); !errors.Is(err, port.ErrOptions) {
		t.Fatal(err)
	}
	s.Reader = basicReader{}
	if _, err := s.Read(t.Context(), "local.spd", nil, nil); !errors.Is(err, port.ErrNotebookUnavailable) {
		t.Fatal(err)
	}
	s.Reader = porttest.NoteReader{Err: fs.ErrPermission}
	if _, err := s.Read(t.Context(), "local.spd", nil, nil); !errors.Is(err, fs.ErrPermission) {
		t.Fatal(err)
	}
	// Rendering and preview do not need a separate metadata parse.
	s.Reader = porttest.NoteReader{Err: errors.New("metadata reader must not be used")}
	s.Renderer = porttest.Renderer{Pages: 1}
	report, err := s.Open(t.Context(), "local.spd", port.RenderOptions{Format: "png", DrawingPage: "painted"})
	if err != nil || report.Pages != 1 {
		t.Fatal(report, err)
	}
}

type basicReader struct{}

func (basicReader) Available() error                    { return nil }
func (basicReader) Parse([]byte) (port.Notebook, error) { return port.Notebook{}, nil }

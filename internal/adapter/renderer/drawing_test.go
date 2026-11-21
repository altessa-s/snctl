//go:build (darwin || linux || windows) && cgo && !norender

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package renderer_test

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"io"
	"os"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/renderer"
	"github.com/altessa-s/snctl/internal/port"
)

func TestDrawingFormats(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/notebooks/drawing.spd")
	if err != nil {
		t.Fatal(err)
	}
	r := renderer.Renderer{}
	for _, page := range []string{"painted", "canvas"} {
		for _, format := range []string{"png", "pdf", "svg"} {
			o := port.RenderOptions{Drawing: true, DrawingPage: page, Format: format, Scale: .5, NoTemplate: true, StrictImages: true,
				Progress: func(done, total int64) {
					if done != 1 || total != 1 {
						t.Error(done, total)
					}
				}}
			var b bytes.Buffer
			n, err := r.Render(t.Context(), data, nil, o, &b)
			if err != nil || n != 1 {
				t.Fatalf("%s/%s: %d %v", page, format, n, err)
			}
			if format == "png" {
				cfg, err := png.DecodeConfig(&b)
				width := 256
				if page == "canvas" {
					width = 128
				}
				if err != nil || cfg.Width != width || cfg.Height != 128 {
					t.Fatal(cfg, err)
				}
			}
			if _, err := r.Render(t.Context(), data, nil, o, badWriter{}); !errors.Is(err, io.ErrClosedPipe) {
				t.Fatal(err)
			}
		}
	}
	for _, o := range []port.RenderOptions{{Drawing: true, Format: "jpg"}, {Drawing: true, Format: "png", Scale: 99}} {
		if _, err := r.Render(t.Context(), data, nil, o, io.Discard); err == nil {
			t.Fatal("options accepted", o)
		}
	}
	if _, err := r.Render(t.Context(), nil, nil, port.RenderOptions{Drawing: true, Format: "png"}, io.Discard); err == nil {
		t.Fatal("empty accepted")
	}
	bad, err := os.ReadFile("../../../testdata/notebooks/drawing-bad.spd")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Render(t.Context(), bad, nil, port.RenderOptions{Drawing: true, Format: "png", StrictImages: true}, io.Discard); err == nil {
		t.Fatal("bad tile accepted")
	}
}

func TestSourcePageText(t *testing.T) {
	pdf, err := os.ReadFile("../../../testdata/notebooks/source-text.pdf")
	if err != nil {
		t.Fatal(err)
	}
	r := renderer.Renderer{}
	text, err := r.PageText(t.Context(), pdf, 1, []*port.Rect{nil, {X: 19, Y: 16, Width: 100, Height: 18}})
	if err != nil || text != "Alpha Beta" {
		t.Fatalf("%q %v", text, err)
	}
	text, err = r.PageText(t.Context(), pdf, 1, nil)
	if err != nil || text != "" {
		t.Fatal(text, err)
	}
	for _, page := range []int{0, 99} {
		if _, err := r.PageText(t.Context(), pdf, page, []*port.Rect{{Width: 100, Height: 100}}); err == nil {
			t.Fatal("page accepted", page)
		}
	}
	if _, err := r.PageText(t.Context(), []byte("bad"), 1, []*port.Rect{{Width: 100, Height: 100}}); err == nil {
		t.Fatal("invalid PDF accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := r.PageText(ctx, pdf, 1, []*port.Rect{{Width: 100, Height: 100}}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

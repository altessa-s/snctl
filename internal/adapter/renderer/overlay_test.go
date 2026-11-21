//go:build (darwin || linux || windows) && cgo && !norender

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package renderer_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/renderer"
	"github.com/altessa-s/snctl/internal/port"
)

func TestOverlay(t *testing.T) {
	mark, err := os.ReadFile("../../../testdata/overlay/Report.pdf.mark")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../../../testdata/overlay/Report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	r := renderer.Renderer{}
	for _, format := range []string{"pdf", "png"} {
		var diagnostics port.OverlayDiagnostics
		opts := port.RenderOptions{Format: format, Scale: .1, Diagnostics: &diagnostics, Progress: func(int64, int64) {}}
		if format == "png" {
			opts.Page = 1
		}
		var out bytes.Buffer
		count, err := r.Render(t.Context(), mark, source, opts, &out)
		if err != nil || count < 1 || out.Len() == 0 || len(diagnostics.Warnings()) != 0 {
			t.Fatal(count, err, diagnostics)
		}
		if format == "pdf" && !bytes.HasPrefix(out.Bytes(), source) {
			t.Fatal("source objects were not preserved")
		}
		if _, err := r.Render(t.Context(), mark, source, opts, badWriter{}); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatal(err)
		}
	}
	for _, opts := range []port.RenderOptions{{Format: "svg", Page: 1}, {Format: "png"}, {Format: "pdf", Page: 1}} {
		if _, err := r.Render(t.Context(), mark, source, opts, io.Discard); !errors.Is(err, port.ErrOptions) {
			t.Fatal(err)
		}
	}
	if _, err := r.Render(t.Context(), mark, source, port.RenderOptions{Format: "png", Page: 999}, io.Discard); !errors.Is(err, port.ErrOptions) {
		t.Fatal(err)
	}
	short, err := os.ReadFile("../../../testdata/overlay/short.pdf")
	if err != nil {
		t.Fatal(err)
	}
	var diagnostics port.OverlayDiagnostics
	if _, err := r.Render(t.Context(), mark, short, port.RenderOptions{Format: "pdf", Diagnostics: &diagnostics}, io.Discard); err != nil || len(diagnostics.Warnings()) == 0 {
		t.Fatal(err, diagnostics)
	}
}

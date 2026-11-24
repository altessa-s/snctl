//go:build (darwin || linux || windows) && cgo && !norender

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package renderer

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/altessa-s/snctl/internal/port"

	snrender "github.com/altessa-s/snrender-go"
)

func TestNotebookOpenErrorClasses(t *testing.T) {
	r := Renderer{}
	_, err := r.Render(t.Context(), []byte("garbage"), nil, port.RenderOptions{}, io.Discard)
	if !errors.Is(err, port.ErrNotebookContent) || !errors.Is(err, port.ErrNotebook) || !errors.Is(err, snrender.ErrUnsupportedFormat) {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		cause   error
		content bool
	}{
		{snrender.ErrParseLimit, true}, {snrender.ErrUnsupported, false}, {snrender.ErrLimit, false}, {snrender.ErrInvalidArgument, false}, {snrender.ErrParseInvalidArgument, false}, {snrender.ErrParseOutOfMemory, false}, {snrender.ErrBackend, false},
	} {
		err = notebookRenderError(tc.cause)
		if !errors.Is(err, port.ErrNotebook) || !errors.Is(err, tc.cause) || errors.Is(err, port.ErrNotebookContent) != tc.content {
			t.Errorf("Open(%v) classified as %v", tc.cause, err)
		}
	}
}

func TestOverlayCapabilityAndOptionErrors(t *testing.T) {
	mark, err := os.ReadFile("../../../testdata/overlay/Report.pdf.mark")
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := os.ReadFile("../../../testdata/overlay/Report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	n, err := snrender.Open(mark)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	for _, tc := range []struct {
		opts snrender.OverlayOptions
		want error
	}{
		{snrender.OverlayOptions{SourceKind: snrender.DocumentEPUB}, snrender.ErrUnsupported},
		{snrender.OverlayOptions{Limits: snrender.OverlayLimits{MaximumSourceBytes: 1}}, snrender.ErrLimit},
	} {
		err = overlayError(n.WriteOverlayPDF(t.Context(), io.Discard, pdf, 0, snrender.AllPages, &tc.opts))
		if !errors.Is(err, tc.want) || errors.Is(err, port.ErrNotebook) || errors.Is(err, port.ErrNotebookContent) {
			t.Errorf("overlay: %v", err)
		}
	}
	_, err = (Renderer{}).Render(t.Context(), mark, pdf, port.RenderOptions{Format: "png", Page: 999}, io.Discard)
	if !errors.Is(err, snrender.ErrInvalidArgument) || !errors.Is(err, port.ErrOptions) || errors.Is(err, port.ErrNotebookContent) {
		t.Fatal(err)
	}
	// Exercise the full render path with a native, configurable output limit.
	_, err = (Renderer{}).Render(t.Context(), mark, nil, port.RenderOptions{Format: "png", Page: 1, Scale: 1000000}, io.Discard)
	if !errors.Is(err, snrender.ErrLimit) || errors.Is(err, port.ErrNotebook) || errors.Is(err, port.ErrNotebookContent) {
		t.Fatal(err)
	}
}

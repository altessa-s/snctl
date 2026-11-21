//go:build norender || !cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package mirror

import (
	"errors"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/localfs"
	"github.com/altessa-s/snctl/internal/adapter/renderer"
	"github.com/altessa-s/snctl/internal/app/notebooks"
	"github.com/altessa-s/snctl/internal/port"
)

func TestRenderlessLegacyDrawingStaysLive(t *testing.T) {
	f, c := legacyDrawing(t)
	f.e.notes = notebooks.Service{Files: localfs.Files{}, Renderer: renderer.Renderer{}}
	for range 2 {
		if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, port.ErrRenderUnavailable) {
			t.Fatal(err)
		}
		if f.read("_annotated/X.pdf") != "previous drawing" {
			t.Fatal("renderless migration removed the live drawing")
		}
		if en := savedJournal(t, f).Items[c.ID]; en.Output != "_annotated/X.pdf" || en.Drawn != c.Hash {
			t.Fatal("renderless migration lost retry state", en)
		}
	}
}

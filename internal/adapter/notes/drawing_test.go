//go:build cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package notes_test

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/notes"
	"github.com/altessa-s/snctl/internal/port"
)

func TestDrawingInspection(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/notebooks/drawing.spd")
	if err != nil {
		t.Fatal(err)
	}
	n, err := (notes.Reader{}).ParseDrawing(data)
	if err != nil {
		t.Fatal(err)
	}
	d := n.Drawing
	if n.Kind != "drawing" || d == nil || d.Width == nil || *d.Width != 256 || d.Height == nil || *d.Height != 256 ||
		len(d.Layers) != 3 || d.Tiles != 6 || d.Layers[0].Tiles != 4 || d.FormatVersion == nil || *d.FormatVersion != "2" {
		t.Fatalf("%+v", n)
	}
	if _, err := (notes.Reader{}).ParseDrawing([]byte("bad")); !errors.Is(err, port.ErrNotebook) {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(data, []byte("surface.width"), []byte("unknown.width"))
	data = bytes.ReplaceAll(data, []byte("surface.height"), []byte("unknown.height"))
	n, err = (notes.Reader{}).ParseDrawing(data)
	if err != nil || n.Drawing.Width != nil || n.Drawing.Height != nil {
		t.Fatal(n, err)
	}

}

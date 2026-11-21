//go:build !cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package notes_test

import (
	"errors"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/notes"
	"github.com/altessa-s/snctl/internal/port"
)

func TestUnavailable(t *testing.T) {
	r := notes.Reader{}
	if !errors.Is(r.Available(), port.ErrNotebookUnavailable) {
		t.Fatal(r.Available())
	}
	_, e := r.Parse(nil)
	if !errors.Is(e, port.ErrNotebookUnavailable) {
		t.Fatal(e)
	}
	_, e = r.ParseDrawing(nil)
	if !errors.Is(e, port.ErrNotebookUnavailable) {
		t.Fatal(e)
	}
}

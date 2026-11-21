//go:build cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package notes_test

import (
	"errors"
	"os"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/notes"
	"github.com/altessa-s/snctl/internal/port"
)

func TestFixtures(t *testing.T) {
	r := notes.Reader{}
	if r.Available() != nil {
		t.Fatal(r.Available())
	}
	for _, name := range []string{"blank.note", "text.note", "highlights.mark"} {
		b, err := os.ReadFile("../../../testdata/notebooks/" + name)
		if err != nil {
			t.Fatal(err)
		}
		n, err := r.Parse(b)
		if err != nil || len(n.Pages) == 0 {
			t.Fatalf("%s: %+v %v", name, n, err)
		}
		if name == "text.note" && len(n.Pages[0].Text) == 0 {
			t.Fatal("missing text")
		}
		if name == "highlights.mark" && (n.Kind != "mark" || len(n.Highlights) == 0) {
			t.Fatalf("%+v", n)
		}
	}
	for _, b := range [][]byte{nil, []byte("hello"), []byte("noteSN_FILE_VER_20260016")} {
		if _, err := r.Parse(b); !errors.Is(err, port.ErrNotebook) {
			t.Fatal(err)
		}
	}
}

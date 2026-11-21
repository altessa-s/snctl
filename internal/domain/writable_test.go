// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain_test

import (
	"testing"

	"github.com/altessa-s/snctl/internal/domain"
)

func TestWritable(t *testing.T) {
	for name, want := range map[string]bool{
		"NOTE/Note/a.note": true, "/NOTE/MyStyle/t.png": true, "DOCUMENT/Document/b.pdf": true, "DOCUMENT/Document/x/y.pdf": true,
		"NOTE/Note": false, "NOTE/a.note": false, "NOTE/Other/a.note": false, "DOCUMENT/b.pdf": false, "DOCUMENT/Other/b.pdf": false,
		"EXPORT/a.pdf": true, "EXPORT": false, "/": false, "": false,
	} {
		if domain.Writable(name) != want {
			t.Errorf("%q: want %t", name, want)
		}
	}
}

func TestUploadSkip(t *testing.T) {
	for name, want := range map[string]string{
		"a.note": "", "dir/B.PDF": "", "c.epub": "", "d.mark": "", `win\e.pdf`: "",
		".hidden.pdf": "hidden file", "dir/.snctl-mget.json": "hidden file",
		"notes.txt": "not a .note, .mark, .pdf or .epub file", "README": "not a .note, .mark, .pdf or .epub file",
		".pdf": "hidden file",
	} {
		if got := domain.UploadSkip(name, 1); got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
	if domain.UploadSkip("a.pdf", 0) != "empty file" {
		t.Fatal("empty file accepted")
	}
}

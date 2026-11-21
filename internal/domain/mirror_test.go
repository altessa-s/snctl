// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain_test

import (
	"testing"

	"github.com/altessa-s/snctl/internal/domain"
)

func TestMirrorIgnored(t *testing.T) {
	t.Parallel()
	for rel, want := range map[string]bool{
		"Paper.pdf":                  false,
		"Книги/Книга.epub":           false,
		"Books/_annotated/Novel.pdf": false,
		"_annotated/Novel.pdf":       true,
		"_ANNOTATED/Novel.pdf":       true,
		".snctl-mirror.json":         true,
		".snctl-removed/Old.pdf":     true,
		".git/HEAD":                  true,
		"Sub/.hidden/Paper.pdf":      true,
		".DS_Store":                  true,
		"Sub/Thumbs.db":              true,
		"desktop.ini":                true,
		"~$Report.pdf":               true,
		"Download.pdf.part":          true,
		"Download.pdf.crdownload":    true,
		"Draft.pdf.tmp":              true,
		"Draft.pdf~":                 true,
		"../outside.pdf":             true,
		"":                           true,
	} {
		if got := domain.MirrorIgnored(rel); got != want {
			t.Errorf("%q: %v, want %v", rel, got, want)
		}
	}
}

func TestMirrorUploadable(t *testing.T) {
	t.Parallel()
	for rel, want := range map[string]bool{"a.pdf": true, "B.PDF": true, "c.epub": true, "d.md": false, "e": false} {
		if got := domain.MirrorUploadable(rel); got != want {
			t.Errorf("%q: %v", rel, got)
		}
	}
}

func TestMirrorNames(t *testing.T) {
	t.Parallel()
	if got := domain.MirrorAnnotatedPath("Книги/Книга.epub", "pdf"); got != "_annotated/Книги/Книга.pdf" {
		t.Error(got)
	}
	if got := domain.MirrorAnnotatedPath("Paper.pdf", "rmdoc"); got != "_annotated/Paper.rmdoc" {
		t.Error(got)
	}
	for name, want := range map[string]bool{"papers": true, "a-b_1": true, "Papers": false, "-x": false, "": false} {
		if got := domain.ValidMirrorName(name); got != want {
			t.Errorf("%q: %v", name, got)
		}
	}
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package port_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
)

func TestSidecarOf(t *testing.T) {
	doc := port.Entry{ID: "doc", Path: "one/a.pdf"}
	mark := port.Entry{ID: "mark", Path: doc.Path + ".mark", Size: new(int64(4)), Hash: "hash"}
	otherMark := port.Entry{ID: "other", Path: mark.Path}
	docFolder := port.Entry{Path: doc.Path, Folder: true}
	markFolder := port.Entry{Path: mark.Path, Folder: true}
	for _, tc := range []struct {
		name     string
		siblings []port.Entry
		want     port.Entry
		counts   port.SidecarCounts
	}{
		{name: "empty"},
		{name: "absent", siblings: []port.Entry{doc}, counts: port.SidecarCounts{Documents: 1}},
		{name: "sidecar", siblings: []port.Entry{mark, doc}, want: mark, counts: port.SidecarCounts{Documents: 1, Marks: 1, Files: 1}},
		{name: "orphan", siblings: []port.Entry{mark}, want: mark, counts: port.SidecarCounts{Marks: 1, Files: 1}},
		{name: "duplicate documents", siblings: []port.Entry{doc, doc, mark}, want: mark, counts: port.SidecarCounts{Documents: 2, Marks: 1, Files: 1}},
		{name: "duplicate sidecars", siblings: []port.Entry{doc, mark, otherMark}, want: otherMark, counts: port.SidecarCounts{Documents: 1, Marks: 2, Files: 2}},
		{name: "document folder", siblings: []port.Entry{docFolder, mark}, want: mark, counts: port.SidecarCounts{Documents: 1, Marks: 1, Files: 1}},
		{name: "duplicate document folder", siblings: []port.Entry{doc, docFolder}, counts: port.SidecarCounts{Documents: 2}},
		{name: "sidecar folder", siblings: []port.Entry{doc, markFolder}, counts: port.SidecarCounts{Documents: 1, Marks: 1}},
		{name: "folder after file", siblings: []port.Entry{doc, mark, markFolder}, want: mark, counts: port.SidecarCounts{Documents: 1, Marks: 2, Files: 1}},
		{name: "folder before file", siblings: []port.Entry{markFolder, mark, doc}, want: mark, counts: port.SidecarCounts{Documents: 1, Marks: 2, Files: 1}},
		{name: "unrelated paths", siblings: []port.Entry{
			{Path: "two/a.pdf.mark"}, {Path: "one/A.pdf.mark"}, {Path: "one/a.pdf.MARK"},
			{Path: "/one/a.pdf.mark"}, {Path: "one/a.mark"}, {Name: "a.pdf.mark"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := slices.Clone(tc.siblings)
			got, counts := port.SidecarOf(tc.siblings, doc.Path)
			if !reflect.DeepEqual(got, tc.want) || counts != tc.counts {
				t.Fatalf("SidecarOf = (%+v, %+v), want (%+v, %+v)", got, counts, tc.want, tc.counts)
			}
			if !reflect.DeepEqual(tc.siblings, before) {
				t.Fatal("input mutated")
			}
		})
	}
}

func TestFoldMarks(t *testing.T) {
	stamp := time.Unix(1, 0)
	entries := []port.Entry{
		{Name: "a.pdf.mark", Path: "one/a.pdf.mark", Size: new(int64(4)), Modified: &stamp, Hash: "hash", ID: "mark"},
		{Name: "a.pdf", Path: "one/a.pdf"},
		{Name: "b.epub", Path: "one/b.epub"}, {Name: "b.epub.mark", Path: "one/b.epub.mark"},
		{Name: "orphan.pdf.mark", Path: "one/orphan.pdf.mark"},
		{Name: "a.pdf.mark", Path: "two/a.pdf.mark"},
		{Name: "legacy.mark", Path: "one/legacy.mark"},
		{Name: "folder.pdf", Path: "one/folder.pdf", Folder: true}, {Name: "folder.pdf.mark", Path: "one/folder.pdf.mark"},
		{Name: "folder-mark.pdf", Path: "one/folder-mark.pdf"}, {Name: "folder-mark.pdf.mark", Path: "one/folder-mark.pdf.mark", Folder: true},
		{Name: "unknown.pdf.mark"},
	}
	for _, raw := range []bool{false, true} {
		got := port.FoldMarks(entries, raw)
		want := len(entries) - 2
		if raw {
			want = len(entries)
		}
		if len(got) != want {
			t.Fatalf("raw %t: %v", raw, got)
		}
		for _, e := range got {
			if e.Path == "one/a.pdf" && (e.Mark == nil || e.Mark.ID != "mark" || *e.Mark.Size != 4 || !e.Mark.Modified.Equal(stamp)) {
				t.Fatal(e)
			}
			if e.Path == "one/b.epub" && e.Mark == nil {
				t.Fatal(e)
			}
		}
	}
	if entries[1].Mark != nil {
		t.Fatal("input mutated")
	}
	if got := port.FoldMarks(nil, false); got == nil || len(got) != 0 {
		t.Fatal(got)
	}
	ambiguous := []port.Entry{{Path: "a.pdf"}, {Path: "a.pdf"}, {Path: "a.pdf.mark"}}
	if len(port.FoldMarks(ambiguous, false)) != 3 {
		t.Fatal("hid an ambiguous mark")
	}
	ambiguous = []port.Entry{{Path: "a.pdf"}, {Path: "a.pdf.mark"}, {Path: "a.pdf.mark"}}
	if len(port.FoldMarks(ambiguous, false)) != 3 {
		t.Fatal("hid duplicate marks")
	}
}

func TestFileFlagVocabulary(t *testing.T) {
	got := domain.FileFlags()
	if !slices.Equal(got, []domain.FileFlag{{Name: "recognition", Letter: "R", Meaning: "recognized handwriting"},
		{Name: "highlights", Letter: "H", Meaning: "highlights"}}) {
		t.Fatal(got)
	}
	got[0].Letter = "wrong"
	if domain.FileFlags()[0].Letter != "R" {
		t.Fatal("mutable vocabulary")
	}
	for _, name := range []string{"a.pdf.mark", "a.epub.mark", "a.PDF.mark"} {
		if domain.MarkOwner(name) == "" {
			t.Fatal(name)
		}
	}
	for _, name := range []string{"a.mark", "a.note.mark", "a.pdf", "a.pdf.MARK"} {
		if domain.MarkOwner(name) != "" {
			t.Fatal(name)
		}
	}
}

func TestUnicodeSidecars(t *testing.T) {
	doc := port.Entry{ID: "doc", Name: "й.pdf", Path: "one/й.pdf"}
	mark := port.Entry{ID: "mark", Name: "и\u0306.pdf.mark", Path: "one/и\u0306.pdf.mark"}
	for _, tc := range []struct {
		name             string
		extra            []port.Entry
		documents, marks int
	}{
		{name: "cross form", documents: 1, marks: 1},
		{name: "duplicate documents", extra: []port.Entry{{ID: "twin", Path: "one/и\u0306.pdf"}}, documents: 2, marks: 1},
		{name: "document folder collision", extra: []port.Entry{{Path: "one/и\u0306.pdf", Folder: true}}, documents: 2, marks: 1},
		{name: "folder collision", extra: []port.Entry{{Path: "one/й.pdf.mark", Folder: true}}, documents: 1, marks: 2},
		{name: "duplicate marks", extra: []port.Entry{{ID: "twin", Path: "one/й.pdf.mark"}}, documents: 1, marks: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries := append([]port.Entry{doc, mark}, tc.extra...)
			_, counts := port.SidecarOf(entries, doc.Path)
			wantFiles := tc.marks
			if tc.name == "folder collision" {
				wantFiles--
			}
			if counts.Documents != tc.documents || counts.Marks != tc.marks || counts.Files != wantFiles {
				t.Fatal(counts)
			}
			for _, raw := range []bool{false, true} {
				got := port.FoldMarks(entries, raw)
				if len(tc.extra) > 0 {
					if len(got) != len(entries) || got[0].Mark != nil {
						t.Fatal("folded ambiguous paths", got)
					}
				} else {
					want := 1
					if raw {
						want = 2
					}
					if len(got) != want || got[0].Path != doc.Path || got[0].Mark == nil || !reflect.DeepEqual(*got[0].Mark, mark) {
						t.Fatal(got)
					}
				}
			}
			if entries[0].Mark != nil {
				t.Fatal("mutated input")
			}
		})
	}
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package notebooks

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/port"
)

type trashCloud struct {
	port.Files
	entries []port.Entry
}

func (c trashCloud) Trash(context.Context) ([]port.Entry, error) { return c.entries, nil }
func TestTrashPlacement(t *testing.T) {
	s := Service{Cloud: trashCloud{entries: []port.Entry{{ID: "1", Name: "gone.note"}, {ID: "2", Name: "folder", Folder: true}, {ID: "3", Name: "paper.pdf"}}}}
	if _, err := s.withTrash(t.Context(), nil, "NOTE/Note"); !errors.Is(err, port.ErrOptions) {
		t.Fatal(err)
	}
	entries, err := s.withTrash(t.Context(), nil, "")
	if err != nil || len(entries) != 1 || entries[0].Path != ".trash/gone.note" {
		t.Fatal(entries, err)
	}
	entries, err = s.withTrash(t.Context(), entries, "")
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	s.Cloud = trashCloud{entries: []port.Entry{{ID: "2", Name: "other.note", Path: "NOTE/Note/other.note"}, {ID: "3", Name: "no.note", Path: "other/no.note"}}}
	entries, err = s.withTrash(t.Context(), entries, "NOTE/Note")
	if err != nil || len(entries) != 2 {
		t.Fatal(entries, err)
	}
}
func TestNamesAndCollisions(t *testing.T) {
	entries := []port.Entry{{ID: "a", Name: "x.note", Path: "top/x.note"}, {ID: "a", Name: "x.mark", Path: "top/x.mark"}, {ID: "a", Name: "x.note", Path: "top/x.note"}}
	paths := treePaths(entries, port.Entry{Path: "top"})
	seen := map[string]bool{}
	for _, p := range paths {
		if seen[p] {
			t.Fatal(paths)
		}
		seen[p] = true
	}
	if safeName(".trash") != ".trash" {
		t.Fatal("lost hidden directory")
	}
	if safeName("../a:b\\c") != "-a-b-c" || safeName(" .. ") != "notebook" {
		t.Fatal("unsafe names")
	}
}

func TestCollisionNamesIndependentOfOrder(t *testing.T) {
	entries := []port.Entry{{ID: "1", Name: "a.note", Path: "Note/a.note"}, {ID: "2", Name: "A.mark", Path: "Note/A.mark"}, {ID: "3", Name: "b.note", Path: "Note/b.note"}}
	first := treePaths(entries, port.Entry{Path: "Note"})
	slices.Reverse(entries)
	second := treePaths(entries, port.Entry{Path: "Note"})
	slices.Reverse(second)
	if !slices.Equal(first, second) || !strings.Contains(first[0], "(1)") || !strings.Contains(first[1], "(2)") {
		t.Fatal(first, second)
	}
}

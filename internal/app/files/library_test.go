// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package files_test

import (
	"errors"
	"testing"

	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestLibrary(t *testing.T) {
	f := &porttest.Cloud{Entries: []port.Entry{
		{Path: "Note", Folder: true}, {Path: "Note/sub", Folder: true},
		{Path: "Note/a", Hash: "same", Size: new(int64(4))}, {Path: "Note/sub/b", Hash: "same", Size: new(int64(4))},
		{Path: "Note/c", Hash: "same", Size: new(int64(5))}, {Path: "Note/unknown"},
	}}
	s := files.Service{Cloud: f, Account: f}
	for _, depth := range []int{0, 1, 2} {
		r, err := s.Usage(t.Context(), "/", depth, false)
		if err != nil || len(r.Folders) != depth+1 || r.Folders[0].Bytes != 13 || r.Folders[0].Files != 4 {
			t.Fatal(r, err)
		}
	}
	r, err := s.Usage(t.Context(), "Note", 1, false)
	if err != nil || len(r.Folders) != 2 || r.Folders[0].Path != "/Note" || r.Folders[1].Bytes != 4 {
		t.Fatal(r, err)
	}
	r, err = s.Usage(t.Context(), "/", 2, true)
	if err != nil || r.Recycled != 13 || r.Capacity != 1 {
		t.Fatal(r, err)
	}
	for _, tc := range []struct {
		query string
		depth int
		all   bool
	}{{"Note", 1, true}, {"/", -1, false}} {
		if _, err := s.Usage(t.Context(), tc.query, tc.depth, tc.all); !errors.Is(err, port.ErrArgument) {
			t.Fatal(err)
		}
	}
	groups, err := s.Duplicates(t.Context(), "/", false)
	if err != nil || len(groups) != 1 || len(groups[0].Files) != 2 || groups[0].Files[0].Path != "Note/a" {
		t.Fatal(groups, err)
	}
	for _, fail := range []string{"list", "trash", "info"} {
		f.FailAt, f.Failure = fail, errors.New("failure")
		if _, err := s.Usage(t.Context(), "/", 2, true); err == nil {
			t.Fatal(fail)
		}
	}
	f.FailAt = "list"
	if _, err := s.Duplicates(t.Context(), "/", false); err == nil {
		t.Fatal("ignored listing error")
	}
	f.Failure = nil
	s.Account = nil
	if _, err := s.Usage(t.Context(), "/", 1, false); err == nil {
		t.Fatal("missing capacity accepted")
	}
}

func TestParents(t *testing.T) {
	for _, tc := range []struct {
		query, fail      string
		parents, wantErr bool
	}{
		{"Note/sub/new", "", true, false}, {"Note/sub", "", true, false}, {"Note/file/new", "", true, true},
		{"/", "", true, true}, {"Note/new", "stat", true, true}, {"Note/new", "mkdir", true, true},
		{"Note/new", "", false, false}, {"Note/new", "mkdir", false, true},
	} {
		f := &porttest.Cloud{Entries: []port.Entry{{Path: "Note", Folder: true}, {Path: "Note/sub", Folder: true}, {Path: "Note/file"}}}
		if tc.fail != "" {
			f.FailAt, f.Failure = tc.fail, errors.New("failure")
		}
		_, err := (files.Service{Cloud: f}).Mkdir(t.Context(), tc.query, tc.parents)
		if (err != nil) != tc.wantErr {
			t.Fatal(tc, err)
		}
	}
	// One snapshot serves every step of mkdir -p.
	d := porttest.NewDrive(&porttest.NotebookFiles{}, map[string]string{"NOTE/Note/a.note": "a"})
	e, err := (files.Service{Cloud: d}).Mkdir(t.Context(), "NOTE/Note/x/y/z", true)
	if err != nil || e.Path != "NOTE/Note/x/y/z" || d.Count("snapshot") != 1 || d.Count("mkdir") != 3 || d.Count("list") != 9 {
		t.Fatal(e, err, d.Calls)
	}
	d.Fail["snapshot"] = errors.New("listing failed")
	for _, run := range []func() error{
		func() error { _, err := (files.Service{Cloud: d}).Mkdir(t.Context(), "NOTE/Note/q", true); return err },
		func() error {
			return (files.Service{Cloud: d}).Move(t.Context(), "NOTE/Note/a.note", "NOTE/Note/b.note")
		},
	} {
		if run() == nil {
			t.Fatal("snapshot failure ignored")
		}
	}
}

func TestRenameKeepsParent(t *testing.T) {
	if err := (files.Service{}).Rename(t.Context(), "NOTE/Note/a", "NOTE/Note/sub/b"); !errors.Is(err, port.ErrArgument) {
		t.Fatal(err)
	}
}

func TestRenameDistinctEquivalentParents(t *testing.T) {
	for _, withMarks := range []bool{false, true} {
		d := porttest.NewDrive(&porttest.NotebookFiles{}, map[string]string{
			"NOTE/Note/й/a.txt": "a", "NOTE/Note/и\u0306/seed": "seed",
		})
		s := files.Service{Cloud: d}
		var err error
		if withMarks {
			_, err = s.MoveWithMarks(t.Context(), "NOTE/Note/й/a.txt", "NOTE/Note/и\u0306/b.txt", true, port.ChangeOptions{})
		} else {
			err = s.Rename(t.Context(), "NOTE/Note/й/a.txt", "NOTE/Note/и\u0306/b.txt")
		}
		if !errors.Is(err, port.ErrArgument) || d.Count("move") != 0 {
			t.Fatal(withMarks, err, d.Calls)
		}
	}
}

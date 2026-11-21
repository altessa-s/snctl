// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package outdir_test

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/app/outdir"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestPaths(t *testing.T) {
	entries := []port.Entry{
		{ID: "9", Name: "a.note", Path: "Top/a.note"}, {ID: "3", Name: "A.note", Path: "Top/A.note"},
		{ID: "5", Name: "x:y.note", Path: "Top/s/x:y.note"}, {ID: "7", Name: "sub", Path: "Top/sub"},
	}
	same := func(n string) string { return n }
	got := outdir.Paths(entries, "Top", []string{"Top/sub", "Top/s"}, same)
	want := []string{"a (9).note", "A (3).note", "s/x-y.note", "sub (7)"}
	if !slices.Equal(got, want) {
		t.Fatal(got)
	}
	// The result does not depend on the order of entries.
	reversed := slices.Clone(entries)
	slices.Reverse(reversed)
	again := outdir.Paths(reversed, "/Top/", []string{"Top/sub", "Top/s"}, same)
	slices.Reverse(again)
	if !slices.Equal(again, want) {
		t.Fatal(again)
	}
	// A repeated identifier suffix is numbered.
	twins := []port.Entry{{ID: "1", Name: "n", Path: "n"}, {ID: "1", Name: "N", Path: "N"}}
	if got := outdir.Paths(twins, "", nil, same); got[0] == got[1] {
		t.Fatal(got)
	}
	if outdir.Folder("Top/a/b:c", "Top") != "a/b-c" {
		t.Fatal(outdir.Folder("Top/a/b:c", "Top"))
	}
}

func TestRecord(t *testing.T) {
	disk := &porttest.NotebookFiles{}
	stamp := time.Unix(1700000000, 0).UTC()
	entries := map[string]outdir.Entry{"a/b.note": {ID: "1", Hash: "h", Modified: stamp, Size: 3}}
	if err := outdir.Write(t.Context(), disk, "out", outdir.MgetRecord, entries); err != nil {
		t.Fatal(err)
	}
	if got := outdir.Read(disk, "out", outdir.MgetRecord); len(got) != 1 || got["a/b.note"].Size != 3 {
		t.Fatal(got)
	}
	// Keys outside the directory are never trusted; a damaged record is no record.
	disk.Data[filepath.Join("out", outdir.MgetRecord)] = []byte(`{"../x":{"id":"1"},"/abs":{"id":"2"},"ok":{"id":"3"}}`)
	if got := outdir.Read(disk, "out", outdir.MgetRecord); len(got) != 1 {
		t.Fatal(got)
	}
	disk.Data[filepath.Join("out", outdir.MgetRecord)] = []byte("damaged")
	if got := outdir.Read(disk, "out", outdir.MgetRecord); len(got) != 0 {
		t.Fatal(got)
	}
	if got := outdir.Read(disk, "missing", outdir.MgetRecord); len(got) != 0 {
		t.Fatal(got)
	}
	disk.FailAt, disk.Err = "produce", errors.New("full disk")
	if err := outdir.Write(t.Context(), disk, "out", outdir.MgetRecord, entries); err == nil {
		t.Fatal("write failure ignored")
	}
	if !outdir.SameTime(stamp.Add(400*time.Millisecond), stamp) || outdir.SameTime(stamp.Add(time.Second), stamp) {
		t.Fatal("time comparison")
	}
}

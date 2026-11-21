// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package files_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func source() (*porttest.NotebookFiles, *porttest.Drive, files.Service) {
	disk := &porttest.NotebookFiles{Data: map[string][]byte{}}
	for name, data := range map[string]string{
		"a.note": "alpha", "sub/b.pdf": "beta", "sub/deep/c.epub": "gamma", ".hidden/d.pdf": "x", ".e.pdf": "x",
		"notes.txt": "x", "empty.pdf": "", "sub/.snctl-mget.json": "{}",
	} {
		disk.Data[filepath.Join("in", filepath.FromSlash(name))] = []byte(data)
	}
	d := porttest.NewDrive(disk, map[string]string{"DOCUMENT/Document/keep.pdf": "k"})
	return disk, d, files.Service{Cloud: d, Disk: disk, Lock: d}
}

func writes(d *porttest.Drive) int {
	return d.Count("mkdir") + d.Count("upload") + d.Count("move") + d.Count("delete") + d.Count("lock")
}

func TestUpload(t *testing.T) {
	_, d, s := source()
	// A dry run plans and writes nothing, and asks nothing.
	r, err := s.Upload(t.Context(), "in", "DOCUMENT/Document/Box", port.UploadOptions{DryRun: true, Confirm: func(port.TransferReport) error {
		t.Fatal("dry run asked")
		return nil
	}})
	if err != nil || !r.DryRun || r.Counts[port.StatusUpload] != 3 || r.Counts[port.StatusCreate] != 3 || r.Counts[port.StatusSkipped] != 5 || writes(d) != 0 {
		t.Fatal(r, err, d.Calls)
	}
	reasons := map[string]bool{}
	for _, item := range r.Items {
		reasons[item.Reason] = true
	}
	for _, want := range []string{"hidden folder", "hidden file", "empty file", "not a .note, .mark, .pdf or .epub file"} {
		if !reasons[want] {
			t.Fatal(want, r.Items)
		}
	}
	// A declined plan writes nothing.
	if _, err = s.Upload(t.Context(), "in", "DOCUMENT/Document/Box", port.UploadOptions{Confirm: func(port.TransferReport) error { return errors.New("no") }}); err == nil || writes(d) != 0 {
		t.Fatal(err)
	}
	asked := 0
	r, err = s.Upload(t.Context(), "in", "DOCUMENT/Document/Box", port.UploadOptions{Jobs: 3, Confirm: func(port.TransferReport) error { asked++; return nil }})
	if err != nil || asked != 1 || r.Counts[port.StatusUploaded] != 3 || r.Counts[port.StatusCreated] != 3 || d.Count("lock") != 1 || d.Count("snapshot") != 3 {
		t.Fatal(r, err, d.Calls)
	}
	if string(d.Files["DOCUMENT/Document/Box/sub/deep/c.epub"]) != "gamma" {
		t.Fatal(d.Files)
	}
	// Again: everything exists; --incremental finds it identical; --replace overwrites.
	r, err = s.Upload(t.Context(), "in", "DOCUMENT/Document/Box", port.UploadOptions{})
	if !errors.Is(err, port.ErrConflict) || r.Counts[port.StatusExists] != 3 || d.Count("upload") != 3 {
		t.Fatal(r, err)
	}
	r, err = s.Upload(t.Context(), "in", "DOCUMENT/Document/Box", port.UploadOptions{Incremental: true})
	if err != nil || r.Counts[port.StatusUnchanged] != 3 || d.Count("upload") != 3 {
		t.Fatal(r, err)
	}
	d.Seed("DOCUMENT/Document/Box/a.note", "changed")
	r, err = s.Upload(t.Context(), "in", "DOCUMENT/Document/Box", port.UploadOptions{Incremental: true, Replace: true})
	if err != nil || r.Counts[port.StatusReplaced] != 1 || string(d.Files["DOCUMENT/Document/Box/a.note"]) != "alpha" {
		t.Fatal(r, err)
	}
}

func TestUploadRefusals(t *testing.T) {
	_, d, s := source()
	// Invalid category destinations fail before a plan is made.
	r, err := s.Upload(t.Context(), "in", "NOTE", port.UploadOptions{DryRun: true})
	if !errors.Is(err, port.ErrArgument) || len(r.Items) != 0 {
		t.Fatal(r, err)
	}
	for _, tc := range []struct {
		dir, folder string
		opts        port.UploadOptions
	}{{"in", "DOCUMENT/Document/keep.pdf", port.UploadOptions{}}, {"in/a.note", "/", port.UploadOptions{}}, {"in", "/", port.UploadOptions{Jobs: -1}}} {
		if _, err := s.Upload(t.Context(), tc.dir, tc.folder, tc.opts); !errors.Is(err, port.ErrArgument) {
			t.Fatal(tc, err)
		}
	}
	// A busy sync lock writes nothing and reports nothing.
	d.Fail["lock"] = port.ErrBusy
	r, err = s.Upload(t.Context(), "in", "DOCUMENT/Document/Box", port.UploadOptions{})
	if !errors.Is(err, port.ErrBusy) || r.Items != nil || d.Count("mkdir") != 0 {
		t.Fatal(r, err)
	}
	delete(d.Fail, "lock")
	// A failed folder fails the files below it; the others still go up.
	d.Fail["mkdir DOCUMENT/Document/Box/sub"] = errors.New("refused")
	r, err = s.Upload(t.Context(), "in", "DOCUMENT/Document/Box", port.UploadOptions{})
	if err == nil || r.Counts[port.StatusUploaded] != 1 || r.Counts[port.StatusFailed] != 4 {
		t.Fatal(r, err)
	}
	if _, err = (files.Service{Cloud: d}).Upload(t.Context(), "in", "/", port.UploadOptions{}); err == nil {
		t.Fatal("missing disk accepted")
	}
	for _, op := range []string{"snapshot", "stat DOCUMENT/Document/Box2"} {
		_, d2, s2 := source()
		d2.Fail[op] = errors.New("cloud failure")
		if _, err := s2.Upload(t.Context(), "in", "DOCUMENT/Document/Box2", port.UploadOptions{}); err == nil {
			t.Fatal(op)
		}
	}
	for _, op := range []string{"info", "walk"} {
		disk2, _, s2 := source()
		disk2.FailAt, disk2.Err = op, errors.New("disk failure")
		if _, err := s2.Upload(t.Context(), "in", "DOCUMENT/Document/Box2", port.UploadOptions{}); err == nil {
			t.Fatal(op)
		}
	}
	// Interrupted before any upload: every planned file is reported interrupted.
	ctx, cancel := context.WithCancel(t.Context())
	_, d3, s3 := source()
	r, err = s3.Upload(ctx, "in", "DOCUMENT/Document/Box3", port.UploadOptions{Confirm: func(port.TransferReport) error { cancel(); return nil }})
	if !errors.Is(err, context.Canceled) || d3.Count("upload") != 0 {
		t.Fatal(r, err)
	}
	for _, item := range r.Items {
		if item.Status == port.StatusUploaded || item.Status == port.StatusUpload {
			t.Fatal(item)
		}
	}
}

func TestPut(t *testing.T) {
	disk, d, s := source()
	disk.Data["one.pdf"] = []byte("one")
	disk.Data["two.pdf"] = []byte("two")
	asked := 0
	confirm := func(port.TransferReport) error { asked++; return nil }
	r, err := s.Put(t.Context(), port.PutRequest{Sources: []port.PutSource{{Path: "one.pdf"}, {Path: "two.pdf"}}, Destination: "DOCUMENT/Document"},
		port.UploadOptions{Confirm: confirm})
	if err != nil || r.Counts[port.StatusUploaded] != 2 || asked != 1 || r.Items[0].Entry == nil || d.Count("lock") != 1 {
		t.Fatal(r, err)
	}
	// A full path, a new name, standard input.
	r, err = s.Put(t.Context(), port.PutRequest{Sources: []port.PutSource{{Path: "-", Stdin: strings.NewReader("piped")}}, Destination: "DOCUMENT/Document/p.pdf"}, port.UploadOptions{})
	if err != nil || string(d.Files["DOCUMENT/Document/p.pdf"]) != "piped" || r.Items[0].Local != "-" {
		t.Fatal(r, err)
	}
	for key := range disk.Data {
		if key == "stage" {
			t.Fatal("staged input left behind")
		}
	}
	r, err = s.Put(t.Context(), port.PutRequest{Sources: []port.PutSource{{Path: "one.pdf"}}, Destination: "DOCUMENT/Document", Name: "renamed.pdf"}, port.UploadOptions{})
	if err != nil || d.Files["DOCUMENT/Document/renamed.pdf"] == nil {
		t.Fatal(r, err)
	}
	// Conflicts are refused per file, the rest still go up; --replace overwrites.
	disk.Data["three.pdf"] = []byte("three")
	r, err = s.Put(t.Context(), port.PutRequest{Sources: []port.PutSource{{Path: "one.pdf"}, {Path: "three.pdf"}, {Path: "missing.pdf"}}, Destination: "DOCUMENT/Document"}, port.UploadOptions{})
	if !errors.Is(err, port.ErrConflict) || r.Counts[port.StatusUploaded] != 1 || r.Counts[port.StatusExists] != 1 || r.Counts[port.StatusFailed] != 1 {
		t.Fatal(r, err)
	}
	r, err = s.Put(t.Context(), port.PutRequest{Sources: []port.PutSource{{Path: "one.pdf"}}, Destination: "DOCUMENT/Document/two.pdf"}, port.UploadOptions{Replace: true})
	if err != nil || r.Counts[port.StatusReplaced] != 1 || string(d.Files["DOCUMENT/Document/two.pdf"]) != "one" {
		t.Fatal(r, err)
	}
	// Places the cloud does not take, and a missing parent, fail the file.
	for _, dest := range []string{"NOTE/x.pdf", "DOCUMENT/Document/nowhere/x.pdf"} {
		r, err = s.Put(t.Context(), port.PutRequest{Sources: []port.PutSource{{Path: "one.pdf"}}, Destination: dest}, port.UploadOptions{DryRun: true})
		if err == nil || r.Counts[port.StatusFailed] != 1 {
			t.Fatal(dest, r, err)
		}
	}
	for _, req := range []port.PutRequest{
		{}, {Sources: []port.PutSource{{Path: "a"}, {Path: "b"}}, Name: "x", Destination: "DOCUMENT/Document"},
		{Sources: []port.PutSource{{Path: "-"}, {Path: "-"}}, Destination: "DOCUMENT/Document"},
		{Sources: []port.PutSource{{Path: "-", Stdin: strings.NewReader("")}}, Destination: "DOCUMENT/Document"},
		{Sources: []port.PutSource{{Path: "one.pdf"}}, Destination: "DOCUMENT/Document/keep.pdf", Folder: true},
	} {
		if _, err := s.Put(t.Context(), req, port.UploadOptions{}); !errors.Is(err, port.ErrArgument) {
			t.Fatal(req, err)
		}
	}
	if _, err = s.Put(t.Context(), port.PutRequest{Sources: []port.PutSource{{Path: "one.pdf"}}, Destination: "DOCUMENT/Missing", Folder: true}, port.UploadOptions{}); err == nil {
		t.Fatal("missing folder accepted")
	}
}

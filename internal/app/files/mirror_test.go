// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package files_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/app/outdir"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func tree() (*porttest.NotebookFiles, *porttest.Drive, files.Service) {
	disk := &porttest.NotebookFiles{}
	d := porttest.NewDrive(disk, map[string]string{
		"NOTE/Note/a.note": "alpha", "NOTE/Note/sub/b.note": "beta", "NOTE/Note/sub/B.note": "Beta",
		"NOTE/Note/c:d.note": "colon", "DOCUMENT/Document/x.pdf": "pdf",
	})
	return disk, d, files.Service{Cloud: d, Disk: disk}
}

func local(name string) string { return filepath.Join("out", filepath.FromSlash(name)) }

func record(t *testing.T, disk *porttest.NotebookFiles) map[string]outdir.Entry {
	t.Helper()
	var m struct {
		Entries map[string]outdir.Entry `json:"entries"`
	}
	if err := json.Unmarshal(disk.Data[local(outdir.MgetRecord)], &m); err != nil {
		t.Fatal(err)
	}
	return m.Entries
}

func TestMirror(t *testing.T) {
	disk, d, s := tree()
	var mu sync.Mutex
	var last port.SyncProgress
	r, err := s.Mirror(t.Context(), "NOTE/Note", "out", port.MirrorOptions{Jobs: 3, Progress: func(p port.SyncProgress) { mu.Lock(); last = p; mu.Unlock() }})
	if err != nil || r.Counts[port.StatusDownloaded] != 4 || d.Count("snapshot") != 1 || d.Count("list") != 1 {
		t.Fatal(r, err, d.Calls)
	}
	if last.Files != 4 || last.Bytes != last.BytesTotal || last.BytesTotal != 18 {
		t.Fatal(last)
	}
	// Case-colliding names get identifiers, independent of order; unsafe characters are replaced.
	for _, name := range []string{"a.note", "c-d.note"} {
		if string(disk.Data[local(name)]) == "" {
			t.Fatal(name, disk.Data)
		}
	}
	collided := 0
	for key := range disk.Data {
		if strings.Contains(key, "sub"+string(filepath.Separator)) && strings.Contains(key, " (") {
			collided++
		}
	}
	if collided != 2 || len(record(t, disk)) != 4 {
		t.Fatal(collided, disk.Data)
	}
	// Incremental: nothing changed, nothing downloaded.
	r, err = s.Mirror(t.Context(), "NOTE/Note", "out", port.MirrorOptions{Incremental: true})
	if err != nil || r.Counts[port.StatusUnchanged] != 4 || d.Count("download") != 4 {
		t.Fatal(r, err)
	}
	// A changed cloud file and a changed local file come down again.
	d.Seed("NOTE/Note/a.note", "alpha two")
	disk.Data[local("c-d.note")] = []byte("edited")
	r, err = s.Mirror(t.Context(), "NOTE/Note", "out", port.MirrorOptions{Incremental: true})
	if err != nil || r.Counts[port.StatusDownloaded] != 2 || string(disk.Data[local("a.note")]) != "alpha two" {
		t.Fatal(r, err)
	}
	// --delete removes only recorded files the cloud no longer holds.
	d.Drop("NOTE/Note/a.note")
	disk.Data[local("mine.txt")] = []byte("not snctl's")
	r, err = s.Mirror(t.Context(), "NOTE/Note", "out", port.MirrorOptions{Incremental: true, Delete: true})
	if err != nil || r.Counts[port.StatusDeleted] != 1 || disk.Data[local("a.note")] != nil || disk.Data[local("mine.txt")] == nil {
		t.Fatal(r, err)
	}
	if _, ok := record(t, disk)["a.note"]; ok {
		t.Fatal("deleted file still recorded")
	}
}

func TestMirrorGuardAndKept(t *testing.T) {
	disk, d, s := tree()
	if _, err := s.Mirror(t.Context(), "NOTE/Note", "out", port.MirrorOptions{}); err != nil {
		t.Fatal(err)
	}
	for name := range d.Files {
		d.Drop(name)
	}
	before := len(disk.Data)
	_, err := s.Mirror(t.Context(), "NOTE/Note", "out", port.MirrorOptions{Delete: true})
	if !errors.Is(err, port.ErrRefused) || !strings.Contains(err.Error(), "empty") || len(disk.Data) != before {
		t.Fatal(err)
	}
	// A file changed since snctl wrote it is kept and forgotten.
	disk.Data[local("a.note")] = []byte("my edits")
	r, err := s.Mirror(t.Context(), "NOTE/Note", "out", port.MirrorOptions{Delete: true, Force: true})
	if err != nil || r.Counts[port.StatusKept] != 1 || r.Counts[port.StatusDeleted] != 3 || disk.Data[local("a.note")] == nil {
		t.Fatal(r, err)
	}
	if len(record(t, disk)) != 0 {
		t.Fatal(record(t, disk))
	}
	for _, tc := range []struct {
		deletions, recorded int
		empty               bool
		refused             bool
	}{{0, 9, true, false}, {1, 1, false, false}, {2, 3, false, true}, {51, 200, false, true}, {2, 4, false, false}} {
		if (outdir.Guard(tc.deletions, tc.recorded, tc.empty) != "") != tc.refused {
			t.Fatal(tc)
		}
	}
}

func TestMirrorFailures(t *testing.T) {
	disk, d, s := tree()
	d.Fail["download NOTE/Note/sub/b.note"] = errors.New("checksum mismatch")
	r, err := s.Mirror(t.Context(), "NOTE/Note", "out", port.MirrorOptions{Jobs: 2})
	if err == nil || r.Counts[port.StatusFailed] != 1 || r.Counts[port.StatusDownloaded] != 3 || len(record(t, disk)) != 3 {
		t.Fatal(r, err)
	}
	for key := range disk.Data {
		if strings.HasPrefix(key, "staged-") {
			t.Fatal("temporary file left", key)
		}
	}
	for _, tc := range []struct {
		query, dir string
		opts       port.MirrorOptions
	}{{"NOTE/Note", "", port.MirrorOptions{}}, {"NOTE/Note", "-", port.MirrorOptions{}}, {"NOTE/Note", "out", port.MirrorOptions{Jobs: -1}},
		{"NOTE/Note/a.note", "out", port.MirrorOptions{}}} {
		if _, err := s.Mirror(t.Context(), tc.query, tc.dir, tc.opts); !errors.Is(err, port.ErrArgument) {
			t.Fatal(tc, err)
		}
	}
	if _, err := (files.Service{Cloud: d}).Mirror(t.Context(), "/", "out", port.MirrorOptions{}); err == nil {
		t.Fatal("missing disk accepted")
	}
	for _, op := range []string{"snapshot", "stat NOTE/Note", "list"} {
		_, d2, s2 := tree()
		d2.Fail[op] = errors.New("cloud failure")
		if _, err := s2.Mirror(t.Context(), "NOTE/Note", "out", port.MirrorOptions{}); err == nil {
			t.Fatal(op)
		}
	}
	for _, op := range []string{"mkdir", "info", "produce", "settime"} {
		disk2, _, s2 := tree()
		disk2.FailAt, disk2.Err = op, errors.New("disk failure")
		if _, err := s2.Mirror(t.Context(), "/", "out", port.MirrorOptions{}); err == nil {
			t.Fatal(op)
		}
	}
}

func TestMirrorInterrupted(t *testing.T) {
	disk, d, s := tree()
	ctx, cancel := context.WithCancel(t.Context())
	d.Fail["download NOTE/Note/sub/b.note"] = context.Canceled
	var once sync.Once
	r, err := s.Mirror(ctx, "NOTE/Note", "out", port.MirrorOptions{Jobs: 1, Progress: func(p port.SyncProgress) {
		if p.Files == 1 {
			once.Do(cancel)
		}
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	downloaded := r.Counts[port.StatusDownloaded]
	if downloaded == 0 || len(record(t, disk)) != downloaded || r.Counts[port.StatusFailed] != len(r.Items)-downloaded {
		t.Fatal(r, record(t, disk))
	}
	for _, item := range r.Items {
		if item.Status == port.StatusFailed && item.Reason != "interrupted" {
			t.Fatal(item)
		}
	}
}

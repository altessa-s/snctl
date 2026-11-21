// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package files_test

import (
	"context"
	"errors"
	"testing"

	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

type uncertainFile struct {
	port.Files
	before, stored port.Entry
	lookups        int
	uploads        int
	lookupErr      error
	onLookup       func()
}

func (f *uncertainFile) Stat(ctx context.Context, name string) (port.Entry, error) {
	if name == f.before.Path {
		return f.before, nil
	}
	return f.Files.Stat(ctx, name)
}
func (f *uncertainFile) Upload(context.Context, string, string, port.Progress) (port.Entry, error) {
	f.uploads++
	return port.Entry{}, port.ErrAmbiguousUpload
}
func (f *uncertainFile) Refresh(context.Context, string) (port.Entry, error) {
	f.lookups++
	if f.onLookup != nil {
		f.onLookup()
	}
	return f.stored, f.lookupErr
}

func TestReplacementNeedsKnownDifference(t *testing.T) {
	for _, tc := range []struct {
		name   string
		size   *int64
		hash   string
		verify bool
	}{
		{name: "unknown"},
		{name: "size only", size: new(int64(3))},
		{name: "hash only identical", hash: porttest.ContentHash([]byte("new"))},
		{name: "different size", size: new(int64(4)), verify: true},
		{name: "different hash", size: new(int64(3)), hash: porttest.ContentHash([]byte("old")), verify: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			disk := &porttest.NotebookFiles{Data: map[string][]byte{"x.pdf": []byte("new")}}
			drive := porttest.NewDrive(disk, map[string]string{"NOTE/Note/x.pdf": "old"})
			entry, err := drive.Stat(t.Context(), "NOTE/Note/x.pdf")
			if err != nil {
				t.Fatal(err)
			}
			f := &uncertainFile{Files: drive, before: entry, stored: entry}
			f.before.Size, f.before.Hash = tc.size, tc.hash
			f.stored.Size, f.stored.Hash = new(int64(3)), porttest.ContentHash([]byte("new"))
			svc := files.Service{Cloud: f, Disk: disk}
			r, err := svc.Put(t.Context(), port.PutRequest{Sources: []port.PutSource{{Path: "x.pdf"}}, Destination: entry.Path}, port.UploadOptions{Replace: true})
			if (err == nil) != tc.verify {
				t.Fatal(r, err)
			}
			if (f.lookups == 1) != tc.verify {
				t.Fatal("unexpected verification", f.lookups)
			}
		})
	}
}

func TestVerificationLookupFailures(t *testing.T) {
	for _, scenario := range []string{"exhausted", "refused", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			disk := &porttest.NotebookFiles{Data: map[string][]byte{"x.pdf": []byte("new")}}
			drive := porttest.NewDrive(disk, map[string]string{"NOTE/Note/old.pdf": "old"})
			f := &uncertainFile{Files: drive, lookupErr: port.ErrAmbiguousUpload}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := 3
			if scenario == "refused" {
				f.lookupErr, want = port.ErrConflict, 1
			}
			if scenario == "cancelled" {
				f.onLookup, want = cancel, 1
			}
			svc := files.Service{Cloud: f, Disk: disk}
			report, err := svc.Put(ctx, port.PutRequest{Sources: []port.PutSource{{Path: "x.pdf"}}, Destination: "NOTE/Note/x.pdf"}, port.UploadOptions{})
			if err == nil || report.Counts[port.StatusFailed] != 1 || f.lookups != want {
				t.Fatal(report, err, f.lookups)
			}
			if scenario == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

func TestPutVerifiesAfterUnlockedBatch(t *testing.T) {
	disk := &porttest.NotebookFiles{Data: map[string][]byte{"a.pdf": []byte("new"), "b.pdf": []byte("new")}}
	drive := porttest.NewDrive(disk, map[string]string{"NOTE/Note/old.pdf": "old"})
	f := &uncertainFile{Files: drive}
	f.onLookup = func() {
		if f.uploads != 2 {
			t.Error("verification interleaved with uploads", f.uploads)
		}
	}
	svc := files.Service{Cloud: f, Disk: disk}
	report, err := svc.Put(t.Context(), port.PutRequest{Sources: []port.PutSource{{Path: "a.pdf"}, {Path: "b.pdf"}}, Destination: "NOTE/Note"}, port.UploadOptions{})
	if err == nil || report.Counts[port.StatusFailed] != 2 || f.lookups != 2 {
		t.Fatal(report, err, f.lookups)
	}
}

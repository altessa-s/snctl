// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package clouddouble_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/localfs"
	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/clouddouble"
	"github.com/altessa-s/snctl/internal/port"
)

func TestRejectedFinishVisibility(t *testing.T) {
	c, a := client(t)
	c.SetVisibilityMode(clouddouble.HiddenUntilUnlock)
	local := filepath.Join(t.TempDir(), "x.pdf")
	if err := os.WriteFile(local, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	target := "NOTE/Note/x.pdf"
	c.Inject(clouddouble.Fault{Path: "/api/file/2/files/upload/finish", Count: 1, FinishStoredRejected: true})
	err := a.WithSyncLock(t.Context(), func(ctx context.Context) error {
		if _, err := a.Upload(ctx, local, target, nil); !errors.Is(err, port.ErrAmbiguousUpload) {
			t.Fatal(err)
		}
		// Verification in the lock must fail even though the bytes are stored.
		if _, err := a.Refresh(ctx, target); !errors.Is(err, port.ErrNotFound) {
			t.Fatal("visible inside lock", err)
		}
		entries, err := a.List(ctx, "NOTE/Note", true)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Path == target {
				t.Fatal("listed inside lock")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	e, err := a.Refresh(t.Context(), target)
	if err != nil || e.Path != target || e.Size == nil || *e.Size != 3 || e.Hash != clouddouble.Sum([]byte("new")) {
		t.Fatal(e, err)
	}
	entries, err := a.List(t.Context(), "NOTE/Note", true)
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
}

type endingLock struct {
	port.SyncLock
	after func()
	err   error
}

func (l endingLock) WithSyncLock(ctx context.Context, body func(context.Context) error) error {
	err := l.SyncLock.WithSyncLock(ctx, body)
	if l.after != nil {
		l.after()
	}
	return errors.Join(err, l.err)
}

func TestVerificationAfterLockReturn(t *testing.T) {
	for _, cancelAfter := range []bool{false, true} {
		t.Run(map[bool]string{false: "release error", true: "cancelled"}[cancelAfter], func(t *testing.T) {
			c, a := client(t)
			c.SetVisibilityMode(clouddouble.HiddenUntilUnlock)
			dir := t.TempDir()
			for _, name := range []string{"a.pdf", "b.pdf"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("new"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			c.Inject(clouddouble.Fault{Path: "/api/file/2/files/upload/finish", Count: 2, FinishStoredRejected: true})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			releaseErr := errors.New("release response lost")
			lock := endingLock{SyncLock: a, err: releaseErr}
			if cancelAfter {
				lock.after = cancel
			}
			svc := files.Service{Cloud: a, Disk: localfs.Files{}, Lock: lock}
			report, err := svc.Upload(ctx, dir, "NOTE/Note", port.UploadOptions{})
			if !errors.Is(err, releaseErr) {
				t.Fatal(err)
			}
			wantStatus, wantLookups := port.StatusUploaded, 2
			if cancelAfter {
				wantStatus, wantLookups = port.StatusFailed, 0
			}
			if report.Counts[wantStatus] != 2 {
				t.Fatal(report)
			}
			if got := c.Count("/api/file/3/files/query/by/path_v3"); got != wantLookups {
				t.Fatal("lookups", got, wantLookups, c.Requests())
			}
		})
	}
}

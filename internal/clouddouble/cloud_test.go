// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package clouddouble_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/adapter/cloud"
	"github.com/altessa-s/snctl/internal/adapter/localfs"
	"github.com/altessa-s/snctl/internal/adapter/vault"
	"github.com/altessa-s/snctl/internal/clouddouble"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

// client is the real sncloud-go client, through snctl's adapter, against the double.
func client(t *testing.T) (*clouddouble.Cloud, *cloud.Client) {
	t.Helper()
	c := clouddouble.New()
	t.Cleanup(c.Close)
	store := &porttest.Store{}
	for key, value := range map[port.SecretName]string{port.SecretAccessToken: clouddouble.Token, port.SecretClientID: "synthetic-device"} {
		if err := store.Save(t.Context(), key, value); err != nil {
			t.Fatal(err)
		}
	}
	a, err := cloud.New(vault.New(store, port.SecretsFile, "synthetic"), c.URL(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	c.Seed("NOTE/Note", true, nil)
	return c, a
}

func TestFileLifecycle(t *testing.T) {
	c, a := client(t)
	ctx := t.Context()
	id := c.Seed("NOTE/Note/a.note", false, []byte("original"))
	e, err := a.Stat(ctx, "NOTE/Note/a.note")
	if err != nil || e.ID != id || e.Modified == nil || e.Hash != clouddouble.Sum([]byte("original")) || *e.Size != 8 {
		t.Fatal(e, err)
	}
	file, err := a.Download(ctx, e, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(file) })
	if data, readErr := os.ReadFile(file); readErr != nil || string(data) != "original" {
		t.Fatal(string(data), readErr)
	}
	local := filepath.Join(t.TempDir(), "new.note")
	if err = os.WriteFile(local, []byte("uploaded"), 0o600); err != nil {
		t.Fatal(err)
	}
	up, err := a.Upload(ctx, local, "NOTE/Note/new.note", nil)
	if err != nil || up.Hash != clouddouble.Sum([]byte("uploaded")) || up.Path != "NOTE/Note/new.note" {
		t.Fatal(up, err)
	}
	if _, err = a.Mkdir(ctx, "NOTE/Note/sub"); err != nil {
		t.Fatal(err)
	}
	if err = a.Move(ctx, up, "NOTE/Note/sub/moved.note"); err != nil {
		t.Fatal(err)
	}
	if up, err = a.Stat(ctx, "NOTE/Note/sub/moved.note"); err != nil {
		t.Fatal(err)
	}
	if err = a.Delete(ctx, up, false); err != nil {
		t.Fatal(err)
	}
	trash, err := a.Trash(ctx)
	if err != nil || len(trash) != 1 || trash[0].ID != up.ID {
		t.Fatal(trash, err)
	}
	if err = a.Restore(ctx, up.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Stat(ctx, up.Path); err != nil {
		t.Fatal(err)
	}
	if err = a.Delete(ctx, up, true); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Stat(ctx, up.Path); !errors.Is(err, port.ErrNotFound) {
		t.Fatal(err)
	}
	if err = a.Delete(ctx, e, false); err != nil {
		t.Fatal(err)
	}
	if err = a.EmptyTrash(ctx); err != nil {
		t.Fatal(err)
	}
	if trash, err = a.Trash(ctx); err != nil || len(trash) != 0 {
		t.Fatal(trash, err)
	}
	info, err := a.Info(ctx)
	if err != nil || info.Total == 0 {
		t.Fatal(info, err)
	}
}

// A snapshot answers every lookup of an invocation from one listing and sees
// its own writes.
func TestSnapshot(t *testing.T) {
	c, a := client(t)
	ctx := t.Context()
	c.Seed("NOTE/Note/a.note", false, []byte("a"))
	view, err := a.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := view.(port.FileSnapshot).Snapshot(ctx); again != view {
		t.Fatal("a snapshot of a snapshot lists again")
	}
	for _, name := range []string{"NOTE/Note/x", "NOTE/Note/x/y"} {
		if _, err = view.Mkdir(ctx, name); err != nil {
			t.Fatal(name, err)
		}
	}
	local := filepath.Join(t.TempDir(), "b.note")
	if err = os.WriteFile(local, []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	up, err := view.Upload(ctx, local, "NOTE/Note/x/y/b.note", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = view.Move(ctx, up, "NOTE/Note/x/b.note"); err != nil {
		t.Fatal(err)
	}
	c.Seed("NOTE/Note/x/y/z.note", false, []byte("z"))
	if _, err = view.Stat(ctx, "NOTE/Note/x/y/z.note"); !errors.Is(err, port.ErrNotFound) {
		t.Fatal("a snapshot saw a change made elsewhere", err)
	}
	moved, err := view.Stat(ctx, "NOTE/Note/x/b.note")
	if err != nil || moved.ParentID == "" {
		t.Fatal(moved, err)
	}
	if err = view.Delete(ctx, moved, false); err != nil {
		t.Fatal(err)
	}
	if _, err = view.Stat(ctx, "NOTE/Note/x/b.note"); !errors.Is(err, port.ErrNotFound) {
		t.Fatal(err)
	}
	folder, _ := view.Stat(ctx, "NOTE/Note/x")
	if err = view.Move(ctx, folder, "NOTE/Note/w"); err != nil {
		t.Fatal(err)
	}
	if _, err = view.Stat(ctx, "NOTE/Note/w/y"); err != nil {
		t.Fatal("children did not follow their folder", err)
	}
	if n := c.Count("/api/file/2/files/list_folder"); n != 1 {
		t.Fatalf("%d listings", n)
	}
}

func TestSyncLock(t *testing.T) {
	c, a := client(t)
	ran := false
	if err := a.WithSyncLock(t.Context(), func(context.Context) error { ran = true; return nil }); err != nil || !ran {
		t.Fatal(err)
	}
	if c.Count("/api/file/2/files/synchronous/start") != 1 || c.Count("/api/file/2/files/synchronous/end") != 1 {
		t.Fatal(c.Requests())
	}
	c.Hold("tablet")
	if err := a.WithSyncLock(t.Context(), func(context.Context) error { t.Fatal("ran under a held lock"); return nil }); !errors.Is(err, port.ErrBusy) {
		t.Fatal(err)
	}
	c.Hold("")
	failure := errors.New("body failed")
	if err := a.WithSyncLock(t.Context(), func(context.Context) error { return failure }); !errors.Is(err, port.ErrBusy) && err == nil {
		t.Fatal(err)
	}
	if c.Count("/api/file/2/files/synchronous/end") != 2 {
		t.Fatal("lock not released after a failure")
	}
}

func TestSyncLockMarkerAfterBodyFailure(t *testing.T) {
	for _, releaseFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("release-fails=%t", releaseFails), func(t *testing.T) {
			c, a := client(t)
			state := localfs.SyncLockState{Path: filepath.Join(t.TempDir(), "sync-lock")}
			a.SetSyncLockState(state)
			if releaseFails {
				c.Inject(clouddouble.Fault{Path: "/api/file/2/files/synchronous/end", Count: 1, Status: 400})
			}
			bodyErr := errors.New("body failed")
			if err := a.WithSyncLock(t.Context(), func(context.Context) error { return bodyErr }); !errors.Is(err, bodyErr) {
				t.Fatal("body failure lost", err)
			}
			if pending, err := state.Pending(); err != nil || pending != releaseFails {
				t.Fatal("incorrect marker after release", pending, err)
			}
			c.Hold("tablet")
			err := a.WithSyncLock(t.Context(), func(context.Context) error { t.Fatal("body ran while busy"); return nil })
			if !errors.Is(err, port.ErrBusy) || strings.Contains(err.Error(), "a previous snctl run") != releaseFails {
				t.Fatal("incorrect ownership diagnostic", err)
			}
		})
	}
}

func TestFaults(t *testing.T) {
	for name, fault := range map[string]clouddouble.Fault{
		"expired":  {Path: "/api/file/2/files/list_folder", Count: 1, Expired: true},
		"checksum": {Path: "/api/file/3/files/download_v3", Count: 1, Checksum: true},
		// The library retries a broken transfer twice; every attempt is cut short.
		"drop":   {Path: "/api/oss/download", Count: 3, Drop: true},
		"status": {Path: "NOTE/Note/a.note", Count: 1, Status: 400},
		"slow":   {Path: "/api/file/3/files/download_v3", Count: 1, Delay: time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			c, a := client(t)
			id := c.Seed("NOTE/Note/a.note", false, []byte("content"))
			c.Inject(fault)
			if fault.Expired {
				if _, err := a.List(t.Context(), "/", true); !errors.Is(err, port.ErrSession) {
					t.Fatal(err)
				}
				return
			}
			ctx := t.Context()
			if fault.Delay > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
				defer cancel()
			}
			file, err := a.Download(ctx, port.Entry{ID: id}, nil)
			if file != "" {
				_ = os.Remove(file)
			}
			if err == nil {
				t.Fatal("fault ignored")
			}
			// The fault is spent: the next request succeeds.
			if file, err = a.Download(t.Context(), port.Entry{ID: id}, nil); err != nil {
				t.Fatal(err)
			}
			_ = os.Remove(file)
		})
	}
}

func TestRecycleClientPages(t *testing.T) {
	c, a := client(t)
	for i := range 105 {
		id := c.Seed(fmt.Sprintf("NOTE/Note/recycled-%03d.note", i), false, []byte("x"))
		entry, err := a.Stat(t.Context(), fmt.Sprintf("NOTE/Note/recycled-%03d.note", i))
		if err != nil || entry.ID != id {
			t.Fatal(entry, err)
		}
		if err = a.Delete(t.Context(), entry, false); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := a.Trash(t.Context())
	if err != nil || len(entries) != 105 || c.Count("/api/file/recycle/list/query") != 2 {
		t.Fatal(len(entries), err)
	}
}

func TestFolderListingOmitsModificationTime(t *testing.T) {
	c, a := client(t)
	const name = "NOTE/Note/listed.note"
	id := c.Seed(name, false, []byte("body"))
	for _, recursive := range []bool{false, true} {
		entries, err := a.ListFresh(t.Context(), "NOTE/Note", recursive)
		if err != nil || len(entries) != 1 {
			t.Fatal(entries, err)
		}
		e := entries[0]
		if e.ID != id || e.Path != name || e.Hash != clouddouble.Sum([]byte("body")) || e.Size == nil || *e.Size != 4 || (e.Modified != nil) != recursive {
			t.Fatalf("recursive=%t: entry = %+v", recursive, e)
		}
	}
}

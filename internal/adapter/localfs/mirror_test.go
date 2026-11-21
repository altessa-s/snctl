// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
)

func TestMirrorDirectorySafety(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := (Files{}).OpenMirror(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err = (Files{}).OpenMirror(t.Context(), dir); !errors.Is(err, port.ErrMirrorBusy) {
		t.Fatal(err)
	}
	put := func(w io.Writer) error { _, err := io.WriteString(w, "original"); return err }
	if err = root.Publish(t.Context(), "sub/a.pdf", put); err != nil {
		t.Fatal(err)
	}
	if err = root.Publish(t.Context(), "sub/a.pdf", put); !errors.Is(err, fs.ErrExist) {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		from, to string
		want     bool
	}{
		{"sub/a.pdf", "sub/a.pdf", true},
		{"missing", "sub/a.pdf", false},
		{"sub/a.pdf", "missing", false},
	} {
		if same, err := root.SameFile(tc.from, tc.to); err != nil || same != tc.want {
			t.Fatal(tc, same, err)
		}
	}
	if _, err = root.SameFile("../escape", "sub/a.pdf"); err == nil {
		t.Fatal("alias check accepted escape")
	}
	if err = root.Move("sub/a.pdf", "sub/a.pdf"); err != nil {
		t.Fatal(err)
	}
	if err = root.Move("sub/a.pdf", "deeper/b.pdf"); err != nil {
		t.Fatal(err)
	}
	if err = root.Move("missing.pdf", "c.pdf"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	if err = root.Move("deeper", "c.pdf"); !errors.Is(err, fs.ErrInvalid) {
		t.Fatal(err)
	}
	if err = root.Move("../escape", "c.pdf"); err == nil {
		t.Fatal("escape accepted")
	}
	if err = root.Publish(t.Context(), "../escape", put); err == nil {
		t.Fatal("escape accepted")
	}
	if err = root.Publish(t.Context(), "bad.pdf", func(io.Writer) error { return io.ErrUnexpectedEOF }); err == nil {
		t.Fatal("write error ignored")
	}
	if err = root.Publish(t.Context(), "deeper/b.pdf/invalid", put); err == nil {
		t.Fatal("parent file accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = root.Publish(ctx, "canceled.pdf", put); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = (Files{}).OpenMirror(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = (Files{}).OpenMirror(t.Context(), filepath.Join(dir, "absent")); err == nil {
		t.Fatal("created absent directory")
	}
	outside := t.TempDir()
	if err = os.Symlink(outside, filepath.Join(dir, "link")); err == nil {
		if err = root.Publish(t.Context(), "link/out.pdf", put); err == nil {
			t.Fatal("followed link")
		}
		if err = root.Move("deeper/b.pdf", "link/out.pdf"); err == nil {
			t.Fatal("followed link")
		}
	}
	files, err := (Files{}).Scan(dir, func(rel string) bool { return rel == "deeper" })
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Path == "deeper/b.pdf" || f.Path == "link" {
			t.Fatal(f)
		}
	}
	if _, err = (Files{}).Scan(filepath.Join(dir, "missing"), nil); err == nil {
		t.Fatal("scan missing")
	}
}
func TestMirrorRefusesLockLink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "lock")
	if err := os.WriteFile(outside, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, domain.MirrorLockName)); err != nil {
		t.Skip(err)
	}
	if _, err := (Files{}).OpenMirror(t.Context(), dir); err == nil {
		t.Fatal("opened linked lock")
	}
}

func TestMirrorRootEntriesStayPinned(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "mirror")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := (Files{}).OpenMirror(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	const stage = ".snctl-drawing-pinned"
	if err := root.Produce(t.Context(), stage, func(w io.Writer) error { _, err := io.WriteString(w, "pinned"); return err }); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(parent, "moved")
	swapped := true
	if err := os.Rename(dir, moved); err != nil {
		// Windows refuses to move a directory a process holds open, so the
		// root cannot be swapped there at all; the pinned root still lists
		// and removes its own stage below.
		if runtime.GOOS != "windows" {
			t.Fatal(err)
		}
		swapped = false
		moved = dir
	} else {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, stage), []byte("replacement root"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := root.RootEntries()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, en := range entries {
		if en.Path == stage {
			found = true
		}
	}
	if !found {
		t.Fatal("stage not listed inside pinned root", entries)
	}
	if err := root.Remove(stage); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(moved, stage)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("pinned stage remains", err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, stage)); swapped && (err != nil || string(b) != "replacement root") {
		t.Fatal("cleanup affected replacement root", string(b), err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := root.RootEntries(); err == nil {
		t.Fatal("listing a closed root succeeded")
	}
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/adapter/localfs"
)

func TestNotebookOutputs(t *testing.T) {
	dir := t.TempDir()
	f := localfs.Files{Dir: dir}
	dest := filepath.Join(dir, "out.pdf")
	write := func(w io.Writer) error { _, err := io.WriteString(w, "complete"); return err }
	if err := f.Produce(t.Context(), dest, write); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	err := f.Produce(ctx, dest, func(w io.Writer) error { cancel(); return write(w) })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err = f.Produce(ctx, dest, write); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err = f.Produce(t.Context(), dest, func(io.Writer) error { return io.ErrClosedPipe }); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dest)
	if string(b) != "complete" {
		t.Fatal(string(b))
	}
	if err = f.Produce(t.Context(), filepath.Join(dir, "missing", "x"), write); err == nil {
		t.Fatal("missing parent")
	}
	if err = f.Produce(t.Context(), dir, write); err == nil {
		t.Fatal("directory replaced")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatal(files)
	}
	for _, d := range []string{"", dir, dest, filepath.Join(dir, "new"), filepath.Join(dir, "missing", "nested", "out.pdf")} {
		if _, err = f.Destination(d, "x.pdf"); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{filepath.Join(dest, "bad"), filepath.Join(dest, "missing", "bad")} {
		if _, err = f.Destination(d, "x"); err == nil {
			t.Fatal("non-directory ancestor accepted", d)
		}
	}
	if err = f.MkdirAll(filepath.Join(dir, "nested")); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	if err = f.SetModTime(dest, now); err != nil {
		t.Fatal(err)
	}
	if tm, err := f.ModTime(dest); err != nil || !tm.Equal(now) {
		t.Fatal(tm, err)
	}
	for _, p := range []string{dir, filepath.Join(dir, "absent")} {
		if _, err = f.ModTime(p); err == nil {
			t.Fatal(p)
		}
	}
	if p, err := f.PreviewDir(); err != nil || p == "" {
		t.Fatal(p, err)
	}
}

func TestPreviewDirectory(t *testing.T) {
	for _, systemTemp := range []bool{false, true} {
		t.Run(fmt.Sprint(systemTemp), func(t *testing.T) {
			dir := t.TempDir()
			f := localfs.Files{Dir: dir}
			if systemTemp {
				// Go uses TMPDIR on Unix and TMP/TEMP on Windows.
				for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
					t.Setenv(key, dir)
				}
				f.Dir = ""
			}
			preview, err := f.PreviewDir()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(preview) })
			info, err := os.Stat(preview)
			if err != nil || !info.IsDir() {
				t.Fatal(info, err)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm() != 0700 {
				t.Fatalf("preview mode = %v, want 0700", info.Mode().Perm())
			}
			parent, err := os.Stat(filepath.Dir(preview))
			wantParent, wantErr := os.Stat(dir)
			if err != nil || wantErr != nil || !os.SameFile(parent, wantParent) {
				t.Fatal("preview is outside the selected temporary directory", preview, err, wantErr)
			}
		})
	}
}

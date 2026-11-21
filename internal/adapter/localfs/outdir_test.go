// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/adapter/localfs"
)

func TestOutputRoot(t *testing.T) {
	dir := t.TempDir()
	root, err := (localfs.Files{}).OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	write := func(w io.Writer) error { _, err := io.WriteString(w, "data"); return err }
	if err = root.MkdirAll("a/b"); err != nil {
		t.Fatal(err)
	}
	if err = root.Produce(t.Context(), "a/b/file", write); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1234567890, 0)
	if err = root.SetModTime("a/b/file", stamp); err != nil {
		t.Fatal(err)
	}
	info, err := root.Info("a/b/file")
	if err != nil || !info.Regular || info.Size != 4 || !info.Modified.Equal(stamp) {
		t.Fatal(info, err)
	}
	r, err := root.Open("a/b/file")
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || string(b) != "data" {
		t.Fatal(string(b), err)
	}
	if info, err = root.Info("missing"); err != nil || info.Exists {
		t.Fatal(info, err)
	}
	if err = root.Produce(t.Context(), "a/b/file", func(io.Writer) error { return errors.New("writer failed") }); err == nil {
		t.Fatal("writer failure ignored")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = root.Produce(ctx, "a/b/file", write); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	if err = root.Produce(ctx, "a/b/file", func(w io.Writer) error { cancel(); return write(w) }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err = root.Produce(t.Context(), "missing/file", write); err == nil || strings.Contains(err.Error(), ".snctl-output-") {
		t.Fatal(err)
	}
	if err = root.MkdirAll("a/b/file/deeper"); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatal(err)
	}
	for _, name := range []string{"../outside", filepath.Join(dir, "absolute")} {
		if _, err = root.Open(name); err == nil {
			t.Fatal(name)
		}
		if _, err = root.Info(name); err == nil {
			t.Fatal(name)
		}
		if err = root.MkdirAll(name); err == nil {
			t.Fatal(name)
		}
		if err = root.Remove(name); err == nil {
			t.Fatal(name)
		}
		if err = root.SetModTime(name, stamp); err == nil {
			t.Fatal(name)
		}
		if err = root.Produce(t.Context(), name, write); err == nil {
			t.Fatal(name)
		}
	}
	if err = root.Remove("a/b/file"); err != nil {
		t.Fatal(err)
	}

}

func TestOutputRootLinkSwap(t *testing.T) {
	for _, name := range []string{"link", "sub/link", "sub/deep/link"} {
		t.Run(name, func(t *testing.T) {
			dir, outside := t.TempDir(), t.TempDir()
			root, err := (localfs.Files{}).OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if err = root.MkdirAll(filepath.Dir(name)); err != nil {
				t.Fatal(err)
			}
			if err = os.Symlink(outside, filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
			for _, target := range []string{name, name + "/file"} {
				if _, err = root.Info(target); err == nil || !strings.Contains(err.Error(), "is a link") {
					t.Fatal(err)
				}
				if err = root.Produce(t.Context(), target, func(io.Writer) error { return nil }); err == nil {
					t.Fatal("link accepted")
				}
			}
			if err = os.Remove(filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
			if err = root.MkdirAll(name); err != nil {
				t.Fatal(err)
			}
			var renameErr error
			err = root.Produce(t.Context(), name+"/file", func(w io.Writer) error {
				renameErr = os.Rename(filepath.Join(dir, name), filepath.Join(dir, "moved"))
				if renameErr != nil {
					return renameErr
				}
				if err := os.Symlink(outside, filepath.Join(dir, name)); err != nil {
					return err
				}
				_, err := io.WriteString(w, "must stay inside")
				return err
			})
			if runtime.GOOS == "windows" && renameErr != nil {
				// Windows denies moving a directory containing an open output
				// file. The writer must fail and leave the original directory clean.
				if err == nil {
					t.Fatal("blocked swap was ignored")
				}
				entries, readErr := os.ReadDir(filepath.Join(dir, name))
				if readErr != nil || len(entries) != 0 {
					t.Fatal("failed output left files behind", entries, readErr)
				}
				if _, statErr := os.Stat(filepath.Join(dir, "moved")); !os.IsNotExist(statErr) {
					t.Fatal("blocked swap moved the directory", statErr)
				}
			} else if err == nil || !strings.Contains(err.Error(), "is a link") {
				t.Fatal(err, renameErr)
			}
			if _, err = os.Stat(filepath.Join(outside, "file")); !os.IsNotExist(err) {
				t.Fatal(err)
			}
		})
	}
}

func TestSyncLockState(t *testing.T) {
	state := localfs.SyncLockState{Path: filepath.Join(t.TempDir(), "state", "sync-lock")}
	if pending, err := state.Pending(); err != nil || pending {
		t.Fatal(pending, err)
	}
	if err := state.Clear(); err != nil {
		t.Fatal(err)
	}
	if err := state.Mark(); err != nil {
		t.Fatal(err)
	}
	if pending, err := state.Pending(); err != nil || !pending {
		t.Fatal(pending, err)
	}
	data, err := os.ReadFile(state.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = time.Parse(time.RFC3339Nano, strings.TrimSpace(string(data))); err != nil {
		t.Fatal(err)
	}
	if err = state.Clear(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Dir(state.Path)+"-file", nil, 0600); err != nil {
		t.Fatal(err)
	}
	state.Path = filepath.Dir(state.Path) + "-file/child"
	if err = state.Mark(); err == nil {
		t.Fatal("invalid parent accepted")
	}
}

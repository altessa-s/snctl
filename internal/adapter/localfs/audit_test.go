// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestMoveWithoutHardLinks(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{syscall.ENOTSUP, syscall.EPERM, syscall.EXDEV} {
		t.Run(failure.Error(), func(t *testing.T) {
			dir := t.TempDir()
			opened, err := (Files{}).OpenMirror(t.Context(), dir)
			if err != nil {
				t.Fatal(err)
			}
			defer opened.Close()
			d := opened.(*mirrorDirectory)
			d.link = func(string, string) error { return failure }
			if err = d.Publish(t.Context(), "nested/a", func(w io.Writer) error { _, err := io.WriteString(w, "data"); return err }); err != nil {
				t.Fatal(err)
			}
			if err = d.Move("nested/a", "b"); err != nil {
				t.Fatal(err)
			}
			if data, err := os.ReadFile(filepath.Join(dir, "b")); err != nil || string(data) != "data" {
				t.Fatal(string(data), err)
			}
			if err = d.Publish(t.Context(), "b", func(w io.Writer) error { _, err := io.WriteString(w, "other"); return err }); !errors.Is(err, os.ErrExist) {
				t.Fatal(err)
			}
			if data, _ := os.ReadFile(filepath.Join(dir, "b")); string(data) != "data" {
				t.Fatal(string(data))
			}
			if _, err = os.Stat(filepath.Join(dir, "nested/a")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
		})
	}
}

func TestCopyMoveFailures(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	d := &mirrorDirectory{outputDirectory: &outputDirectory{root: root}}
	if err = root.Mkdir("directory", 0700); err != nil {
		t.Fatal(err)
	}
	info, err := root.Stat("directory")
	if err != nil {
		t.Fatal(err)
	}
	if err = d.copyMove("absent", "target", info); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err = d.copyMove("directory", "target", info); err == nil {
		t.Fatal("directory copied")
	}
	if _, err = root.Stat("target"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed copy left target", err)
	}
	if _, err = root.Stat("directory"); err != nil {
		t.Fatal("source removed", err)
	}
}

func TestCopyMoveStagesBeforeInstall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	opened, err := (Files{}).OpenMirror(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	d := opened.(*mirrorDirectory)
	d.link = func(string, string) error { return syscall.ENOTSUP }
	if err := os.WriteFile(filepath.Join(dir, "source"), []byte("complete"), 0600); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("interrupted before installation")
	d.install = func(from, to string) error {
		if filepath.Dir(from) != filepath.Dir(to) || filepath.Base(from)[:14] != ".snctl-mirror-" {
			t.Fatalf("not a hidden sibling: %s", from)
		}
		if data, err := d.root.ReadFile(from); err != nil || string(data) != "complete" {
			t.Fatalf("staging data: %q, %v", data, err)
		}
		if _, err := d.root.Lstat(to); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("target visible before installation: %v", err)
		}
		return failure
	}
	if err := d.Move("source", "nested/target"); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if data, err := d.root.ReadFile("source"); err != nil || string(data) != "complete" {
		t.Fatalf("source lost: %q, %v", data, err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "nested"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed installation left files: %v, %v", entries, err)
	}
}

func TestInstallExclusiveOccupied(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, name := range []string{"source", "target"} {
		if err := root.WriteFile(name, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := installExclusive(root, "source", "target"); !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}
	for _, name := range []string{"source", "target"} {
		if data, err := root.ReadFile(name); err != nil || string(data) != name {
			t.Fatalf("%s: %q, %v", name, data, err)
		}
	}
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/localfs"
	"github.com/altessa-s/snctl/internal/port"
)

func TestLocalInput(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for _, name := range []string{"Note", "out", "out/adir.note"} {
		if err := os.MkdirAll(name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"a.NOTE", "b.Mark", "plain.txt", "out/c.note"} {
		if err := os.WriteFile(name, []byte("data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f := localfs.Files{}
	for _, tc := range []struct {
		query          string
		local, missing bool
	}{
		{"a.NOTE", true, false}, {"b.Mark", true, false}, {"plain.txt", true, false},
		{"./plain.txt", true, false}, {"./a.NOTE", true, false}, {filepath.Join(root, "a.NOTE"), true, false},
		{"Note", false, false}, {"out/adir.note", false, false}, {"/a.NOTE", false, false},
		{"/NOTE/cloud.note", false, false}, {"remote/cloud.note", false, false},
		{"./missing.note", false, true}, {"../missing.note", false, true},
		{"missing.note", false, true}, {"out/missing.mark", false, true}, {filepath.Join(root, "missing.note"), false, true},
	} {
		t.Run(tc.query, func(t *testing.T) {
			local, err := f.LocalInput(tc.query)
			if local != tc.local || errors.Is(err, os.ErrNotExist) != tc.missing || err != nil && !tc.missing {
				t.Fatalf("%q: %v %v", tc.query, local, err)
			}
		})
	}
	if volume := filepath.VolumeName(root); volume != "" {
		// A Windows drive-relative path reaches a stable drive root during
		// the missing-notebook hint search; that search must terminate.
		if local, err := f.LocalInput(volume + "missing.note"); local || !errors.Is(err, os.ErrNotExist) {
			t.Fatal("missing drive-relative notebook", local, err)
		}
	}
	if err := os.Chdir("out"); err != nil {
		t.Fatal(err)
	}
	if local, err := f.LocalInput("../a.NOTE"); !local || err != nil {
		t.Fatal(local, err)
	}
	if _, err := f.LocalInput("../plain.txt/child.note"); err == nil {
		t.Fatal("invalid parent accepted")
	}
	if !f.IsDir(".") || f.IsDir("c.note") || f.IsDir("missing") {
		t.Fatal("directory classification")
	}
}

func TestProducePanicAndDestinationError(t *testing.T) {
	dir := t.TempDir()
	f := localfs.Files{}
	func() {
		defer func() {
			if recover() != "stop" {
				t.Error("panic lost")
			}
		}()
		_ = f.Produce(t.Context(), filepath.Join(dir, "out.pdf"), func(io.Writer) error { panic("stop") })
	}()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
	missing := filepath.Join(dir, "missing")
	err = f.Produce(t.Context(), filepath.Join(missing, "out.pdf"), func(io.Writer) error { return nil })
	if err == nil || !strings.Contains(err.Error(), missing) || strings.Contains(err.Error(), ".snctl-render-") {
		t.Fatal(err)
	}
}

func TestExplicitDirectoryAndMissingHint(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, dir := range []string{"Note", "export/sub"} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile("export/.snctl-render.json", []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	f := localfs.Files{}
	if _, err := f.LocalInput("./Note"); !errors.Is(err, port.ErrOptions) {
		t.Fatal(err)
	}
	for _, query := range []string{"Note/missing.note", "export/sub/missing.note"} {
		_, err := f.LocalInput(query)
		if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "a leading / addresses the cloud") || strings.Contains(err.Error(), "stat ") {
			t.Fatal(err)
		}
	}
}

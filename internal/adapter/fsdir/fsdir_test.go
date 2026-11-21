// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fsdir_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/fsdir"
)

func TestRead(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	entries, err := fsdir.Read(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "a" {
		t.Fatal(entries, err)
	}
	if _, err := fsdir.Read(filepath.Join(dir, "a")); !errors.Is(err, fs.ErrInvalid) {
		t.Fatal("listed a regular file", err)
	}
	if _, err := fsdir.Read(filepath.Join(dir, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("missing directory", err)
	}
}

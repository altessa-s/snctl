//go:build unix

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"io"
	"path/filepath"
	"syscall"
	"testing"
)

func TestOutputRejectsFIFO(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "manifest.json"), 0600); err != nil {
		t.Fatal(err)
	}
	opened, err := (Files{}).OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	d := opened.(*outputDirectory)
	// Check first so the regression fails without blocking on the old Open.
	if err = d.check("manifest.json"); err == nil {
		t.Fatal("FIFO accepted")
	}
	if _, err = d.Open("manifest.json"); err == nil {
		t.Fatal("FIFO opened")
	}
	if err = d.Produce(t.Context(), "manifest.json", func(io.Writer) error { t.Fatal("writer called"); return nil }); err == nil {
		t.Fatal("FIFO replaced")
	}
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBackupDirectoryLock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "backup")
	f := Files{}
	if _, err := f.OpenBackup(t.Context(), dir, false); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.OpenBackup(ctx, dir, true); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	first, err := f.OpenBackup(t.Context(), dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		second, err := f.OpenBackup(t.Context(), dir, true)
		if err == nil {
			second.Close()
			t.Fatal("simultaneous backup accepted")
		}
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := f.OpenBackup(t.Context(), dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".snctl-backup.lock")); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		lock := filepath.Join(dir, ".snctl-backup.lock")
		if err := os.Remove(lock); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(t.TempDir(), "foreign"), lock); err != nil {
			t.Fatal(err)
		}
		if _, err := f.OpenBackup(t.Context(), dir, true); err == nil {
			t.Fatal("lock link followed")
		}
	}
}

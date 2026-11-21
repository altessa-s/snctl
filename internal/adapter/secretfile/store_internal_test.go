// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package secretfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise write failures directly: Save rejects these paths during read.
// Neither failure depends on POSIX permissions, so both also work as root.
func TestWriteFailures(t *testing.T) {
	t.Parallel()
	t.Run("parent is a file", func(t *testing.T) {
		t.Parallel()
		parent := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(parent, []byte("keep"), fileMode); err != nil {
			t.Fatal(err)
		}
		s := New(filepath.Join(parent, "tokens.json"))
		if err := s.write(map[string]string{"account": "user"}); err == nil || !strings.Contains(err.Error(), "create the token directory") {
			t.Fatal(err)
		}
		if data, err := os.ReadFile(parent); err != nil || string(data) != "keep" {
			t.Fatalf("parent changed: %q %v", data, err)
		}
	})
	t.Run("destination is a directory", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		target := filepath.Join(dir, "tokens.json")
		if err := os.Mkdir(target, dirMode); err != nil {
			t.Fatal(err)
		}
		s := New(target)
		if err := s.write(map[string]string{"account": "user"}); err == nil || !strings.Contains(err.Error(), "install the token file") {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 || entries[0].Name() != "tokens.json" || !entries[0].IsDir() {
			t.Fatalf("destination changed or staging file leaked: %v %v", entries, err)
		}
	})
}

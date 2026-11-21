// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wiring

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/config"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestProfileStoreErrors(t *testing.T) {
	t.Parallel()
	opts := Options{Dirs: config.BaseDirs{Config: t.TempDir(), Cache: t.TempDir()}, Keychain: &porttest.Items{}, Read: config.ReadFile}
	cfg, err := config.Load(nil, "work", opts.Dirs)
	if err != nil {
		t.Fatal(err)
	}
	p := profileStore{opts: opts, cfg: cfg}
	if _, err := p.Vaults("../bad"); err == nil {
		t.Fatal("invalid profile")
	}
	if err := p.Remove("../bad"); err == nil {
		t.Fatal("invalid profile")
	}
	if err := os.MkdirAll(cfg.ConfigRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.ConfigFile, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Names(); err == nil {
		t.Fatal("invalid config ignored")
	}
	if _, err := p.Settings("work"); err == nil {
		t.Fatal("invalid config ignored")
	}
	if err := p.Remove("work"); err == nil {
		t.Fatal("invalid config ignored")
	}
	p.cfg.ConfigRoot = filepath.Join(cfg.ConfigFile, "child")
	if _, err := p.Names(); err == nil {
		t.Fatal("not a directory")
	}
	// A file read can change between the initial configuration and vault build.
	n := 0
	opts.Read = func(string) ([]byte, bool, error) {
		n++
		if n > 1 {
			return nil, false, errors.New("changed")
		}
		return nil, false, nil
	}
	if _, err := Build(t.Context(), opts); err == nil {
		t.Fatal("second read failure")
	}
}

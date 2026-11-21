// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wiring

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/config"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestCacheLocalFailures(t *testing.T) {
	opts := Options{Dirs: config.BaseDirs{Config: t.TempDir(), Cache: t.TempDir()}, Keychain: &porttest.Items{}}
	s, err := Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cache.Status(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cache.Clear(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	opts.NoCache = true
	s, err = Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cache.Status(t.Context(), false); err == nil {
		t.Fatal("no cache status")
	}
	if _, err := s.Cache.Clear(t.Context(), true); err == nil {
		t.Fatal("no cache clear")
	}
	opts.NoCache = false
	cfg, err := config.Load(nil, "", opts.Dirs)
	if err != nil {
		t.Fatal(err)
	}
	p := profileStore{opts: opts, cfg: cfg}
	if _, err := p.Settings("../bad"); err == nil {
		t.Fatal("bad name")
	}
	if err := p.Remove("../bad"); err == nil {
		t.Fatal("bad removal")
	}
	root := filepath.Join(cfg.CacheDir, cfg.Profile)
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(t.Context(), opts); err != nil {
		t.Fatal("cache blocked online operation", err)
	}
	opts.Offline = new(true)
	if _, err := Build(t.Context(), opts); err == nil {
		t.Fatal("offline accepted blocked cache root")
	}
	opts.Offline = nil
	if _, err := p.Settings(cfg.Profile); err == nil {
		t.Fatal("blocked profile cache")
	}
	if err := p.Remove(cfg.Profile); err == nil {
		t.Fatal("blocked remove")
	}
	a := cacheAdmin{store: p}
	if _, err := a.Status(t.Context(), false); err == nil {
		t.Fatal("blocked status")
	}
	if _, err := a.Clear(t.Context(), false); err == nil {
		t.Fatal("blocked clear")
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	opts.Completion = true
	if _, err := Build(t.Context(), opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("completion created cache")
	}
}

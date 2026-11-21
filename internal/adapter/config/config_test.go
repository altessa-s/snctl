// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/config"
)

func TestLoadPaths(t *testing.T) {
	t.Parallel()
	dirs := config.BaseDirs{Config: t.TempDir(), Cache: t.TempDir()}
	for _, tt := range []struct {
		env        map[string]string
		flag, want string
	}{
		{nil, "", "default"}, {map[string]string{config.EnvProfile: "work"}, "", "work"},
		{map[string]string{config.EnvProfile: "work"}, "home", "home"},
	} {
		c, err := config.Load(tt.env, tt.flag, dirs)
		if err != nil || c.Profile != tt.want || c.Endpoint != "official" || c.TokenPath != filepath.Join(dirs.Config, "snctl", tt.want, "token.json") {
			t.Fatalf("%+v %v", c, err)
		}
	}
	for _, name := range []string{"../x", "Bad", "a/b", strings.Repeat("a", 65)} {
		if _, err := config.Load(nil, name, dirs); !errors.Is(err, config.ErrProfile) {
			t.Fatal(err)
		}
	}
	for _, d := range []config.BaseDirs{{}, {Config: dirs.Config}} {
		if _, err := config.Load(nil, "", d); err == nil {
			t.Fatal("accepted absent base directories")
		}
	}
	c, err := config.Load(map[string]string{config.EnvConfigDir: dirs.Config, config.EnvCacheDir: dirs.Cache}, "", config.BaseDirs{})
	if err != nil || c.ConfigRoot != dirs.Config || c.CacheDir != dirs.Cache {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestSystemDirsAndEnviron(t *testing.T) {
	root := t.TempDir()
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "AppData", "LocalAppData"} {
		t.Setenv(key, root)
	}
	t.Setenv("SNCTL_TEST_VALUE", "synthetic=a")
	if config.Environ()["SNCTL_TEST_VALUE"] != "synthetic=a" {
		t.Fatal("environment was split at every equals")
	}
	if dirs, err := config.SystemDirs(); err != nil || dirs.Config == "" || dirs.Cache == "" {
		t.Fatalf("%+v %v", dirs, err)
	}
}

func TestAddProfile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	w := config.Writer{Path: path}
	if err := w.AddProfile("work", "http://private.example/", "file"); err != nil {
		t.Fatal(err)
	}
	if err := w.AddProfile("home", "official", ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := config.ParseFile(data)
	if err != nil || len(f.Profiles) != 2 {
		t.Fatalf("%+v %v", f, err)
	}
	for _, tt := range []struct{ name, endpoint, backend string }{{"work", "official", ""}, {"../bad", "official", ""}, {"bad", "ftp://x", ""}, {"bad", "official", "bad"}} {
		if err := w.AddProfile(tt.name, tt.endpoint, tt.backend); err == nil {
			t.Fatalf("accepted %+v", tt)
		}
	}
	if _, err := config.ParseFile([]byte("[profiles.work]\nendpoint='ftp://x'\n")); err == nil {
		t.Fatal("accepted invalid endpoint")
	}
	bad := config.Writer{Path: filepath.Join(t.TempDir(), "config.toml"), Read: func(string) ([]byte, bool, error) { return nil, false, errors.New("read failed") }}
	if err := bad.AddProfile("work", "official", ""); err == nil {
		t.Fatal("read failure ignored")
	}
	bad.Read = files(map[string]string{bad.Path: "not = valid toml"})
	if err := bad.AddProfile("work", "official", ""); err == nil {
		t.Fatal("parse failure ignored")
	}
}

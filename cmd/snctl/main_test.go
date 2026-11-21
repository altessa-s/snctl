// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/cmd/snctl/commands"
	"github.com/altessa-s/snctl/internal/adapter/config"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestRunWith(t *testing.T) {
	root := t.TempDir()
	dirs := config.BaseDirs{Config: filepath.Join(root, "config"), Cache: filepath.Join(root, "cache")}
	s := seams{keychain: &porttest.Items{}, env: map[string]string{}, dirs: &dirs, streams: func(s *commands.Streams) { s.Getenv = func(string) string { return "" } }}
	in, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for _, tt := range []struct {
		args     []string
		code     int
		contains string
	}{
		{[]string{"version"}, 0, ""}, {[]string{"profile", "add", "work", "--yes", "--endpoint", "http://private.example"}, 0, "work"},
		{[]string{"profile", "ls"}, 0, "work"}, {[]string{"profile", "info", "work"}, 0, "http://private.example"},
		{[]string{"secrets", "status"}, 0, "file"}, {[]string{"debug", "paths", "--verbose"}, 0, "config.toml"},
		{[]string{"profile", "del", "work"}, 2, ""}, {[]string{"profile", "del", "work", "--yes"}, 0, "removed work"},
		{[]string{"login"}, 2, ""}, {[]string{"secrets", "migrate", "--to", "keychain", "--yes"}, 1, ""},
	} {
		var out, errOut bytes.Buffer
		code := runWith(tt.args, in, &out, &errOut, func(int) { t.Fatal("unexpected forced exit") }, s)
		if code != tt.code || !strings.Contains(out.String(), tt.contains) {
			t.Fatalf("%v: code %d out %q err %q", tt.args, code, out.String(), errOut.String())
		}
	}
	s.env[config.EnvProfile] = "../bad"
	if _, _, err := build(context.Background(), commands.Request{}, &bytes.Buffer{}, s); err == nil {
		t.Fatal("bad config")
	}
	if isTerminal(&bytes.Buffer{}) {
		t.Fatal("buffer is a terminal")
	}
}

// Explicit roots are sufficient in services and containers without a home.
func TestBuildExplicitRootsWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("AppData", "")
	t.Setenv("LocalAppData", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	env := map[string]string{config.EnvConfigDir: t.TempDir(), config.EnvCacheDir: t.TempDir()}
	svc, closeFn, err := build(t.Context(), commands.Request{}, &bytes.Buffer{}, seams{env: env, keychain: &porttest.Items{}})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Paths.ConfigRoot != env[config.EnvConfigDir] {
		t.Fatalf("%+v", svc.Paths)
	}
	if err := closeFn(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := build(t.Context(), commands.Request{}, &bytes.Buffer{}, seams{env: map[string]string{}, keychain: &porttest.Items{}}); err == nil {
		t.Fatal("missing system paths accepted")
	}
}

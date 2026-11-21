//go:build integration

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/cmd/snctl/commands"
	"github.com/altessa-s/snctl/internal/adapter/config"
	"github.com/altessa-s/snctl/internal/adapter/secretfile"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestE2EBinary(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	binary := filepath.Join(root, "snctl")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	// Only execution-related variables are inherited. No real config or
	// secret-backend choice can escape into the subprocess.
	env := []string{"PATH=" + os.Getenv("PATH"), "SystemRoot=" + os.Getenv("SystemRoot"), "HOME=" + root, "SNCTL_CONFIG_DIR=" + filepath.Join(root, "config"), "SNCTL_CACHE_DIR=" + filepath.Join(root, "cache"), "SNCTL_SECRETS=file"}
	for _, tt := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"version", "--full", "--format", "json"}, 0, "go_version"},
		{[]string{"profile", "add", "work", "--endpoint", "http://private.example", "--yes"}, 0, "work"},
		{[]string{"profile", "ls", "--json"}, 0, "work"}, {[]string{"profile", "info", "work"}, 0, "private.example"},
		{[]string{"secrets", "status"}, 0, "file"}, {[]string{"debug", "paths"}, 0, "config.toml"},
		{[]string{"completion", "bash"}, 0, "snctl"}, {[]string{"completion", "zsh"}, 0, "snctl"},
		{[]string{"completion", "fish"}, 0, "snctl"}, {[]string{"completion", "powershell"}, 0, "snctl"},
		{[]string{"profile", "del", "work"}, 2, "--yes"},
		// Empty source fails before opening the Keychain target.
		{[]string{"secrets", "migrate", "--to", "keychain", "--yes"}, 1, "no token"},
		{[]string{"login"}, 2, "requires --account"},
		{[]string{"login", "--account", "reader@example.invalid"}, 2, "--password-stdin"},
		{[]string{"account"}, 3, "not logged in"},
		{[]string{"check", "--help"}, 0, "--write"},
		{[]string{"tasks", "--help"}, 0, "lists"},
		{[]string{"digests", "--help"}, 0, "handwriting"},
	} {
		cmd := exec.CommandContext(t.Context(), binary, tt.args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				code = e.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		if code != tt.code || !strings.Contains(string(out), tt.want) {
			t.Fatalf("%v: %d %s", tt.args, code, out)
		}
	}
}

func TestE2EMigrateAndRemoveWithKeychainSeam(t *testing.T) {
	t.Parallel()
	dirs := config.BaseDirs{Config: t.TempDir(), Cache: t.TempDir()}
	s := seams{keychain: &porttest.Items{}, dirs: &dirs, env: map[string]string{}, streams: func(s *commands.Streams) { s.Getenv = func(string) string { return "" } }}
	call := func(args ...string) string {
		t.Helper()
		var out, diag bytes.Buffer
		if code := runWith(args, nil, &out, &diag, func(int) { t.Fatal("forced exit") }, s); code != 0 {
			t.Fatalf("%v: %d %s", args, code, diag.String())
		}
		return out.String()
	}
	call("profile", "add", "work", "--yes")
	path := filepath.Join(dirs.Config, "snctl", "work", "token.json")
	store := secretfile.New(path)
	for _, n := range []port.SecretName{port.SecretAccessToken, port.SecretAccount, port.SecretClientID} {
		if err := store.Save(t.Context(), n, "synthetic-value"); err != nil {
			t.Fatal(err)
		}
	}
	out := call("-P", "work", "secrets", "migrate", "--to", "keychain", "--set-default", "--yes")
	if strings.Contains(out, "synthetic-value") {
		t.Fatal("secret leaked")
	}
	call("-P", "work", "secrets", "status", "--json")
	call("-P", "work", "secrets", "migrate", "--to", "file", "--yes")
	call("profile", "del", "work", "--yes")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("tokens remain", err)
	}
}

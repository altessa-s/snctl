//go:build integration && unix

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/adapter/secretfile"
	"github.com/altessa-s/snctl/internal/clouddouble"
	"github.com/altessa-s/snctl/internal/port"
)

// An interrupted mget, by Ctrl-C or SIGTERM, stops with the signal's exit
// status and notice, leaves every finished file recorded and no temporary
// file, in the output directory or in the temporary directory.
func TestE2ESignalsLeaveRecordsConsistent(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "snctl")
	if out, err := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	for _, tc := range []struct {
		sig    syscall.Signal
		code   int
		notice string
	}{{syscall.SIGINT, 130, "Ctrl-C again"}, {syscall.SIGTERM, 143, "terminated"}} {
		t.Run(tc.sig.String(), func(t *testing.T) {
			c := clouddouble.New()
			defer c.Close()
			for i := range 6 {
				c.Seed(fmt.Sprintf("NOTE/Note/S/%sfile%02d.note", []string{"", "a/", "a/b/", "c/"}[i%4], i), false, []byte(strings.Repeat("x", 50+i)))
			}
			// The fourth download in path order stalls until the signal comes.
			c.Inject(clouddouble.Fault{Path: "NOTE/Note/S/c/file03.note", Count: 5, Delay: time.Minute})
			home := t.TempDir()
			tmp := t.TempDir()
			env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "TMPDIR=" + tmp, "SNCTL_CONFIG_DIR=" + filepath.Join(home, "config"),
				"SNCTL_CACHE_DIR=" + filepath.Join(home, "cache"), "SNCTL_SECRETS=file", "SNCTL_PROFILE=e2e"}
			run := func(args ...string) []byte {
				cmd := exec.CommandContext(t.Context(), binary, args...)
				cmd.Env = env
				out, err := cmd.Output()
				if err != nil {
					t.Fatal(args, err)
				}
				return out
			}
			run("profile", "add", "e2e", "--endpoint", c.URL(), "--yes")
			var info port.ProfileSettings
			if err := json.Unmarshal(run("profile", "info", "e2e", "--json"), &info); err != nil {
				t.Fatal(err)
			}
			store := secretfile.New(info.TokenPath)
			for name, value := range map[port.SecretName]string{port.SecretAccessToken: clouddouble.Token, port.SecretClientID: "synthetic-device"} {
				if err := store.Save(t.Context(), name, value); err != nil {
					t.Fatal(err)
				}
			}
			out := filepath.Join(home, "out")
			cmd := exec.CommandContext(t.Context(), binary, "mget", "NOTE/Note/S", "-o", out, "-j", "1")
			cmd.Env = env
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(30 * time.Second)
			for !stalled(c) {
				if time.Now().After(deadline) {
					t.Fatal("the stalled download never started")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := cmd.Process.Signal(tc.sig); err != nil {
				t.Fatal(err)
			}
			err := cmd.Wait()
			exit, ok := errors.AsType[*exec.ExitError](err)
			if !ok || exit.ExitCode() != tc.code || !strings.Contains(stderr.String(), tc.notice) {
				t.Fatalf("%v %s", err, stderr.String())
			}
			data, err := os.ReadFile(filepath.Join(out, ".snctl-mget.json"))
			var record struct {
				Entries map[string]any `json:"entries"`
			}
			if err != nil || json.Unmarshal(data, &record) != nil || len(record.Entries) != 3 {
				t.Fatalf("record %s: %v", data, err)
			}
			for key := range record.Entries {
				if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(key))); err != nil {
					t.Fatal("recorded file missing", err)
				}
			}
			noTemporaryFiles(t, out)
			if left, _ := os.ReadDir(tmp); len(left) != 0 {
				t.Fatal("temporary files left", left)
			}
		})
	}
}

// stalled reports whether the stalled file's download has been requested.
func stalled(c *clouddouble.Cloud) bool {
	for _, r := range c.Requests() {
		if r.Target == "NOTE/Note/S/c/file03.note" {
			return true
		}
	}
	return false
}

func TestUploadSignalsReleaseLock(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "snctl")
	if out, err := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			e := newE2E(t)
			src := e.path("src")
			if err := os.MkdirAll(src, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(src, "a.pdf"), []byte("data"), 0600); err != nil {
				t.Fatal(err)
			}
			tmp := e.path("tmp")
			if err := os.Mkdir(tmp, 0700); err != nil {
				t.Fatal(err)
			}
			e.cloud.Inject(clouddouble.Fault{Method: "POST", Path: "/storage/upload", Count: 1, Delay: time.Minute})
			cmd := exec.CommandContext(t.Context(), binary, "mput", src, "DOCUMENT/Document", "--yes", "-j", "1")
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "TMPDIR=" + tmp, "SNCTL_CONFIG_DIR=" + e.path("config/snctl"),
				"SNCTL_CACHE_DIR=" + e.path("cache/snctl"), "SNCTL_SECRETS=file", "SNCTL_PROFILE=e2e"}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(20 * time.Second)
			for e.cloud.Count("/storage/upload") == 0 {
				if time.Now().After(deadline) {
					t.Fatal("upload never started")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			err := cmd.Wait()
			exit, ok := errors.AsType[*exec.ExitError](err)
			if !ok || exit.ExitCode() != 128+int(sig) {
				t.Fatal(err, stderr.String())
			}
			if e.cloud.Count("/api/file/2/files/synchronous/end") != 1 {
				t.Fatal(e.cloud.Requests())
			}
			if left, err := os.ReadDir(tmp); err != nil || len(left) != 0 {
				t.Fatal(left, err)
			}
		})
	}
}

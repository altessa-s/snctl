// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bufio"
	"bytes"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/config"
	"github.com/altessa-s/snctl/internal/adapter/flock"
	"github.com/altessa-s/snctl/internal/adapter/secretfile"
	"github.com/altessa-s/snctl/internal/clouddouble"
	"github.com/altessa-s/snctl/internal/port"
)

func TestCacheReadRequests(t *testing.T) {
	e := newE2E(t)
	e.cloud.Seed("NOTE/Note/test.txt", false, []byte("hello"))
	e.s.env[config.EnvCacheTTL] = "1h"
	e.must("ls")
	before := len(e.cloud.Requests())
	e.must("tree")
	e.cloud.AssertRequests(t, before, 0)
	e.must("sync", "--files", "--json")
	before = len(e.cloud.Requests())
	var r port.SyncReport
	if err := json.Unmarshal([]byte(e.must("sync", "--files", "--json")), &r); err != nil {
		t.Fatal(err)
	}
	if !r.Current || r.Fetched != 0 || e.listings(before) != 1 {
		t.Fatalf("%+v requests=%+v", r, e.cloud.Requests()[before:])
	}
	e.cloud.AssertRequests(t, before, 1)
	before = len(e.cloud.Requests())
	e.must("get", "NOTE/Note/test.txt", "-o", e.path("held.txt"))
	e.cloud.AssertRequests(t, before, 0)
	for _, args := range [][]string{
		{"ls"}, {"tree"}, {"stat", "NOTE/Note/test.txt"}, {"find", "test"}, {"du"}, {"dupes"},
		{"get", "NOTE/Note/test.txt", "-o", e.path("offline.txt")}, {"mget", "NOTE/Note", "-o", e.path("mirror")},
		{"profile", "ls"}, {"profile", "info"}, {"secrets", "status"}, {"debug", "paths"}, {"debug", "cache", "status"},
	} {
		t.Run(strings.Join(args[:1], ""), func(t *testing.T) {
			before := len(e.cloud.Requests())
			e.must(append([]string{"--offline"}, args...)...)
			e.cloud.AssertRequests(t, before, 0)
		})
	}
	for _, args := range [][]string{{"sync"}, {"mkdir", "NOTE/Note/new", "--yes"}, {"account"}, {"trash"}, {"check"}, {"logout", "--yes"}} {
		before := len(e.cloud.Requests())
		_, stderr, code := e.run(append([]string{"--offline"}, args...)...)
		if code != 2 {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
		e.cloud.AssertRequests(t, before, 0)
	}
	before = len(e.cloud.Requests())
	out := e.must("__complete", "get", "NOTE/Note/te")
	if !strings.Contains(out, "test.txt") {
		t.Fatal(out)
	}
	e.cloud.AssertRequests(t, before, 0)
	before = len(e.cloud.Requests())
	_, stderr, code := e.run("--offline", "get", "NOTE/Note/missing")
	if code != 1 || !strings.Contains(stderr, "missing") {
		t.Fatalf("%d %s", code, stderr)
	}
	e.cloud.AssertRequests(t, before, 0)
	e.must("debug", "cache", "clear", "--yes")
	_, stderr, code = e.run("--offline", "ls")
	if code != 1 || !strings.Contains(stderr, "listing") {
		t.Fatalf("%d %s", code, stderr)
	}
}

func TestCacheBudgetAndNoCache(t *testing.T) {
	e := newE2E(t)
	e.cloud.Seed("NOTE/Note/a", false, bytes.Repeat([]byte("a"), 700000))
	e.cloud.Seed("NOTE/Note/b", false, bytes.Repeat([]byte("b"), 700000))
	e.s.env[config.EnvCacheBudget] = "0"
	e.must("sync", "--files")
	e.s.env[config.EnvCacheBudget] = "1MiB"
	var statuses []port.CacheStatus
	if err := json.Unmarshal([]byte(e.must("debug", "cache", "status", "--json")), &statuses); err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].Bytes > 1<<20 || statuses[0].Blobs != 1 || statuses[0].Removals == 0 {
		t.Fatalf("%+v", statuses)
	}
	before := len(e.cloud.Requests())
	_, stderr, code := e.run("sync", "--files")
	if code != 1 || !strings.Contains(stderr, "1400000") || !strings.Contains(stderr, "SNCTL_CACHE_BUDGET") {
		t.Fatalf("%d %s", code, stderr)
	}
	e.cloud.AssertRequests(t, before, 1)
	e.must("--no-cache", "ls")
	_, stderr, code = e.run("--no-cache", "sync")
	if code != 2 {
		t.Fatalf("%d %s", code, stderr)
	}
	before = len(e.cloud.Requests())
	_, stderr, code = e.run("--no-cache", "--offline", "get", "NOTE/Note/a")
	if code != 1 {
		t.Fatalf("%d %s", code, stderr)
	}
	e.cloud.AssertRequests(t, before, 0)
	e.must("sync")
	e.must("debug", "cache", "status", "--all")
	e.must("debug", "cache", "clear", "--all", "--yes")
}

func TestOfflineNotebookReads(t *testing.T) {
	e := newE2E(t)
	data, err := os.ReadFile("../../testdata/notebooks/blank.note")
	if err != nil {
		t.Fatal(err)
	}
	e.cloud.Seed("NOTE/Note/blank.note", false, data)
	mark, err := os.ReadFile("../../testdata/notebooks/sparse.mark")
	if err != nil {
		t.Fatal(err)
	}
	e.cloud.Seed("NOTE/Note/sparse.mark", false, mark)
	e.must("sync", "NOTE/Note", "--files")
	for _, command := range []string{"text", "highlights", "note", "render", "open"} {
		before := len(e.cloud.Requests())
		name := "NOTE/Note/blank.note"
		if command == "highlights" || command == "note" {
			name = "NOTE/Note/sparse.mark"
		}
		args := []string{"--offline", command, name}
		if command == "render" {
			args = append(args, "-o", e.path("offline.pdf"))
		}
		// Opening an OS viewer is outside a process test; validation still must never request the cloud.
		if command == "open" {
			args = append(args, "--page", "999")
		}
		_, stderr, code := e.run(args...)
		if command != "open" && code != 0 && !strings.Contains(stderr, "not available") && !strings.Contains(stderr, "install") && !strings.Contains(stderr, port.ErrNotebookUnavailable.Error()) {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
		e.cloud.AssertRequests(t, before, 0)
	}
	before := len(e.cloud.Requests())
	e.must("--offline", "get", "NOTE/Note/blank.note", "-o", e.path("blank.note"))
	e.must("--offline", "mget", "NOTE/Note", "-o", e.path("notes"))
	e.cloud.AssertRequests(t, before, 0)
}

func TestCacheWriteIgnoresTTL(t *testing.T) {
	e := newE2E(t)
	e.s.env[config.EnvCacheTTL] = "1h"
	e.must("ls")
	before := len(e.cloud.Requests())
	e.must("mkdir", "NOTE/Note/new", "--yes")
	if e.listings(before) != 3 {
		t.Fatalf("%+v", e.cloud.Requests()[before:])
	}
	before = len(e.cloud.Requests())
	e.must("ls")
	if e.listings(before) != 0 {
		t.Fatal("post-write verification did not refresh TTL listing")
	}
}

func TestCacheCommandFailures(t *testing.T) {
	e := newE2E(t)
	for _, args := range [][]string{{"debug", "cache", "clear"}, {"sync", "--jobs", "-1"}} {
		_, _, code := e.run(args...)
		if code != 2 {
			t.Fatalf("%v: %d", args, code)
		}
	}
	_, _, code := e.run("debug", "cache", "status", "--no-cache")
	if code != 1 {
		t.Fatal(code)
	}
	_, _, code = e.run("debug", "cache", "clear", "--no-cache", "--yes")
	if code != 1 {
		t.Fatal(code)
	}
}

func TestCompletionWithoutCredentials(t *testing.T) {
	e := newE2E(t)
	e.cloud.Seed("NOTE/Note/test", false, []byte("hello"))
	e.must("ls")
	if err := os.Remove(e.path("config/snctl/e2e/token.json")); err != nil {
		t.Fatal(err)
	}
	before := len(e.cloud.Requests())
	if out := e.must("__complete", "get", "NOTE/Note/te"); !strings.Contains(out, "test") {
		t.Fatal(out)
	}
	e.cloud.AssertRequests(t, before, 0)
}

func TestCacheConfigOfflineOverride(t *testing.T) {
	e := newE2E(t)
	e.must("ls")
	file := e.path("config/snctl/config.toml")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("\n[profiles.e2e.cache]\noffline=true\n")...)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	before := len(e.cloud.Requests())
	e.must("ls")
	e.cloud.AssertRequests(t, before, 0)
	_, _, code := e.run("sync")
	if code != 2 {
		t.Fatal(code)
	}
	e.must("--offline=false", "ls")
	e.cloud.AssertRequests(t, before, 1)
	for _, cmd := range []string{"login", "logout"} {
		_, _, code := e.run(cmd, "--no-cache", "--yes")
		if code != 2 {
			t.Fatal(code)
		}
	}
}

func TestCacheOnlyProfile(t *testing.T) {
	e := newE2E(t)
	e.must("ls")
	if err := os.Remove(e.path("config/snctl/config.toml")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(e.path("config/snctl/e2e")); err != nil {
		t.Fatal(err)
	}
	e.s.env[config.EnvProfile] = "default"
	if out := e.must("profile", "ls"); !strings.Contains(out, "e2e") {
		t.Fatal("cache-only profile omitted", out)
	}
	if out := e.must("debug", "cache", "status", "--all"); !strings.Contains(out, "e2e") {
		t.Fatal("cache-only status omitted", out)
	}
	e.must("debug", "cache", "clear", "--all", "--yes")
}

func TestCacheUploadedBodyAndStatus(t *testing.T) {
	e := newE2E(t)
	source := e.path("upload.pdf")
	if err := os.WriteFile(source, []byte("local upload bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	e.must("put", source, "DOCUMENT/Document/upload.pdf", "--yes")
	before := len(e.cloud.Requests())
	for _, format := range []string{"table", "plain", "json"} {
		out := e.must("--offline", "get", "DOCUMENT/Document/upload.pdf", "-o", e.path("saved.pdf"), "--format", format)
		if !strings.Contains(out, "cached") {
			t.Fatal(out)
		}
		e.golden("cached-get-"+format, out)
		out = e.must("--offline", "mget", "DOCUMENT/Document", "-o", e.path("mirror"), "--format", format)
		if !strings.Contains(out, "cached") {
			t.Fatal(out)
		}
		if format != "table" {
			e.golden("cached-mget-"+format, out)
		}
	}
	data, err := os.ReadFile(e.path("saved.pdf"))
	if err != nil || string(data) != "local upload bytes" {
		t.Fatal(string(data), err)
	}
	e.cloud.AssertRequests(t, before, 0)
	t.Log("put followed immediately by offline get and mget succeeded; both reported cached in table, plain and JSON")
}

func TestCacheListingIdentity(t *testing.T) {
	for _, change := range []string{"endpoint", "account"} {
		t.Run(change, func(t *testing.T) {
			e := newE2E(t)
			e.must("ls")
			if change == "account" {
				store := secretfile.New(e.path("config/snctl/e2e/token.json"))
				if err := store.Save(t.Context(), port.SecretAccount, "other@example.invalid"); err != nil {
					t.Fatal(err)
				}
			} else {
				file := e.path("config/snctl/config.toml")
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				data = bytes.ReplaceAll(data, []byte(e.cloud.URL()), []byte("http://127.0.0.1:1"))
				if err := os.WriteFile(file, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := len(e.cloud.Requests())
			_, stderr, code := e.run("--offline", "ls")
			if code != 1 || !strings.Contains(stderr, "cached listing belongs to another endpoint/account; run sync") {
				t.Fatalf("%d %s", code, stderr)
			}
			e.cloud.AssertRequests(t, before, 0)
			if change == "account" {
				e.s.env[config.EnvCacheTTL] = "1h"
				e.must("ls")
				if e.listings(before) != 1 {
					t.Fatal("reused another account's listing")
				}
			}
		})
	}
}

// TestCacheLockProcess holds the operating-system lock until its parent closes stdin.
func TestCacheLockProcess(t *testing.T) {
	dir := os.Getenv("SNCTL_TEST_CACHE_LOCK")
	if dir == "" {
		t.Skip("lock helper")
	}
	f, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if busy, err := flock.TryLock(f); err != nil || busy {
		t.Fatal(busy, err)
	}
	defer flock.Unlock(f)
	fmt.Println("locked")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestCacheBusyOnlineDownload(t *testing.T) {
	if os.Getenv("SNCTL_TEST_BINARY") == "" {
		t.Skip("exercised against the built executable")
	}
	e := newE2E(t)
	e.cloud.Seed("NOTE/Note/online.txt", false, []byte("network bytes"))
	e.must("ls")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestCacheLockProcess$")
	cmd.Env = append(os.Environ(), "SNCTL_TEST_CACHE_LOCK="+e.path("cache/snctl/e2e"))
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = input.Close()
		if err := cmd.Wait(); err != nil {
			t.Error(err)
		}
	}()
	ready, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || ready != "locked\n" {
		t.Fatal(ready, err)
	}
	before := len(e.cloud.Requests())
	out, stderr, code := e.run("-v", "get", "NOTE/Note/online.txt", "-o", e.path("online.txt"))
	if code != 0 || !strings.Contains(out, "downloaded") || !strings.Contains(stderr, "profile cache is busy") {
		t.Fatalf("%d %s %s", code, out, stderr)
	}
	if e.listings(before) != 1 {
		t.Fatal("online fallback did not list")
	}
	data, err := os.ReadFile(e.path("online.txt"))
	if err != nil || string(data) != "network bytes" {
		t.Fatal(string(data), err)
	}
	t.Log("a second process held the cache lock beyond the 30-second wait; online get downloaded successfully with a verbose cache diagnostic")
}

func TestCacheUploadedOutcomes(t *testing.T) {
	for _, outcome := range []string{"uploaded", "replaced", "verified"} {
		t.Run(outcome, func(t *testing.T) {
			e := newE2E(t)
			dir := e.path("input")
			if err := os.MkdirAll(filepath.Join(dir, "empty"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "file.pdf"), []byte("new bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			if outcome == "replaced" {
				e.cloud.Seed("DOCUMENT/Document/file.pdf", false, []byte("old bytes"))
			}
			if outcome == "verified" {
				e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/upload/finish", Count: 1, FinishStoredRejected: true})
			}
			var report port.TransferReport
			out := e.must("mput", dir, "DOCUMENT/Document", "--replace", "--yes", "--json")
			if err := json.Unmarshal([]byte(out), &report); err != nil {
				t.Fatal(err)
			}
			status := port.StatusUploaded
			if outcome == "replaced" {
				status = port.StatusReplaced
			}
			if report.Counts[status] != 1 || report.Counts[port.StatusCreated] != 0 {
				t.Fatal(out)
			}
			before := len(e.cloud.Requests())
			out = e.must("--offline", "get", "DOCUMENT/Document/file.pdf", "-o", e.path("saved.pdf"), "--json")
			if !strings.Contains(out, "cached") {
				t.Fatal(out)
			}
			e.cloud.AssertRequests(t, before, 0)
		})
	}
}

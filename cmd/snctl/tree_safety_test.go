// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/clouddouble"
	"github.com/altessa-s/snctl/internal/port"
)

func (e *e2e) binaryRun(binary string, args []string) (string, string, int) {
	e.t.Helper()
	cmd := exec.CommandContext(e.t.Context(), binary, args...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "SystemRoot=" + os.Getenv("SystemRoot"),
		"TMPDIR=" + e.root, "TMP=" + e.root, "TEMP=" + e.root,
		"SNCTL_CONFIG_DIR=" + e.path("config/snctl"), "SNCTL_CACHE_DIR=" + e.path("cache/snctl"), "SNCTL_SECRETS=file",
		"SNCTL_PROFILE=" + e.s.env["SNCTL_PROFILE"]}
	for _, key := range []string{"SNCTL_CACHE_TTL", "SNCTL_CACHE_BUDGET"} {
		if v := e.s.env[key]; v != "" {
			cmd.Env = append(cmd.Env, key+"="+v)
		}
	}
	var out, errout bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errout
	err := cmd.Run()
	code := 0
	if err != nil {
		code = cmd.ProcessState.ExitCode()
	}
	clean := func(s string) string { return strings.ReplaceAll(s, e.root, "$ROOT") }
	return clean(out.String()), clean(errout.String()), code
}

func TestTreeLinkConfinement(t *testing.T) {
	for _, depth := range []string{"sub", "sub/deep", "sub/deep/leaf"} {
		t.Run(depth, func(t *testing.T) {
			e := newE2E(t)
			top := "NOTE/Note/D/" + depth
			for i := range 4 {
				e.cloud.Seed(fmt.Sprintf("%s/f%d.note", top, i), false, []byte(fmt.Sprint(i)))
			}
			e.cloud.Seed("NOTE/Note/D/top.note", false, []byte("top"))
			out := e.path("out")
			e.must("mget", "NOTE/Note/D", "-o", out)
			original := filepath.Join(out, filepath.FromSlash(depth))
			outside := e.path("archive")
			if err := os.Rename(original, outside); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, original); err != nil {
				t.Fatal(err)
			}
			e.cloud.Remove(top + "/f0.note")
			e.cloud.Seed(top+"/new.note", false, []byte("new"))
			stdout, stderr, code := e.run("mget", "NOTE/Note/D", "-o", out, "--incremental", "--delete", "--json")
			r := report(t, stdout)
			if code == 0 || r.Counts[port.StatusSkipped] < 5 || !strings.Contains(stderr, "is a link") {
				t.Fatal(code, r, stderr)
			}
			if b, err := os.ReadFile(filepath.Join(outside, "f0.note")); err != nil || string(b) != "0" {
				t.Fatal(string(b), err)
			}
			if _, err := os.Stat(filepath.Join(outside, "new.note")); !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if len(r.Items) < 6 {
				t.Fatal(r)
			}
		})
	}
}

func TestTreeForeignRecord(t *testing.T) {
	e := newE2E(t)
	e.cloud.Seed("NOTE/Note/A/old.note", false, []byte("old"))
	e.cloud.Seed("NOTE/Note/B/new.note", false, []byte("new"))
	out := e.path("out")
	e.must("mget", "NOTE/Note/A", "-o", out)
	if _, stderr, code := e.run("mget", "NOTE/Note/B", "-o", out, "--delete"); code != 4 || !strings.Contains(stderr, "source") {
		t.Fatal(code, stderr)
	}
	e.must("mget", "NOTE/Note/B", "-o", out, "--delete", "--force")
	if b, err := os.ReadFile(filepath.Join(out, "old.note")); err != nil || string(b) != "old" {
		t.Fatal(string(b), err)
	}
	recordPath := filepath.Join(out, ".snctl-mget.json")
	outside := e.path("record")
	if err := os.Rename(recordPath, outside); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, recordPath); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := e.run("mget", "NOTE/Note/B", "-o", out, "--force"); code == 0 || !strings.Contains(stderr, "is a link") {
		t.Fatal(code, stderr)
	}
	after, err := os.ReadFile(outside)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal(err)
	}
}

func TestTreePortableNames(t *testing.T) {
	e := newE2E(t)
	names := []string{"café.note", "cafe\u0301.note", "CAFÉ.note", "..", "CON.note", "x:y.note", "x?y.note"}
	for _, name := range names {
		e.cloud.Seed("NOTE/Note/N/"+name, false, []byte(name))
	}
	stdout := e.must("mget", "NOTE/Note/N", "-o", e.path("out"), "--json")
	r := report(t, stdout)
	if r.Counts[port.StatusDownloaded] != len(names) {
		t.Fatal(r)
	}
	seen := map[string]bool{}
	for _, item := range r.Items {
		local := strings.ReplaceAll(item.Local, "$ROOT", e.root)
		b, err := os.ReadFile(local)
		if err != nil || string(b) != strings.TrimPrefix(item.Cloud, "NOTE/Note/N/") {
			t.Fatal(item, string(b), err)
		}
		if seen[local] || strings.Contains(local, "\u0301") || strings.Contains(item.Reason, ".snctl-download-") {
			t.Fatal(item)
		}
		seen[local] = true
	}
}

func TestUploadModes(t *testing.T) {
	for _, mode := range []clouddouble.UploadMode{clouddouble.Duplicate, clouddouble.Overwrite, clouddouble.Autorename} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			e := newE2E(t)
			e.cloud.SetUploadMode(mode)
			old := e.cloud.Seed("DOCUMENT/Document/x.pdf", false, []byte("old"))
			file := e.path("x.pdf")
			if err := os.WriteFile(file, []byte("new"), 0600); err != nil {
				t.Fatal(err)
			}
			stdout := e.must("put", file, "DOCUMENT/Document/x.pdf", "--replace", "--yes", "--json")
			var entries []port.Entry
			if err := json.Unmarshal([]byte(stdout), &entries); err != nil || len(entries) != 1 {
				t.Fatal(stdout, err)
			}
			if mode == clouddouble.Overwrite && entries[0].ID != old {
				t.Fatal("identifier changed")
			}
			b, ok := e.cloud.File("DOCUMENT/Document/x.pdf")
			if !ok || string(b) != "new" {
				t.Fatal(string(b), ok)
			}
			count := 0
			for _, name := range e.cloud.Paths() {
				if name == "DOCUMENT/Document/x.pdf" {
					count++
				}
			}
			if count != 1 {
				t.Fatal(e.cloud.Paths())
			}
			trash := e.must("trash", "--json")
			if strings.Contains(trash, `"id": "`+old+`"`) != (mode != clouddouble.Overwrite) {
				t.Fatal(trash)
			}

			if err := os.Mkdir(e.path("src"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(e.path("src/x.pdf"), []byte("third"), 0600); err != nil {
				t.Fatal(err)
			}
			r := report(t, e.must("mput", e.path("src"), "DOCUMENT/Document", "--replace", "--yes", "--json"))

			if r.Counts[port.StatusReplaced] != 1 {
				t.Fatal(r)
			}
		})
	}
}

func TestPersistentTransferFaults(t *testing.T) {
	e := newE2E(t)
	e.cloud.Seed("NOTE/Note/a.note", false, []byte("good"))
	out := e.path("out")
	e.must("mget", "NOTE/Note", "-o", out)
	e.cloud.Inject(clouddouble.Fault{Method: "POST", Path: "/api/file/2/files/list_folder", UntilCleared: true, Status: 500})
	if _, _, code := e.run("mget", "NOTE/Note", "-o", out, "--delete", "--force"); code == 0 {
		t.Fatal("listing failure accepted")
	}
	if b, err := os.ReadFile(filepath.Join(out, "a.note")); err != nil || string(b) != "good" {
		t.Fatal(string(b), err)
	}
	e.cloud.ClearFaults()
	e.cloud.Inject(clouddouble.Fault{Method: "POST", Path: "/api/file/2/files/list_folder", Count: 1, Status: 500})
	e.must("mget", "NOTE/Note", "-o", out, "--incremental")
	local := e.path("x.pdf")
	if err := os.WriteFile(local, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	e.cloud.Inject(clouddouble.Fault{Method: "POST", Path: "/storage/upload", UntilCleared: true, Status: 500})
	if _, _, code := e.run("put", local, "DOCUMENT/Document/x.pdf", "--yes"); code == 0 {
		t.Fatal("upload failure accepted")
	}
	e.cloud.ClearFaults()
	e.cloud.Inject(clouddouble.Fault{Method: "POST", Path: "/storage/upload", Count: 1, Status: 500})
	e.must("put", local, "DOCUMENT/Document/x.pdf", "--yes")
	e.cloud.Inject(clouddouble.Fault{Method: "POST", Path: "/api/file/3/files/upload/apply", Count: 1, Before: func() { e.cloud.Seed("DOCUMENT/Document/race.pdf", true, nil) }})
	if _, _, code := e.run("put", local, "DOCUMENT/Document/race.pdf", "--yes"); code != 4 {
		t.Fatal(code)
	}
}

func TestChunkUploadAndInvalidDestination(t *testing.T) {
	e := newE2E(t)
	data := bytes.Repeat([]byte("p"), (8<<20)+1)
	if err := os.Mkdir(e.path("src"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.path("src/large.pdf"), data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, dest := range []string{"NOTE", "NOTE/Other"} {
		start := len(e.cloud.Writes())
		if _, stderr, code := e.run("mput", e.path("src"), dest, "--yes"); code != 2 || !strings.Contains(stderr, "NOTE/Note") {
			t.Fatal(code, stderr)
		}
		if len(e.cloud.Writes()) != start {
			t.Fatal(e.cloud.Writes())
		}
	}
	e.must("mput", e.path("src"), "EXPORT/large", "--yes")
	if e.cloud.Count("/storage/part") != 2 {
		t.Fatal(e.cloud.Requests())
	}
	got, ok := e.cloud.File("EXPORT/large/large.pdf")
	if !ok || !bytes.Equal(got, data) {
		t.Fatal(len(got), ok)
	}
}

func TestConcurrentUploadOutcomes(t *testing.T) {
	for _, mode := range []clouddouble.UploadMode{clouddouble.Duplicate, clouddouble.Overwrite, clouddouble.Autorename} {
		for _, command := range []string{"put", "mput"} {
			t.Run(fmt.Sprintf("%s/%d", command, mode), func(t *testing.T) {
				e := newE2E(t)
				e.cloud.SetUploadMode(mode)
				if err := os.Mkdir(e.path("src"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(e.path("src/x.pdf"), []byte("new"), 0600); err != nil {
					t.Fatal(err)
				}
				e.cloud.Inject(clouddouble.Fault{Method: "POST", Path: "/api/file/3/files/upload/apply", Count: 1, Before: func() {
					e.cloud.Seed("DOCUMENT/Document/x.pdf", false, []byte("concurrent"))
				}})
				args := []string{"put", e.path("src/x.pdf"), "DOCUMENT/Document/x.pdf", "--yes", "--json"}
				if command == "mput" {
					args = []string{"mput", e.path("src"), "DOCUMENT/Document", "--yes", "--json"}
				}
				stdout, stderr, code := e.run(args...)
				wantCode := 0
				if mode == clouddouble.Duplicate {
					wantCode = 6
				}
				if code != wantCode {
					t.Fatal(code, stderr)
				}
				assertUploadListings(t, e.cloud, 2, 1)
				if mode == clouddouble.Overwrite {
					return
				}
				r := report(t, stdout)
				want := port.StatusDuplicate
				phrase := "a second entry named x.pdf now exists"
				if mode == clouddouble.Autorename {
					want, phrase = port.StatusRenamed, "uploaded as DOCUMENT/Document/x(1).pdf"
				}
				if r.Counts[want] != 1 || !strings.Contains(stderr, phrase) || strings.Contains(stdout, `"replaced"`) {
					t.Fatal(r, stderr)
				}
			})
		}
	}
}

func TestLostSyncLockResponse(t *testing.T) {
	e := newE2E(t)
	if err := os.WriteFile(e.path("x.pdf"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	e.cloud.Inject(clouddouble.Fault{Method: "POST", Path: "/api/file/2/files/synchronous/start", Count: 1, LostResponse: true})
	if _, _, code := e.run("put", e.path("x.pdf"), "DOCUMENT/Document/x.pdf", "--yes"); code == 0 {
		t.Fatal("lost response accepted")
	}
	if _, stderr, code := e.run("put", e.path("x.pdf"), "DOCUMENT/Document/x.pdf", "--yes"); code == 0 || !strings.Contains(stderr, "a previous snctl run") {
		t.Fatal(code, stderr)
	}
	e.cloud.ExpireLockAfter(-1)
	e.must("put", e.path("x.pdf"), "DOCUMENT/Document/x.pdf", "--yes")
	if _, err := os.Stat(e.path("config/snctl/e2e/sync-lock")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestLinkChangesDuringDownload(t *testing.T) {
	e := newE2E(t)
	e.cloud.Seed("NOTE/Note/R/sub/a.note", false, []byte("a"))
	out := e.path("out")
	if err := os.MkdirAll(filepath.Join(out, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(e.path("outside"), 0700); err != nil {
		t.Fatal(err)
	}
	e.cloud.Inject(clouddouble.Fault{Method: "POST", Path: "/api/file/3/files/download_v3", Count: 1, Before: func() {
		if err := os.Rename(filepath.Join(out, "sub"), e.path("moved")); err != nil {
			t.Error(err)
			return
		}
		if err := os.Symlink(e.path("outside"), filepath.Join(out, "sub")); err != nil {
			t.Error(err)
		}
	}})
	stdout, stderr, code := e.run("mget", "NOTE/Note/R", "-o", out, "--json")
	if code == 0 || report(t, stdout).Counts[port.StatusSkipped] != 1 || !strings.Contains(stderr, "is a link") {
		t.Fatal(code, stdout, stderr)
	}
	if entries, err := os.ReadDir(e.path("outside")); err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}

func TestDuplicateGroupsExcludeEmpty(t *testing.T) {
	e := newE2E(t)
	for _, name := range []string{"a", "b"} {
		e.cloud.Seed("EXPORT/"+name, false, nil)
	}
	stdout := e.must("dupes", "--json")
	if strings.TrimSpace(stdout) != "[]" {
		t.Fatal(stdout)
	}
}

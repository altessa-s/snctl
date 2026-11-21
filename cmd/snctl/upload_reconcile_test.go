// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/clouddouble"
	"github.com/altessa-s/snctl/internal/port"
)

func TestUploadReconciliation(t *testing.T) {
	t.Setenv("SNCLOUD_WIRE_LOG", "bodies")
	for _, bulk := range []bool{false, true} {
		for _, scenario := range []string{"stored", "retry", "http", "size", "hash", "missing", "refused", "replace", "identical"} {
			t.Run(scenario+map[bool]string{false: "/put", true: "/mput"}[bulk], func(t *testing.T) {
				e := newE2E(t)
				e.cloud.SetVisibilityMode(clouddouble.HiddenUntilUnlock)
				target := "NOTE/Note/x.pdf"
				if err := os.Mkdir(e.path("src"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(e.path("src/x.pdf"), []byte("new"), 0600); err != nil {
					t.Fatal(err)
				}
				fault := clouddouble.Fault{Path: "/api/file/2/files/upload/finish", Count: 1, FinishStoredRejected: true}
				want, lookups := 1, 1
				switch scenario {
				case "http":
					fault.FinishStoredRejected = false
					fault.FinishStoredError = true
					want = 0
				case "retry":
					want, lookups = 0, 2
					e.cloud.Inject(clouddouble.Fault{Path: "/api/file/3/files/query/by/path_v3", Count: 1, Code: "500"})
				case "stored":
					want = 0
				case "replace":
					e.cloud.Seed(target, false, []byte("old"))
					want = 0
				case "identical":
					e.cloud.Seed(target, false, []byte("new"))
					lookups = 0
				case "size", "hash":
					data := "other size"
					if scenario == "hash" {
						data = "bad"
					}
					e.cloud.Inject(clouddouble.Fault{Path: "/api/file/3/files/query/by/path_v3", Count: 1,
						Before: func() { e.cloud.Seed(target, false, []byte(data)) }})
				case "missing":
					fault.FinishStoredRejected = false
					fault.Status = 500
				case "refused":
					fault.FinishStoredRejected = false
					fault.Code = "E0335"
					want = 4
					lookups = 0
				}
				e.cloud.Inject(fault)
				args := []string{"put", e.path("src/x.pdf"), target, "--yes", "--json", "-v"}
				if bulk {
					args = []string{"mput", e.path("src"), "NOTE/Note", "--yes", "--json", "-v"}
				}
				if scenario == "replace" || scenario == "identical" {
					args = append(args, "--replace")
				}
				out, stderr, code := e.run(args...)
				if code != want {
					t.Fatal(code, want, out, stderr)
				}
				if n := e.cloud.Count("/api/file/3/files/query/by/path_v3"); n != lookups {
					t.Fatal("verification requests", n, lookups)
				}
				if !strings.Contains(stderr, "cloud failure") || !strings.Contains(stderr, "operation=upload") || !strings.Contains(stderr, target) {
					t.Fatal("missing diagnostic", stderr)
				}
				if strings.Contains(stderr+out, clouddouble.Token) || strings.Contains(stderr+out, clouddouble.SignedURL) {
					t.Fatal("secret in output")
				}
				if want == 0 {
					if !strings.Contains(stderr, target+": server reported an error after storing it") {
						t.Fatal(stderr)
					}
					if bulk && report(t, out).Counts[port.StatusUploaded] != 1 {
						t.Fatal(out)
					}
				}
				assertVerificationAfterUnlock(t, e.cloud)
			})
		}
	}
}

func TestMoveDestinations(t *testing.T) {
	e := newE2E(t)
	e.cloud.Seed("NOTE/Note/a.pdf", false, []byte("a"))
	e.cloud.Seed("NOTE/Note/folder", true, nil)
	e.must("mv", "NOTE/Note/a.pdf", "NOTE/Note/folder", "--yes")
	e.must("mv", "NOTE/Note/folder/a.pdf", "NOTE/Note/b.pdf", "--yes")
	e.must("rename", "NOTE/Note/b.pdf", "c.pdf", "--yes")
	e.cloud.Seed("NOTE/Note/existing.pdf", false, []byte("exists"))
	for _, args := range [][]string{
		{"mv", "NOTE/Note/c.pdf", "NOTE/Note/existing.pdf", "--yes"},
		{"mv", "NOTE/Note/folder", "NOTE/Note/folder", "--yes"},
		{"mv", "NOTE/Note/folder", "NOTE/Note/folder/child", "--yes"},
		{"rename", "NOTE/Note/c.pdf", "folder", "--yes"},
	} {
		if _, stderr, code := e.run(args...); code != 4 {
			t.Fatal(args, code, stderr)
		}
	}
	if _, stderr, code := e.run("mv", "NOTE/Note/c.pdf", "NOTE/Note/missing/a.pdf", "--yes"); code == 0 {
		t.Fatal(stderr)
	}
	if data, ok := e.cloud.File("NOTE/Note/c.pdf"); !ok || string(data) != "a" {
		t.Fatal(e.cloud.Paths())
	}
}

func TestMoveGoldens(t *testing.T) {
	for _, format := range []string{"table", "plain", "json"} {
		t.Run(format, func(t *testing.T) {
			e := newE2E(t)
			e.cloud.Seed("NOTE/Note/a.pdf", false, []byte("a"))
			e.cloud.Seed("NOTE/Note/folder", true, nil)
			e.golden("mv-folder-"+format, e.must("mv", "NOTE/Note/a.pdf", "NOTE/Note/folder", "--yes", "--format", format))
			e.golden("mv-path-"+format, e.must("mv", "NOTE/Note/folder/a.pdf", "NOTE/Note/b.pdf", "--yes", "--format", format))
		})
	}
	e := newE2E(t)
	e.golden("mv-help", e.must("mv", "--help"))
}

func TestReplacementRecovery(t *testing.T) {
	for _, stage := range []string{"hash", "download", "delete", "move"} {
		t.Run(stage, func(t *testing.T) {
			e := newE2E(t)
			e.cloud.SetUploadMode(clouddouble.Autorename)
			target := "NOTE/Note/x.pdf"
			old := e.cloud.Seed(target, false, []byte("old"))
			local := e.path("x.pdf")
			if err := os.WriteFile(local, []byte("new"), 0600); err != nil {
				t.Fatal(err)
			}
			fault := clouddouble.Fault{Count: 1, Code: "E0335"}
			switch stage {
			case "hash":
				fault.Code = ""
				fault.Path = "/api/file/2/files/upload/finish"
				fault.Before = func() {
					if err := os.Remove(local); err != nil {
						t.Error(err)
					}
				}
			case "download":
				fault.Path = "/api/file/3/files/download_v3"
			case "delete":
				fault.Path = "/api/file/delete"
			case "move":
				fault.Path = "/api/file/3/files/move_v3"
			}
			e.cloud.Inject(fault)
			_, stderr, code := e.run("put", local, target, "--replace", "--yes")
			if code == 0 || !strings.Contains(stderr, "uploaded copy retained at NOTE/Note/x(1).pdf") {
				t.Fatal(code, stderr)
			}
			if strings.Contains(stderr, "cloud failure") {
				t.Fatal("diagnostics without verbose", stderr)
			}
			data, ok := e.cloud.File(target)
			if stage == "move" {
				if ok || !strings.Contains(e.must("trash", "--json"), `"id": "`+old+`"`) {
					t.Fatal("old version not recycled")
				}
				if !strings.Contains(stderr, "old version is in the recycle bin") {
					t.Fatal(stderr)
				}
			} else if !ok || string(data) != "old" {
				t.Fatal("old version changed")
			}
			if data, ok := e.cloud.File("NOTE/Note/x(1).pdf"); !ok || string(data) != "new" {
				t.Fatal("new copy lost")
			}
		})
	}
}

func TestParallelUploadVerification(t *testing.T) {
	e := newE2E(t)
	e.cloud.SetVisibilityMode(clouddouble.HiddenUntilUnlock)
	if err := os.Mkdir(e.path("src"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.pdf", "b.pdf", "c.pdf", "d.pdf", "e.pdf", "f.pdf", "g.pdf", "h.pdf", "i.pdf", "j.pdf", "k.pdf", "l.pdf"} {
		if err := os.WriteFile(e.path("src/"+name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/upload/finish", Count: 6, FinishStoredRejected: true})
	stdout, stderr, code := e.run("mput", e.path("src"), "NOTE/Note", "--yes", "--json", "-v")
	if code != 0 || report(t, stdout).Counts[port.StatusUploaded] != 12 {
		t.Fatal(code, stdout, stderr)
	}
	if strings.Count(stderr, "server reported an error after storing it") != 6 {
		t.Fatal(stderr)
	}
	if e.cloud.Count("/api/file/3/files/query/by/path_v3") != 6 {
		t.Fatal("verification was not bounded per file")
	}
	for _, name := range []string{"a.pdf", "b.pdf", "c.pdf", "d.pdf", "e.pdf", "f.pdf", "g.pdf", "h.pdf", "i.pdf", "j.pdf", "k.pdf", "l.pdf"} {
		if data, ok := e.cloud.File("NOTE/Note/" + name); !ok || string(data) != name {
			t.Fatal("stored bytes differ", name)
		}
	}
	if strings.Count(stderr, "errorCode=500") != 6 || strings.Count(stderr, `errorMsg="The system is temporarily unable`) != 6 {
		t.Fatal(stderr)
	}
	assertUploadListings(t, e.cloud, 2, 1)
	t.Logf("mput exit=%d; uploaded=12; verified warnings=6; list_folder=3\n%s", code, stderr)
	assertVerificationAfterUnlock(t, e.cloud)
}

func assertVerificationAfterUnlock(t *testing.T, c *clouddouble.Cloud) {
	t.Helper()
	released := false
	for _, r := range c.Requests() {
		if r.Path == "/api/file/2/files/synchronous/end" {
			released = true
		}
		if r.Path == "/api/file/3/files/query/by/path_v3" && !released {
			t.Fatal("verification preceded lock release")
		}
	}
}

// assertUploadListings bounds each phase and refuses listings interleaved with
// file uploads: re-checks precede apply; verification follows lock release.
func assertUploadListings(t *testing.T, c *clouddouble.Cloud, wantBefore, wantAfter int) {
	t.Helper()
	applied, released := false, false
	before, after := 0, 0
	for _, r := range c.Requests() {
		switch r.Path {
		case "/api/file/3/files/upload/apply":
			applied = true
		case "/api/file/2/files/synchronous/end":
			released = true
		case "/api/file/2/files/list_folder":
			if applied && !released {
				t.Fatal("listing interleaved with file uploads", c.Requests())
			}
			if released {
				after++
			} else {
				before++
			}
		}
	}
	if before != wantBefore || after != wantAfter {
		t.Fatalf("list_folder before/after=%d/%d, want %d/%d", before, after, wantBefore, wantAfter)
	}
}

func TestListAfterUploadFailure(t *testing.T) {
	e := newE2E(t)
	if err := os.Mkdir(e.path("src"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.pdf", "b.pdf", "c.pdf"} {
		if err := os.WriteFile(e.path("src/"+name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/list_folder", Method: "POST", Count: 2})
	e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/list_folder", Code: "500", UntilCleared: true})
	stdout, stderr, code := e.run("mput", e.path("src"), "NOTE/Note/L", "--yes", "--json", "-j", "1", "-v")
	if code != 6 || report(t, stdout).Counts[port.StatusUploaded] != 3 {
		t.Fatal(code, stdout, stderr)
	}
	if !strings.Contains(stderr, "write happened") || !strings.Contains(stderr, "cannot verify destinations") || !strings.Contains(stderr, "no rollback") {
		t.Fatal(stderr)
	}
	assertUploadListings(t, e.cloud, 2, 1)
	for _, name := range []string{"a.pdf", "b.pdf", "c.pdf"} {
		if data, ok := e.cloud.File("NOTE/Note/L/" + name); !ok || string(data) != name {
			t.Fatal("stored bytes differ", name)
		}
	}
	t.Logf("mput exit=%d; uploaded=3; verification failed; list_folder=3\n%s", code, stderr)
}

func TestUploadDuplicateCheckCommonParent(t *testing.T) {
	for _, tc := range []struct {
		name  string
		paths []string
	}{
		{"nested", []string{"NOTE/Note/A/a.pdf", "NOTE/Note/B/b.pdf"}},
		{"root", []string{"NOTE/Note/a.pdf", "DOCUMENT/Document/b.pdf"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newE2E(t)
			e.cloud.SetUploadMode(clouddouble.Duplicate)
			for _, name := range tc.paths {
				local := e.path("src/" + name)
				if err := os.MkdirAll(filepath.Dir(local), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(local, []byte("new"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			e.cloud.Inject(clouddouble.Fault{Path: "/api/file/3/files/upload/apply", Count: 1, Before: func() {
				for _, name := range tc.paths {
					e.cloud.Seed(name, false, []byte("concurrent"))
				}
			}})
			stdout, stderr, code := e.run("mput", e.path("src"), "--yes", "--json", "-j", "1")
			if code != 6 || report(t, stdout).Counts[port.StatusDuplicate] != 2 || strings.Count(stderr, "now exists") != 2 {
				t.Fatal(code, stdout, stderr)
			}
			assertUploadListings(t, e.cloud, 2, 1)
			for _, r := range e.cloud.Requests() {
				if r.Path == "/api/file/2/files/list_folder" && r.Target != "" {
					t.Fatalf("snapshot target %q, want root", r.Target)
				}
			}
		})
	}
}

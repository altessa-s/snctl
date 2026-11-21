// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/config"
	"github.com/altessa-s/snctl/internal/clouddouble"
)

func TestUnicodeWarmTTLMutations(t *testing.T) {
	for _, op := range []string{"rm", "mv"} {
		t.Run(op, func(t *testing.T) {
			e := newE2E(t)
			source := "DOCUMENT/Document/warm.pdf"
			e.cloud.Seed(source, false, []byte("warm"))
			e.s.env[config.EnvCacheTTL] = "1h"
			e.must("ls") // Persist timestamps through the real JSON cache.
			before := len(e.cloud.Requests())
			args := []string{op, source}
			if op == "mv" {
				args = append(args, "DOCUMENT/Document/moved.pdf")
			}
			e.must(append(args, "--yes")...)
			want := 3
			if op == "mv" {
				want++
			}
			if got := e.listings(before); got != want {
				t.Fatalf("warm %s listings = %d, want %d", op, got, want)
			}
			if _, ok := e.cloud.File(source); ok {
				t.Fatal("source remains", e.cloud.Paths())
			}
			if op == "mv" {
				if data, ok := e.cloud.File("DOCUMENT/Document/moved.pdf"); !ok || string(data) != "warm" {
					t.Fatal("move did not preserve bytes")
				}
			}
		})
	}
}

func TestUnicodeBatchListingBounds(t *testing.T) {
	for _, n := range []int{1, 100} {
		for _, op := range []string{"mput", "mirror"} {
			t.Run(fmt.Sprintf("%s/%d", op, n), func(t *testing.T) {
				e := newE2E(t)
				dir := e.path("src")
				e.cloud.Seed("DOCUMENT/Document/Papers", true, nil)
				for i := range n {
					file := filepath.Join(dir, fmt.Sprintf("folder%03d", i), "file.pdf")
					if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
						t.Fatal(err)
					}
					writeMirrorFile(t, file, "synthetic")
				}
				args := []string{"mput", dir, "DOCUMENT/Document/Papers", "--yes", "-j", "1", "--json"}
				if op == "mirror" {
					e.must("mirror", "add", "papers", dir, "/DOCUMENT/Document/Papers")
					args = []string{"mirror", "run", "papers", "--json"}
				}
				before := len(e.cloud.Requests())
				out := e.must(args...)
				if got := e.listings(before); got != 3 {
					t.Fatalf("%s %d folders listings = %d, want 3\n%s", op, n, got, out)
				}
				// The re-check list is accepted by the real client and emulator under
				// the lock. Verification comes after every write session has ended.
				locked, checks, verified, startedWriting := false, 0, 0, false
				for _, req := range e.cloud.Requests()[before:] {
					switch req.Path {
					case "/api/file/2/files/synchronous/start":
						locked = true
					case "/api/file/2/files/synchronous/end":
						locked = false
					case "/api/file/3/files/upload/apply":
						startedWriting = true
					case "/api/file/2/files/list_folder":
						if strings.Trim(req.Target, "/") != "" {
							t.Fatalf("create-only batch made a targeted destructive check: %s", req.Target)
						}
						if locked {
							checks++
						} else if startedWriting {
							verified++
						}
					}
				}
				if checks != 1 || verified != 1 {
					t.Fatalf("lists under lock/after writes = %d/%d", checks, verified)
				}
				for i := range n {
					if data, ok := e.cloud.File(fmt.Sprintf("DOCUMENT/Document/Papers/folder%03d/file.pdf", i)); !ok || string(data) != "synthetic" {
						t.Fatal("missing upload", i)
					}
				}
				t.Logf("%s %d new folders: 1 planning + 1 locked re-check + 1 verification = 3 listings", op, n)
			})
		}
	}
}

func TestMirrorVerificationWarningKeepsJournal(t *testing.T) {
	for _, mode := range []string{"twin", "listing failure", "move"} {
		t.Run(mode, func(t *testing.T) {
			e := newE2E(t)
			dir := e.path("mirror")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			e.cloud.Seed("DOCUMENT/Document/Papers", true, nil)
			e.must("mirror", "add", "papers", dir, "/DOCUMENT/Document/Papers")
			source := "й.pdf"
			fault := "/api/file/2/files/upload/finish"
			if mode == "move" {
				e.cloud.Seed("DOCUMENT/Document/Papers/old.pdf", false, []byte("initial"))
				e.must("mirror", "run", "papers")
				if err := os.Rename(filepath.Join(dir, "old.pdf"), filepath.Join(dir, source)); err != nil {
					t.Fatal(err)
				}
				writeMirrorFile(t, filepath.Join(dir, source), "initial")
				fault = "/api/file/3/files/move_v3"
			} else {
				writeMirrorFile(t, filepath.Join(dir, source), "initial")
			}
			writeMirrorFile(t, filepath.Join(dir, "z.pdf"), "continue")
			if mode == "listing failure" {
				e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/list_folder", Count: 2})
				e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/list_folder", Code: "500", UntilCleared: true})
			} else {
				e.cloud.Inject(clouddouble.Fault{Path: fault, Count: 1, Before: func() { e.cloud.Seed("DOCUMENT/Document/Papers/и\u0306.pdf", false, []byte("raced")) }})
			}
			out, diag, code := e.run("mirror", "run", "papers", "--json")
			wantCode := 0
			if mode == "listing failure" {
				wantCode = 6
			}
			if code != wantCode || !strings.Contains(out+diag, "write happened") || mode != "listing failure" && !strings.Contains(out, "warning") || strings.Contains(out, `"pending"`) {
				t.Fatal(code, out, diag)
			}
			if mode != "listing failure" && (!strings.Contains(out, "NFC-equivalent") || !strings.Contains(out, "ID ")) {
				t.Fatal(out)
			}
			items := mirrorJournal(t, filepath.Join(dir, ".snctl-mirror.json"))
			if len(items) != 2 {
				t.Fatal(items, out)
			}
			for _, en := range items {
				if en.Recover || en.Path == "old.pdf" {
					t.Fatal("stale journal", items)
				}
			}
			if data, ok := e.cloud.File("DOCUMENT/Document/Papers/z.pdf"); !ok || string(data) != "continue" {
				t.Fatal("pass did not continue")
			}
		})
	}
}

func TestMirrorChangedBeforeTrashReplans(t *testing.T) {
	e := newE2E(t)
	dir := e.path("mirror")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	e.cloud.Seed("DOCUMENT/Document/Papers/a.pdf", false, []byte("before"))
	e.must("mirror", "add", "papers", dir, "/DOCUMENT/Document/Papers")
	e.must("mirror", "run", "papers")
	if err := os.Remove(filepath.Join(dir, "a.pdf")); err != nil {
		t.Fatal(err)
	}
	writeMirrorFile(t, filepath.Join(dir, "keep.pdf"), "keep")
	// First allow the pass listing, then mutate at the fresh re-check.
	e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/list_folder", Count: 1})
	e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/list_folder", Count: 1, Before: func() { e.cloud.Seed("DOCUMENT/Document/Papers/a.pdf", false, []byte("tablet edit")) }})
	out, diag, code := e.run("mirror", "run", "papers", "--force-delete", "--json")
	if code != 4 || !strings.Contains(out, "hash") || !strings.Contains(out, "next pass will re-plan") {
		t.Fatal(code, out, diag)
	}
	if data, ok := e.cloud.File("DOCUMENT/Document/Papers/a.pdf"); !ok || string(data) != "tablet edit" {
		t.Fatal("changed cloud original trashed")
	}
	e.must("mirror", "run", "papers")
	if data, err := os.ReadFile(filepath.Join(dir, "a.pdf")); err != nil || string(data) != "tablet edit" {
		t.Fatal("next pass did not re-plan", string(data), err)
	}
}

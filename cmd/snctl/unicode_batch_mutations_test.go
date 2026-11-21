// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/clouddouble"
)

func TestMirrorChangedAfterFirstWriteReplans(t *testing.T) {
	for _, op := range []string{"trash", "move"} {
		t.Run(op, func(t *testing.T) {
			e := newE2E(t)
			dir := e.path("mirror")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			const folder = "DOCUMENT/Document/Papers/"
			paths := map[string]string{}
			for _, name := range []string{"a.pdf", "b.pdf", "keep.pdf"} {
				paths[e.cloud.Seed(folder+name, false, []byte("initial "+name))] = name
			}
			ids := make([]string, 0, len(paths))
			for id := range paths {
				ids = append(ids, id)
			}
			slices.Sort(ids)
			first, second := paths[ids[0]], paths[ids[1]]
			e.must("mirror", "add", "papers", dir, "/DOCUMENT/Document/Papers")
			e.must("mirror", "run", "papers")
			if err := os.Rename(filepath.Join(dir, first), filepath.Join(dir, "first.pdf")); err != nil {
				t.Fatal(err)
			}
			writeMirrorFile(t, filepath.Join(dir, "first.pdf"), "initial "+first)
			if op == "trash" {
				if err := os.Remove(filepath.Join(dir, second)); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Rename(filepath.Join(dir, second), filepath.Join(dir, "second.pdf")); err != nil {
				t.Fatal(err)
			}
			if op == "move" {
				writeMirrorFile(t, filepath.Join(dir, "second.pdf"), "initial "+second)
			}
			// Planning, the shared re-check, and the first move's targeted check pass.
			// The next targeted check runs after that first write and sees a tablet edit.
			e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/list_folder", Count: 3})
			e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/list_folder", Count: 1, Before: func() {
				if _, ok := e.cloud.File(folder + "first.pdf"); !ok {
					t.Error("edit preceded first write")
				}
				e.cloud.Seed(folder+second, false, []byte("tablet edit"))
			}})
			out, diag, code := e.run("mirror", "run", "papers", "--force-delete", "--json")
			if code != 4 || !strings.Contains(out, "next pass will re-plan") || !strings.Contains(out, "hash") {
				t.Fatal(code, out, diag)
			}
			if data, ok := e.cloud.File(folder + second); !ok || string(data) != "tablet edit" {
				t.Fatal("changed document mutated", e.cloud.Paths())
			}
			if _, ok := e.cloud.File(folder + "second.pdf"); ok {
				t.Fatal("changed document moved")
			}
			e.must("mirror", "run", "papers")
			restored := second
			if op == "move" {
				restored = "second.pdf"
			}
			if data, err := os.ReadFile(filepath.Join(dir, restored)); err != nil || string(data) != "tablet edit" {
				t.Fatal("next pass did not re-plan", string(data), err)
			}
		})
	}
}

func TestUnicodeDestructiveListingBounds(t *testing.T) {
	for _, n := range []int{1, 100} {
		for _, op := range []string{"trash", "move"} {
			t.Run(fmt.Sprintf("mirror/%s/%d", op, n), func(t *testing.T) {
				e := newE2E(t)
				dir := e.path("mirror")
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				const folder = "DOCUMENT/Document/Papers"
				for i := range n {
					e.cloud.Seed(fmt.Sprintf("%s/file%03d.pdf", folder, i), false, []byte(fmt.Sprintf("initial %d", i)))
				}
				e.must("mirror", "add", "papers", dir, "/"+folder)
				e.must("mirror", "run", "papers")
				for i := range n {
					source := filepath.Join(dir, fmt.Sprintf("file%03d.pdf", i))
					var err error
					if op == "trash" {
						err = os.Remove(source)
					} else {
						err = os.Rename(source, filepath.Join(dir, fmt.Sprintf("moved%03d.pdf", i)))
					}
					if err != nil {
						t.Fatal(err)
					}
					if op == "move" {
						writeMirrorFile(t, filepath.Join(dir, fmt.Sprintf("moved%03d.pdf", i)), fmt.Sprintf("initial %d", i))
					}
				}
				before := len(e.cloud.Requests())
				out := e.must("mirror", "run", "papers", "--force-delete", "--json")
				want := n + 2 // Planning + shared re-check + one targeted list per operation.
				if op == "move" {
					want++
				} // One shared post-write verification.
				if got := e.listings(before); got != want {
					t.Fatalf("listings = %d, want %d: %s", got, want, out)
				}
				targeted := 0
				for _, req := range e.cloud.Requests()[before:] {
					if req.Path == "/api/file/2/files/list_folder" && req.Target == folder {
						targeted++
					}
				}
				if targeted != n {
					t.Fatalf("targeted parent listings = %d, want %d", targeted, n)
				}
			})
		}
	}
	for _, op := range []string{"rm", "mv"} {
		t.Run("cli/"+op, func(t *testing.T) {
			e := newE2E(t)
			const source = "DOCUMENT/Document/paper.pdf"
			e.cloud.Seed(source, false, []byte("doc"))
			e.cloud.Seed(source+".mark", false, []byte("mark"))
			before := len(e.cloud.Requests())
			args := []string{op, source}
			want := 4 // Planning + shared re-check + two targeted checks.
			if op == "mv" {
				args = append(args, "DOCUMENT/Document/new.pdf")
				want++
			}
			e.must(append(args, "--yes")...)
			if got := e.listings(before); got != want {
				t.Fatalf("listings = %d, want %d", got, want)
			}
		})
	}
}

func TestUnicodeBatchRechecksLaterMember(t *testing.T) {
	for _, op := range []string{"rm", "mv"} {
		t.Run(op, func(t *testing.T) {
			e := newE2E(t)
			const source = "DOCUMENT/Document/paper.pdf"
			e.cloud.Seed(source, false, []byte("doc"))
			e.cloud.Seed(source+".mark", false, []byte("mark"))
			// Mutate the sidecar only after the shared check and the document check.
			e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/list_folder", Count: 3})
			e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/list_folder", Count: 1, Before: func() { e.cloud.Seed(source+".mark", false, []byte("tablet mark")) }})
			args := []string{op, source}
			if op == "mv" {
				args = append(args, "DOCUMENT/Document/new.pdf")
			}
			_, diag, code := e.run(append(args, "--yes")...)
			if code != 4 || !strings.Contains(diag, "changed") {
				t.Fatal(code, diag)
			}
			if data, ok := e.cloud.File(source + ".mark"); !ok || string(data) != "tablet mark" {
				t.Fatal("changed sidecar mutated")
			}
		})
	}
}

func TestReplacementSamePathChecksBeforeRecycle(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprintf("mirror=%t", batch), func(t *testing.T) {
			e := newE2E(t)
			const folder = "DOCUMENT/Document/Papers"
			const target = folder + "/й.pdf"
			old := e.cloud.Seed(target, false, []byte("old"))
			e.cloud.SetUploadMode(clouddouble.Duplicate)
			dir := e.path("mirror")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			args := []string{"put", filepath.Join(dir, "й.pdf"), target, "--replace", "--yes"}
			if batch {
				e.must("mirror", "add", "papers", dir, "/"+folder)
				e.must("mirror", "run", "papers")
				args = []string{"mirror", "run", "papers", "--json"}
			}
			writeMirrorFile(t, filepath.Join(dir, "й.pdf"), "new")
			// Verification has passed; a twin arrives while validating the copy's bytes.
			twinArrived := false
			e.cloud.Inject(clouddouble.Fault{Path: "/api/file/3/files/download_v3", Count: 1, Before: func() {
				twinArrived = true
				e.cloud.Seed(folder+"/и\u0306.pdf", false, []byte("tablet"))
			}})
			before := len(e.cloud.Requests())
			out, diag, code := e.run(args...)
			if code == 0 || !twinArrived {
				t.Fatal(code, out, diag)
			}
			targeted := 0
			for _, req := range e.cloud.Requests()[before:] {
				if req.Path == "/api/file/2/files/list_folder" && req.Target == folder {
					targeted++
				}
				if req.Path == "/api/file/delete" || req.Path == "/api/file/3/files/move_v3" {
					t.Fatal("recycled or moved after twin", req)
				}
			}
			if targeted != 1 {
				t.Fatalf("pre-recycle parent listings = %d, want 1", targeted)
			}
			if strings.Contains(e.must("trash", "--json"), `"id": "`+old+`"`) {
				t.Fatal("old copy recycled")
			}
		})
	}
}

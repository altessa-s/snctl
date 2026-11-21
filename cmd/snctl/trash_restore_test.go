// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package main

import (
	"encoding/json/v2"
	"fmt"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/clouddouble"
	"github.com/altessa-s/snctl/internal/port"
)

func TestTrashRestoreUnicodeAndBounds(t *testing.T) {
	for _, n := range []int{1, 100} {
		for _, mode := range []string{"success", "occupied", "racing twin", "verification failure", "missing document", "missing mark", "empty"} {
			t.Run(fmt.Sprintf("%s/%d", mode, n), func(t *testing.T) {
				e := newE2E(t)
				const doc = "DOCUMENT/Document/й.pdf"
				const mark = "DOCUMENT/Document/и\u0306.pdf.mark"
				id := e.cloud.Seed(doc, false, []byte("document"))
				markID := e.cloud.Seed(mark, false, []byte("mark"))
				e.must("rm", doc, "--yes")
				for i := range n {
					e.cloud.Seed(fmt.Sprintf("DOCUMENT/Document/unrelated%03d/file.pdf", i), false, []byte("unrelated"))
				}
				beforeWrites := len(e.cloud.Writes())
				if mode == "occupied" {
					e.cloud.Seed("DOCUMENT/Document/и\u0306.pdf", false, []byte("existing"))
				}
				if mode == "racing twin" {
					e.cloud.Inject(clouddouble.Fault{Path: "/api/file/recycle/revert", Count: 1, Before: func() {
						e.cloud.Seed("DOCUMENT/Document/и\u0306.pdf", false, []byte("racer"))
					}})
				}
				if mode == "verification failure" {
					// Planning, global recheck, then two targeted parent lists.
					e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/list_folder", Count: 4})
					e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/list_folder", Count: 1, Status: 400})
				}
				if strings.HasPrefix(mode, "missing ") {
					// Remove a completed restore just before the shared verification.
					e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/list_folder", Count: 4})
					e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/list_folder", Count: 1, Before: func() {
						missing := doc
						if mode == "missing mark" {
							missing = mark
						}
						e.cloud.Remove(missing)
					}})
				}
				before := len(e.cloud.Requests())
				args := []string{"trash", "restore", id, "--yes"}
				if mode == "empty" {
					args = []string{"trash", "empty", "--yes"}
				}
				out, diag, code := e.run(args...)
				wantCode, wantLists, wantWrites := 0, 5, 2
				switch mode {
				case "occupied":
					wantCode, wantLists, wantWrites = 4, 1, 0
				case "racing twin", "verification failure", "missing document", "missing mark":
					wantCode = 6
					if !strings.Contains(diag, "write happened") || !strings.Contains(diag, "no rollback") {
						t.Fatal(code, out, diag)
					}
				case "empty":
					wantLists, wantWrites = 0, 1
				}
				if code != wantCode || e.listings(before) != wantLists || len(e.cloud.Writes())-beforeWrites != wantWrites {
					t.Fatalf("code %d; listings %d; writes %d; %s %s", code, e.listings(before), len(e.cloud.Writes())-beforeWrites, out, diag)
				}
				if strings.HasPrefix(mode, "missing ") {
					missing, missingID := doc, id
					if mode == "missing mark" {
						missing, missingID = mark, markID
					}
					if !strings.Contains(diag, fmt.Sprintf("expected ID %s at %q", missingID, missing)) {
						t.Fatal("missing planned destination diagnostic", diag)
					}
				}
				if mode == "success" {
					for name, expectedID := range map[string]string{doc: id, mark: markID} {
						stat := e.must("stat", name, "--json")
						var entry port.Entry
						if err := json.Unmarshal([]byte(stat), &entry); err != nil || entry.ID != expectedID || entry.Path != name {
							t.Fatalf("restored %s lost ID %s: %s", name, expectedID, stat)
						}
					}
					locations, bins, parents := 0, 0, 0
					for _, req := range e.cloud.Requests()[before:] {
						switch req.Path {
						case "/api/file/path/query":
							locations++
						case "/api/file/recycle/list/query":
							bins++
						case "/api/file/2/files/list_folder":
							if req.Target == "DOCUMENT/Document" {
								parents++
							}
						}
					}
					if locations != 6 || bins != 2 || parents != 2 {
						t.Fatalf("location/bin/targeted reads = %d/%d/%d, want 6/2/2", locations, bins, parents)
					}
				}
				if mode == "success" || wantCode == 6 && !strings.HasPrefix(mode, "missing ") {
					if data, ok := e.cloud.File(doc); !ok || string(data) != "document" {
						t.Fatal("restored document missing", e.cloud.Paths())
					}
					if data, ok := e.cloud.File(mark); !ok || string(data) != "mark" {
						t.Fatal("restored mark missing", e.cloud.Paths())
					}
				}
			})
		}
	}
}

func TestTrashRestoreTwoVersionsAndLiveSidecar(t *testing.T) {
	e := newE2E(t)
	const doc = "DOCUMENT/Document/Report.pdf"
	var docID, markID string
	for _, version := range []string{"first", "second"} {
		docID = e.cloud.Seed(doc, false, []byte(version))
		markID = e.cloud.Seed(doc+".mark", false, []byte(version+" mark"))
		e.must("rm", doc, "--yes")
	}
	before := len(e.cloud.Writes())
	_, diag, code := e.run("restore", docID)
	if code != 4 || !strings.Contains(diag, "ambiguous recycled") || len(e.cloud.Writes()) != before {
		t.Fatal("two versions did not conflict before confirmation", code, diag)
	}
	e.cloud.Seed(doc+".mark", false, []byte("live mark"))
	_, diag, code = e.run("restore", markID)
	if code != 4 || !strings.Contains(diag, "would collide") || len(e.cloud.Writes()) != before {
		t.Fatal("direct mark restore bypassed live sidecar", code, diag)
	}
	if data, ok := e.cloud.File(doc + ".mark"); !ok || string(data) != "live mark" {
		t.Fatal("live sidecar changed")
	}
}

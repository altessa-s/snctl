// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/clouddouble"
)

func TestUnicodeTwinVerificationStderr(t *testing.T) {
	for _, operation := range []string{"put", "mkdir", "mv", "rename"} {
		t.Run(operation, func(t *testing.T) {
			e := newE2E(t)
			source, target, twin := "NOTE/Note/source.txt", "NOTE/Note/й.txt", "NOTE/Note/и\u0306.txt"
			e.cloud.Seed(source, false, []byte("source"))
			args := []string{operation, source, target, "--yes"}
			faultPath := "/api/file/3/files/move_v3"
			switch operation {
			case "put":
				if err := os.WriteFile(e.path("local.txt"), []byte("new"), 0600); err != nil {
					t.Fatal(err)
				}
				args = []string{"put", e.path("local.txt"), target, "--yes"}
				faultPath = "/api/file/2/files/upload/finish"
			case "mkdir":
				args = []string{"mkdir", target, "--yes"}
				faultPath = "/api/file/2/files/create_folder_v2"
			case "rename":
				args = []string{"rename", source, "й.txt", "--yes"}
			}
			var twinID string
			e.cloud.Inject(clouddouble.Fault{Path: faultPath, Method: "POST", Count: 1, Before: func() { twinID = e.cloud.Seed(twin, false, []byte("other client")) }})
			_, stderr, code := e.run(args...)
			if code != 6 || !strings.Contains(stderr, fmt.Sprintf("%q (ID %s)", twin, twinID)) || !strings.Contains(stderr, fmt.Sprintf("%q (ID ", target)) || !strings.Contains(stderr, "no rollback") {
				t.Fatal(code, stderr, e.cloud.Requests())
			}
			paths := strings.Join(e.cloud.Paths(), "\n")
			if !strings.Contains(paths, target) || !strings.Contains(paths, twin) {
				t.Fatal("write rolled back", paths)
			}
			for _, request := range e.cloud.Writes() {
				if strings.Contains(request.Path, "delete") {
					t.Fatal("rolled back", request)
				}
			}
		})
	}
}

func TestUnicodeExactCommandFlags(t *testing.T) {
	for _, op := range []string{"rm", "mv", "rename"} {
		t.Run(op, func(t *testing.T) {
			e := newE2E(t)
			source, twin := "NOTE/Note/й.txt", "NOTE/Note/и\u0306.txt"
			sourceID := e.cloud.Seed(source, false, []byte("one"))
			twinID := e.cloud.Seed(twin, false, []byte("two"))
			args := []string{op, source}
			if op == "mv" {
				args = append(args, "NOTE/Note/new.txt")
			}
			if op == "rename" {
				args = append(args, "new.txt")
			}
			args = append(args, "--yes")
			before := len(e.cloud.Writes())
			_, stderr, code := e.run(args...)
			if code != 4 || len(e.cloud.Writes()) != before || !strings.Contains(stderr, fmt.Sprintf("%q (ID %s)", source, sourceID)) || !strings.Contains(stderr, fmt.Sprintf("%q (ID %s)", twin, twinID)) || !strings.Contains(stderr, "--exact") {
				t.Fatal(code, stderr, e.cloud.Writes())
			}
			e.must(append(args, "--exact")...)
			if data, ok := e.cloud.File(twin); !ok || string(data) != "two" {
				t.Fatal("changed equivalent twin", e.cloud.Paths())
			}
		})
	}
}

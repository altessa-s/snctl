//go:build integration && (norender || !cgo)

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package main

import (
	"os"
	"strings"
	"testing"
)

func TestOverlayRemainsPendingWithoutRenderer(t *testing.T) {
	e := newE2E(t)
	e.cloud.Seed("DOCUMENT/Document/Papers/Report.pdf", false, []byte("original"))
	e.cloud.Seed("DOCUMENT/Document/Papers/Report.pdf.mark", false, []byte("mark"))
	dir := e.path("mirror")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	e.must("mirror", "add", "papers", dir, "/DOCUMENT/Document/Papers", "--pull-new")
	for range 2 {
		out, _, code := e.run("mirror", "run", "papers", "--json")
		if code == 0 || !strings.Contains(out, "this build cannot render") || !strings.Contains(out, `"pending": 1`) && !strings.Contains(out, `"pending":1`) {
			t.Fatal(code, out)
		}
	}
	if b, err := os.ReadFile(e.path("mirror/Report.pdf")); err != nil || string(b) != "original" {
		t.Fatal(string(b), err)
	}
}

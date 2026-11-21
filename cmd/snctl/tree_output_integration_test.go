//go:build integration && cgo && !norender

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderTreeConfinement(t *testing.T) {
	e := newE2E(t)
	data, err := os.ReadFile("../../testdata/notebooks/blank.note")
	if err != nil {
		t.Fatal(err)
	}
	e.cloud.Seed("NOTE/Note/Render/sub/a.note", false, data)
	out := e.path("rendered")
	e.must("render", "/NOTE/Note/Render", "-o", out)
	if err = os.Rename(filepath.Join(out, "sub"), e.path("archive")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(e.path("archive"), filepath.Join(out, "sub")); err != nil {
		t.Fatal(err)
	}
	e.cloud.Seed("NOTE/Note/Render/sub/new.note", false, data)
	stdout, stderr, code := e.run("render", "/NOTE/Note/Render", "-o", out, "--incremental")
	if code == 0 || !strings.Contains(stdout+stderr, "is a link") {
		t.Fatal(code, stdout, stderr)
	}
	if _, err = os.Stat(e.path("archive/new-rendered.pdf")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err = os.Stat(e.path("archive/new.pdf")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	record := filepath.Join(out, ".snctl-render.json")
	if err = os.Rename(record, e.path("record")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(e.path("record"), record); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code = e.run("render", "/NOTE/Note/Render", "-o", out); code == 0 || !strings.Contains(stderr, "is a link") {
		t.Fatal(code, stderr)
	}
}

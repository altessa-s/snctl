// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package main

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarksCacheAndCarry(t *testing.T) {
	e := newE2E(t)
	folder := "DOCUMENT/Document/"
	for _, name := range []string{"Report.pdf", "Report.pdf.mark", "orphan.pdf.mark"} {
		e.cloud.Seed(folder+name, false, []byte("synthetic "+name))
	}
	before := len(e.cloud.Requests())
	out := e.must("ls", folder, "--json")
	var entries []struct {
		Name  string   `json:"name"`
		Flags []string `json:"flags"`
		Mark  *struct {
			Path string `json:"path"`
		} `json:"mark"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil || len(entries) != 2 {
		t.Fatal(out, err)
	}
	for _, en := range entries {
		if en.Name == "Report.pdf" && (en.Mark == nil || en.Mark.Path != folder+"Report.pdf.mark" || len(en.Flags) != 1 || en.Flags[0] != "highlights") {
			t.Fatal(en)
		}
	}
	for _, request := range e.cloud.Requests()[before:] {
		if strings.Contains(request.Path, "download") {
			t.Fatal(request)
		}
	}
	before = len(e.cloud.Requests())
	for _, args := range [][]string{{"ls", folder}, {"tree", folder}, {"find", "Report", folder}, {"stat", folder + "Report.pdf"}, {"dupes", folder}} {
		e.must(append([]string{"--offline"}, append(args, "--json")...)...)
	}
	if len(e.cloud.Requests()) != before {
		t.Fatal("offline listing contacted fixture server")
	}
	if raw := e.must("ls", folder, "--marks", "--format", "plain"); !strings.Contains(raw, "Report.pdf.mark") {
		t.Fatal(raw)
	}
	dir := e.path("download")
	e.must("mget", folder, "-o", dir)
	for _, name := range []string{"Report.pdf", "Report.pdf.mark", "orphan.pdf.mark"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	e.must("rename", folder+"Report.pdf", "Renamed.pdf", "--yes")
	e.must("stat", folder+"Renamed.pdf.mark")
	e.must("rm", folder+"Renamed.pdf", "--yes")
	if raw := e.must("ls", folder, "--marks", "--format", "plain"); strings.Contains(raw, "Renamed.pdf") {
		t.Fatal(raw)
	}
}

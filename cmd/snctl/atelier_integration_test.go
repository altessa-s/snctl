//go:build integration && cgo && !norender && (darwin || linux || windows)

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package main

import (
	"bytes"
	"encoding/json/v2"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtelierAndHighlightCache(t *testing.T) {
	e := newE2E(t)
	read := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile("../../testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	e.cloud.Seed("EXPORT/sketch.spd", false, read("notebooks/drawing.spd"))
	e.cloud.Seed("DOCUMENT/Document/Report.pdf.mark", false, read("overlay/Report.pdf.mark"))
	e.cloud.Seed("DOCUMENT/Document/Report.pdf", false, read("overlay/Report.pdf"))
	dest := e.path("drawing.png")
	e.must("render", "/EXPORT/sketch.spd", "--drawing-page", "canvas", "--scale", ".5", "-o", dest)
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != 128 || cfg.Height != 128 {
		t.Fatal(cfg, err)
	}
	if out := e.must("note", "/EXPORT/sketch.spd", "--json"); !strings.Contains(out, `"tiles": 6`) {
		t.Fatal(out)
	}
	out := e.must("highlights", "/DOCUMENT/Document/Report.pdf.mark", "--json")
	if viaDocument := e.must("highlights", "/DOCUMENT/Document/Report.pdf", "--json"); viaDocument != out {
		t.Fatal("document highlights differ", viaDocument)
	}
	var result struct {
		Highlights []struct {
			Text *string `json:"text"`
		} `json:"highlights"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Highlights) != 3 || result.Highlights[0].Text == nil {
		t.Fatal(out)
	}
	// Ordinary downloads do not retain arbitrary bodies; sync explicitly prepares offline input.
	e.must("sync", "/", "--files")
	requests := len(e.cloud.Requests())
	if offline := e.must("--offline", "highlights", "/DOCUMENT/Document/Report.pdf.mark", "--json"); offline != out {
		t.Fatal("offline highlights changed", offline)
	}
	if viaDocument := e.must("--offline", "highlights", "/DOCUMENT/Document/Report.pdf", "--json"); viaDocument != out {
		t.Fatal(viaDocument)
	}
	e.must("--offline", "render", "/EXPORT/sketch.spd", "-o", e.path("offline.pdf"))
	if len(e.cloud.Requests()) != requests {
		t.Fatal("offline operation contacted the fixture server")
	}
	// An unavailable retained PDF is a warning, while mark geometry remains readable.
	e.must("debug", "cache", "clear", "--yes")
	e.must("sync", "/DOCUMENT/Document/Report.pdf.mark", "--files")
	_, warning, code := e.run("--offline", "highlights", "/DOCUMENT/Document/Report.pdf.mark", "--json")
	if code != 0 || !strings.Contains(warning, "highlight text unavailable") {
		t.Fatal(code, warning)
	}
	// Explicit local pairing never requests the fixture server.
	local := t.TempDir()
	for _, name := range []string{"Report.pdf", "Report.pdf.mark"} {
		if err := os.WriteFile(filepath.Join(local, name), read("overlay/"+name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	requests = len(e.cloud.Requests())
	localOut := e.must("highlights", filepath.Join(local, "Report.pdf.mark"), "--json")
	if viaDocument := e.must("highlights", filepath.Join(local, "Report.pdf"), "--json"); viaDocument != localOut {
		t.Fatal(viaDocument)
	}
	if len(e.cloud.Requests()) != requests {
		t.Fatal("local highlights contacted fixture server")
	}
}

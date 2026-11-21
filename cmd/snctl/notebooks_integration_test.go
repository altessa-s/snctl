//go:build integration && cgo && !norender && (darwin || linux || windows)

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNotebookBinary(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "snctl")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	fixture, err := filepath.Abs("../../testdata/notebooks/blank.note")
	if err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=" + os.Getenv("PATH"), "SystemRoot=" + os.Getenv("SystemRoot"), "HOME=" + root,
		"SNCTL_CONFIG_DIR=" + filepath.Join(root, "config"), "SNCTL_CACHE_DIR=" + filepath.Join(root, "cache"), "SNCTL_SECRETS=file"}
	for _, tc := range []struct {
		format string
		magic  []byte
		page   bool
	}{{"pdf", []byte("%PDF-"), false}, {"png", []byte("\x89PNG\r\n\x1a\n"), true}} {
		dest := filepath.Join(root, "out."+tc.format)
		args := []string{"render", fixture, "-o", dest}
		if tc.page {
			args = append(args, "--page", "1", "--scale", "0.1")
		}
		cmd := exec.CommandContext(t.Context(), binary, args...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		data, err := os.ReadFile(dest)
		if err != nil || !bytes.HasPrefix(data, tc.magic) {
			t.Fatalf("%s: %v", dest, err)
		}
		streamArgs := []string{"render", fixture, "-o", "-"}
		if tc.page {
			streamArgs = append(streamArgs, "--page", "1", "--scale", "0.1")
		}
		stream := exec.CommandContext(t.Context(), binary, streamArgs...)
		stream.Env = env
		raw, err := stream.Output()
		if err != nil || !bytes.HasPrefix(raw, tc.magic) {
			t.Fatalf("stdout: %v %q", err, raw)
		}
		if tc.page && !bytes.Equal(data, raw) {
			t.Fatal("stdout PNG differs from file output")
		}
		if !tc.page && !bytes.HasSuffix(bytes.TrimSpace(raw), []byte("%%EOF")) {
			t.Fatal("PDF stdout contains extra output")
		}
	}
	sparse, err := filepath.Abs("../../testdata/notebooks/sparse.mark")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), binary, "render", sparse, "--page", "22", "--scale", ".1", "-o", filepath.Join(root, "p22.png"))
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sparse mark: %v %s", err, out)
	}
	cmd = exec.CommandContext(t.Context(), binary, "render", sparse, "--page", "1")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 2 || !strings.Contains(string(out), "available pages: 22") {
		t.Fatalf("missing stored page: %v %s", err, out)
	}
}

func TestMirrorNotebookDrawingBinary(t *testing.T) {
	e := newE2E(t)
	data, err := os.ReadFile("../../testdata/notebooks/blank.note")
	if err != nil {
		t.Fatal(err)
	}
	e.cloud.Seed("DOCUMENT/Document/Papers/sub/Journal.note", false, data)
	e.cloud.Seed("DOCUMENT/Document/Papers/Report.mark", false, []byte("sidecar stays remote"))
	dir := e.path("mirror")
	if err = os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	e.must("mirror", "add", "papers", dir, "/DOCUMENT/Document/Papers", "--pull-new")
	before := len(e.cloud.Requests())
	out := e.must("mirror", "run", "papers")
	if !strings.Contains(out, "draw") {
		t.Fatal(out)
	}
	lookups, downloads := 0, 0
	for _, request := range e.cloud.Requests()[before:] {
		switch request.Path {
		case "/api/file/3/files/query/by/path_v3":
			lookups++
		case "/storage/download", "/api/oss/download":
			downloads++
		}
	}
	if lookups != 0 || downloads != 1 {
		t.Fatalf("drawing requests: lookups=%d downloads=%d", lookups, downloads)
	}
	drawing := filepath.Join(dir, "_annotated", "notebooks", "sub", "Journal.pdf")
	pdf, err := os.ReadFile(drawing)
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatal(err, string(pdf))
	}
	if _, err = os.Stat(filepath.Join(dir, "Report.mark")); !os.IsNotExist(err) {
		t.Fatal("sidecar appeared locally", err)
	}
	if out = e.must("mirror", "run", "papers"); !strings.Contains(out, "in step") {
		t.Fatal(out)
	}
	e.cloud.Remove("DOCUMENT/Document/Papers/sub/Journal.note")
	e.must("mirror", "run", "papers", "--force-delete")
	if _, err = os.Stat(filepath.Join(dir, ".snctl-removed", "_annotated", "notebooks", "sub", "Journal.pdf")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(drawing); !os.IsNotExist(err) {
		t.Fatal("drawing still active", err)
	}
}

func TestMirrorPDFOverlay(t *testing.T) {
	e := newE2E(t)
	read := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile("../../testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	original := read("overlay/Report.pdf")
	mark := read("overlay/Report.pdf.mark")
	const folder = "DOCUMENT/Document/Papers/"
	e.cloud.Seed(folder+"Report.pdf", false, original)
	e.cloud.Seed(folder+"Report.pdf.mark", false, mark)
	// The same stem is safe because notebooks and PDF overlays have separate namespaces.
	e.cloud.Seed(folder+"Report.note", false, read("notebooks/blank.note"))
	dir := e.path("mirror")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	e.must("mirror", "add", "papers", dir, "/DOCUMENT/Document/Papers", "--pull-new")
	writes := len(e.cloud.Writes())
	downloads := func() int { return e.cloud.Count("/storage/download") + e.cloud.Count("/api/oss/download") }
	before := downloads()
	if out := e.must("mirror", "run", "papers", "--dry-run", "--json"); !strings.Contains(out, `"action":"draw"`) && !strings.Contains(out, `"action": "draw"`) {
		t.Fatal(out)
	}
	if len(e.cloud.Writes()) != writes || downloads() != before {
		t.Fatal("dry run transferred data")
	}
	drawing := filepath.Join(dir, "_annotated", "documents", "Report.pdf")
	assertSource := func(source []byte) {
		t.Helper()
		pdf, err := os.ReadFile(drawing)
		if err != nil || !bytes.HasPrefix(pdf, source) || len(pdf) < len(source) {
			t.Fatal("source PDF not preserved", err)
		}
	}
	e.must("mirror", "run", "papers")
	assertSource(original)
	if downloads()-before != 3 {
		t.Fatal("first draw did not reuse the original download", downloads()-before)
	}
	if e.cloud.Count("/api/file/3/files/query/by/path_v3") != 0 {
		t.Fatal("drawing used per-file path lookups")
	}
	if _, err := os.Stat(filepath.Join(dir, "_annotated", "notebooks", "Report.pdf")); err != nil {
		t.Fatal(err)
	}
	if out := e.must("mirror", "run", "papers"); !strings.Contains(out, "in step") {
		t.Fatal(out)
	}
	if downloads()-before != 3 {
		t.Fatal("unchanged pass downloaded inputs")
	}
	// Another real mark changes the MD5 and exercises dropped sheets/highlights.
	e.cloud.Seed(folder+"Report.pdf.mark", false, read("notebooks/sparse.mark"))
	out := e.must("mirror", "run", "papers", "--json")
	if !strings.Contains(out, "warnings") || !strings.Contains(out, "dropped") {
		t.Fatal(out)
	}
	assertSource(original)
	if downloads()-before != 4 {
		t.Fatal("sidecar redraw did not reuse the source", downloads()-before)
	}
	short := read("overlay/short.pdf")
	e.cloud.Seed(folder+"Report.pdf", false, short)
	if out = e.must("mirror", "run", "papers"); !strings.Contains(out, "warning:") || !strings.Contains(out, "draw") {
		t.Fatal(out)
	}
	assertSource(short)
	if downloads()-before != 5 {
		t.Fatal("original redraw did not reuse the sidecar", downloads()-before)
	}
	// Cloud render resolves the source PDF next to the selected sidecar.
	e.cloud.Seed(folder+"Report.pdf.mark", false, mark)
	if out = e.must("render", folder+"Report.pdf.mark", "-o", e.path("cloud-overlay.pdf"), "--json"); strings.Contains(out, `"sheets_only": true`) || !strings.Contains(out, "warnings") {
		t.Fatal(out)
	}
	e.must("mv", folder+"Report.pdf", folder+"Renamed.pdf", "--yes")
	e.must("stat", folder+"Renamed.pdf.mark")
	if _, _, code := e.run("stat", folder+"Report.pdf.mark"); code == 0 {
		t.Fatal("move left the old sidecar")
	}
	e.must("mirror", "run", "papers")
	if _, err := os.Stat(filepath.Join(dir, ".snctl-removed", "_annotated", "documents", "Report.pdf")); err != nil {
		t.Fatal(err)
	}
	drawing = filepath.Join(dir, "_annotated", "documents", "Renamed.pdf")
	assertSource(short)
	e.cloud.Remove(folder + "Renamed.pdf")
	e.must("mirror", "run", "papers", "--force-delete")
	if _, err := os.Stat(filepath.Join(dir, ".snctl-removed", "_annotated", "documents", "Renamed.pdf")); err != nil {
		t.Fatal(err)
	}
	e.cloud.Seed(folder+"Book.epub", false, []byte("epub fixture; overlay must never read it"))
	e.cloud.Seed(folder+"Book.epub.mark", false, mark)
	if out = e.must("mirror", "run", "papers"); !strings.Contains(out, "EPUB annotation overlay is unsupported") {
		t.Fatal(out)
	}
	if _, err := os.Stat(filepath.Join(dir, "Renamed.pdf.mark")); !os.IsNotExist(err) {
		t.Fatal("sidecar downloaded into mirror", err)
	}
	if len(e.cloud.Writes()) != writes+2 {
		t.Fatal("unexpected cloud writes", e.cloud.Writes()[writes:])
	}
}

func TestRelativeOverlaySourceIntegration(t *testing.T) {
	e := newE2E(t)
	for _, name := range []string{"Report.pdf", "Report.pdf.mark"} {
		b, err := os.ReadFile("../../testdata/overlay/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(e.path(name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(e.root)
	for _, source := range []string{"", "Report.pdf"} {
		args := []string{"render", "Report.pdf.mark", "-o", "out.pdf", "--json"}
		if source != "" {
			args = append(args, "--source", source)
		}
		out, errOut, code := e.run(args...)
		if code != 0 || !strings.Contains(out, `"sheets_only": false`) {
			t.Fatalf("source %q: exit %d %s %s", source, code, out, errOut)
		}
	}
}

func TestCloudLegacySourceAmbiguityIntegration(t *testing.T) {
	e := newE2E(t)
	const folder = "DOCUMENT/Document/"
	for _, name := range []string{"Report.pdf", "Report.pdf.mark"} {
		b, err := os.ReadFile("../../testdata/overlay/" + name)
		if err != nil {
			t.Fatal(err)
		}
		cloudName := name
		if strings.HasSuffix(name, ".mark") {
			cloudName = "Report.mark"
		}
		e.cloud.Seed(folder+cloudName, false, b)
	}
	e.cloud.Seed(folder+"Report.epub", false, []byte("epub"))
	out := e.must("render", folder+"Report.mark", "-o", e.path("ambiguous.pdf"), "--json")
	if !strings.Contains(out, `"sheets_only": true`) || !strings.Contains(out, "ambiguous stem-only") {
		t.Fatal(out)
	}
	out = e.must("render", folder+"Report.mark", "--source", "/"+folder+"Report.pdf", "-o", e.path("explicit.pdf"), "--json")
	if !strings.Contains(out, `"sheets_only": false`) || strings.Contains(out, "ambiguous stem-only") {
		t.Fatal(out)
	}
}

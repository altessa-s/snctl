//go:build integration && cgo && !norender && (darwin || linux || windows)

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/cmd/snctl/commands/clitest"
	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/notebooks"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
	"github.com/altessa-s/snctl/internal/wiring"
)

func TestOfflineDocuments(t *testing.T) {
	e := newE2E(t)
	const folder = "DOCUMENT/Document/"
	for _, name := range []string{"Report.pdf", "Report.pdf.mark"} {
		data, err := os.ReadFile("../../testdata/overlay/" + name)
		if err != nil {
			t.Fatal(err)
		}
		e.cloud.Seed(folder+name, false, data)
	}
	e.cloud.Seed(folder+"Bare.epub", false, []byte("PK synthetic EPUB"))
	e.must("sync", "/DOCUMENT/Document", "--files")
	requests := len(e.cloud.Requests())
	for _, name := range []string{"Report.pdf", "Report.pdf.mark"} {
		e.must("--offline", "render", "/"+folder+name, "--page", "1", "-o", e.path(name+".png"), "--scale", ".1")
	}
	direct, err := os.ReadFile(e.path("Report.pdf.mark.png"))
	if err != nil {
		t.Fatal(err)
	}
	viaDoc, err := os.ReadFile(e.path("Report.pdf.png"))
	if err != nil || !bytes.Equal(direct, viaDoc) {
		t.Fatal("offline document overlay differs from sidecar", err)
	}
	// Use the actual offline wiring with a recording opener, keeping the test
	// independent of the machine's installed viewers.
	svc, err := wiring.Build(t.Context(), wiring.Options{Dirs: *e.s.dirs, Env: e.s.env, Keychain: e.s.keychain, Offline: new(true), ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Session.Close() }()
	opener := &porttest.Opener{}
	svc.Notebooks.Opener = opener
	for _, tc := range []struct {
		name, notice string
		flags        []string
	}{
		{"Bare.epub", "has no annotations", nil},
		{"Report.pdf", "annotations skipped", []string{"--no-marks"}},
	} {
		args := append([]string{"open", "/" + folder + tc.name}, tc.flags...)
		r := clitest.Exec(t, cliutil.Services{Notebooks: svc.Notebooks}, clitest.Options{}, notebooks.Commands, args...)
		if r.Code != 0 || !strings.Contains(r.Err, tc.notice) || strings.Contains(r.Out, tc.notice) {
			t.Fatal(args, r)
		}
		original := e.path("original-" + tc.name)
		e.must("--offline", "get", "/"+folder+tc.name, "-o", original)
		want, err := os.ReadFile(original)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(opener.Path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("original document preview changed bytes", err)
		}
		info, err := os.Stat(filepath.Dir(opener.Path))
		// POSIX mode bits do not describe Windows ACLs; the localfs Windows
		// tests check the preview directory and child file ACLs instead.
		if err != nil || !info.IsDir() || runtime.GOOS != "windows" && info.Mode().Perm() != 0700 {
			t.Fatal("preview directory is not private", info, err)
		}
		if err := os.RemoveAll(filepath.Dir(opener.Path)); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.cloud.Requests()) != requests {
		t.Fatal("offline document operation requested the fixture server")
	}
	// Removing retained input must fail rather than using the live cloud.
	e.must("debug", "cache", "clear", "--yes")
	e.must("sync", "/DOCUMENT/Document")
	requests = len(e.cloud.Requests())
	_, stderr, code := e.run("--offline", "render", "/"+folder+"Report.pdf", "-o", e.path("missing.pdf"))
	if code == 0 || !strings.Contains(stderr, "offline") {
		t.Fatal(code, stderr)
	}
	_, err = svc.Notebooks.Open(t.Context(), "/"+folder+"Report.pdf", port.RenderOptions{NoMarks: true})
	if err == nil || !strings.Contains(err.Error(), "offline") || len(e.cloud.Requests()) != requests {
		t.Fatal("missing original bypassed offline policy", err)
	}
}

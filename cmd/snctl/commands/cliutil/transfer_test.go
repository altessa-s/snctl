// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestTransferLine(t *testing.T) {
	t.Parallel()
	p := port.SyncProgress{Files: 120, FilesTotal: 310, Bytes: 9 << 20, BytesTotal: 20 << 20}
	if got, want := TransferLine("syncing", p, 3*time.Second),
		"syncing [=========>          ]  45%  9.0 MiB/20.0 MiB  120/310 files  3.0 MiB/s  0:04 left"; got != want {
		t.Fatalf("\n%q\n%q", got, want)
	}
	// No rate before a second, none of nothing, and no time left at the end.
	if got := TransferLine("syncing", p, 500*time.Millisecond); strings.Contains(got, "/s") {
		t.Fatal(got)
	}
	done := port.SyncProgress{Files: 2, FilesTotal: 2, Bytes: 10, BytesTotal: 10}
	if got := TransferLine("syncing", done, time.Hour); strings.Contains(got, "left") || !strings.Contains(got, "[====================] 100%") {
		t.Fatal(got)
	}
	// Nothing to fetch in bytes is all done.
	if got := TransferLine("syncing", port.SyncProgress{FilesTotal: 1}, 0); !strings.Contains(got, "100%") {
		t.Fatal(got)
	}
	if clockTime(3*time.Hour+4*time.Minute+5*time.Second) != "3:04:05" || clockTime(65*time.Second) != "1:05" {
		t.Fatal(clockTime(65 * time.Second))
	}
}

func TestTransferRedrawsAtMostEveryTenth(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	update, done := Transfer(Streams{Err: &buf, IsTerminal: true, Now: func() time.Time { return now }}, "syncing")
	p := port.SyncProgress{FilesTotal: 3, BytesTotal: 30}
	update(p)
	p.Bytes = 5
	update(p) // within the tenth: skipped
	now = now.Add(200 * time.Millisecond)
	p.Bytes = 10
	update(p)
	done()
	done()
	if n := strings.Count(buf.String(), "\r"); n != 2 || !strings.HasSuffix(buf.String(), "\n") || strings.Count(buf.String(), "\n") != 1 {
		t.Fatalf("%q", buf.String())
	}
	if update, done := Transfer(Streams{Err: &buf}, "syncing"); update != nil {
		t.Fatal("a progress bar off a terminal")
	} else {
		done()
	}
}

// A dumb terminal cannot clear a line, so a shorter line is padded over
// the longer one before it; with no clock given the machine's is used.
func TestTransferOnADumbTerminal(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	getenv := func(name string) string {
		if name == "TERM" {
			return "dumb"
		}
		return ""
	}
	update, done := Transfer(Streams{Err: &buf, IsTerminal: true, Getenv: getenv}, "syncing")
	update(port.SyncProgress{Files: 1, FilesTotal: 10000, Bytes: 1 << 30, BytesTotal: 3 << 30})
	first := strings.TrimPrefix(buf.String(), "\r")
	buf.Reset()
	// A finished sync draws at once, however soon.
	update(port.SyncProgress{Files: 1, FilesTotal: 1, Bytes: 1, BytesTotal: 1})
	done()
	out := buf.String()
	if strings.Contains(first+out, "\x1b") || !strings.HasPrefix(out, "\r") || !strings.HasSuffix(out, "\n") {
		t.Fatalf("escape sequences or no carriage return: %q %q", first, out)
	}
	last := strings.TrimSuffix(strings.TrimPrefix(out, "\r"), "\n")
	text := strings.TrimRight(last, " ")
	if len(last) != len(first) || len(text) >= len(first) || !strings.HasPrefix(text, "syncing [====================] 100%") {
		t.Fatalf("the shorter line does not cover the longer one:\n%q\n%q", first, last)
	}
}

func TestTransferBytes(t *testing.T) {
	var b bytes.Buffer
	p, done := TransferBytes(Streams{Err: &b, IsTerminal: true}, "download")
	p(1, 2)
	p(2, 2)
	done()
	if !strings.Contains(b.String(), "100%") {
		t.Fatal(b.String())
	}
	p, done = TransferBytes(Streams{Err: &b}, "download")
	if p != nil {
		t.Fatal("off-terminal callback")
	}
	done()
	p, done = Progress(Streams{Err: &b, IsTerminal: true}, "drawing")
	p(1, 0)
	p(1, 2)
	p(1, 2)
	p(2, 2)
	done()
	if !strings.Contains(b.String(), "1 bytes") {
		t.Fatal(b.String())
	}
}

func TestNotebookCompletion(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"one.note", "two.mark", "ignore.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	env := NewEnv(Streams{Getenv: func(string) string { return "" }}, Fixed(Services{}))
	cmd := &cobra.Command{Use: "test"}
	cmd.SetContext(t.Context())
	got, _ := CompleteNotebook(env)(cmd, nil, dir+string(filepath.Separator))
	if len(got) != 3 {
		t.Fatal(got)
	}
	if got, _ = CompleteNotebook(env)(cmd, []string{"x"}, ""); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestHighlightCompletion(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"paired.pdf", "paired.pdf.mark", "paired.epub", "paired.epub.mark", "bare.pdf", "orphan.mark", "skip.note"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	remote := &porttest.Cloud{Entries: []port.Entry{
		{Name: "paired.pdf"}, {Name: "paired.pdf.mark"}, {Name: "bare.pdf"}, {Name: "orphan.mark"}, {Name: "folder", Folder: true}, {Name: "skip.note"},
	}}
	env := NewEnv(Streams{Getenv: func(string) string { return "" }}, Fixed(Services{}))
	cmd := &cobra.Command{Use: "test"}
	cmd.SetContext(t.Context())
	got, _ := CompleteHighlights(env)(cmd, nil, dir+string(filepath.Separator))
	if len(got) != 5 {
		t.Fatal(got)
	}
	for _, name := range got {
		if strings.HasSuffix(name, "bare.pdf") || strings.HasSuffix(name, "skip.note") {
			t.Fatal(got)
		}
	}
	env = NewEnv(Streams{Getenv: func(string) string { return "" }}, Fixed(Services{Completion: remote}))
	got, _ = CompleteHighlights(env)(cmd, nil, "remote/")
	if len(got) != 4 {
		t.Fatal(got)
	}
	if got, _ = CompleteHighlights(env)(cmd, []string{"first"}, ""); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestRenderCompletion(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"paired.pdf", "paired.pdf.mark", "paired.epub", "paired.epub.mark", "bare.pdf", "orphan.mark", "one.note", "sketch.spd"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	env := NewEnv(Streams{Getenv: func(string) string { return "" }}, Fixed(Services{}))
	cmd := &cobra.Command{Use: "test"}
	cmd.SetContext(t.Context())
	got, _ := CompleteRender(env)(cmd, nil, dir+string(filepath.Separator))
	if len(got) != 7 {
		t.Fatal(got)
	}
	remote := &porttest.Cloud{Entries: []port.Entry{
		{Name: "paired.pdf"}, {Name: "paired.pdf.mark"}, {Name: "bare.pdf"}, {Name: "orphan.mark"},
		{Name: "folder", Folder: true}, {Name: "one.note"}, {Name: "sketch.spd"},
	}}
	env = NewEnv(Streams{Getenv: func(string) string { return "" }}, Fixed(Services{Completion: remote}))
	got, _ = CompleteRender(env)(cmd, nil, "remote/")
	if len(got) != 6 {
		t.Fatal(got)
	}
	for _, name := range got {
		if strings.HasSuffix(name, "bare.pdf") {
			t.Fatal(got)
		}
	}
}

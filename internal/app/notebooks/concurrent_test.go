//go:build (darwin || linux || windows) && cgo && !norender

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package notebooks_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/adapter/localfs"
	"github.com/altessa-s/snctl/internal/adapter/notes"
	"github.com/altessa-s/snctl/internal/adapter/renderer"
	"github.com/altessa-s/snctl/internal/app/notebooks"
	"github.com/altessa-s/snctl/internal/port"
)

var fixtures = "../../../testdata/notebooks/"

type fixtureFetch struct{ top port.Entry }

func (f fixtureFetch) Resolve(context.Context, string) (port.Entry, error) { return f.top, nil }
func (f fixtureFetch) Read(context.Context, string, port.Progress) (port.Entry, []byte, error) {
	return port.Entry{}, nil, os.ErrNotExist
}
func (f fixtureFetch) Get(context.Context, string, string, io.Writer, port.Progress) (string, error) {
	return "", nil
}

type fixtureCloud struct {
	port.Files
	entries   []port.Entry
	downloads *atomic.Int32
	cancel    func()
	dir       string
	fixtures  string
}

func (c fixtureCloud) List(context.Context, string, bool) ([]port.Entry, error) {
	return c.entries, nil
}
func (c fixtureCloud) Trash(context.Context) ([]port.Entry, error) { return nil, nil }
func (c fixtureCloud) Download(ctx context.Context, e port.Entry, _ port.Progress) (string, error) {
	n := c.downloads.Add(1)
	if c.cancel != nil && n == 2 {
		c.cancel()
	}
	root := c.fixtures
	if root == "" {
		root = fixtures
	}
	src := root + "text.note"
	if strings.HasSuffix(e.Name, ".mark") {
		src = root + "highlights.mark"
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp(c.dir, "dl-*")
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	return f.Name(), errorsJoin(err, f.Close())
}

func errorsJoin(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

func TestLocalDirectoryAndDefaultFolderOutput(t *testing.T) {
	fixturePath, err := filepath.Abs(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	if err := os.Mkdir("Note", 0o755); err != nil {
		t.Fatal(err)
	}
	s := notebooks.Service{Fetch: fixtureFetch{top: port.Entry{Name: "Note", Path: "Note", Folder: true}},
		Files: localfs.Files{}, Reader: notes.Reader{}, Renderer: renderer.Renderer{}}
	folder, err := s.Folder(t.Context(), "Note")
	if err != nil || !folder {
		t.Fatal(folder, err)
	}
	var dl atomic.Int32
	s.Cloud = fixtureCloud{entries: entries()[:1], downloads: &dl, dir: ".", fixtures: fixturePath + string(filepath.Separator)}
	r, err := s.RenderTree(t.Context(), "Note", "", port.RenderOptions{}, port.TreeOptions{Jobs: 2})
	if err != nil || r.Rendered != 1 {
		t.Fatal(r, err)
	}
	folder, err = s.Folder(t.Context(), "Note")
	if err != nil || !folder {
		t.Fatal(folder, err)
	}
	r, err = s.RenderTree(t.Context(), "Note", "", port.RenderOptions{}, port.TreeOptions{Jobs: 2, Incremental: true})
	if err != nil || r.Unchanged != 1 || dl.Load() != 1 {
		t.Fatal(r, err, dl.Load())
	}

}

func entries() []port.Entry {
	now := time.Unix(1_700_000_000, 0)
	var out []port.Entry
	add := func(id, p string) {
		out = append(out, port.Entry{ID: id, Name: filepath.Base(p), Path: p, Hash: "h" + id, Modified: &now})
	}
	add("1", "Note/a.note")
	add("2", "Note/A.note")
	add("3", "Note/sub/a.note")
	add("4", "Note/x.note")
	add("5", "Note/x.mark")
	add("6", "Note/../evil.note")
	add("7", "Note/..")
	add("8", "Note/CON.note")
	add("9", "Note/trail. .note")
	add("10", "Note/"+strings.Repeat("л", 200)+".note")
	for i := 11; i < 40; i++ {
		add(string(rune('a'+i%26))+string(rune('0'+i/26)), "Note/many/n"+string(rune('a'+i%26))+string(rune('0'+i/26))+".note")
	}
	return out
}

func TestConcurrentTree(t *testing.T) {
	work := t.TempDir()
	out := filepath.Join(work, "out")
	var dl atomic.Int32
	es := entries()
	s := notebooks.Service{Fetch: fixtureFetch{top: port.Entry{Name: "Note", Path: "Note", Folder: true}},
		Cloud: fixtureCloud{entries: es, downloads: &dl, dir: work}, Files: localfs.Files{}, Reader: notes.Reader{}, Renderer: renderer.Renderer{}}
	r, err := s.RenderTree(t.Context(), "Note", out, port.RenderOptions{}, port.TreeOptions{Jobs: 8})
	if err != nil || r.Rendered != 38 || r.Failed != 0 {
		t.Fatal(r, err)
	}
	t.Logf("first run: rendered=%d unchanged=%d failed=%d err=%v", r.Rendered, r.Unchanged, r.Failed, err)
	for _, d := range r.Documents[:12] {
		rel, _ := filepath.Rel(out, d.Path)
		t.Logf("  %-8s %-28s -> %s %s", d.Status, d.Document, rel, d.Error)
	}
	_ = filepath.Walk(work, func(p string, info os.FileInfo, err error) error {
		if err == nil && !strings.HasPrefix(p, out) && !info.IsDir() && p != work {
			t.Errorf("output outside destination: %s", p)
		}
		if err == nil && strings.Contains(p, ".snctl-render-") {
			t.Errorf("temporary output remains: %s", p)
		}
		return nil
	})
	r, err = s.RenderTree(t.Context(), "Note", out, port.RenderOptions{}, port.TreeOptions{Jobs: 8, Incremental: true})
	if err != nil || r.Unchanged != 38 || dl.Load() != 38 {
		t.Fatal(r, err, dl.Load())
	}
	t.Logf("incremental: rendered=%d unchanged=%d failed=%d err=%v", r.Rendered, r.Unchanged, r.Failed, err)
	es[0].Hash = "changed"
	es[3].Hash = ""
	s.Cloud = fixtureCloud{entries: es, downloads: &dl, dir: work}
	r, err = s.RenderTree(t.Context(), "Note", out, port.RenderOptions{}, port.TreeOptions{Jobs: 8, Incremental: true})
	if err != nil || r.Rendered != 2 || r.Unchanged != 36 {
		t.Fatal(r, err)
	}
	t.Logf("after hash change + empty hash: rendered=%d unchanged=%d failed=%d err=%v", r.Rendered, r.Unchanged, r.Failed, err)
	for _, d := range r.Documents[:5] {
		t.Logf("  %-9s %s", d.Status, d.Document)
	}
	r, err = s.RenderTree(t.Context(), "Note", out, port.RenderOptions{NoTemplate: true}, port.TreeOptions{Jobs: 8, Incremental: true})
	if err != nil || r.Rendered != 38 {
		t.Fatal(r, err)
	}
	t.Logf("options changed: rendered=%d unchanged=%d err=%v", r.Rendered, r.Unchanged, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	out2 := out
	before, err := os.ReadFile(filepath.Join(out, notebooks.ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	s.Cloud = fixtureCloud{entries: es, downloads: new(atomic.Int32), dir: work, cancel: cancel}
	r, err = s.RenderTree(ctx, "Note", out2, port.RenderOptions{}, port.TreeOptions{Jobs: 4})
	t.Logf("cancelled: rendered=%d failed=%d errIsCanceled=%v", r.Rendered, r.Failed, err != nil && strings.Contains(err.Error(), "canceled"))
	m, _ := os.ReadFile(filepath.Join(out2, notebooks.ManifestName))
	if string(m) != string(before) {
		t.Fatal("cancellation replaced the record")
	}
	_ = filepath.Walk(work, func(p string, info os.FileInfo, err error) error {
		if err == nil && (strings.Contains(p, ".snctl-render-") || strings.HasPrefix(filepath.Base(p), "dl-")) {
			t.Errorf("temporary output remains: %s", p)
		}
		return nil
	})
}

func TestSparseMarkRendering(t *testing.T) {
	s := notebooks.Service{Files: localfs.Files{}, Reader: notes.Reader{}, Renderer: renderer.Renderer{}}
	dest := t.TempDir()
	r, err := s.Render(t.Context(), filepath.Join(fixtures, "sparse.mark"), dest+string(filepath.Separator), port.RenderOptions{Format: "png", Scale: .1})
	if err != nil || r.Pages != 1 {
		t.Fatal(r, err)
	}
	if _, err = os.Stat(filepath.Join(dest, "sparse-p0022.png")); err != nil {
		t.Fatal(err)
	}
	r, err = s.Render(t.Context(), filepath.Join(fixtures, "sparse.mark"), dest, port.RenderOptions{Page: 22, Scale: .1})
	if err != nil || r.Pages != 1 {
		t.Fatal(r, err)
	}
}

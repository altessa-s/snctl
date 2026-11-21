// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package notebooks_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/app/notebooks"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func setup() (notebooks.Service, *porttest.NotebookFiles) {
	disk := &porttest.NotebookFiles{Data: map[string][]byte{"local.note": []byte("note")}}
	reader := porttest.NoteReader{Notebook: port.Notebook{Kind: "mark", Pages: []port.NotePage{{Number: 1}, {Number: 2}}, Titles: []port.NoteTitle{{Page: 1}, {Page: 2}}, Highlights: []port.NoteHighlight{{Page: 1}, {Page: 2}}}}
	return notebooks.Service{Files: disk, Reader: reader, Renderer: porttest.Renderer{Pages: 2}, Opener: &porttest.Opener{}}, disk
}

type fetch struct {
	err    error
	folder bool
}

func (f fetch) Resolve(context.Context, string) (port.Entry, error) {
	return port.Entry{Name: "cloud.note", Folder: f.folder}, f.err
}
func (f fetch) Read(context.Context, string, port.Progress) (port.Entry, []byte, error) {
	return port.Entry{Name: "cloud.note"}, []byte("note"), f.err
}
func (f fetch) Get(context.Context, string, string, io.Writer, port.Progress) (string, error) {
	return "", f.err
}
func TestRead(t *testing.T) {
	s, d := setup()
	for _, pages := range [][]int{nil, {1}, {2, 1, 2}} {
		n, err := s.Read(t.Context(), "local.note", pages, nil)
		if err != nil || n.Name != "local.note" {
			t.Fatal(n, err)
		}
	}
	if _, err := s.Read(t.Context(), "local.note", []int{99}, nil); !errors.Is(err, port.ErrPage) {
		t.Fatal(err)
	}
	if _, err := s.Read(t.Context(), "missing", nil, nil); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	s.Fetch = fetch{}
	if n, err := s.Read(t.Context(), "missing", nil, nil); err != nil || n.Name != "cloud.note" {
		t.Fatal(n, err)
	}
	s.Reader = porttest.NoteReader{Err: port.ErrNotebook}
	if _, err := s.Read(t.Context(), "local.note", nil, nil); !errors.Is(err, port.ErrNotebook) {
		t.Fatal(err)
	}
	s.Reader = porttest.NoteReader{Unavailable: port.ErrNotebookUnavailable}
	if _, err := s.Read(t.Context(), "local.note", nil, nil); !errors.Is(err, port.ErrNotebookUnavailable) {
		t.Fatal(err)
	}
	s, _ = setup()
	d.FailAt = "open"
	d.Err = fs.ErrPermission
	s.Files = d
	if _, err := s.Read(t.Context(), "local.note", nil, nil); !errors.Is(err, fs.ErrPermission) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Read(ctx, "local.note", nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestRender(t *testing.T) {
	for _, o := range []port.RenderOptions{{}, {Format: "png"}, {Page: 1}, {Format: "svg", Page: 2}, {Format: "jpeg", Page: 1, Quality: 90}} {
		s, d := setup()
		r, err := s.Render(t.Context(), "local.note", "", o)
		if err != nil || r.Pages == 0 || !r.SheetsOnly {
			t.Fatal(r, err)
		}
		if len(d.Data) < 2 {
			t.Fatal("no output")
		}
	}
	for _, fail := range []string{"destination", "produce", "mkdir"} {
		s, d := setup()
		d.FailAt = fail
		d.Err = fs.ErrPermission
		o := port.RenderOptions{}
		if fail == "mkdir" {
			o.Format = "png"
		}
		if _, err := s.Render(t.Context(), "local.note", "", o); !errors.Is(err, fs.ErrPermission) {
			t.Fatal(fail, err)
		}
	}
	for _, which := range []string{"reader", "renderer", "unavailable", "load", "page", "options", "image"} {
		s, _ := setup()
		o := port.RenderOptions{}
		switch which {
		case "reader":
			s.Reader = porttest.NoteReader{Err: port.ErrNotebook}
		case "renderer":
			s.Renderer = porttest.Renderer{Err: io.ErrClosedPipe}
		case "image":
			s.Renderer = porttest.Renderer{Err: io.ErrClosedPipe}
			o.Format = "png"
		case "unavailable":
			s.Renderer = porttest.Renderer{Unavailable: port.ErrRenderUnavailable}
		case "load":
			s.Files = &porttest.NotebookFiles{}
		case "page":
			o.Page = 99
			o.Format = "png"
		case "options":
			o.Format = "bad"
		}
		if _, err := s.Render(t.Context(), "local.note", "", o); err == nil {
			t.Fatal(which)
		}
	}
	for _, o := range []port.RenderOptions{{Format: "bad"}, {Page: -1}, {Page: 1, Format: "pdf"}, {Quality: 101}, {Quality: -1}, {Quality: 1}, {Scale: -1}, {Scale: math.NaN()}, {Scale: math.Inf(1)}} {
		if _, err := notebooks.Normalize(o); !errors.Is(err, port.ErrOptions) {
			t.Fatal(o, err)
		}
	}
}
func TestFolderAndOpen(t *testing.T) {
	s, d := setup()
	if folder, err := s.Folder(t.Context(), "local.note"); err != nil || folder {
		t.Fatal(folder, err)
	}
	if _, err := s.Folder(t.Context(), "missing"); err == nil {
		t.Fatal("missing")
	}
	s.Fetch = fetch{folder: true}
	if folder, err := s.Folder(t.Context(), "remote"); err != nil || !folder {
		t.Fatal(folder, err)
	}
	d.FailAt = "open"
	d.Err = fs.ErrPermission
	if _, err := s.Folder(t.Context(), "local.note"); err == nil {
		t.Fatal("permission")
	}
	d.FailAt = ""
	r, err := s.Open(t.Context(), "local.note", port.RenderOptions{})
	if err != nil || r.Path == "" {
		t.Fatal(r, err)
	}
	for _, which := range []string{"unavailable", "preview", "draw", "opener", "format", "image"} {
		s, d = setup()
		o := port.RenderOptions{}
		switch which {
		case "unavailable":
			s.Renderer = porttest.Renderer{Unavailable: port.ErrRenderUnavailable}
		case "preview":
			d.FailAt = "preview"
			d.Err = fs.ErrPermission
		case "draw":
			s.Renderer = porttest.Renderer{Err: io.ErrClosedPipe}
		case "opener":
			s.Opener = &porttest.Opener{Err: fs.ErrNotExist}
		case "format":
			o.Format = "bad"
		case "image":
			o.Format = "png"
		}
		if _, err = s.Open(t.Context(), "local.note", o); err == nil {
			t.Fatal(which)
		}
	}
	s.Renderer = porttest.Renderer{Unavailable: port.ErrRenderUnavailable}
	if _, err = s.Folder(t.Context(), "x"); err == nil {
		t.Fatal("unavailable")
	}
}

type treeCloud struct {
	port.Files
	entries []port.Entry
	err     error
	fail    string
	disk    *porttest.NotebookFiles
}

func (c treeCloud) List(context.Context, string, bool) ([]port.Entry, error) {
	if c.fail == "list" {
		return nil, c.err
	}
	return c.entries, nil
}
func (c treeCloud) Trash(context.Context) ([]port.Entry, error) { return nil, c.err }
func (c treeCloud) Download(ctx context.Context, e port.Entry, _ port.Progress) (string, error) {
	if c.err != nil {
		return "", c.err
	}
	name := "download-" + e.ID
	err := c.disk.Produce(ctx, name, func(w io.Writer) error { _, err := io.WriteString(w, "note"); return err })
	return name, err
}
func TestTree(t *testing.T) {
	now := time.Unix(1000, 0)
	entries := []port.Entry{{ID: "a", Name: "one.note", Path: "folder/one.note", Hash: "hash", Modified: &now}, {ID: "b", Name: "one.mark", Path: "folder/one.mark", Hash: "hash", Modified: &now}}
	s, d := setup()
	s.Fetch = fetch{folder: true}
	s.Cloud = treeCloud{entries: entries, disk: d}
	d.Data["download"] = []byte("note")
	// Independent staging paths let both documents complete concurrently.
	r, err := s.RenderTree(t.Context(), "folder", "pdfs", port.RenderOptions{}, port.TreeOptions{Jobs: 2})
	if err != nil || r.Rendered != 2 {
		t.Fatal(r, err)
	}
	s.Cloud = treeCloud{entries: entries[:1], disk: d}
	d.Data["download"] = []byte("note")
	r, err = s.RenderTree(t.Context(), "folder", "pdfs", port.RenderOptions{}, port.TreeOptions{Jobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.RenderTree(t.Context(), "folder", "pdfs", port.RenderOptions{}, port.TreeOptions{Incremental: true})
	if err != nil || r.Documents[0].Status != "unchanged" {
		t.Fatal(r, err)
	}
	d.Data[filepath.Join("pdfs", notebooks.ManifestName)] = []byte("damaged")
	d.Data["download"] = []byte("note")
	if _, err = s.RenderTree(t.Context(), "folder", "pdfs", port.RenderOptions{}, port.TreeOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	for _, which := range []string{"options", "unavailable", "resolve", "notfolder", "list", "trash", "mkdir", "produce", "download", "settime", "cancel"} {
		s, d = setup()
		s.Fetch = fetch{folder: true}
		s.Cloud = treeCloud{entries: entries[:1], disk: d}
		d.Data["download"] = []byte("note")
		o := port.RenderOptions{}
		tree := port.TreeOptions{}
		ctx := t.Context()
		switch which {
		case "options":
			o.Page = 1
		case "unavailable":
			s.Renderer = porttest.Renderer{Unavailable: port.ErrRenderUnavailable}
		case "resolve":
			s.Fetch = fetch{err: fs.ErrPermission}
		case "notfolder":
			s.Fetch = fetch{}
		case "list":
			s.Cloud = treeCloud{fail: "list", err: fs.ErrPermission}
		case "trash":
			s.Cloud = treeCloud{err: fs.ErrPermission}
			tree.All = true
		case "mkdir", "produce", "settime":
			d.FailAt = which
			d.Err = fs.ErrPermission
		case "download":
			s.Cloud = treeCloud{entries: entries[:1], err: fs.ErrPermission}
		case "cancel":
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		}
		if _, err = s.RenderTree(ctx, "folder", "", o, tree); err == nil {
			t.Fatal(which)
		}
	}
}

type controlledCloud struct {
	treeCloud
	cancel func()
	failID string
}

func (c controlledCloud) Download(ctx context.Context, e port.Entry, p port.Progress) (string, error) {
	if e.ID == c.failID {
		if c.cancel != nil {
			c.cancel()
			return "", ctx.Err()
		}
		return "", io.ErrUnexpectedEOF
	}
	return c.treeCloud.Download(ctx, e, p)
}

func TestRecordSurvivesPartialRuns(t *testing.T) {
	s, d := setup()
	now := time.Unix(1000, 0)
	entries := []port.Entry{{ID: "a", Name: "a.note", Path: "folder/a.note", Hash: "a", Modified: &now}, {ID: "b", Name: "b.note", Path: "folder/b.note", Hash: "b", Modified: &now}}
	cloud := treeCloud{entries: entries, disk: d}
	s.Cloud = cloud
	s.Fetch = fetch{folder: true}
	_, err := s.RenderTree(t.Context(), "folder", "pdfs", port.RenderOptions{}, port.TreeOptions{Jobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join("pdfs", notebooks.ManifestName)
	before := bytes.Clone(d.Data[key])
	entries[0].Hash = "changed"
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.Cloud = controlledCloud{treeCloud: cloud, cancel: cancel, failID: "b"}
	r, err := s.RenderTree(ctx, "folder", "pdfs", port.RenderOptions{}, port.TreeOptions{Jobs: 1})
	if !errors.Is(err, context.Canceled) || r.Rendered != 1 || r.Failed != 1 {
		t.Fatal(r, err)
	}
	if bytes.Equal(before, d.Data[key]) || !bytes.Contains(d.Data[key], []byte(`"hash":"b"`)) || !bytes.Contains(d.Data[key], []byte(`"hash":"changed"`)) {
		t.Fatal(string(d.Data[key]))
	}
	entries[0].Hash = "changed-again"
	delete(d.Data, filepath.Join("pdfs", "folder", "b.pdf"))
	s.Cloud = controlledCloud{treeCloud: cloud, failID: "b"}
	r, err = s.RenderTree(t.Context(), "folder", "pdfs", port.RenderOptions{}, port.TreeOptions{Jobs: 1, Incremental: true})
	if err == nil || r.Rendered != 1 || bytes.Contains(d.Data[key], []byte(`"hash":"b"`)) {
		t.Fatal(r, err, string(d.Data[key]))
	}
}

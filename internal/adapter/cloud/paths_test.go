// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"context"
	"errors"
	"path"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/port"

	sncloud "github.com/altessa-s/sncloud-go"
)

type treeDevice struct {
	fakeDeviceAPI
	entries   []sncloud.DeviceEntry
	listCalls int
}

func (f *treeDevice) List(_ context.Context, folder string, recursive bool) ([]sncloud.DeviceEntry, error) {
	f.listCalls++
	folder = cleanPath(folder)
	var out []sncloud.DeviceEntry
	for _, e := range f.entries {
		if deviceParent(e.Path) == folder || recursive && (folder == "" || strings.HasPrefix(e.Path, folder+"/")) {
			out = append(out, e)
		}
	}
	return out, f.err
}
func (f *treeDevice) EntryAt(_ context.Context, name string) (*sncloud.DeviceEntry, error) {
	for _, e := range f.entries {
		if e.Path == cleanPath(name) {
			return &e, f.err
		}
	}
	return nil, f.err
}
func (f *treeDevice) CreateFolder(_ context.Context, name string, _ bool) (sncloud.DeviceEntry, error) {
	e := sncloud.DeviceEntry{ID: sncloud.FileID(len(f.entries) + 100), Name: path.Base(name), Path: name, IsFolder: true}
	f.entries = append(f.entries, e)
	return e, nil
}
func (f *treeDevice) Upload(ctx context.Context, _, name string, _ sncloud.Progress) (sncloud.DeviceEntry, error) {
	e, err := f.CreateFolder(ctx, name, false)
	f.entries[len(f.entries)-1].IsFolder = false
	e.IsFolder = false
	return e, err
}
func (f *treeDevice) Move(_ context.Context, id sncloud.FileID, name string, _ bool) (*sncloud.DeviceEntry, error) {
	for i := range f.entries {
		if f.entries[i].ID == id {
			f.entries[i].Path = name
			f.entries[i].Name = path.Base(name)
			return &f.entries[i], nil
		}
	}
	return nil, sncloud.ErrNotFound
}

func TestCanonicalTree(t *testing.T) {
	t.Parallel()
	d := &treeDevice{entries: []sncloud.DeviceEntry{
		{ID: 1, Name: "NOTE", Path: "NOTE", IsFolder: true},
		{ID: 2, Name: "Note", Path: "NOTE/Note", IsFolder: true},
		{ID: 3, Name: "x", Path: "NOTE/x", IsFolder: true},
		{ID: 4, Name: "x", Path: "NOTE/Note/x", IsFolder: true},
		{ID: 5, Name: "c.txt", Path: "NOTE/x/c.txt"},
		{ID: 6, Name: "c.txt", Path: "NOTE/Note/x/c.txt"},
	}}
	c := synthetic()
	c.device = d
	for name, id := range map[string]string{"NOTE/x": "3", "/NOTE/Note/x": "4"} {
		e, err := c.Stat(t.Context(), name)
		if err != nil || e.ID != id {
			t.Fatalf("%s: %v %v", name, e, err)
		}
	}
	if _, err := c.Stat(t.Context(), "Note/x"); err == nil || !strings.Contains(err.Error(), "candidates: NOTE") {
		t.Fatal(err)
	}
	for _, name := range []string{"Note/x/new", "NOTE/x/new", "NOTE/new", "DOCUMENT/new", "NOTE/Note/missing/new", "NOTE/Note/x/c.txt/new"} {
		if _, err := c.Mkdir(t.Context(), name); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	if _, err := c.Mkdir(t.Context(), "NOTE/Note"); !errors.Is(err, port.ErrConflict) {
		t.Fatal(err)
	}
	if _, err := c.Mkdir(t.Context(), "/NOTE/Note/new"); err != nil {
		t.Fatal(err)
	}
	e, err := c.Upload(t.Context(), "local", "/NOTE/Note/new/a.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Move(t.Context(), e, "NOTE/Note/new/b.txt"); err != nil {
		t.Fatal(err)
	}
	if err = c.Move(t.Context(), e, "NOTE/x/b.txt"); err == nil {
		t.Fatal("unsafe move")
	}
	if _, err = c.Upload(t.Context(), "local", "NOTE/x/a.txt", nil); err == nil {
		t.Fatal("unsafe upload")
	}
	for _, recursive := range []bool{false, true} {
		rows, err := c.List(t.Context(), "NOTE/Note", recursive)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if !strings.HasPrefix(row.Path, "NOTE/Note/") {
				t.Fatal(row)
			}
		}
	}
	if _, err = c.List(t.Context(), "Note", false); err == nil {
		t.Fatal("alias listing")
	}
	if _, err = c.List(t.Context(), "NOTE/x/c.txt", false); err == nil {
		t.Fatal("file listing")
	}
	d.err = errors.New("synthetic private credential")
	if _, err = c.Mkdir(t.Context(), "NOTE/Note/fail"); err == nil || strings.Contains(err.Error(), "credential") {
		t.Fatal(err)
	}
	if _, err = c.List(t.Context(), "/", false); err == nil {
		t.Fatal("ignored listing error")
	}
}

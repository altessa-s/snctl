// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest

import (
	"bytes"
	"context"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/altessa-s/snctl/internal/port"
)

// The drive's clock starts at a fixed second; its capacity is nominal.
const (
	driveEpoch    = 1700000000
	driveUsed     = 1
	driveCapacity = 2
)

// Drive is an in-memory tablet tree with the semantics of the cloud adapter:
// exact paths, refused collisions, downloads staged on Disk. Fail injects an
// error for an operation on a path ("download NOTE/Note/a.note") or for every
// call of an operation ("list"). It records every call in order.
type Drive struct {
	mu      sync.Mutex
	Disk    *NotebookFiles
	Files   map[string][]byte
	Folders map[string]bool
	Fail    map[string]error
	Calls   []string
	Locked  bool
	next    int
	ids     map[string]string
	stamps  map[string]time.Time
}

// NewDrive seeds folders and files; every ancestor folder exists.
func NewDrive(disk *NotebookFiles, files map[string]string) *Drive {
	d := &Drive{Disk: disk, Files: map[string][]byte{}, Folders: map[string]bool{}, Fail: map[string]error{}}
	for name, data := range files {
		d.seed(name, []byte(data))
	}
	return d
}

// Seed adds or replaces one file and its ancestors.
func (d *Drive) Seed(name, data string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seed(name, []byte(data))
}

// Drop forgets a file, as a change made by another device would.
func (d *Drive) Drop(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.Files, name)
}

func (d *Drive) seed(name string, data []byte) {
	for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
		d.Folders[dir] = true
		d.id(dir)
	}
	d.Files[name] = bytes.Clone(data)
	d.touch(name)
}

func (d *Drive) id(name string) string {
	if d.ids == nil {
		d.ids = map[string]string{}
	}
	if id, ok := d.ids[name]; ok {
		return id
	}
	d.next++
	d.ids[name] = strconv.Itoa(d.next)
	return d.ids[name]
}

func (d *Drive) touch(name string) {
	if d.stamps == nil {
		d.stamps = map[string]time.Time{}
	}
	d.next++
	d.stamps[name] = time.Unix(driveEpoch+int64(d.next), 0).UTC()
	d.id(name)
}

func (d *Drive) call(op, name string) error {
	d.Calls = append(d.Calls, strings.TrimSpace(op+" "+name))
	if err := d.Fail[op+" "+name]; err != nil {
		return err
	}
	return d.Fail[op]
}

// Count is how many calls an operation received.
func (d *Drive) Count(op string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, c := range d.Calls {
		if c == op || strings.HasPrefix(c, op+" ") {
			n++
		}
	}
	return n
}

func (d *Drive) entry(name string) (port.Entry, bool) {
	if d.Folders[name] {
		return port.Entry{ID: d.id(name), Name: path.Base(name), Path: name, Folder: true}, true
	}
	data, ok := d.Files[name]
	if !ok {
		return port.Entry{}, false
	}
	size := int64(len(data))
	stamp := d.stamps[name]
	return port.Entry{ID: d.id(name), Name: path.Base(name), Path: name, Size: &size, Hash: ContentHash(data), Modified: &stamp}, true
}

func clean(name string) string { return strings.Trim(path.Clean("/"+name), "/") }

// List returns the subtree in path order.
func (d *Drive) List(_ context.Context, folder string, recursive bool) ([]port.Entry, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.call("list", ""); err != nil {
		return nil, err
	}
	folder = clean(folder)
	if folder != "" && !d.Folders[folder] {
		return nil, port.ErrNotFound
	}
	names := slices.Sorted(func(yield func(string) bool) {
		for name := range d.Folders {
			if !yield(name) {
				return
			}
		}
		for name := range d.Files {
			if !yield(name) {
				return
			}
		}
	})
	out := []port.Entry{}
	for _, name := range names {
		parent := clean(path.Dir(name))
		if parent == folder || recursive && (folder == "" || strings.HasPrefix(name, folder+"/")) {
			e, _ := d.entry(name)
			out = append(out, e)
		}
	}
	return out, nil
}

// Stat resolves an exact path.
func (d *Drive) Stat(_ context.Context, name string) (port.Entry, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.call("stat", clean(name)); err != nil {
		return port.Entry{}, err
	}
	e, ok := d.entry(clean(name))
	if !ok {
		return port.Entry{}, port.ErrNotFound
	}
	return e, nil
}

// Snapshot answers from the same tree; the double has no listing cost.
func (d *Drive) Snapshot(context.Context) (port.Files, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d, d.call("snapshot", "")
}

// WithSyncLock records the lock around body.
func (d *Drive) WithSyncLock(ctx context.Context, body func(context.Context) error) error {
	d.mu.Lock()
	if err := d.call("lock", ""); err != nil {
		d.mu.Unlock()
		return err
	}
	d.Locked = true
	d.mu.Unlock()
	defer func() { d.mu.Lock(); d.Locked = false; d.mu.Unlock() }()
	return body(ctx)
}

// Download stages the bytes on Disk under a private name.
func (d *Drive) Download(ctx context.Context, e port.Entry, p port.Progress) (string, error) {
	d.mu.Lock()
	err := d.call("download", e.Path)
	data, ok := d.Files[e.Path]
	d.next++
	staged := "staged-" + strconv.Itoa(d.next)
	d.mu.Unlock()
	if err != nil {
		return "", err
	}
	if !ok {
		return "", port.ErrNotFound
	}
	if p != nil {
		p(int64(len(data)), int64(len(data)))
	}
	return staged, d.Disk.Produce(ctx, staged, func(w io.Writer) error { _, werr := w.Write(data); return werr })
}

// Upload reads the local file from Disk; the parent folder must exist.
func (d *Drive) Upload(_ context.Context, file, name string, p port.Progress) (port.Entry, error) {
	r, err := d.Disk.Open(file)
	if err != nil {
		return port.Entry{}, err
	}
	data, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		return port.Entry{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	name = clean(name)
	if err = d.call("upload", name); err != nil {
		return port.Entry{}, err
	}
	if !d.Folders[clean(path.Dir(name))] || d.Folders[name] {
		return port.Entry{}, port.ErrNotFound
	}
	if p != nil {
		p(int64(len(data)), int64(len(data)))
	}
	d.Files[name] = data
	d.touch(name)
	e, _ := d.entry(name)
	return e, nil
}

// Mkdir creates one folder below an existing one.
func (d *Drive) Mkdir(_ context.Context, name string) (port.Entry, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	name = clean(name)
	if err := d.call("mkdir", name); err != nil {
		return port.Entry{}, err
	}
	if _, exists := d.entry(name); exists {
		return port.Entry{}, port.ErrConflict
	}
	if parent := clean(path.Dir(name)); parent != "" && !d.Folders[parent] {
		return port.Entry{}, port.ErrNotFound
	}
	d.Folders[name] = true
	e, _ := d.entry(name)
	return e, nil
}

// Move renames a file.
func (d *Drive) Move(_ context.Context, e port.Entry, destination string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.call("move", e.Path); err != nil {
		return err
	}
	data, ok := d.Files[e.Path]
	if !ok {
		return port.ErrNotFound
	}
	delete(d.Files, e.Path)
	d.Files[clean(destination)] = data
	d.ids[clean(destination)] = e.ID
	delete(d.ids, e.Path)
	d.touch(clean(destination))
	return nil
}

// Delete forgets a file.
func (d *Drive) Delete(_ context.Context, e port.Entry, _ bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.call("delete", e.Path); err != nil {
		return err
	}
	delete(d.Files, e.Path)
	return nil
}

// Trash is always empty.
func (d *Drive) Trash(context.Context) ([]port.Entry, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return []port.Entry{}, d.call("trash", "")
}

// EmptyTrash records the call.
func (d *Drive) EmptyTrash(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.call("empty", "")
}

// Restore records the call.
func (d *Drive) Restore(_ context.Context, id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.call("restore", id)
}

// Info reports account capacity for usage reports.
func (d *Drive) Info(context.Context) (port.AccountInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return port.AccountInfo{Used: driveUsed, Total: driveCapacity}, d.call("info", "")
}

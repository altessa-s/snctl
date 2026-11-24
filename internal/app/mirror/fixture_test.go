// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"iter"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/adapter/localfs"
	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/port"
)

var errBoom = errors.New("fixture failure")
var now = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

type cloud struct {
	port.Files
	mu                sync.Mutex
	entries           map[string]port.Entry
	data              map[string]string
	trash             []port.Entry
	next              int
	fail              map[string]error
	afterUpload       func()
	afterDelete       func()
	feed              chan port.Change
	listings          int
	locked            bool
	refuseLockedMoves bool
}

func (c *cloud) seed(name, body string) port.Entry {
	c.next++
	sum := md5.Sum([]byte(body))
	size := int64(len(body))
	en := port.Entry{ID: fmt.Sprint(c.next), Path: strings.Trim(name, "/"), Name: path.Base(name), Size: &size, Hash: hex.EncodeToString(sum[:])}
	if body == "<folder>" {
		en.Folder = true
	}
	c.entries[en.ID], c.data[en.ID] = en, body
	return en
}
func (c *cloud) List(ctx context.Context, _ string, _ bool) ([]port.Entry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.listings++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := c.fail["list"]; err != nil {
		return nil, err
	}
	out := make([]port.Entry, 0, len(c.entries))
	for _, e := range c.entries {
		out = append(out, e)
	}
	return out, nil
}
func (c *cloud) Stat(_ context.Context, name string) (port.Entry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.fail["stat"]; err != nil {
		return port.Entry{}, err
	}
	for _, e := range c.entries {
		if e.Path == strings.Trim(name, "/") || e.ID == name {
			return e, nil
		}
	}
	return port.Entry{}, port.ErrNotFound
}
func (c *cloud) Upload(ctx context.Context, local, target string, _ port.Progress) (port.Entry, error) {
	if err := c.fail["upload"]; err != nil {
		return port.Entry{}, err
	}
	data, err := os.ReadFile(local)
	if err != nil {
		return port.Entry{}, err
	}
	c.mu.Lock()
	e := c.seed(target, string(data))
	c.mu.Unlock()
	if c.afterUpload != nil {
		c.afterUpload()
	}
	return e, ctx.Err()
}
func (c *cloud) Download(_ context.Context, e port.Entry, _ port.Progress) (string, error) {
	if err := c.fail["download"]; err != nil {
		return "", err
	}
	return localfs.Files{}.Stage(strings.NewReader(c.data[e.ID]))
}
func (c *cloud) Mkdir(_ context.Context, name string) (port.Entry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.fail["mkdir"]; err != nil {
		return port.Entry{}, err
	}
	return c.seed(strings.Trim(name, "/"), "<folder>"), nil
}
func (c *cloud) Move(_ context.Context, e port.Entry, to string) error {
	if err := c.fail["move"]; err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.locked && c.refuseLockedMoves {
		return port.ErrBusy
	}
	for id, ch := range c.entries {
		if id == e.ID || strings.HasPrefix(ch.Path, e.Path+"/") {
			ch.Path = strings.Trim(to, "/") + strings.TrimPrefix(ch.Path, e.Path)
			ch.Name = path.Base(ch.Path)
			c.entries[id] = ch
		}
	}
	return nil
}
func (c *cloud) Delete(_ context.Context, e port.Entry, permanent bool) error {
	if permanent {
		panic("permanent delete")
	}
	if err := c.fail["delete"]; err != nil {
		return err
	}
	c.mu.Lock()
	if c.locked {
		c.mu.Unlock()
		return port.ErrBusy
	}
	delete(c.entries, e.ID)
	c.trash = append(c.trash, e)
	c.mu.Unlock()
	if c.afterDelete != nil {
		c.afterDelete()
	}
	return nil
}
func (c *cloud) WithSyncLock(ctx context.Context, f func(context.Context) error) error {
	if err := c.fail["lock"]; err != nil {
		return err
	}
	c.mu.Lock()
	c.locked = true
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.locked = false; c.mu.Unlock() }()
	return f(ctx)
}
func (c *cloud) Watch(ctx context.Context) iter.Seq2[port.Change, error] {
	return func(yield func(port.Change, error) bool) {
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-c.feed:
				if !yield(ev, nil) {
					return
				}
			}
		}
	}
}

type noteRender struct {
	port.Notebooks
	err error
}

func (n noteRender) Render(_ context.Context, _, target string, _ port.RenderOptions) (port.RenderReport, error) {
	if n.err != nil {
		return port.RenderReport{}, n.err
	}
	return port.RenderReport{Path: target}, os.WriteFile(target, []byte("%PDF notebook"), 0o600)
}

func (n noteRender) Read(context.Context, string, []int, port.Progress) (port.Notebook, error) {
	return port.Notebook{}, n.err
}

type noWatcher struct{}

func (noWatcher) Watch(ctx context.Context, _ string, _ func(string) bool, _, _ func()) error {
	<-ctx.Done()
	return nil
}

type timer struct{}

func (timer) Now() time.Time { return now }
func (timer) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type fixture struct {
	t    *testing.T
	c    *cloud
	e    *Engine
	dir  string
	spec port.MirrorSpec
}

func setup(t *testing.T) *fixture {
	t.Helper()
	c := &cloud{entries: map[string]port.Entry{}, data: map[string]string{}, fail: map[string]error{}, feed: make(chan port.Change, 100)}
	c.refuseLockedMoves = os.Getenv("SNCTL_TEST_REFUSE_LOCKED_MOVES") == "1"
	c.seed("DOCUMENT", "<folder>")
	c.seed("DOCUMENT/Document", "<folder>")
	c.seed("DOCUMENT/Document/Papers", "<folder>")
	disk := localfs.Files{}
	writer := files.Service{Cloud: c, Local: disk, Disk: disk, Lock: c}
	dir := t.TempDir()
	e := NewEngine(Deps{LibraryVersions: "reader1/render1", Cloud: c, Upload: writer, Library: writer, Files: disk, Disk: disk, Notes: noteRender{}, Feed: c, Dirs: noWatcher{}, Timer: timer{}, Lock: c})
	return &fixture{t: t, c: c, e: e, dir: dir, spec: port.MirrorSpec{Name: "papers", Dir: dir, Folder: "/DOCUMENT/Document/Papers", Profile: "default"}}
}
func (f *fixture) write(rel, body string) {
	f.t.Helper()
	name := filepath.Join(f.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Chtimes(name, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) read(rel string) string {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, filepath.FromSlash(rel)))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}
func (f *fixture) pass(o port.MirrorRunOptions) port.MirrorPass {
	f.t.Helper()
	p, err := f.e.Pass(f.t.Context(), f.spec, o)
	if err != nil {
		f.t.Fatalf("%+v: %v", p, err)
	}
	return p
}
func (f *fixture) remote(rel string) port.Entry {
	f.t.Helper()
	e, err := f.c.Stat(f.t.Context(), "DOCUMENT/Document/Papers/"+rel)
	if err != nil {
		f.t.Fatal(err)
	}
	return e
}
func (f *fixture) remoteChange(rel, body string) {
	old := f.remote(rel)
	sum := md5.Sum([]byte(body))
	size := int64(len(body))
	old.Hash, old.Size = hex.EncodeToString(sum[:]), &size
	f.c.entries[old.ID], f.c.data[old.ID] = old, body
}
func has(p port.MirrorPass, k port.MirrorActionKind) bool {
	for _, a := range p.Actions {
		if a.Kind == k {
			return true
		}
	}
	return false
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package notebooks_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/localfs"
	"github.com/altessa-s/snctl/internal/app/notebooks"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

type recognitionCache struct {
	port.FileCache
	values map[string]bool
	status port.CacheStatus
	err    error
	file   string
	found  bool
}

func (c *recognitionCache) Recognition(hash string) *bool {
	v, ok := c.values[hash]
	if !ok {
		return nil
	}
	return &v
}
func (c *recognitionCache) RememberRecognition(_ context.Context, hash string, v bool) error {
	if c.values == nil {
		c.values = map[string]bool{}
	}
	c.values[hash] = v
	return c.err
}
func (c *recognitionCache) Status() (port.CacheStatus, error) { return c.status, c.err }
func (c *recognitionCache) Body(context.Context, port.Entry) (string, bool, error) {
	return c.file, c.found, c.err
}

func TestRecognitionDiscovery(t *testing.T) {
	s, d := setup()
	cache := &recognitionCache{}
	s.Index = cache
	s.Reader = porttest.NoteReader{Notebook: port.Notebook{Kind: "note", Pages: []port.NotePage{{Number: 1, Recognition: "recognized"}}}}
	s.Fetch = fetch{}
	entries := []port.Entry{{Name: "cloud.note", Path: "cloud.note", Hash: "aad653ca3ee669635f2938b73098b6d7"}, {Name: "dir", Folder: true}, {Name: "x.pdf"}}
	known, err := s.Recognition(t.Context(), entries, false, nil)
	if err != nil || known[0].Recognition != nil {
		t.Fatal(known, err)
	}
	known, err = s.Recognition(t.Context(), entries, true, nil)
	if err != nil || known[0].Recognition == nil || !*known[0].Recognition || entries[0].Recognition != nil {
		t.Fatal(known, err)
	}
	s.ObserveBody(t.Context(), entries[0], "local.note")
	if len(cache.values) != 1 {
		t.Fatal(cache.values)
	}
	cache.file, cache.found = "local.note", true
	s.ObserveCached(t.Context(), entries[0])
	d.Data["local.note"] = []byte("other bytes")
	entries[0].Hash = "other"
	s.ObserveCached(t.Context(), entries[0])
	if len(cache.values) != 2 {
		t.Fatal(cache.values)
	}
	cache.values[entries[0].Hash] = false
	known, err = s.Recognition(t.Context(), entries, true, nil)
	if err != nil || *known[0].Recognition {
		t.Fatal(known, err)
	}
	s.Reader = porttest.NoteReader{Err: port.ErrNotebook}
	delete(cache.values, entries[0].Hash)
	if _, err = s.Recognition(t.Context(), entries, true, nil); !errors.Is(err, port.ErrNotebook) {
		t.Fatal(err)
	}
	for _, point := range []string{"open", "read"} {
		d.FailAt, d.Err = point, fs.ErrPermission
		s.ObserveBody(t.Context(), entries[0], "local.note")
	}
	s.Reader = porttest.NoteReader{Unavailable: port.ErrNotebookUnavailable}
	s.ObserveBody(t.Context(), entries[0], "local.note")
	s.ObserveCached(t.Context(), entries[0])
	s.Index = nil
	s.ObserveCached(t.Context(), entries[0])
}

func TestRecognitionBudget(t *testing.T) {
	s, _ := setup()
	size := int64(4)
	cache := &recognitionCache{status: port.CacheStatus{Budget: 3}}
	s.Index = cache
	s.Fetch = fetch{}
	entry := port.Entry{Name: "x.note", Path: "x.note", Hash: "hash", Size: &size}
	for _, entries := range [][]port.Entry{{entry}, {{Name: "x.note", Hash: "hash"}}} {
		if _, err := s.Recognition(t.Context(), entries, true, nil); err == nil {
			t.Fatal("budget/size accepted")
		}
	}
	cache.err = fs.ErrPermission
	if _, err := s.Recognition(t.Context(), []port.Entry{entry}, true, nil); !errors.Is(err, fs.ErrPermission) {
		t.Fatal(err)
	}
	cache.err = nil
	cache.status.Budget = 10
	s.Reader = porttest.NoteReader{Notebook: port.Notebook{Pages: []port.NotePage{{Number: 1}}}}
	if _, err := s.Recognition(t.Context(), []port.Entry{entry, entry}, true, nil); err != nil {
		t.Fatal(err)
	}
}

func TestSearchTextErrorsAndTypedWords(t *testing.T) {
	s, d := setup()
	s.Fetch = fetch{}
	s.Reader = porttest.NoteReader{Notebook: port.Notebook{Pages: []port.NotePage{
		{Number: 1, Text: []string{"Ty\u00adped"}}, {Number: 4, Recognition: "WORDS"},
	}}}
	s.Cloud = &porttest.Cloud{Entries: []port.Entry{{Name: "x.note", Path: "x.note"}, {Name: "skip.pdf"}}}
	found, err := s.FindText(t.Context(), "/", "typed words", nil, false)
	if err != nil || len(found) != 1 || len(found[0].Pages) != 2 {
		t.Fatal(found, err)
	}
	found, err = s.FindText(t.Context(), "local.note", "absent", nil, false)
	if err != nil || len(found) != 0 {
		t.Fatal(found, err)
	}
	for _, words := range []string{"", " ", "\u00ad"} {
		if _, err = s.FindText(t.Context(), "local.note", words, nil, false); !errors.Is(err, port.ErrOptions) {
			t.Fatal(err)
		}
	}
	s.Reader = porttest.NoteReader{Unavailable: port.ErrNotebookUnavailable}
	if _, err = s.FindText(t.Context(), "/", "word", nil, false); !errors.Is(err, port.ErrNotebookUnavailable) {
		t.Fatal(err)
	}
	s.Reader = porttest.NoteReader{Err: port.ErrNotebook}
	if _, err = s.FindText(t.Context(), "local.note", "word", nil, false); !errors.Is(err, port.ErrNotebook) {
		t.Fatal(err)
	}
	d.FailAt, d.Err = "open", fs.ErrPermission
	if _, err = s.FindText(t.Context(), "local.note", "word", nil, false); !errors.Is(err, fs.ErrPermission) {
		t.Fatal(err)
	}
	d.FailAt = ""
	s.Fetch = fetch{err: port.ErrNotFound}
	if _, err = s.FindText(t.Context(), "missing", "word", nil, false); !errors.Is(err, port.ErrNotFound) {
		t.Fatal(err)
	}
	s.Fetch = fetch{folder: true}
	s.Cloud = &porttest.Cloud{Failure: fs.ErrPermission}
	if _, err = s.FindText(t.Context(), "/", "word", nil, false); !errors.Is(err, fs.ErrPermission) {
		t.Fatal(err)
	}
}

func TestTextExportFailures(t *testing.T) {
	encode := func(n port.Notebook) ([]byte, error) { return []byte(n.Text(true)), nil }
	for _, point := range []string{"encode", "destination", "produce", "open", "unavailable", "read", "resolve", "pages", "list", "mkdir"} {
		t.Run(point, func(t *testing.T) {
			s, d := setup()
			s.Fetch = fetch{}
			opts := port.TextOptions{Extension: "md", Encode: encode}
			query := "local.note"
			switch point {
			case "encode":
				opts.Encode = func(port.Notebook) ([]byte, error) { return nil, fs.ErrPermission }
			case "unavailable":
				s.Reader = porttest.NoteReader{Unavailable: port.ErrNotebookUnavailable}
			case "read":
				s.Reader = porttest.NoteReader{Err: port.ErrNotebook}
			case "resolve":
				query = "remote"
				s.Fetch = fetch{err: port.ErrNotFound}
			case "pages":
				query = "remote"
				s.Fetch = fetch{folder: true}
				opts.Pages = []int{1}
			case "list":
				query = "remote"
				s.Fetch = fetch{folder: true}
				s.Cloud = &porttest.Cloud{Failure: fs.ErrPermission}
			case "mkdir":
				query = "remote"
				s.Fetch = fetch{folder: true}
				s.Cloud = &porttest.Cloud{}
				d.FailAt, d.Err = "mkdir", fs.ErrPermission
			default:
				d.FailAt, d.Err = point, fs.ErrPermission
			}
			if _, err := s.ExportText(t.Context(), query, "out", opts); err == nil {
				t.Fatal("failure ignored")
			}
		})
	}
	s, _ := setup()
	for _, opts := range []port.TextOptions{{}, {Extension: "pdf", Encode: encode}} {
		if _, err := s.ExportText(t.Context(), "local.note", "out", opts); !errors.Is(err, port.ErrOptions) {
			t.Fatal(err)
		}
	}
	for _, dest := range []string{"", "-"} {
		if _, err := s.ExportText(t.Context(), "local.note", dest, port.TextOptions{Extension: "txt", Encode: encode}); !errors.Is(err, port.ErrOptions) {
			t.Fatal(err)
		}
	}
}

type batchRecognitionCache struct {
	recognitionCache
	snapshots, merges, lookups int
}

func (c *batchRecognitionCache) RecognitionSnapshot() map[string]bool { c.snapshots++; return c.values }
func (c *batchRecognitionCache) Recognition(hash string) *bool {
	c.lookups++
	return c.recognitionCache.Recognition(hash)
}
func (c *batchRecognitionCache) RememberRecognitions(ctx context.Context, values map[string]bool) error {
	c.merges++
	if err := ctx.Err(); err != nil {
		return err
	}
	for hash, value := range values {
		_ = c.recognitionCache.RememberRecognition(ctx, hash, value)
	}
	return nil
}

type scanFetch struct {
	fetch
	cancel context.CancelFunc
	reads  int
}

func (f *scanFetch) Read(ctx context.Context, name string, _ port.Progress) (port.Entry, []byte, error) {
	f.reads++
	if name == "/bad.note" {
		return port.Entry{}, nil, port.ErrNotebook
	}
	if name == "/cancel.note" {
		f.cancel()
		return port.Entry{}, nil, ctx.Err()
	}
	return port.Entry{Name: name}, []byte(name), nil
}

func TestRecognitionPartialResultsAndBatchFlush(t *testing.T) {
	for _, cancelScan := range []bool{false, true} {
		t.Run(map[bool]string{false: "damaged", true: "canceled"}[cancelScan], func(t *testing.T) {
			s, _ := setup()
			cache := &batchRecognitionCache{}
			s.Index = cache
			s.Reader = porttest.NoteReader{Notebook: port.Notebook{Kind: "note", Pages: []port.NotePage{{Recognition: "yes"}}}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := &scanFetch{cancel: cancel}
			s.Fetch = f
			name := "bad.note"
			if cancelScan {
				name = "cancel.note"
			}
			entries := []port.Entry{{Name: "good.note", Path: "good.note", Hash: "good"}, {Name: name, Path: name, Hash: "bad"}, {Name: "last.note", Path: "last.note", Hash: "last"}}
			got, err := s.Recognition(ctx, entries, true, nil)
			if err == nil || len(got) != 3 || got[0].Recognition == nil || !*got[0].Recognition || got[1].Recognition != nil {
				t.Fatal(got, err)
			}
			if cancelScan {
				if !errors.Is(err, context.Canceled) || got[2].Recognition != nil {
					t.Fatal(got, err)
				}
			} else if !errors.Is(err, port.ErrNotebook) || got[2].Recognition == nil {
				t.Fatal(got, err)
			}
			if cache.snapshots != 1 || cache.merges != 1 || cache.lookups != 0 || !cache.values["good"] {
				t.Fatal(cache)
			}
		})
	}
}

type exportFetch struct {
	fetch
	entries []port.Entry
}

func (f exportFetch) Resolve(context.Context, string) (port.Entry, error) {
	return port.Entry{Path: "Top", Folder: f.folder}, nil
}
func (f exportFetch) Read(_ context.Context, name string, _ port.Progress) (port.Entry, []byte, error) {
	for _, e := range f.entries {
		if strings.TrimPrefix(name, "/") == strings.TrimPrefix(e.Path, "/") {
			return e, []byte(e.ID), nil
		}
	}
	return port.Entry{}, nil, port.ErrNotFound
}

func textExportService(entries []port.Entry, folder bool) notebooks.Service {
	return notebooks.Service{Files: localfs.Files{}, Cloud: &porttest.Cloud{Entries: entries}, Fetch: exportFetch{fetch: fetch{folder: folder}, entries: entries},
		Reader: porttest.NoteReader{Notebook: port.Notebook{Kind: "note", Pages: []port.NotePage{{Number: 1, Recognition: "synthetic text"}}}}}
}
func textExportOptions() port.TextOptions {
	return port.TextOptions{Extension: "md", Encode: func(n port.Notebook) ([]byte, error) { return []byte(n.Text(true)), nil }}
}

func TestExportTextPortableCloudPaths(t *testing.T) {
	entries := []port.Entry{
		{ID: "1", Name: "../../escape.note", Path: "Top/../../escape.note"},
		{ID: "2", Name: "/absolute.note", Path: "/absolute.note"},
		{ID: "3", Name: `C:\root\evil.note`, Path: `Top/C:\root\evil.note`},
		{ID: "4", Name: `CON.note`, Path: `Top/CON.note`},
		{ID: "10", Name: "COM¹.note", Path: "Top/COM¹.note"},
		{ID: "11", Name: "CON .note", Path: "Top/CON .note"},
		{ID: "5", Name: `a:b*?"<>|.note`, Path: `Top/a:b*?"<>|.note`},
		{ID: "6", Name: "Name.note", Path: "Top/Name.note"},
		{ID: "7", Name: "name.note", Path: "Top/name.note"},
		{ID: "8", Name: "name (7).note", Path: "Top/name (7).note"},
		{ID: "9", Name: "name (7)-2.note", Path: "Top/name (7)-2.note"},
	}
	for _, folder := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "folder"}[folder], func(t *testing.T) {
			dir := t.TempDir()
			seen := map[string]bool{}
			check := func(paths []string) {
				for _, p := range paths {
					rel, err := filepath.Rel(dir, p)
					if err != nil || !filepath.IsLocal(rel) || strings.ContainsAny(filepath.ToSlash(rel), `\:*?"<>|`) {
						t.Fatal("unsafe output", p, err)
					}
					key := strings.ToLower(rel)
					if seen[key] {
						t.Fatal("output collision", paths)
					}
					seen[key] = true
					b, err := os.ReadFile(p)
					if err != nil || !strings.Contains(string(b), "synthetic text") {
						t.Fatal(p, err)
					}
				}
			}
			if folder {
				s := textExportService(entries, true)
				paths, err := s.ExportText(t.Context(), "remote", dir, textExportOptions())
				if err != nil || len(paths) != len(entries) {
					t.Fatal(paths, err)
				}
				check(paths)
				if _, err := os.Stat(filepath.Join(dir, "_CON.md")); err != nil {
					t.Fatal("reserved Windows name", err)
				}
			} else {
				// Each notebook receives its own DIR so single-file tests do not intentionally replace one another.
				for _, e := range entries {
					one := filepath.Join(dir, e.ID)
					if err := os.Mkdir(one, 0700); err != nil {
						t.Fatal(err)
					}
					s := textExportService([]port.Entry{e}, false)
					paths, err := s.ExportText(t.Context(), "/"+strings.TrimPrefix(e.Path, "/"), one, textExportOptions())
					if err != nil || len(paths) != 1 {
						t.Fatal(paths, err)
					}
					check(paths)
				}
			}
		})
	}
}

func TestExportTextRejectsLinksInsideDirectory(t *testing.T) {
	for _, target := range []string{"file", "ancestor", "inside"} {
		t.Run(target, func(t *testing.T) {
			dir, outside := t.TempDir(), t.TempDir()
			sentinel := filepath.Join(outside, "out.md")
			if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			en := port.Entry{ID: "1", Name: "out.note", Path: "Top/out.note"}
			link, dest := filepath.Join(dir, "out.md"), sentinel
			if target != "file" {
				en.Path = "Top/link/out.note"
				link = filepath.Join(dir, "link")
				dest = outside
			}
			if target == "inside" {
				dest = filepath.Join(dir, "real")
				if err := os.Mkdir(dest, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(dest, link); err != nil {
				t.Skip(err)
			}
			s := textExportService([]port.Entry{en}, true)
			if _, err := s.ExportText(t.Context(), "remote", dir, textExportOptions()); err == nil {
				t.Fatal("symlink accepted")
			}
			if b, err := os.ReadFile(sentinel); err != nil || string(b) != "keep" {
				t.Fatal("escaped directory", err)
			}
			if _, err := os.Lstat(link); err != nil {
				t.Fatal("link changed", err)
			}
		})
	}
}

func TestFindTextUnicodeNormalization(t *testing.T) {
	for _, words := range []string{"Й café", "И\u0306 cafe\u0301"} {
		s, _ := setup()
		s.Fetch = fetch{}
		s.Reader = porttest.NoteReader{Notebook: port.Notebook{Pages: []port.NotePage{
			{Number: 1, Text: []string{"и\u0306"}}, {Number: 2, Recognition: "CAFÉ"},
		}}}
		s.Cloud = &porttest.Cloud{Entries: []port.Entry{{Name: "и\u0306.note", Path: "и\u0306.note"}}}
		found, err := s.FindText(t.Context(), "/", words, nil, false)
		if err != nil || len(found) != 1 || len(found[0].Pages) != 2 {
			t.Fatal(found, err)
		}
	}
}

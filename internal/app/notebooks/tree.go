// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package notebooks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/altessa-s/snctl/internal/app/outdir"
	"github.com/altessa-s/snctl/internal/port"
)

// ManifestName records the input identity and options of each completed PDF.
const ManifestName = outdir.RenderRecord

const statusRendered = "rendered"

type record = outdir.Entry

// RenderTree draws independent documents concurrently while preserving report order.
func (s Service) RenderTree(ctx context.Context, query, dir string, o port.RenderOptions, tree port.TreeOptions) (port.TreeReport, error) {
	if dir == "-" {
		return port.TreeReport{}, fmt.Errorf("%w: a folder needs an output directory", port.ErrOptions)
	}
	if o.NoMarks || o.DrawingPage != "" || o.Drawing || o.Source != "" || o.Format != "" && o.Format != formatPDF ||
		o.Page != 0 || o.Scale != 0 || o.Quality != 0 || tree.Jobs < 0 {
		return port.TreeReport{}, port.ErrOptions
	}
	o.Format = formatPDF
	if err := s.Renderer.Available(); err != nil {
		return port.TreeReport{}, err
	}
	top, err := s.Fetch.Resolve(ctx, query)
	if err != nil {
		return port.TreeReport{}, err
	}
	if !top.Folder {
		return port.TreeReport{}, port.ErrOptions
	}
	entries, err := s.Cloud.List(ctx, query, true)
	if err != nil {
		return port.TreeReport{}, err
	}
	if tree.All {
		entries, err = s.withTrash(ctx, entries, top.Path)
		if err != nil {
			return port.TreeReport{}, err
		}
	}

	dir = treeDirectory(dir, top)
	original := s.Files
	disk, err := outdir.Open(original, dir)
	if err != nil {
		return port.TreeReport{}, err
	}
	defer func() { _ = disk.Close() }()
	if _, err = disk.Info(filepath.Join(dir, ManifestName)); err != nil {
		return port.TreeReport{}, err
	}
	s.Files = disk
	entries = slices.DeleteFunc(entries, func(e port.Entry) bool {
		return e.Folder || !notebookName(e.Name)
	})
	slices.SortFunc(entries, func(a, b port.Entry) int { return strings.Compare(a.Path+"\x00"+a.ID, b.Path+"\x00"+b.ID) })
	source, err := outdir.Source(ctx, s.Cloud, top)
	if err != nil {
		return port.TreeReport{}, err
	}
	old, err := outdir.Load(s.Files, dir, ManifestName, source, tree.Force)
	if err != nil {
		return port.TreeReport{}, err
	}
	if tree.Incremental {
		s.pruneRecord(dir, old)
	}

	options := fmt.Sprintf("pdf/%t/%t/%t", o.NoTemplate, o.NoText, o.StrictImages)

	report := port.TreeReport{Dir: dir, Documents: make([]port.RenderedDocument, len(entries))}
	paths := treePaths(entries, top)
	jobs := tree.Jobs
	if jobs == 0 {
		jobs = 4
	}
	jobs = min(jobs, max(1, len(entries)))
	work := make(chan int)
	var wg sync.WaitGroup
	var progressMu sync.Mutex
	completed := 0
	progress := o.Progress
	o.Progress = nil
	for range jobs {
		wg.Go(func() {
			for i := range work {
				report.Documents[i] = s.treeOne(ctx, original, entries[i], dir, paths[i], o, options, old[paths[i]], tree.Incremental)
				progressMu.Lock()
				completed++
				if progress != nil {
					progress(int64(completed), int64(len(entries)))
				}
				progressMu.Unlock()
			}
		})
	}
	for i := range entries {
		work <- i
	}
	close(work)
	wg.Wait()
	summarize(&report)
	return report, s.record(ctx, source, dir, report, entries, paths, options, old)
}

func (s Service) pruneRecord(dir string, old map[string]record) {
	for name := range old {
		if _, err := s.Files.ModTime(filepath.Join(dir, name)); err != nil {
			delete(old, name)
		}
	}
}

func (s Service) record(
	ctx context.Context, source port.OutputSource, dir string, report port.TreeReport,
	entries []port.Entry, paths []string, options string, old map[string]record,
) error {
	var err error
	fresh := old
	var failures []error
	for i, d := range report.Documents {
		if d.Error != "" {
			failures = append(failures, fmt.Errorf("%s: %s", d.Document, d.Error))
			continue
		}
		e := entries[i]
		if d.Status == statusRendered {
			delete(fresh, paths[i])
		}
		if e.Modified != nil && e.Hash != "" && d.SkippedPages == 0 {
			fresh[paths[i]] = record{ID: e.ID, Hash: e.Hash, Options: options, Modified: *e.Modified}
		}
	}
	// Completed outputs remain reusable after an interrupt; no unfinished file is recorded.
	if report.Rendered > 0 {
		err = outdir.Store(ctx, s.Files, dir, ManifestName, source, fresh)
	}
	return errors.Join(append(failures, err, ctx.Err())...)
}

func (s Service) treeOne(
	ctx context.Context, original port.NotebookFiles, e port.Entry, dir, rel string, o port.RenderOptions, options string, old record, incremental bool,
) port.RenderedDocument {
	dest := filepath.Join(dir, rel)
	d := port.RenderedDocument{Document: e.Path, Status: "failed", Path: dest}
	fail := func(err error) port.RenderedDocument { d.Error = err.Error(); return d }
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if incremental && e.Hash != "" && e.Modified != nil && old.ID == e.ID && old.Hash == e.Hash && old.Options == options && old.Modified.Equal(*e.Modified) {
		if t, err := s.Files.ModTime(dest); err == nil && t.Equal(*e.Modified) {
			d.Status = "unchanged"
			return d
		}
	}
	if err := s.Files.MkdirAll(filepath.Dir(dest)); err != nil {
		return fail(err)
	}
	// Download by the resolved entry so recycled identifiers do not need path lookup.
	file, err := s.Cloud.Download(ctx, e, nil)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = original.Remove(file) }()
	r, err := original.Open(file)
	if err != nil {
		return fail(err)
	}
	data, err := io.ReadAll(r)
	if err = errors.Join(err, r.Close()); err != nil {
		return fail(err)
	}
	d.RenderReport, err = s.renderData(ctx, e.Name, data, dest, o)
	if err != nil {
		return fail(err)
	}
	if e.Modified != nil {
		if err = s.Files.SetModTime(dest, *e.Modified); err != nil {
			return fail(err)
		}
	}
	d.Status = statusRendered
	return d
}

func treePaths(entries []port.Entry, top port.Entry) []string {
	paths := outdir.Paths(entries, top.Path, nil, func(name string) string { return strings.TrimSuffix(name, path.Ext(name)) + ".pdf" })
	for i, rel := range paths {
		paths[i] = filepath.FromSlash(rel)
	}
	return paths
}

func (s Service) withTrash(ctx context.Context, entries []port.Entry, top string) ([]port.Entry, error) {
	trash, err := s.Cloud.Trash(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.ID] = true
	}
	root := strings.Trim(top, "/") == ""
	for _, e := range trash {
		if seen[e.ID] || e.Folder || !notebookName(e.Name) {
			continue
		}
		if e.Path == "" {
			if !root {
				return nil, fmt.Errorf("%w: the recycle bin has no original paths; use render / --all to place recycled notebooks under .trash", port.ErrOptions)
			}
			e.Path = ".trash/" + e.Name
		}
		if root || strings.HasPrefix(e.Path, strings.TrimSuffix(top, "/")+"/") {
			entries = append(entries, e)
			seen[e.ID] = true
		}
	}

	return entries, nil
}

func notebookName(name string) bool {
	ext := notebookExtension(name)
	return ext == noteExtension || ext == ".mark"
}

func notebookExtension(name string) string { return strings.ToLower(path.Ext(name)) }

func summarize(report *port.TreeReport) {
	for _, d := range report.Documents {
		switch d.Status {
		case statusRendered:
			report.Rendered++
		case "unchanged":
			report.Unchanged++
		default:
			report.Failed++
		}
	}
}

func treeDirectory(dir string, top port.Entry) string {
	if dir != "" {
		return dir
	}
	if top.Path == "/" || top.Path == "" {
		return "supernote"
	}
	return safeName(top.Name)
}

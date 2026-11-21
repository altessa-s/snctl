// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package files

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/altessa-s/snctl/internal/app/outdir"
	"github.com/altessa-s/snctl/internal/port"
)

// errNoDisk is a wiring defect: a folder transfer without local files.
var errNoDisk = errors.New("local files are unavailable")

// Mirror downloads every file below a cloud folder into dir, keeping the
// tree. Each file appears whole or not at all. The record in dir lists the
// files it wrote with the cloud entry they came from; it is written once at
// the end, even after an interruption, and lists only files already in
// place, so it always matches the directory.
func (s Service) Mirror(ctx context.Context, query, dir string, o port.MirrorOptions) (port.TransferReport, error) {
	report := port.TransferReport{Items: []port.TransferItem{}}
	switch {
	case dir == "" || dir == "-":
		return report, fmt.Errorf("%w: mget needs -o with a directory", port.ErrArgument)
	case o.Jobs < 0:
		return report, fmt.Errorf("%w: --jobs must not be negative", port.ErrArgument)
	case s.Disk == nil:
		return report, errNoDisk
	}
	src, err := s.source(ctx, query)
	if err != nil {
		return report, err
	}
	cloud, files := src.cloud, src.files
	rels := outdir.Paths(files, src.top.Path, src.folders, func(name string) string { return name })
	disk, err := outdir.Open(s.Disk, dir)
	if err != nil {
		return report, err
	}
	defer func() { _ = disk.Close() }()
	if _, err = disk.Info(filepath.Join(dir, outdir.MgetRecord)); err != nil {
		return report, err
	}
	source, err := outdir.Source(ctx, cloud, src.top)
	if err != nil {
		return report, err
	}
	old, err := outdir.Load(disk, dir, outdir.MgetRecord, source, o.Force)
	if err != nil {
		return report, err
	}
	stale := staleKeys(old, rels)
	if o.Delete && !o.Force {
		if why := outdir.Guard(len(stale), len(old), len(files) == 0); why != "" {
			return report, fmt.Errorf("%w: --delete: %s; nothing was changed; --force deletes anyway", port.ErrRefused, why)
		}
	}
	folderItems := makeFolders(disk, dir, src)
	report.Items = make([]port.TransferItem, len(files))
	var todo []int
	var total int64
	for i, e := range files {
		local := filepath.Join(dir, filepath.FromSlash(rels[i]))
		report.Items[i] = port.TransferItem{Cloud: e.Path, Local: local, Size: size(e)}
		if o.Incremental && s.unchanged(disk, old[rels[i]], e, local) {
			report.Items[i].Status = port.StatusUnchanged
			continue
		}
		todo = append(todo, i)
		total += size(e)
	}
	progress := newTracker(o.Progress, len(todo), total)
	each(ctx, todo, o.Jobs, func(ctx context.Context, i int) {
		report.Items[i] = s.fetch(ctx, disk, cloud, files[i], report.Items[i], progress.file(i))
		progress.finish()
	})
	for _, i := range todo {
		if report.Items[i].Status == "" {
			report.Items[i] = failed(report.Items[i], cmp.Or(ctx.Err(), errors.New("not attempted")))
		}
	}
	fresh := maps.Clone(old)
	for i, item := range report.Items {
		if e := files[i]; item.Status == port.StatusDownloaded || item.Status == port.StatusCached {
			delete(fresh, rels[i])
			if e.Modified != nil && e.Hash != "" {
				fresh[rels[i]] = outdir.Entry{ID: e.ID, Hash: e.Hash, Modified: *e.Modified, Size: size(e)}
			}
		}
	}
	report.Items = append(report.Items, folderItems...)
	if o.Delete && ctx.Err() == nil {
		report.Items = append(report.Items, s.prune(disk, dir, stale, fresh)...)
	}
	report.Count()
	var recordErr error
	if !maps.Equal(fresh, old) || len(old) == 0 {
		recordErr = outdir.Store(ctx, disk, dir, outdir.MgetRecord, source, fresh)
	}
	return report, errors.Join(outcome(ctx, report, "files"), recordErr)
}

// folderSource is a cloud folder as one listing shows it.
type folderSource struct {
	cloud   port.Files
	top     port.Entry
	files   []port.Entry
	folders []string
}

// source lists the folder query names, "/" for everything, in path order.
func (s Service) source(ctx context.Context, query string) (folderSource, error) {
	cloud, err := s.view(ctx)
	if err != nil {
		return folderSource{}, err
	}
	src := folderSource{cloud: cloud, top: port.Entry{Folder: true}}
	if clean(query) != "" {
		if src.top, err = cloud.Stat(ctx, query); err != nil {
			return folderSource{}, err
		}
	}
	if !src.top.Folder {
		return folderSource{}, fmt.Errorf("%w: %s is a file; get downloads one file", port.ErrArgument, query)
	}
	entries, err := cloud.List(ctx, query, true)
	if err != nil {
		return folderSource{}, err
	}
	for _, e := range entries {
		if e.Folder {
			src.folders = append(src.folders, e.Path)
		} else {
			src.files = append(src.files, e)
		}
	}
	slices.SortFunc(src.files, func(a, b port.Entry) int { return cmp.Or(strings.Compare(a.Path, b.Path), strings.Compare(a.ID, b.ID)) })
	return src, nil
}

// staleKeys are the recorded files the plan no longer produces, in order.
func staleKeys(old map[string]outdir.Entry, rels []string) []string {
	planned := map[string]bool{}
	for _, rel := range rels {
		planned[rel] = true
	}
	var stale []string
	for key := range old {
		if !planned[key] {
			stale = append(stale, key)
		}
	}
	slices.Sort(stale)
	return stale
}

// unchanged reports whether the file at local is the one the record says
// snctl wrote from this cloud entry as it is now: same identifier, content
// hash and modification time in the cloud, same size and modification time
// on disk.
func (s Service) unchanged(disk *outdir.Files, r outdir.Entry, e port.Entry, local string) bool {
	if r.ID != e.ID || e.Hash == "" || r.Hash != e.Hash || e.Modified == nil || !r.Modified.Equal(*e.Modified) {
		return false
	}
	info, err := disk.Info(local)
	return err == nil && info.Regular && info.Size == r.Size && outdir.SameTime(info.Modified, *e.Modified)
}

// fetch downloads one file, checked against its hash by the cloud adapter,
// and installs it atomically with the cloud's modification time. Files
// without one are not recorded, because --incremental could not judge them.
func (s Service) fetch(ctx context.Context, disk *outdir.Files, cloud port.Files, e port.Entry, item port.TransferItem, p port.Progress) port.TransferItem {
	if err := ctx.Err(); err != nil {
		return failed(item, err)
	}
	if info, err := disk.Info(item.Local); err != nil || info.Exists && !info.Regular {
		if err == nil {
			err = errors.New("a directory or link has this name")
		}
		return failed(item, err)
	}
	if err := disk.MkdirAll(filepath.Dir(item.Local)); err != nil {
		return failed(item, err)
	}
	file, cached, err := download(ctx, cloud, e, p)
	if err != nil {
		return failed(item, err)
	}
	defer func() { _ = s.Disk.Remove(file) }()
	if err = ctx.Err(); err != nil {
		return failed(item, err)
	}
	if err = disk.Copy(ctx, file, item.Local); err != nil {
		return failed(item, err)
	}
	item.Status = port.StatusDownloaded
	if cached {
		item.Status = port.StatusCached
	}
	if e.Modified == nil || e.Hash == "" {
		item.Reason = "not recorded: the cloud gave no content hash or time"
		return item
	}
	if err = disk.SetModTime(item.Local, *e.Modified); err != nil {
		return failed(item, err)
	}
	return item
}

// prune removes the recorded files the cloud folder no longer holds. A file
// changed since snctl wrote it is kept, and no longer recorded: it is the
// person's now. Only paths inside dir are ever considered.
func (s Service) prune(disk *outdir.Files, dir string, stale []string, fresh map[string]outdir.Entry) []port.TransferItem {
	var items []port.TransferItem
	for _, key := range stale {
		r := fresh[key]
		delete(fresh, key)
		local := filepath.Join(dir, filepath.FromSlash(key))
		item := port.TransferItem{Local: local, Size: r.Size}
		info, err := disk.Info(local)
		switch {
		case err != nil:
			items = append(items, failed(item, err))
		case !info.Exists:
		case !info.Regular || info.Size != r.Size || !outdir.SameTime(info.Modified, r.Modified):
			item.Status, item.Reason = port.StatusKept, "changed since snctl wrote it"
			items = append(items, item)
		default:
			if err = disk.Remove(local); err != nil {
				fresh[key] = r
				items = append(items, failed(item, err))
				continue
			}
			item.Status = port.StatusDeleted
			items = append(items, item)
		}
	}
	return items
}

func makeFolders(disk *outdir.Files, dir string, src folderSource) []port.TransferItem {
	var items []port.TransferItem
	for _, folder := range src.folders {
		local := filepath.Join(dir, filepath.FromSlash(outdir.Folder(folder, src.top.Path)))
		if err := disk.MkdirAll(local); err != nil {
			items = append(items, failed(port.TransferItem{Cloud: folder, Local: local, Folder: true}, err))
		}
	}
	return items
}

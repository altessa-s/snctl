// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package files

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/altessa-s/snctl/internal/port"

	"golang.org/x/text/unicode/norm"
)

// view is the listing one invocation works from: a snapshot when the cloud
// adapter offers one, so that planning lookups share one full-tree request. Write checks
// deliberately bypass this snapshot after confirmation and after writes.
func (s Service) view(ctx context.Context) (port.Files, error) {
	if batch, ok := ctx.Value(writeBatchKey{}).(*writeBatch); ok {
		return batch, nil
	}
	if snapshots, ok := s.Cloud.(port.FileSnapshot); ok {
		return snapshots.Snapshot(ctx)
	}
	return s.Cloud, nil
}

func clean(name string) string { return strings.Trim(path.Clean("/"+name), "/") }

// Move resolves the source and destination against one invocation-local listing.
func (s Service) Move(ctx context.Context, source, destination string) error {
	return s.move(ctx, source, destination, true)
}

// Remove checks an already planned entry before a raw deletion. Mirror handles
// its legacy companions separately and must retain the planned identity.
func (s Service) Remove(ctx context.Context, expected port.Entry, permanent bool) error {
	cloud, err := s.view(ctx)
	if err != nil {
		return err
	}
	current, err := changeSource(ctx, cloud, expected.Path, false)
	if err != nil {
		return err
	}
	if !sameEntry(current, expected) {
		return fmt.Errorf("%w: source changed: %s (hash %q); now %s (hash %q); next pass will re-plan", port.ErrConflict,
			visibleEntries([]port.Entry{expected}), expected.Hash, visibleEntries([]port.Entry{current}), current.Hash)
	}
	_, err = executeChanges(ctx, cloud, []port.FileChange{{Entry: current}}, port.ChangeOptions{}, "deleted", permanent)
	return err
}

// Rename keeps a full destination path within the source folder.
func (s Service) Rename(ctx context.Context, source, destination string) error {
	if norm.NFC.String(clean(path.Dir(source))) != norm.NFC.String(clean(path.Dir(destination))) {
		return fmt.Errorf("%w: rename keeps the source folder", port.ErrArgument)
	}
	return s.move(ctx, source, destination, false)
}

func (s Service) move(ctx context.Context, source, destination string, into bool) error {
	cloud, err := s.view(ctx)
	if err != nil {
		return err
	}
	entry, destination, err := moveTarget(ctx, cloud, source, destination, into)
	if err != nil {
		return err
	}
	if !into && path.Dir(entry.Path) != path.Dir(destination) {
		return fmt.Errorf("%w: rename keeps the source folder", port.ErrArgument)
	}
	_, err = executeChanges(ctx, cloud, []port.FileChange{{Entry: entry, Destination: destination}}, port.ChangeOptions{}, "moved", false)
	return err
}

func moveTarget(ctx context.Context, cloud port.Files, source, destination string, into bool, exact ...bool) (port.Entry, string, error) {
	entry, err := changeSource(ctx, cloud, source, len(exact) > 0 && exact[0])
	if err != nil {
		return entry, "", err
	}
	destination = clean(destination)
	dest, statErr := cloud.Stat(ctx, destination)
	if statErr == nil && dest.Folder && into {
		destination = path.Join(dest.Path, entry.Name)
		dest, statErr = cloud.Stat(ctx, destination)
	}
	if statErr == nil {
		if err = sameMoveSource(ctx, cloud, entry, dest, destination); err != nil {
			return entry, "", err
		}
	} else if !errors.Is(statErr, port.ErrNotFound) {
		return entry, "", statErr
	}
	if parentName := path.Dir(destination); parentName != "." {
		parent, err := cloud.Stat(ctx, parentName)
		if err != nil {
			return entry, "", err
		}
		if !parent.Folder {
			return entry, "", port.ErrConflict
		}
		destination = path.Join(parent.Path, path.Base(destination))
	}
	if entry.Folder && (destination == entry.Path || strings.HasPrefix(destination, entry.Path+"/")) {
		return entry, "", port.ErrConflict
	}
	return entry, destination, nil
}

// sameMoveSource permits a spelling change only when the source is the sole
// NFC-equivalent entry. An exact match must not conceal another twin.
func sameMoveSource(ctx context.Context, cloud port.Files, source, existing port.Entry, destination string) error {
	if source.ID != existing.ID || source.Path != existing.Path || path.Base(destination) == path.Base(source.Path) {
		return port.ErrConflict
	}
	entries, err := cloud.List(ctx, path.Dir(source.Path), false)
	if err != nil {
		return err
	}
	key := norm.NFC.String(source.Path)
	count := 0
	for _, e := range entries {
		if norm.NFC.String(e.Path) == key {
			count++
		}
	}
	if count != 1 {
		return port.ErrConflict
	}
	return nil
}

// Mkdir creates one folder, or with parents every missing folder down to it,
// accepting folders already there. Planning shares one listing; confirmed
// changes re-check parents and verify every created name with fresh listings.
func (s Service) Mkdir(ctx context.Context, query string, parents bool, options ...port.ChangeOptions) (port.Entry, error) {
	cloud, err := s.view(ctx)
	if err != nil {
		return port.Entry{}, err
	}
	target := clean(query)
	if !parents {
		target, err = destinationName(ctx, cloud, target)
		if err != nil {
			return port.Entry{}, err
		}
	}
	if target == "" {
		return port.Entry{}, fmt.Errorf("%w: mkdir needs a folder path", port.ErrArgument)
	}
	var last port.Entry
	var names []string
	current := ""
	for part := range strings.SplitSeq(target, "/") {
		current = path.Join(current, part)
		if !parents && current != target {
			continue
		}
		e, statErr := cloud.Stat(ctx, current)
		switch {
		case statErr == nil && (!parents || !e.Folder):
			return port.Entry{}, fmt.Errorf("%w: %q already exists", port.ErrConflict, current)
		case statErr == nil:
			current = e.Path
			last = e
		case !errors.Is(statErr, port.ErrNotFound):
			return port.Entry{}, statErr
		default:
			names = append(names, current)
		}
	}
	if len(names) == 0 {
		return last, nil
	}
	check, err := planCheck(ctx, cloud, names)
	if err != nil {
		return port.Entry{}, err
	}
	for _, name := range names {
		if len(check.names[name]) > 0 {
			return port.Entry{}, fmt.Errorf("%w: %s", port.ErrConflict, visibleEntries(check.names[name]))
		}
	}
	var o port.ChangeOptions
	if len(options) > 0 {
		o = options[0]
	}
	if o.DryRun {
		return port.Entry{Path: target, Name: path.Base(target), Folder: true}, nil
	}
	if o.Confirm != nil {
		plan := make([]port.FileChange, 0, len(names))
		for _, name := range names {
			plan = append(plan, port.FileChange{Destination: name})
		}
		if err = o.Confirm(plan); err != nil {
			return port.Entry{}, err
		}
	}
	if err = check.recheck(ctx, cloud); err != nil {
		return port.Entry{}, err
	}
	var written []port.Entry
	for _, name := range names {
		last, err = cloud.Mkdir(ctx, name)
		if err != nil {
			return last, errors.Join(err, verifyWritten(ctx, cloud, written))
		}
		written = append(written, last)
	}
	return last, verifyWritten(ctx, cloud, written)
}

// Usage sums declared file sizes; absent sizes contribute zero rather than invented bytes.
func (s Service) Usage(ctx context.Context, query string, depth int, all bool) (port.UsageReport, error) {
	report := port.UsageReport{Folders: []port.FolderUsage{}}
	if depth < 0 {
		return report, fmt.Errorf("%w: --depth must not be negative", port.ErrArgument)
	}
	root := clean(query)
	if all && root != "" {
		return report, fmt.Errorf("%w: --all needs / because recycled entries have no original paths", port.ErrArgument)
	}
	if s.Account == nil {
		return report, errors.New("account capacity is unavailable")
	}
	entries, err := s.Cloud.List(ctx, query, true)
	if err != nil {
		return report, err
	}
	for _, u := range folderTotals(entries, root) {
		if u.Depth <= depth {
			report.Folders = append(report.Folders, *u)
		}
	}
	slices.SortFunc(report.Folders, func(a, b port.FolderUsage) int { return strings.Compare(a.Path, b.Path) })
	if all {
		trash, trashErr := s.Cloud.Trash(ctx)
		if trashErr != nil {
			return report, trashErr
		}
		for _, e := range trash {
			if !e.Folder && e.Size != nil {
				report.Recycled += *e.Size
			}
		}
	}
	account, err := s.Account.Info(ctx)
	report.Used, report.Capacity = account.Used, account.Total
	return report, err
}

// folderTotals adds every file's declared size to the root and to each
// folder above it.
func folderTotals(entries []port.Entry, root string) map[string]*port.FolderUsage {
	totals := map[string]*port.FolderUsage{root: {Path: "/" + root}}
	for _, e := range entries {
		if e.Folder {
			totals[e.Path] = &port.FolderUsage{Path: "/" + e.Path, Depth: strings.Count(strings.TrimPrefix(e.Path, root+"/"), "/") + 1}
		}
	}
	for _, e := range entries {
		if e.Folder {
			continue
		}
		for folder, u := range totals {
			if folder == root || strings.HasPrefix(e.Path, folder+"/") {
				u.Files++
				u.Bytes += size(e)
			}
		}
	}
	return totals
}

// minGroup is the fewest files that make a group of duplicates.
const minGroup = 2

// Duplicates compares full-file hashes and sizes, not source-document ancestry.
func (s Service) Duplicates(ctx context.Context, query string, raw bool) ([]port.DuplicateGroup, error) {
	entries, err := s.Cloud.List(ctx, query, true)
	if err != nil {
		return nil, err
	}
	entries = port.FoldMarks(entries, raw)
	type key struct {
		hash string
		size int64
	}
	groups := map[key][]port.Entry{}
	for _, e := range entries {
		if !e.Folder && e.Hash != "" && e.Size != nil && *e.Size > 0 {
			k := key{e.Hash, *e.Size}
			groups[k] = append(groups[k], e)
		}
	}
	result := []port.DuplicateGroup{}
	for k, entries := range groups {
		if len(entries) < minGroup {
			continue
		}
		slices.SortFunc(entries, func(a, b port.Entry) int { return strings.Compare(a.Path, b.Path) })
		result = append(result, port.DuplicateGroup{Hash: k.hash, Size: k.size, Files: entries})
	}
	slices.SortFunc(result, func(a, b port.DuplicateGroup) int { return strings.Compare(a.Files[0].Path, b.Files[0].Path) })
	return result, nil
}

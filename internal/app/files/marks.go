// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package files

import (
	"context"
	"errors"
	"fmt"
	"path"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"

	"golang.org/x/text/unicode/norm"
)

// companion resolves only full-name sidecars; legacy matching belongs to mirror.
func companion(ctx context.Context, cloud port.Files, entry port.Entry) (port.Entry, bool, error) {
	if entry.Folder || !domain.DocumentName(entry.Name) {
		return port.Entry{}, false, nil
	}
	entries, err := cloud.List(ctx, path.Dir(entry.Path), false)
	if err != nil {
		return port.Entry{}, false, err
	}
	mark, counts := port.SidecarOf(entries, entry.Path)
	if counts.Documents != 1 || counts.Marks > 1 {
		return port.Entry{}, false, fmt.Errorf("%q: ambiguous document or annotation sidecar: %s; %s: %w", entry.Path,
			visibleEntries(equivalents(entries, entry.Path)), visibleEntries(equivalents(entries, entry.Path+".mark")), port.ErrConflict)
	}
	if counts.Files == 0 {
		return port.Entry{}, false, nil
	}
	return mark, true, nil
}

// MoveWithMarks plans and confirms both files against one snapshot. Move itself
// stays a raw operation because mirror already reconciles companions separately.
func (s Service) MoveWithMarks(
	ctx context.Context, source, destination string, rename bool, o port.ChangeOptions,
) ([]port.FileChange, error) {
	if rename && norm.NFC.String(clean(path.Dir(source))) != norm.NFC.String(clean(path.Dir(destination))) {
		return nil, fmt.Errorf("%w: rename keeps the source folder", port.ErrArgument)
	}
	cloud, err := s.view(ctx)
	if err != nil {
		return nil, err
	}
	entry, destination, err := moveTarget(ctx, cloud, source, destination, !rename, o.Exact)
	if err != nil {
		return nil, err
	}
	if rename && path.Dir(entry.Path) != path.Dir(destination) {
		return nil, fmt.Errorf("%w: rename keeps the source folder", port.ErrArgument)
	}
	plan := []port.FileChange{{Entry: entry, Destination: destination}}
	mark, found, err := companion(ctx, cloud, entry)
	if err != nil {
		return nil, err
	}
	if found {
		// A cross-form sidecar can already have the requested destination bytes.
		if mark.Path != destination+".mark" {
			if _, _, err = moveTarget(ctx, cloud, mark.Path, destination+".mark", false); err != nil {
				return nil, fmt.Errorf("%s: %w", destination+".mark", err)
			}
			plan = append(plan, port.FileChange{Entry: mark, Destination: destination + ".mark"})
		}
	} else if !entry.Folder && (domain.DocumentName(entry.Name) || domain.DocumentName(path.Base(destination))) {
		if _, err = cloud.Stat(ctx, destination+".mark"); !errors.Is(err, port.ErrNotFound) {
			if err == nil {
				err = port.ErrConflict
			}
			return nil, fmt.Errorf("%s: %w", destination+".mark", err)
		}
	}
	return executeChanges(ctx, cloud, plan, o, "moved", false)
}

// RemoveWithMarks applies the same recycle/permanent choice to both files.
func (s Service) RemoveWithMarks(ctx context.Context, query string, permanent bool, o port.ChangeOptions) ([]port.FileChange, error) {
	cloud, err := s.view(ctx)
	if err != nil {
		return nil, err
	}
	entry, err := changeSource(ctx, cloud, query, o.Exact)
	if err != nil {
		return nil, err
	}
	plan := []port.FileChange{{Entry: entry}}
	mark, found, err := companion(ctx, cloud, entry)
	if err != nil {
		return nil, err
	}
	if found {
		plan = append(plan, port.FileChange{Entry: mark})
	}
	verb := "recycled"
	if permanent {
		verb = "permanently deleted"
	}
	return executeChanges(ctx, cloud, plan, o, verb, permanent)
}

func executeChanges(
	ctx context.Context, cloud port.Files, plan []port.FileChange, o port.ChangeOptions, verb string, permanent bool,
) ([]port.FileChange, error) {
	if o.DryRun {
		return plan, nil
	}
	names := make([]string, 0, len(plan)*2)
	for _, change := range plan {
		names = append(names, change.Entry.Path)
		if change.Destination != "" {
			names = append(names, change.Destination)
		}
	}
	// Absent sidecars are also a dependency: a document must not gain a
	// companion while the user is confirming its operation.
	for _, change := range plan {
		if !change.Entry.Folder && domain.DocumentName(change.Entry.Name) {
			names = append(names, change.Entry.Path+".mark")
		}
		if domain.DocumentName(path.Base(change.Destination)) {
			names = append(names, change.Destination+".mark")
		}
	}
	check, err := planCheck(ctx, cloud, names)
	if err != nil {
		return nil, err
	}
	if o.Confirm != nil {
		if err := o.Confirm(plan); err != nil {
			return nil, err
		}
	}
	if err := check.recheck(ctx, cloud); err != nil {
		return nil, err
	}
	var written []port.Entry
	for i, change := range plan {
		err := destructiveCheck(ctx, cloud, change.Entry, change.Destination)
		if err == nil {
			if change.Destination != "" {
				err = cloud.Move(ctx, change.Entry, change.Destination)
			} else {
				err = cloud.Delete(ctx, change.Entry, permanent)
			}
		}
		if err != nil {
			if i > 0 {
				partial := fmt.Errorf("%s %s, but %s was not %s: %w", plan[0].Entry.Path, verb, change.Entry.Path, verb, err)
				return plan[:i], errors.Join(verifyWritten(ctx, cloud, written), partial)
			}
			return nil, err
		}
		if change.Destination != "" {
			e := change.Entry
			e.Path, e.Name = change.Destination, path.Base(change.Destination)
			written = append(written, e)
		}
	}
	return plan, verifyWritten(ctx, cloud, written)
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package files

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"

	"golang.org/x/text/unicode/norm"
)

// RestoreWithMarks checks original destinations before confirmation and before
// each restore, then verifies the restored names without rolling back changes.
func (s Service) RestoreWithMarks(ctx context.Context, id string, o port.ChangeOptions) ([]port.FileChange, error) {
	cloud, err := s.view(ctx)
	if err != nil {
		return nil, err
	}
	resolver, ok := cloud.(port.FileRestoreTarget)
	if !ok {
		return nil, fmt.Errorf("%w: cannot resolve recycle-bin destinations", port.ErrConflict)
	}
	trash, err := cloud.Trash(ctx)
	if err != nil {
		return nil, err
	}
	plan, err := planTrashRestore(ctx, resolver, trash, id)
	if err != nil {
		return nil, err
	}
	entry := plan[0].Entry
	names := []string{entry.Path}
	if !entry.Folder && domain.DocumentName(entry.Name) {
		names = append(names, entry.Path+".mark")
	}
	for _, change := range plan[1:] {
		names = append(names, change.Destination)
	}
	check, err := planCheck(ctx, cloud, names)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if len(check.names[name]) > 0 {
			return nil, fmt.Errorf("%w: restore %q (ID %s) would collide with %s", port.ErrConflict, name, id, visibleEntries(check.names[name]))
		}
	}
	if o.DryRun {
		return plan, nil
	}
	if o.Confirm != nil {
		if err = o.Confirm(plan); err != nil {
			return nil, err
		}
	}
	if err = recheckTrashRestore(ctx, cloud, resolver, plan); err != nil {
		return nil, err
	}
	if err = check.recheck(ctx, cloud); err != nil {
		return nil, err
	}
	var written []port.Entry
	for i, change := range plan {
		// Keep the original expected parent and absent name, rather than
		// accepting a new planning baseline after confirmation or earlier writes.
		next := writeCheck{names: map[string][]port.Entry{change.Destination: nil}, parents: check.parents}
		if i == 0 {
			next.names = check.names
		}
		err = restoreOne(ctx, cloud, resolver, change, next)
		if err != nil {
			if i > 0 {
				err = fmt.Errorf("%s restored, but %s was not restored: %w", plan[0].Destination, change.Destination, err)
			}
			return plan[:i], errors.Join(err, verifyRestored(ctx, cloud, written))
		}
		written = append(written, change.Entry)
	}
	return plan, verifyRestored(ctx, cloud, written)
}

func restoreOne(ctx context.Context, cloud port.Files, resolver port.FileRestoreTarget, change port.FileChange, check writeCheck) error {
	current, err := resolver.RestoreTarget(ctx, change.Entry)
	if err != nil {
		return fmt.Errorf("%w: cannot recheck restore target ID %s: %w", port.ErrConflict, change.Entry.ID, err)
	}
	if current.Path != change.Destination || current.ParentID != change.Entry.ParentID {
		return fmt.Errorf("%w: restore target changed for ID %s; planned %q; now %q", port.ErrConflict, change.Entry.ID, change.Destination, current.Path)
	}
	if err = check.recheckDestructive(ctx, cloud); err != nil {
		return err
	}
	return cloud.Restore(ctx, change.Entry.ID)
}

func recheckTrashRestore(ctx context.Context, cloud port.Files, resolver port.FileRestoreTarget, plan []port.FileChange) error {
	trash, err := cloud.Trash(ctx)
	if err != nil {
		return fmt.Errorf("%w: cannot recheck recycle bin; nothing written: %w", port.ErrConflict, err)
	}
	now, err := planTrashRestore(ctx, resolver, trash, plan[0].Entry.ID)
	if err != nil {
		return fmt.Errorf("%w: recycle bin changed; nothing written: %w", port.ErrConflict, err)
	}
	if !slices.EqualFunc(plan, now, func(a, b port.FileChange) bool { return sameEntry(a.Entry, b.Entry) && a.Destination == b.Destination }) {
		return fmt.Errorf("%w: recycled source or annotations changed; nothing written", port.ErrConflict)
	}
	return nil
}

func planTrashRestore(ctx context.Context, resolver port.FileRestoreTarget, trash []port.Entry, id string) ([]port.FileChange, error) {
	index := slices.IndexFunc(trash, func(e port.Entry) bool { return e.ID == id })
	if index < 0 {
		return nil, fmt.Errorf("recycled ID %s: %w", id, port.ErrNotFound)
	}
	entry, err := resolver.RestoreTarget(ctx, trash[index])
	if err != nil {
		return nil, err
	}
	return restorePlan(ctx, resolver, trash, entry)
}

func restorePlan(ctx context.Context, resolver port.FileRestoreTarget, trash []port.Entry, entry port.Entry) ([]port.FileChange, error) {
	plan := []port.FileChange{{Entry: entry, Destination: entry.Path}}
	if entry.Folder || !domain.DocumentName(entry.Name) {
		return plan, nil
	}
	siblings := []port.Entry{entry}
	for _, e := range trash {
		if e.ID == entry.ID || norm.NFC.String(e.Name) != norm.NFC.String(entry.Name) && norm.NFC.String(e.Name) != norm.NFC.String(entry.Name+".mark") {
			continue
		}
		target, err := resolver.RestoreTarget(ctx, e)
		if err != nil {
			return nil, err
		}
		if path.Dir(target.Path) == path.Dir(entry.Path) {
			siblings = append(siblings, target)
		}
	}
	mark, counts := port.SidecarOf(siblings, entry.Path)
	if counts.Documents != 1 || counts.Marks > 1 || counts.Marks != counts.Files {
		return nil, fmt.Errorf("%w: ambiguous recycled document or annotation sidecar: %s", port.ErrConflict, visibleEntries(siblings))
	}
	if counts.Files == 1 {
		plan = append(plan, port.FileChange{Entry: mark, Destination: mark.Path})
	}
	return plan, nil
}

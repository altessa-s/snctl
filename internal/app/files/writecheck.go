// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package files

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/altessa-s/snctl/internal/port"

	"golang.org/x/text/unicode/norm"
)

// writeCheck retains only the names a plan depends on, including absent names.
// Unrelated sibling changes must not invalidate a confirmed plan.
type writeCheck struct {
	names   map[string][]port.Entry
	parents map[string]port.Entry
}

type parentListing struct {
	entry    port.Entry
	children []port.Entry
}

func equivalents(entries []port.Entry, name string) []port.Entry {
	var matches []port.Entry
	for _, e := range entries {
		if path.Dir(e.Path) == path.Dir(name) && norm.NFC.String(path.Base(e.Path)) == norm.NFC.String(path.Base(name)) {
			matches = append(matches, e)
		}
	}
	slices.SortFunc(matches, func(a, b port.Entry) int {
		if n := strings.Compare(a.Path, b.Path); n != 0 {
			return n
		}
		return strings.Compare(a.ID, b.ID)
	})
	return matches
}

func visibleEntries(entries []port.Entry) string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, fmt.Sprintf("%q (ID %s)", e.Path, e.ID))
	}
	return strings.Join(names, ", ")
}

func entryVersions(entries []port.Entry) string {
	versions := make([]string, 0, len(entries))
	for _, e := range entries {
		versions = append(versions, fmt.Sprintf("%s hash %q", visibleEntries([]port.Entry{e}), e.Hash))
	}
	return strings.Join(versions, ", ")
}

func listParents(ctx context.Context, cloud port.Files, names []string, fresh bool) (map[string]parentListing, error) {
	if fresh && len(names) > 0 {
		var err error
		cloud, fresh, err = recheckView(ctx, cloud)
		if err != nil {
			return nil, err
		}
	}
	return readParents(ctx, cloud, names, fresh)
}

// readParents can bypass the shared batch snapshot for each destructive write.
func readParents(ctx context.Context, cloud port.Files, names []string, fresh bool) (map[string]parentListing, error) {
	if batch, ok := cloud.(*writeBatch); ok {
		cloud = batch.Files
	}
	parents := map[string]parentListing{}
	for _, name := range names {
		parent := path.Dir(name)
		if _, ok := parents[parent]; ok {
			continue
		}
		listing, err := readParent(ctx, cloud, parent, fresh)
		// Recursive uploads plan children of folders which do not exist yet.
		if err != nil && !errors.Is(err, port.ErrNotFound) {
			return nil, fmt.Errorf("list %q: %w", parent, err)
		}
		parents[parent] = listing
	}
	return parents, nil
}

func readParent(ctx context.Context, cloud port.Files, parent string, fresh bool) (parentListing, error) {
	if live, ok := cloud.(port.FileParentListing); fresh && ok {
		e, entries, err := live.ListParentFresh(ctx, parent)
		return parentListing{entry: e, children: entries}, err
	}
	list := cloud.List
	if live, ok := cloud.(port.FileListing); fresh && ok {
		list = live.ListFresh
	}
	entries, err := list(ctx, parent, false)
	if err != nil && !errors.Is(err, port.ErrNotFound) {
		return parentListing{}, err
	}
	e := port.Entry{Folder: true}
	if clean(parent) != "" {
		var statErr error
		e, statErr = cloud.Stat(ctx, parent)
		if statErr != nil {
			if !errors.Is(statErr, port.ErrNotFound) {
				return parentListing{}, statErr
			}
			e = port.Entry{}
		}
	}
	return parentListing{entry: e, children: entries}, err
}

func planCheck(ctx context.Context, cloud port.Files, names []string) (writeCheck, error) {
	parents, err := listParents(ctx, cloud, names, false)
	if err != nil {
		return writeCheck{}, err
	}
	check := writeCheck{names: map[string][]port.Entry{}, parents: map[string]port.Entry{}}
	for _, name := range names {
		check.names[name] = equivalents(parents[path.Dir(name)].children, name)
		check.parents[path.Dir(name)] = parents[path.Dir(name)].entry
	}
	return check, nil
}

func (c writeCheck) recheck(ctx context.Context, cloud port.Files) error {
	names := slices.Sorted(maps.Keys(c.names))
	parents, err := listParents(ctx, cloud, names, true)
	if err != nil {
		return fmt.Errorf("nothing written; cannot re-check %q: %w", names, errors.Join(port.ErrConflict, err))
	}
	return c.compare(parents)
}

func (c writeCheck) compare(parents map[string]parentListing) error {
	names := slices.Sorted(maps.Keys(c.names))
	var problems []error
	for _, parent := range slices.Sorted(maps.Keys(c.parents)) {
		before := c.parents[parent]
		now := parents[parent].entry
		if before.ID != now.ID || before.Path != now.Path || before.Folder != now.Folder {
			problems = append(problems, fmt.Errorf("%w: parent %q changed; before [%s]; now [%s]; nothing written", port.ErrConflict, parent,
				visibleEntries([]port.Entry{before}), visibleEntries([]port.Entry{now})))
		}
	}
	for _, name := range names {
		now := equivalents(parents[path.Dir(name)].children, name)
		if !slices.EqualFunc(c.names[name], now, sameEntry) {
			problems = append(problems, fmt.Errorf("%w: %q changed after planning; before: [%s]; now: [%s]; nothing written",
				port.ErrConflict, name, entryVersions(c.names[name]), entryVersions(now)))
		}
	}
	return errors.Join(problems...)
}

// destructiveCheck reads only the affected parents immediately before a move
// or deletion, even after the batch has refreshed once. The planning view includes
// our earlier writes but cannot stand in for a live check of another client's edits.
func destructiveCheck(ctx context.Context, cloud port.Files, expected port.Entry, destination string) error {
	names := []string{expected.Path}
	if destination != "" {
		names = append(names, destination)
	}
	check, err := planCheck(ctx, cloud, names)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(check.names[expected.Path], func(e port.Entry) bool { return sameEntry(e, expected) }) {
		return fmt.Errorf("%w: source changed: %s; next pass will re-plan", port.ErrConflict, entryVersions([]port.Entry{expected}))
	}
	return check.recheckDestructive(ctx, cloud)
}

func (c writeCheck) recheckDestructive(ctx context.Context, cloud port.Files) error {
	parents, err := readParents(ctx, cloud, slices.Sorted(maps.Keys(c.names)), true)
	if err == nil {
		err = c.compare(parents)
	}
	if err != nil {
		return fmt.Errorf("%w: source or destination changed; next pass will re-plan: %w", port.ErrConflict, err)
	}
	return nil
}

// verifyWritten checks every written name together. Callers choose the lock
// boundary; uploads verify after release, while mkdir may run under a caller's lock.
func verifyWritten(ctx context.Context, cloud port.Files, written []port.Entry) error {
	return verifyWrites(ctx, cloud, written, nil, nil)
}

// verifyRestored also requires the recycled identity at its planned path.
func verifyRestored(ctx context.Context, cloud port.Files, written []port.Entry) error {
	return verifyDestinations(ctx, cloud, written, nil, nil, true)
}

func verifyWrites(ctx context.Context, cloud port.Files, written []port.Entry, twinFound func(port.Entry), superseded []port.Entry) error {
	return verifyDestinations(ctx, cloud, written, twinFound, superseded, false)
}

func verifyDestinations(ctx context.Context, cloud port.Files, written []port.Entry,
	twinFound func(port.Entry), superseded []port.Entry, requirePresent bool) error {
	if batch, ok := ctx.Value(writeBatchKey{}).(*writeBatch); ok {
		// Separate-copy replacement must be verified before recycling the old
		// copy. Its final destination joins the normal end-of-pass check.
		// Restores must confirm their original identity before returning.
		if len(superseded) > 0 || requirePresent {
			return verifyDestinations(context.WithValue(ctx, writeBatchKey{}, nil), batch.source, written, twinFound, superseded, requirePresent)
		}
		batch.written = append(batch.written, written...)
		return nil
	}
	if len(written) == 0 {
		return nil
	}
	// One complete snapshot bounds verification independently of folder count.
	if snapshots, ok := cloud.(port.FileFreshSnapshot); ok {
		view, err := snapshots.FreshSnapshot(ctx)
		if err != nil {
			return fmt.Errorf("write happened for [%s]; warning: cannot verify destinations: %w; no rollback",
				visibleEntries(written), errors.Join(port.ErrWriteVerification, err))
		}
		return verifyFrom(ctx, view.List, written, twinFound, superseded, requirePresent)
	}
	list := cloud.List
	if live, ok := cloud.(port.FileListing); ok {
		list = live.ListFresh
	}
	return verifyFrom(ctx, list, written, twinFound, superseded, requirePresent)
}

func sameEntry(a, b port.Entry) bool {
	if a.ID != b.ID || a.Path != b.Path || a.Folder != b.Folder || a.Hash != b.Hash {
		return false
	}
	if (a.Size == nil) != (b.Size == nil) || a.Size != nil && *a.Size != *b.Size {
		return false
	}
	// A folder listing on the service omits the modification time that the
	// full tree reports, so a missing time on either side is unknown, not a
	// change; the identifier, hash and size still catch a different version.
	return a.Modified == nil || b.Modified == nil || a.Modified.Equal(*b.Modified)
}

func verifyFrom(ctx context.Context, list func(context.Context, string, bool) ([]port.Entry, error), written []port.Entry,
	twinFound func(port.Entry), superseded []port.Entry, requirePresent bool) error {
	groups := map[string][]port.Entry{}
	for _, e := range written {
		groups[path.Dir(e.Path)] = append(groups[path.Dir(e.Path)], e)
	}
	var problems []error
	for _, parent := range slices.Sorted(maps.Keys(groups)) {
		var entries []port.Entry
		err := ctx.Err()
		if err == nil {
			entries, err = list(ctx, parent, false)
		}
		if err != nil {
			problems = append(problems, fmt.Errorf("write happened for [%s]; warning: cannot verify destination parent %q: %w; no rollback",
				visibleEntries(groups[parent]), parent, errors.Join(port.ErrWriteVerification, err)))
			continue
		}
		for _, e := range groups[parent] {
			if requirePresent && !slices.ContainsFunc(entries, func(other port.Entry) bool { return other.ID == e.ID && other.Path == e.Path }) {
				problems = append(problems, fmt.Errorf("%w: write happened; expected ID %s at %q, but it was not found; no rollback",
					port.ErrWriteVerification, e.ID, e.Path))
			}
			if problem := writtenTwin(entries, e, superseded); problem != nil {
				if twinFound != nil {
					twinFound(e)
				}
				problems = append(problems, problem)
			}
		}
	}
	return errors.Join(problems...)
}

func writtenTwin(entries []port.Entry, e port.Entry, superseded []port.Entry) error {
	matches := equivalents(entries, e.Path)
	matches = slices.DeleteFunc(matches, func(other port.Entry) bool {
		return slices.ContainsFunc(superseded, func(old port.Entry) bool { return old.ID == other.ID && old.Path == other.Path })
	})
	if !slices.ContainsFunc(matches, func(other port.Entry) bool { return other.ID != e.ID || other.Path != e.Path }) {
		return nil
	}
	// Include the returned identity even if the listing omitted the write.
	if !slices.ContainsFunc(matches, func(other port.Entry) bool { return other.ID == e.ID && other.Path == e.Path }) {
		matches = append(matches, e)
	}
	return fmt.Errorf("%w: write happened; NFC-equivalent entries: %s; no rollback", port.ErrWriteVerification, visibleEntries(matches))
}

func changeSource(ctx context.Context, cloud port.Files, query string, exact bool) (port.Entry, error) {
	e, err := cloud.Stat(ctx, query)
	if err != nil && !errors.Is(err, port.ErrConflict) {
		return e, err
	}
	name := e.Path
	if exact || err != nil {
		name = clean(query)
	}
	if (err != nil || exact) && path.Dir(clean(query)) != "." { // An ambiguous normalized lookup can still have no exact name.
		parent, parentErr := cloud.Stat(ctx, path.Dir(clean(query)))
		if parentErr != nil {
			return e, parentErr
		}
		name = path.Join(parent.Path, path.Base(clean(query)))
	}
	entries, listErr := cloud.List(ctx, path.Dir(name), false)
	if listErr != nil {
		return e, listErr
	}
	matches := equivalents(entries, name)
	if exact {
		exactMatches := slices.DeleteFunc(slices.Clone(matches), func(e port.Entry) bool { return e.Path != name })
		if len(exactMatches) > 1 {
			return e, fmt.Errorf("%w: multiple byte-exact entries: %s", port.ErrConflict, visibleEntries(exactMatches))
		}
		for _, match := range matches {
			if match.Path == name {
				if err == nil && match.ID == e.ID {
					return e, nil
				}
				match.ParentID = e.ParentID
				return match, nil
			}
		}
		return e, fmt.Errorf("%q: %w (--exact requires byte-exact name)", query, port.ErrNotFound)
	}
	if len(matches) > 1 {
		return e, fmt.Errorf("%w: NFC-equivalent entries: %s; use --exact with the byte-exact name", port.ErrConflict, visibleEntries(matches))
	}
	return e, err
}

func uploadCheck(ctx context.Context, cloud port.Files, p planner) (writeCheck, error) {
	names := slices.Clone(p.folders)
	for _, u := range p.uploads {
		if u.item.Status == port.StatusUpload || u.item.Status == port.StatusReplace {
			names = append(names, u.item.Cloud)
		}
	}
	planned := map[string]string{}
	for _, name := range names {
		key := path.Dir(name) + "/" + norm.NFC.String(path.Base(name))
		if previous, exists := planned[key]; exists {
			return writeCheck{}, fmt.Errorf("%w: planned upload names %q and %q are NFC-equivalent", port.ErrConflict, previous, name)
		}
		planned[key] = name
	}
	check, err := planCheck(ctx, cloud, names)
	if err != nil {
		return writeCheck{}, err
	}
	for _, name := range p.folders {
		if len(check.names[name]) > 0 {
			return writeCheck{}, fmt.Errorf("%w: %s", port.ErrConflict, visibleEntries(check.names[name]))
		}
	}
	for _, u := range p.uploads {
		matches, work := check.names[u.item.Cloud]
		if !work {
			continue
		}
		if len(matches) == 0 && u.item.Status == port.StatusUpload {
			continue
		}
		if len(matches) == 1 && u.item.Status == port.StatusReplace && matches[0].ID == u.previous.ID && matches[0].Path == u.item.Cloud {
			if u.expected != nil && !strings.EqualFold(matches[0].Hash, u.expected.Hash) {
				return writeCheck{}, fmt.Errorf("%w: expected upload source changed: %q (ID %s)", port.ErrConflict, u.expected.Path, u.expected.ID)
			}
			continue
		}
		return writeCheck{}, fmt.Errorf("%w: upload target %q: [%s]", port.ErrConflict, u.item.Cloud, visibleEntries(matches))
	}
	return check, nil
}

func destinationName(ctx context.Context, cloud port.Files, name string) (string, error) {
	parent := path.Dir(name)
	if parent == "." {
		return name, nil
	}
	e, err := cloud.Stat(ctx, parent)
	if err == nil {
		return path.Join(e.Path, path.Base(name)), nil
	}
	if !errors.Is(err, port.ErrNotFound) {
		return "", err
	}
	canonical, err := destinationName(ctx, cloud, parent)
	if err != nil {
		return "", err
	}
	return path.Join(canonical, path.Base(name)), nil
}

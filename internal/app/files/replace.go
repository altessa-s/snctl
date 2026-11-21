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
	"time"

	"github.com/altessa-s/snctl/internal/port"

	"golang.org/x/text/unicode/norm"
)

func (s Service) sendUpload(ctx context.Context, cloud port.Files, u upload, progress port.Progress) (port.TransferItem, bool) {
	item := u.item
	if u.expected != nil {
		if err := unchangedUpload(ctx, cloud, *u.expected); err != nil {
			return failed(item, err), false
		}
	}
	e, err := cloud.Upload(ctx, u.local, item.Cloud, progress)
	if item.Status == port.StatusReplace && errors.Is(err, port.ErrConflict) {
		e, err = s.uploadSeparate(ctx, cloud, u, progress)
	}
	if err != nil {
		return failed(item, err), u.verify && errors.Is(err, port.ErrAmbiguousUpload)
	}
	item.Entry = &e
	if item.Status == port.StatusReplace && e.ID != u.previous.ID {
		// Recycling and the final rename must wait until the sync session ends.
		return item, false
	}
	if e.Path != item.Cloud {
		item.Status, item.Reason = port.StatusRenamed, "uploaded as "+e.Path
		item.Cloud = e.Path
		return item, false
	}
	switch item.Status {
	case port.StatusReplace:
		item.Status = port.StatusReplaced
	default:
		item.Status = port.StatusUploaded
	}
	retainUpload(ctx, cloud, e, u.local)
	return item, false
}

// unchangedUpload checks the expected version after taking the sync lock.
func unchangedUpload(ctx context.Context, cloud port.Files, expected port.Entry) error {
	lookup := cloud.Stat
	if fresh, ok := cloud.(port.FileRefresh); ok {
		lookup = fresh.Refresh
	}
	current, err := lookup(ctx, expected.Path)
	if err != nil {
		return err
	}
	if current.ID != expected.ID || !strings.EqualFold(current.Hash, expected.Hash) {
		return port.ErrConflict
	}
	return nil
}

// uploadSeparate avoids a refused overwrite and leaves verification and recycling
// to the normal replacement completion after the sync session ends.
func (s Service) uploadSeparate(ctx context.Context, cloud port.Files, u upload, progress port.Progress) (port.Entry, error) {
	want, err := s.Disk.Hash(u.local)
	if err != nil {
		return port.Entry{}, err
	}
	list := cloud.List
	if fresh, ok := cloud.(port.FileListing); ok {
		list = fresh.ListFresh
	}
	entries, err := list(ctx, path.Dir(u.item.Cloud), false)
	if err != nil {
		return port.Entry{}, err
	}
	byPath := make(map[string][]port.Entry, len(entries))
	for _, e := range entries {
		key := norm.NFC.String(e.Path)
		byPath[key] = append(byPath[key], e)
	}
	if u.item.Status == port.StatusReplace {
		matches := byPath[norm.NFC.String(u.item.Cloud)]
		if len(matches) != 1 || matches[0].ID != u.previous.ID || matches[0].Path != u.previous.Path ||
			!strings.EqualFold(matches[0].Hash, u.previous.Hash) {
			return port.Entry{}, fmt.Errorf("%w: replacement target %q changed; now [%s]", port.ErrConflict, u.item.Cloud, visibleEntries(matches))
		}
	}
	ext := path.Ext(u.item.Cloud)
	available := ""
	for n := 1; n <= 999; n++ {
		target := strings.TrimSuffix(u.item.Cloud, ext) + fmt.Sprintf(" (replacement %s %d)", want, n) + ext
		matches := byPath[norm.NFC.String(target)]
		if len(matches) == 1 && !matches[0].Folder && want != "" && strings.EqualFold(matches[0].Hash, want) {
			return matches[0], nil
		}
		if len(matches) == 0 && available == "" {
			available = target
		}
	}
	if available != "" {
		return cloud.Upload(ctx, u.local, available, progress)
	}
	return port.Entry{}, port.ErrConflict
}

func (s Service) completeUpload(ctx context.Context, cloud port.Files, u upload, item port.TransferItem, lockErr error) port.TransferItem {
	if item.Status != port.StatusReplace || item.Entry == nil {
		return item
	}
	fresh := *item.Entry
	item.Entry = nil
	if lockErr != nil {
		return failed(item, fmt.Errorf("%w; uploaded copy retained at %s", lockErr, fresh.Path))
	}
	e, err := s.finishReplacement(ctx, cloud, u, fresh)
	if err != nil {
		return failed(item, err)
	}
	item.Entry, item.Status = &e, port.StatusReplaced
	retainUpload(ctx, cloud, e, u.local)
	return item
}

// verifyUpload runs after the batch and lock have ended because committed files
// can remain invisible while the account sync lock is held.
func (s Service) verifyUpload(ctx context.Context, cloud port.Files, u upload, item port.TransferItem) port.TransferItem {
	if err := ctx.Err(); err != nil {
		return failed(item, err)
	}
	refresh, ok := cloud.(port.FileRefresh)
	if !ok {
		return item
	}
	stored, lookupErr := refreshUpload(ctx, refresh, item.Cloud)
	want, hashErr := s.Disk.Hash(u.local)
	if lookupErr != nil || hashErr != nil || stored.Path != item.Cloud || stored.Folder ||
		stored.Size == nil || *stored.Size != item.Size || want == "" || !strings.EqualFold(stored.Hash, want) {
		return item
	}
	item.Entry = &stored
	item.Status = port.StatusUploaded
	item.Reason = "server reported an error after storing it"
	retainUpload(ctx, cloud, stored, u.local)
	return item
}

// refreshUpload retries only uncertain lookup failures, never missing entries
// or mismatched content, and never resends an upload.
func refreshUpload(ctx context.Context, refresh port.FileRefresh, name string) (port.Entry, error) {
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return port.Entry{}, err
		}
		entry, err := refresh.Refresh(ctx, name)
		if !errors.Is(err, port.ErrAmbiguousUpload) || attempt == 2 {
			return entry, err
		}
		delay := [...]time.Duration{200 * time.Millisecond, 800 * time.Millisecond}
		timer := time.NewTimer(delay[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return port.Entry{}, ctx.Err()
		case <-timer.C:
		}
	}
}

// finishReplacement preserves replacement semantics on servers that allocate
// another entry. The old version is recycled only after verifying the new bytes.
func (s Service) finishReplacement(ctx context.Context, cloud port.Files, u upload, fresh port.Entry) (port.Entry, error) {
	fail := func(err error) (port.Entry, error) {
		return port.Entry{}, fmt.Errorf("%w; uploaded copy retained at %s", err, fresh.Path)
	}
	want, err := s.Disk.Hash(u.local)
	if err != nil {
		return fail(err)
	}
	downloaded, err := cloud.Download(ctx, fresh, nil)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = s.Disk.Remove(downloaded) }()
	got, err := s.Disk.Hash(downloaded)
	if err != nil {
		return fail(err)
	}
	if got != want {
		return fail(errors.New("uploaded copy failed verification"))
	}
	if err = replacementSource(ctx, cloud, u, fresh); err != nil {
		return fail(err)
	}

	if err = cloud.Delete(ctx, u.previous, false); err != nil {
		return fail(err)
	}
	if fresh.Path != u.item.Cloud {
		if err = destructiveCheck(ctx, cloud, fresh, u.item.Cloud); err == nil {
			err = cloud.Move(ctx, fresh, u.item.Cloud)
		}
		if err != nil {
			return fail(fmt.Errorf("old version is in the recycle bin: %w", err))
		}
	}
	fresh.Path, fresh.Name = u.item.Cloud, path.Base(u.item.Cloud)
	return fresh, nil
}

// replacementSource checks the approved old identity and the verified uploaded
// copy before recycling. A third entry must never be mistaken for either one.
func replacementSource(ctx context.Context, cloud port.Files, u upload, uploaded port.Entry) error {
	check, err := planCheck(ctx, cloud, []string{u.previous.Path, uploaded.Path})
	if err != nil {
		return err
	}
	matches := slices.DeleteFunc(slices.Clone(check.names[u.previous.Path]), func(e port.Entry) bool {
		return e.ID == uploaded.ID && e.Path == uploaded.Path
	})
	copies := slices.DeleteFunc(slices.Clone(check.names[uploaded.Path]), func(e port.Entry) bool {
		return e.ID == u.previous.ID && e.Path == u.previous.Path
	})
	if len(matches) != 1 || !sameEntry(matches[0], u.previous) || len(copies) != 1 || !sameEntry(copies[0], uploaded) {
		return fmt.Errorf("%w: replacement source %q changed; now [%s]", port.ErrConflict, u.previous.Path, visibleEntries(matches))
	}
	return check.recheckDestructive(ctx, cloud)
}

// checkDuplicates observes all successful uploads once the sync session ends.
// A complete fresh snapshot checks all destination parents, including created folders.
func checkDuplicates(ctx context.Context, cloud port.Files, report *port.TransferReport) error {
	return verifyTransfers(ctx, cloud, report, nil, nil)
}

func verifyTransfers(ctx context.Context, cloud port.Files, report *port.TransferReport, created, superseded []port.Entry) error {
	written := slices.Clone(created)
	for _, item := range report.Items {
		if item.Entry != nil {
			written = append(written, *item.Entry)
		}
	}
	err := verifyWrites(ctx, cloud, written, func(e port.Entry) {
		for i := range report.Items {
			item := &report.Items[i]
			if item.Entry == nil || item.Entry.ID != e.ID || item.Entry.Path != e.Path {
				continue
			}
			reason := "a second entry named " + item.Entry.Name + " now exists"
			if item.Reason != "" {
				reason = item.Reason + "; " + reason
			}
			item.Status, item.Reason = port.StatusDuplicate, reason
		}
	}, superseded)
	if err != nil {
		report.Warnings = append(report.Warnings, err.Error())
	}
	return err
}

func retainUpload(ctx context.Context, cloud port.Files, e port.Entry, file string) {
	if cache, ok := cloud.(port.FileRetention); ok {
		cache.RetainUpload(ctx, e, file)
	}
}

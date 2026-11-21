// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package files

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
)

const notWritable = "not writable: use NOTE/Note, NOTE/MyStyle, DOCUMENT/Document, or a folder below another top-level folder (such as EXPORT)"

// upload is one planned file of mput or put.
type upload struct {
	item     port.TransferItem
	local    string
	verify   bool
	previous port.Entry
	expected *port.Entry
}

// Upload sends the files below a local directory into a cloud folder,
// creating the folders the directory has. The plan is made from one listing
// and shown to Confirm before anything is written; the writes then run under
// the account sync lock. The cloud has no create-only upload: a file another
// client creates between listing and upload can collide. The result is checked
// against the returned metadata and refreshed listing.
func (s Service) Upload(ctx context.Context, dir, folder string, o port.UploadOptions) (port.TransferReport, error) {
	report := port.TransferReport{Items: []port.TransferItem{}, DryRun: o.DryRun}
	if clean(folder) != "" && !domain.Writable(path.Join(clean(folder), "file")) {
		return report, fmt.Errorf("%w: %s: %s", port.ErrArgument, folder, notWritable)
	}
	switch {
	case o.Jobs < 0:
		return report, fmt.Errorf("%w: --jobs must not be negative", port.ErrArgument)
	case s.Disk == nil:
		return report, errNoDisk
	}
	info, err := s.Disk.Info(dir)
	if err != nil {
		return report, err
	}
	if !info.Dir {
		return report, fmt.Errorf("%w: %s is not a directory", port.ErrArgument, dir)
	}
	locals, err := s.Disk.Walk(dir)
	if err != nil {
		return report, err
	}
	cloud, err := s.view(ctx)
	if err != nil {
		return report, err
	}
	folder = clean(folder)
	if folder != "" {
		if top, statErr := cloud.Stat(ctx, folder); statErr == nil && !top.Folder {
			return report, fmt.Errorf("%w: %s is a file", port.ErrArgument, folder)
		} else if statErr != nil && !errors.Is(statErr, port.ErrNotFound) {
			return report, statErr
		}
	}
	p := planner{s: s, ctx: ctx, cloud: cloud, known: map[string]bool{}, opts: o, create: true}
	hidden := map[string]bool{}
	for _, f := range locals {
		if f.Dir {
			continue
		}
		local := filepath.Join(dir, filepath.FromSlash(f.Rel))
		if top := hiddenFolder(f.Rel); top != "" {
			if !hidden[top] {
				hidden[top] = true
				p.skip(filepath.Join(dir, filepath.FromSlash(top)), "hidden folder")
			}
			continue
		}
		if !f.Regular {
			p.skip(local, "not a regular file")
			continue
		}
		if why := domain.UploadSkip(f.Rel, f.Size); why != "" {
			p.skip(local, why)
			continue
		}
		if err = p.file(local, local, path.Join(folder, f.Rel), f.Size); err != nil {
			return report, err
		}
	}
	return s.carryOut(ctx, cloud, p, report, "uploads", true)
}

// hiddenFolder is the outermost hidden folder a relative path lies in, or "".
func hiddenFolder(rel string) string {
	parts := strings.Split(rel, "/")
	for i, part := range parts[:len(parts)-1] {
		if strings.HasPrefix(part, ".") {
			return strings.Join(parts[:i+1], "/")
		}
	}
	return ""
}

// planner turns local files into planned uploads against one listing.
type planner struct {
	s       Service
	ctx     context.Context //nolint:containedctx // one planning pass
	cloud   port.Files
	opts    port.UploadOptions
	known   map[string]bool
	folders []string
	uploads []upload
	other   []port.TransferItem
	// create plans missing folders (mput); put requires its folder to exist
	// and fails a file it cannot place, because the person named that file.
	create bool
}

func (p *planner) skip(local, why string) {
	p.other = append(p.other, port.TransferItem{Status: port.StatusSkipped, Local: local, Reason: why})
}

// file plans one upload of local, shown as display, to target: new,
// replacing, unchanged or refused.
func (p *planner) file(local, display, target string, n int64) error {
	target, nameErr := destinationName(p.ctx, p.cloud, target)
	if nameErr != nil {
		return nameErr
	}
	item := port.TransferItem{Cloud: target, Local: display, Size: n}
	if !domain.Writable(target) {
		item.Status, item.Reason = port.StatusSkipped, notWritable
		if !p.create {
			item.Status = port.StatusFailed
		}
		p.other = append(p.other, item)
		return nil
	}
	e, err := p.cloud.Stat(p.ctx, target)
	absent := errors.Is(err, port.ErrNotFound)
	switch {
	case errors.Is(err, port.ErrNotFound) && p.create:
		if err = p.parents(target); err != nil {
			return err
		}
		item.Status = port.StatusUpload
	case errors.Is(err, port.ErrNotFound):
		item.Status = port.StatusUpload
		if parent, statErr := p.cloud.Stat(p.ctx, path.Dir(target)); statErr != nil || !parent.Folder {
			item.Status, item.Reason = port.StatusFailed, "its folder does not exist"
		}
	case err != nil:
		return err
	case e.Folder:
		item.Status, item.Reason = port.StatusFailed, "a folder has this name"
	case p.opts.Incremental && p.same(local, e, n):
		item.Status = port.StatusUnchanged
	case p.opts.Replace:
		item.Status = port.StatusReplace
	default:
		item.Status, item.Reason = port.StatusExists, "already in the cloud"
	}
	verify := absent || item.Status == port.StatusReplace && p.different(local, e, n)
	p.uploads = append(p.uploads, upload{item: item, local: local, previous: e, verify: verify})
	return nil
}

// same compares size first and then the MD5 of the whole local file, the
// definition of the cloud's content hash.
func (p *planner) same(local string, e port.Entry, n int64) bool {
	if e.Size == nil || *e.Size != n || e.Hash == "" {
		return false
	}
	h, err := p.s.Disk.Hash(local)
	return err == nil && strings.EqualFold(h, e.Hash)
}

// different requires evidence of changed content: incomplete metadata cannot
// establish that a failed replacement actually stored anything new.
func (p *planner) different(local string, e port.Entry, n int64) bool {
	if e.Size != nil && *e.Size != n {
		return true
	}
	if e.Hash == "" {
		return false
	}
	h, err := p.s.Disk.Hash(local)
	return err == nil && !strings.EqualFold(h, e.Hash)
}

// parents plans the folders missing above target, outermost first.
func (p *planner) parents(target string) error {
	var missing []string
	for dir := path.Dir(target); dir != "." && dir != "/" && !p.known[dir]; dir = path.Dir(dir) {
		e, err := p.cloud.Stat(p.ctx, dir)
		if err == nil {
			if !e.Folder {
				return fmt.Errorf("%w: %s is a file", port.ErrConflict, dir)
			}
			p.known[dir] = true
			break
		}
		if !errors.Is(err, port.ErrNotFound) {
			return err
		}
		p.known[dir] = true
		missing = append(missing, dir)
	}
	slices.Reverse(missing)
	p.folders = append(p.folders, missing...)
	return nil
}

// carryOut confirms and runs a plan: folders first, outermost first, then
// the files, jobs at a time. A dry run returns the plan.
func (s Service) carryOut(
	ctx context.Context, cloud port.Files, p planner, report port.TransferReport, verb string, lock bool,
) (port.TransferReport, error) {
	slices.Sort(p.folders)
	for _, f := range p.folders {
		report.Items = append(report.Items, port.TransferItem{Status: port.StatusCreate, Cloud: f, Folder: true})
	}
	first := len(report.Items)
	for _, u := range p.uploads {
		report.Items = append(report.Items, u.item)
	}
	report.Items = append(report.Items, p.other...)
	report.Count()
	work := report.Counts[port.StatusCreate] + report.Counts[port.StatusUpload] + report.Counts[port.StatusReplace]
	if p.opts.DryRun || work == 0 {
		return report, outcome(ctx, report, verb)
	}
	check, checkErr := uploadCheck(ctx, cloud, p)
	if checkErr != nil {
		return report, checkErr
	}
	if p.opts.Confirm != nil {
		if err := p.opts.Confirm(report); err != nil {
			return port.TransferReport{}, err
		}
	}
	pending := make([]bool, len(p.uploads))
	ran := false
	var created []port.Entry
	run := func(ctx context.Context) error {
		if err := check.recheck(ctx, cloud); err != nil {
			return err
		}
		ran = true
		created = s.write(ctx, cloud, p, report.Items, first, pending)
		return nil
	}
	var err error
	if lock && s.Lock != nil {
		err = s.Lock.WithSyncLock(ctx, run)
	} else {
		err = run(ctx)
	}
	if !ran {
		// The lock was not taken: nothing was written, so there is no report.
		return port.TransferReport{}, err
	}
	for j, verify := range pending {
		if verify {
			report.Items[first+j] = s.verifyUpload(ctx, cloud, p.uploads[j], report.Items[first+j])
		}
	}
	// Verify uploaded copies before a replacement can recycle anything.
	var superseded []port.Entry
	for j, u := range p.uploads {
		item := report.Items[first+j]
		if item.Status == port.StatusReplace && item.Entry != nil && item.Entry.ID != u.previous.ID {
			superseded = append(superseded, u.previous)
		}
	}
	verifyErr := verifyTransfers(ctx, cloud, &report, created, superseded)
	if verifyErr == nil {
		var moved []port.Entry
		for j, u := range p.uploads {
			before := report.Items[first+j].Entry
			report.Items[first+j] = s.completeUpload(ctx, cloud, u, report.Items[first+j], err)
			after := report.Items[first+j].Entry
			if before != nil && after != nil && before.Path != after.Path {
				moved = append(moved, *after)
			}
		}
		verifyErr = verifyWritten(ctx, cloud, moved)
	}

	report.Count()
	return report, errors.Join(err, verifyErr, outcome(ctx, report, verb))
}

// write carries out the planned creations and uploads in items, in place.
func (s Service) write(ctx context.Context, cloud port.Files, p planner, items []port.TransferItem, first int, pending []bool) []port.Entry {
	var created []port.Entry
	failedFolders := map[string]bool{}
	for i := range first {
		e, err := cloud.Mkdir(ctx, items[i].Cloud)
		if err != nil {
			items[i] = failed(items[i], err)
			failedFolders[items[i].Cloud] = true
			continue
		}
		items[i].Status = port.StatusCreated
		created = append(created, e)
	}
	var todo []int
	var total int64
	for j, u := range p.uploads {
		i := first + j
		if u.item.Status != port.StatusUpload && u.item.Status != port.StatusReplace {
			continue
		}
		for dir := path.Dir(u.item.Cloud); dir != "."; dir = path.Dir(dir) {
			if failedFolders[dir] {
				items[i] = failed(items[i], errors.New("its folder was not created"))
			}
		}
		if items[i].Status == port.StatusFailed {
			continue
		}
		todo = append(todo, j)
		total += u.item.Size
	}
	progress := newTracker(p.opts.Progress, len(todo), total)
	each(ctx, todo, p.opts.Jobs, func(ctx context.Context, j int) {
		i := first + j
		defer progress.finish()
		if err := ctx.Err(); err != nil {
			items[i] = failed(items[i], err)
			return
		}
		items[i], pending[j] = s.sendUpload(ctx, cloud, p.uploads[j], progress.file(j))
	})
	for _, j := range todo {
		if st := items[first+j].Status; (st == port.StatusUpload || st == port.StatusReplace) && items[first+j].Entry == nil {
			items[first+j] = failed(items[first+j], context.Canceled)
		}
	}
	return created
}

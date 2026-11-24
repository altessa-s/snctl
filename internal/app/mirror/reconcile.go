// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
)

func (r *run) tracked(ctx context.Context, id string) {
	en := r.j.Items[id]
	c, remote := r.docs[id]
	if en.Detached && (remote || en.ownsLocalOutput()) {
		return
	}
	f, local := r.local.find(en.Path)
	if local {
		r.claimed[f.Path] = true
	}
	if en.Recover {
		r.recoverUpload(ctx, id, en, f, local)
		return
	}
	if en.drawingOnlyDocument() {
		if !remote {
			r.finishOriginal(ctx, id, en)
		}
		// A restored ID has a new original. Let newFiles/newCloud reconcile
		// the released path after the remaining owners have been processed.
		return
	}
	if en.Path == "" {
		r.notebook(ctx, id, en, c, remote)
		return
	}
	if en.RestoreLocal && remote {
		r.downloadAction(ctx, c, en.Path, "")
		return
	}
	renamed := false
	if !local {
		if f, local = r.findRename(en); local {
			r.claimed[f.Path] = true
			renamed = true
		}
	}
	switch {
	case !local && !remote:
		r.finishOriginal(ctx, id, en)
	case !remote:
		if r.deletion(id, en, true) {
			r.remove(ctx, id, en, f.Path)
		}
	case !local:
		if !strings.EqualFold(c.Hash, en.Record) {
			r.downloadAction(ctx, c, r.relative(c), "deleted here, changed there: restored")
		} else if r.deletion(id, en, false) {
			r.trash(ctx, id, en, c)
		}
	default:
		if !r.align(ctx, id, &en, &c, f, renamed) {
			return
		}
		r.changed(ctx, id, en, c, f)
	}
}

// A deleted original releases its path immediately. A pending drawing keeps
// its journal owner until recovery can finish and the output can be retained.
func (r *run) forgetOriginal(id string) {
	if r.drawingPending(id, "") {
		en := r.j.Items[id]
		en.Path, en.Recover, en.RestoreLocal = "", false, false
		r.j.Items[id] = en
		return
	}
	delete(r.j.Items, id)
}

func (r *run) finishOriginal(ctx context.Context, id string, en entry) {
	if en.Output != "" && !r.drawingPending(id, "") {
		r.remove(ctx, id, en, en.Output)
	} else if !r.opts.DryRun {
		r.forgetOriginal(id)
	}
}

func (r *run) align(ctx context.Context, id string, en *entry, c *port.Entry, f *localFile, renamed bool) bool {
	want := r.relative(*c)
	if renamed && c.Path != en.CloudPath && foldName(want) != foldName(f.Path) {
		r.record(port.MirrorAction{Kind: port.MirrorSkip, Path: f.Path, Document: id, Detail: "renamed differently on both sides; reconcile the names first"})
		return false
	}
	target := path.Join(strings.Trim(r.spec.Folder, "/"), f.Path)
	if (renamed || f.Path != en.Path && c.Path == en.CloudPath) && c.Path != target {
		a := port.MirrorAction{Kind: port.MirrorMoveCloud, Path: en.Path, To: f.Path, Document: id}
		if !r.opts.DryRun {
			a.Err = r.e.lock.WithSyncLock(ctx, func(ctx context.Context) error {
				_, err := r.e.library.Mkdir(ctx, path.Dir(target), true)
				return err
			})
			a.Err = r.applied(a.Err, &a)
			// Moves must finish outside the sync session before uploads use the new path.
			if a.Err == nil {
				a.Err = r.applied(r.e.library.Move(ctx, c.Path, target), &a)
				if errors.Is(a.Err, port.ErrConflict) {
					a.Detail = a.Err.Error() + "; cloud entry changed before move; next pass will re-plan"
				}
			}
			if a.Err == nil {
				delete(r.cloudPaths, foldName(r.relative(*c)))
				c.Path, c.Name = target, path.Base(target)
				r.docs[id], r.cloudPaths[foldName(f.Path)] = *c, *c
				current := r.j.Items[id]
				current.Path, current.CloudPath = f.Path, c.Path
				r.j.Items[id] = current
				a.Err = r.writeJournal(ctx)
			}
		}
		r.record(a)
		if a.Err != nil {
			return false
		}
	} else if c.Path != en.CloudPath && want != f.Path {
		a := port.MirrorAction{Kind: port.MirrorMoveLocal, Path: f.Path, To: want, Document: id}
		if !r.opts.DryRun {
			a.Err = r.root.Move(f.Path, want)
		}
		r.record(a)
		if a.Err != nil {
			return false
		}
		delete(r.local.files, f.Path)
		f.Path = want
		r.local.files[want] = f
		r.claimed[want] = true
	}
	en.Path, en.CloudPath = f.Path, c.Path
	return true
}

func (r *run) changed(ctx context.Context, id string, en entry, c port.Entry, f *localFile) {
	if f.sameStat(en) && strings.EqualFold(c.Hash, en.Record) {
		r.inStep(id, f, c)
		return
	}

	if err := hash(r.root, f); err != nil {
		r.record(port.MirrorAction{Kind: port.MirrorSkip, Path: f.Path, Document: id, Detail: err.Error()})
		return
	}
	localChanged := f.sha != en.SHA256
	if localChanged && !strings.EqualFold(c.Hash, f.md5) && !f.settled(r.pass.Started) {
		r.record(port.MirrorAction{Kind: port.MirrorSkip, Path: f.Path, Document: id, Detail: stillWriting})
		return
	}
	cloudChanged := !strings.EqualFold(c.Hash, en.Record)
	switch {
	case strings.EqualFold(c.Hash, f.md5):
		r.inStep(id, f, c)
	case localChanged && cloudChanged:
		r.conflict(ctx, id, en, c, f)
	case localChanged:
		r.uploadAction(ctx, f, c.Path, id, &c)
	case cloudChanged:
		r.downloadAction(ctx, c, f.Path, "")
	default:
		r.inStep(id, f, c)
	}
}
func (r *run) inStep(id string, f *localFile, c port.Entry) {
	if r.opts.DryRun {
		return
	}
	en := r.j.Items[id]
	en.Path, en.Size, en.ModTime = f.Path, f.Size, f.ModTime
	if f.sha != "" {
		en.SHA256 = f.sha
	}
	en.CloudPath, en.Record, en.Recover, en.RestoreLocal = c.Path, c.Hash, false, false
	r.j.Items[id] = en
}

func (r *run) newFiles(ctx context.Context) {
	for _, p := range r.local.paths() {
		if ctx.Err() != nil || r.failure != nil {
			return
		}
		if r.claimed[p] || replacementName.MatchString(path.Base(p)) {
			continue
		}
		f := r.local.files[p]
		r.claimed[p] = true
		if !f.settled(r.pass.Started) {
			r.record(port.MirrorAction{Kind: port.MirrorSkip, Path: p, Detail: stillWriting})
			continue
		}
		if err := hash(r.root, f); err != nil {
			r.record(port.MirrorAction{Kind: port.MirrorSkip, Path: p, Detail: err.Error()})
			continue
		}
		c, exists := r.cloudPaths[foldName(p)]
		if exists {
			if en, tracked := r.j.Items[c.ID]; tracked && (en.Detached || !en.drawingOnlyDocument()) {
				continue
			}
			if strings.EqualFold(f.md5, c.Hash) {
				r.inStep(c.ID, f, c)
				r.record(port.MirrorAction{Kind: port.MirrorAdopt, Path: p, Document: c.ID})
			} else {
				r.conflict(ctx, c.ID, entry{}, c, f)
			}
		} else {
			r.uploadAction(ctx, f, path.Join(strings.Trim(r.spec.Folder, "/"), p), "", nil)
		}
	}
}

// Replacement copies remain available for upload recovery, never as new originals.
var replacementName = regexp.MustCompile(`(?i) \(replacement [0-9a-f]{32} [1-9][0-9]*\)(\.[^./]+)?$`)

func (r *run) newCloud(ctx context.Context) {
	for _, id := range slices.Sorted(maps.Keys(r.docs)) {
		if ctx.Err() != nil || r.failure != nil {
			return
		}
		if en, known := r.j.Items[id]; known && (en.Detached || !en.drawingOnlyDocument()) {
			continue
		}
		c := r.docs[id]
		if replacementName.MatchString(path.Base(c.Path)) {
			continue
		}
		rel := r.relative(c)
		if strings.EqualFold(path.Ext(rel), ".note") {
			r.notebook(ctx, id, entry{}, c, true)
			continue
		}
		if _, exists := r.local.find(rel); exists {
			continue
		}
		r.downloadAction(ctx, c, rel, "")
	}
}

func (r *run) trash(ctx context.Context, id string, en entry, c port.Entry) {
	a := port.MirrorAction{Kind: port.MirrorTrash, Path: en.Path, Document: id}
	if !r.opts.DryRun {
		info, err := r.root.Info(en.Path)
		switch {
		case err != nil:
			a.Err = err
		case info.Exists:
			a.Kind, a.Detail = port.MirrorSkip, "back since the directory was listed: not deleted"
		default:
			a.Err = r.e.library.Remove(ctx, c, false)
			if errors.Is(a.Err, port.ErrConflict) {
				a.Detail = a.Err.Error() + "; cloud entry changed before trash; next pass will re-plan"
			}
			if a.Err == nil && en.Output != "" && !r.drawingPending(id, "") {
				_, a.Err = r.aside(en.Output)
			}
			if a.Err == nil {
				r.forgetOriginal(id)
				delete(r.docs, id)
				delete(r.cloudPaths, foldName(r.relative(c)))
			}
		}
	}
	r.record(a)
}
func (r *run) remove(_ context.Context, id string, en entry, rel string) {
	a := port.MirrorAction{Kind: port.MirrorRemove, Path: rel, Document: id}
	if !r.opts.DryRun {
		a.To, a.Err = r.aside(rel)
		pending := r.drawingPending(id, "")
		if a.Err == nil && !pending && en.Output != "" && en.Output != rel {
			_, a.Err = r.aside(en.Output)
		}
		if a.Err == nil && !pending && en.Text {
			_, a.Err = r.aside(drawingTextPath(en.Output))
		}
		if a.Err == nil {
			r.forgetOriginal(id)
		}
	}
	r.record(a)
}
func (r *run) aside(rel string) (string, error) {
	info, err := r.root.Info(rel)
	if err != nil || !info.Exists {
		return "", err
	}
	for n := 1; n <= maxAside; n++ {
		to := path.Join(domain.MirrorRemovedDir, rel)
		if n > 1 {
			ext := path.Ext(to)
			to = strings.TrimSuffix(to, ext) + fmt.Sprintf(" (%d)", n) + ext
		}
		err = r.root.Move(rel, to)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return to, err
	}
	return "", errors.New("removed directory has too many copies")
}

const maxAside = 999

func (r *run) recoverUpload(ctx context.Context, id string, en entry, f *localFile, local bool) {
	if !local {
		r.record(port.MirrorAction{Kind: port.MirrorSkip, Path: en.Path, Document: id, Detail: "interrupted replacement needs its local original"})
		return
	}
	if err := hash(r.root, f); err != nil {
		r.record(port.MirrorAction{Kind: port.MirrorSkip, Path: f.Path, Document: id, Detail: err.Error()})
		return
	}
	c, exists := r.cloudPaths[foldName(strings.TrimPrefix(en.CloudPath, strings.Trim(r.spec.Folder, "/")+"/"))]
	if exists && strings.EqualFold(c.Hash, f.md5) {
		if !r.opts.DryRun && c.ID != id {
			r.replaceOriginal(id, c.ID)
		}
		r.inStep(c.ID, f, c)
		r.record(port.MirrorAction{Kind: port.MirrorAdopt, Path: f.Path, Document: c.ID, Detail: "resumed replacement"})
		return
	}
	if exists && c.ID != id {
		r.record(port.MirrorAction{Kind: port.MirrorSkip, Path: f.Path, Document: id, Detail: "replacement destination changed while interrupted"})
		return
	}
	if exists && !strings.EqualFold(c.Hash, en.Record) {
		if !r.opts.DryRun {
			current := r.j.Items[id]
			current.Recover = false
			r.j.Items[id] = current
		}
		r.conflict(ctx, id, en, c, f)
		return
	}
	var previous *port.Entry
	if exists {
		previous = &c
	}
	r.uploadAction(ctx, f, en.CloudPath, id, previous)
}

func (r *run) uploadAction(ctx context.Context, f *localFile, target, id string, previous *port.Entry) {
	kind := port.MirrorUpload
	if previous != nil {
		kind = port.MirrorUpdate
	}
	a := port.MirrorAction{Kind: kind, Path: f.Path, Document: id}
	if !r.opts.DryRun {
		a.Err = r.upload(ctx, f, target, previous, &a)
	}
	r.record(a)
	r.checkpoint(ctx)
}
func (r *run) upload(ctx context.Context, f *localFile, target string, previous *port.Entry, a *port.MirrorAction) error {
	// Staging from the pinned directory gives the upload planner the exact bytes hashed here.
	in, err := r.root.Open(f.Path)
	if err != nil {
		return err
	}
	staged, err := r.e.files.Stage(in)
	err = errors.Join(err, in.Close())
	if staged != "" {
		defer func() { _ = r.e.files.Remove(staged) }()
	}
	if err != nil {
		return err
	}
	sum, err := r.e.files.Hash(staged)
	if err != nil {
		return err
	}
	if !strings.EqualFold(sum, f.md5) {
		return errors.New(stillWriting)
	}
	if previous != nil {
		en := r.j.Items[previous.ID]
		en.Path, en.CloudPath, en.Recover = f.Path, previous.Path, true
		r.j.Items[previous.ID] = en
		if err = r.writeJournal(ctx); err != nil {
			return err
		}
		lookup := r.e.cloud.Stat
		if fresh, ok := r.e.cloud.(port.FileRefresh); ok {
			lookup = fresh.Refresh
		}
		current, lookupErr := lookup(ctx, previous.Path)
		if lookupErr != nil {
			return lookupErr
		}
		if current.ID != previous.ID || !strings.EqualFold(current.Hash, en.Record) {
			return port.ErrConflict
		}
		// The upload layer checks this recorded version again under the sync lock.
		expected := *previous
		expected.Hash = en.Record
		previous = &expected
	}
	report, err := r.e.upload.RestoreFiles(ctx, []port.RestoreFile{{Local: staged, Display: abs(r.spec.Dir, f.Path), Path: target, Expected: previous}},
		port.UploadOptions{Jobs: 1, Replace: previous != nil})
	if err = r.applied(err, a); err != nil {
		return err
	}
	for _, item := range report.Items {
		if item.Entry == nil {
			continue
		}
		c := *item.Entry
		if c.Path != target || !strings.EqualFold(c.Hash, f.md5) {
			return fmt.Errorf("uploaded copy retained at %s; destination did not verify", c.Path)
		}
		r.replaceOriginal(a.Document, c.ID)
		if previous != nil {
			delete(r.docs, previous.ID)
			a.Detail = "replaced in place"
			if c.ID != previous.ID {
				a.Detail = "server allocated another copy; recycled prior cloud copy"
			}
		}
		r.inStep(c.ID, f, c)
		r.docs[c.ID], r.cloudPaths[foldName(r.relative(c))] = c, c
		a.Document = c.ID
		return nil
	}
	return errors.New("upload returned no verified entry")
}

// An in-place update keeps its drawing. A new ID can inherit the old entry
// only when recovery no longer needs the old drawing owner.
func (r *run) replaceOriginal(oldID, newID string) {
	if oldID == "" || oldID == newID {
		return
	}
	if r.drawingPending(oldID, "") {
		r.forgetOriginal(oldID)
		return
	}
	old := r.j.Items[oldID]
	if r.drawingPending(newID, "") || r.j.Items[newID].Output != "" {
		// A recovered destination may already own an independent drawing.
		// Release the old original path and retire its drawing normally.
		old.Path, old.Recover, old.RestoreLocal = "", false, false
		r.j.Items[oldID] = old
		return
	}
	r.j.Items[newID] = old
	delete(r.j.Items, oldID)
}

func (r *run) downloadAction(ctx context.Context, c port.Entry, rel, detail string) {
	a := port.MirrorAction{Kind: port.MirrorDownload, Path: rel, Document: c.ID, Detail: detail}
	if !r.opts.DryRun {
		a.Err = r.download(ctx, c, rel)
	}
	r.record(a)
	r.checkpoint(ctx)
}
func (r *run) download(ctx context.Context, c port.Entry, rel string) error {
	tmp, err := r.e.cloud.Download(ctx, c, nil)
	if err != nil {
		return err
	}
	defer func() { _ = r.e.files.Remove(tmp) }()
	sum, err := r.e.files.Hash(tmp)
	if err != nil {
		return err
	}
	info, err := r.e.files.Info(tmp)
	if err != nil {
		return err
	}
	if c.Size == nil || info.Size != *c.Size || !strings.EqualFold(sum, c.Hash) {
		return errors.New("download failed content verification")
	}
	current, err := r.root.Info(rel)
	if err != nil {
		return err
	}
	if current.Exists {
		f := &localFile{Path: rel, Size: current.Size, ModTime: current.Modified}
		if err = hash(r.root, f); err != nil {
			return err
		}
		if strings.EqualFold(f.md5, c.Hash) {
			r.inStep(c.ID, f, c)
			return nil
		}
	}
	en := r.j.Items[c.ID]
	en.Path, en.CloudPath, en.Record, en.RestoreLocal = rel, c.Path, c.Hash, true
	r.j.Items[c.ID] = en
	if err = r.writeJournal(ctx); err != nil {
		return err
	}
	if current.Exists {
		if _, err = r.aside(rel); err != nil {
			return err
		}
	}
	in, err := r.e.files.Open(tmp)
	if err != nil {
		return err
	}
	err = r.root.Publish(ctx, rel, func(w io.Writer) error { _, copyErr := io.Copy(w, in); return copyErr })
	err = errors.Join(err, in.Close())
	if err != nil {
		return err
	}
	fi, err := r.root.Info(rel)
	if err != nil {
		return err
	}
	f := &localFile{Path: rel, Size: fi.Size, ModTime: fi.Modified}
	if err = hash(r.root, f); err != nil {
		return err
	}
	r.inStep(c.ID, f, c)
	return nil
}

func (r *run) conflict(ctx context.Context, id string, _ entry, c port.Entry, f *localFile) {
	ext := path.Ext(f.Path)
	to := strings.TrimSuffix(f.Path, ext) + " (local conflict " + f.sha[:12] + ")" + ext
	a := port.MirrorAction{Kind: port.MirrorConflict, Path: f.Path, To: to, Document: id,
		Detail: "kept local bytes under a distinct name; cloud original comes down"}
	if !r.opts.DryRun {
		a.Err = r.keepConflict(ctx, c, f, to)
	}
	r.record(a)
}
func (r *run) keepConflict(ctx context.Context, c port.Entry, f *localFile, to string) error {
	fi, err := r.root.Info(to)
	if err != nil {
		return err
	}
	if !fi.Exists {
		in, openErr := r.root.Open(f.Path)
		if openErr != nil {
			return openErr
		}
		err = r.root.Publish(ctx, to, func(w io.Writer) error { _, copyErr := io.Copy(w, in); return copyErr })
		err = errors.Join(err, in.Close())
		if err != nil {
			return err
		}
		fi, err = r.root.Info(to)
		if err != nil {
			return err
		}
	}
	conflictFile := &localFile{Path: to, Size: fi.Size, ModTime: fi.Modified}
	if err = hash(r.root, conflictFile); err != nil {
		return err
	}
	if conflictFile.sha != f.sha {
		return port.ErrConflict
	}
	r.claimed[to] = true
	target := path.Join(strings.Trim(r.spec.Folder, "/"), to)
	if existing, ok := r.cloudPaths[foldName(to)]; ok {
		if !strings.EqualFold(existing.Hash, conflictFile.md5) {
			return port.ErrConflict
		}
		r.inStep(existing.ID, conflictFile, existing)
	} else {
		if err = r.upload(ctx, conflictFile, target, nil, &port.MirrorAction{}); err != nil {
			return err
		}
	}
	return r.download(ctx, c, f.Path)
}

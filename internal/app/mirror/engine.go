// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
)

// Engine reconciles the journal with fresh cloud entries and confined local files.
type Engine struct {
	connections     port.MirrorConnections
	fresh           bool
	cloud           port.Files
	upload          port.BackupUploader
	library         port.Library
	files           port.TreeFiles
	disk            port.MirrorDisk
	notes           port.Notebooks
	feed            port.Feed
	dirs            port.DirWatcher
	timer           port.Timer
	lock            port.SyncLock
	libraryVersions string
	profile         string
}

// Deps supplies one profile's mirror capabilities.
type Deps struct {
	Connections     port.MirrorConnections
	Cloud           port.Files
	Upload          port.BackupUploader
	Library         port.Library
	Files           port.TreeFiles
	Disk            port.MirrorDisk
	Notes           port.Notebooks
	Feed            port.Feed
	Dirs            port.DirWatcher
	Timer           port.Timer
	Lock            port.SyncLock
	LibraryVersions string
	Profile         string
}

// NewEngine constructs the engine without contacting the cloud.
func NewEngine(d Deps) *Engine {
	need(dependency{"Cloud", d.Cloud == nil}, dependency{"Upload", d.Upload == nil}, dependency{"Library", d.Library == nil},
		dependency{"Files", d.Files == nil}, dependency{"Disk", d.Disk == nil}, dependency{"Notes", d.Notes == nil},
		dependency{"Feed", d.Feed == nil}, dependency{"Dirs", d.Dirs == nil}, dependency{depTimer, d.Timer == nil},
		dependency{"Lock", d.Lock == nil})
	return &Engine{connections: d.Connections, cloud: d.Cloud, upload: d.Upload, library: d.Library, files: d.Files, disk: d.Disk, notes: d.Notes,
		feed: d.Feed, dirs: d.Dirs, timer: d.Timer, lock: d.Lock, profile: d.Profile, libraryVersions: d.LibraryVersions}
}

type dependency struct {
	name    string
	missing bool
}

const depTimer = "Timer"

func need(ds ...dependency) {
	for _, d := range ds {
		if d.missing {
			panic("mirror: " + d.name + " is required")
		}
	}
}

// Pass holds an exclusive directory lock for one reconciliation.
func (e *Engine) Pass(ctx context.Context, spec port.MirrorSpec, o port.MirrorRunOptions) (port.MirrorPass, error) {
	checked, err := Check(spec)
	if err != nil {
		return failedPass(spec, o, e.timer.Now(), err), err
	}
	root, err := e.disk.OpenMirror(ctx, checked.Dir)
	if err != nil {
		return failedPass(spec, o, e.timer.Now(), err), err
	}
	defer func() { _ = root.Close() }()
	p := e.pass(ctx, checked, o, root)
	return p, p.Err
}
func (e *Engine) started(spec port.MirrorSpec, o port.MirrorRunOptions) port.MirrorPass {
	return port.MirrorPass{Mirror: spec.Name, Dir: spec.Dir, Folder: spec.Folder, DryRun: o.DryRun, Started: e.timer.Now()}
}
func (e *Engine) failed(p port.MirrorPass, err error) port.MirrorPass {
	p.Err, p.Ended = err, e.timer.Now()
	return p
}

const checkpointEvery = 10 * time.Second
const MaxDeletes = 50
const minShare = 2

var errSteps = errors.New("steps of the pass failed")

type run struct {
	e                    *Engine
	spec                 port.MirrorSpec
	opts                 port.MirrorRunOptions
	root                 port.MirrorDirectory
	j                    *journal
	local                local
	docs                 map[string]port.Entry
	cloudPaths           map[string]port.Entry
	marks                map[string]port.Entry
	markProblems         map[string]string
	deferredDrawings     map[string]bool
	claimed              map[string]bool
	pass                 port.MirrorPass
	failure              error
	loaded               bool
	saved                time.Time
	refuseUp, refuseDown bool
}

func (e *Engine) pass(ctx context.Context, spec port.MirrorSpec, o port.MirrorRunOptions, root port.MirrorDirectory) port.MirrorPass {
	r := &run{e: e, spec: spec, opts: o, root: root, pass: e.started(spec, o), claimed: map[string]bool{}, deferredDrawings: map[string]bool{}}
	r.saved = r.pass.Started
	j, err := readJournal(root)
	if err != nil {
		return e.failed(r.pass, err)
	}
	r.j = j
	if !o.DryRun {
		if err = r.cleanupDrawingStages(); err != nil {
			r.record(port.MirrorAction{Kind: port.MirrorWarning, Detail: "cleaning orphaned drawing stages", Err: err})
		}
	}
	if e.connections != nil {
		connection, openErr := e.connections.OpenMirrorConnection(ctx)
		if openErr != nil {
			return e.failed(r.pass, openErr)
		}
		freshEngine := *e
		freshEngine.cloud, freshEngine.upload, freshEngine.library, freshEngine.notes = connection.Cloud, connection.Upload, connection.Library, connection.Notes
		freshEngine.fresh = true
		r.e = &freshEngine
	}

	if notes, ok := r.e.notes.(interface {
		BatchRecognition(context.Context) (context.Context, func())
	}); ok {
		var flush func()
		ctx, flush = notes.BatchRecognition(ctx)
		defer flush()
	}
	if batch, ok := r.e.library.(port.FileWriteBatch); ok && !o.DryRun {
		err = batch.WithWriteBatch(ctx, r.reconcile)
	} else {
		err = r.reconcile(ctx)
	}
	err = r.applied(err, nil)
	if !r.loaded {
		return e.failed(r.pass, err)
	}
	return r.finish(ctx, err)
}

func (r *run) reconcile(ctx context.Context) error {
	if err := r.load(ctx); err != nil {
		return err
	}
	r.loaded = true
	var err error
	if r.local, err = scan(r.e.disk, r.spec.Dir); err != nil {
		return err
	}
	for _, en := range r.j.Items {
		if f, ok := r.local.find(en.Path); ok {
			r.claimed[f.Path] = true
		}
	}
	if err = r.migrateDrawings(ctx); err != nil {
		return err
	}
	r.retainDrawingInputs()
	r.guard()
	for _, id := range slices.Sorted(maps.Keys(r.j.Items)) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r.tracked(ctx, id)
		r.checkpoint(ctx)
		if r.failure != nil {
			return r.failure
		}
	}
	r.newFiles(ctx)
	r.newCloud(ctx)
	r.documentDrawings(ctx)
	return ctx.Err()
}

func (r *run) load(ctx context.Context) error {
	list := r.e.cloud.List
	if fresh, ok := r.e.cloud.(port.FileListing); ok && !r.e.fresh {
		list = fresh.ListFresh
	}
	all, err := list(ctx, "/", true)
	if err != nil {
		return err
	}
	r.docs, r.cloudPaths, r.marks = map[string]port.Entry{}, map[string]port.Entry{}, map[string]port.Entry{}
	r.markProblems = map[string]string{}
	folder := strings.Trim(r.spec.Folder, "/")
	if err = r.identity(ctx); err != nil {
		return err
	}
	top, err := r.findFolder(all, folder)
	if err != nil {
		return err
	}
	profile := r.e.profile
	if profile == "" {
		profile = r.spec.Profile
	}
	if r.j.Path != "" && foldName(r.j.Path) != foldName(folder) {
		return fmt.Errorf("%w: journal belongs to /%s", port.ErrMirrorRefused, r.j.Path)
	}
	if r.j.Profile != "" && r.j.Profile != profile {
		return fmt.Errorf("%w: journal belongs to profile %s", port.ErrMirrorRefused, r.j.Profile)
	}
	if top.ID == "" {
		if r.j.Folder != "" {
			return fmt.Errorf("%w: folder %s is gone or recycled", port.ErrMirrorRefused, r.spec.Folder)
		}
		if !r.opts.DryRun {
			err = r.e.lock.WithSyncLock(ctx, func(ctx context.Context) error {
				var makeErr error
				top, makeErr = r.e.library.Mkdir(ctx, folder, true)
				return makeErr
			})
			if err = r.applied(err, nil); err != nil {
				return err
			}
		}
	} else if !top.Folder {
		return fmt.Errorf("%w: %s is not a folder", port.ErrMirrorSpec, folder)
	}
	if r.j.Folder != "" && r.j.Folder != top.ID {
		return fmt.Errorf("%w: another folder is now at %s", port.ErrMirrorRefused, folder)
	}
	if top.Path != "" {
		folder = top.Path
		r.spec.Folder = "/" + folder
	}
	r.j.Folder, r.j.Path, r.j.Profile = top.ID, folder, profile
	return r.documents(all, folder)
}

func (r *run) documents(all []port.Entry, folder string) error {
	sidecars := map[string][]port.Entry{}
	for _, c := range all {
		rel, ok := strings.CutPrefix(c.Path, folder+"/")
		if !ok || c.Folder || domain.MirrorIgnored(rel) {
			continue
		}
		if strings.EqualFold(path.Ext(rel), ".mark") && r.spec.PullNew {
			key := foldName(rel)
			sidecars[key] = append(sidecars[key], c)
			continue
		}
		if !domain.MirrorUploadable(rel) && (!r.spec.PullNew && r.j.Items[c.ID].Output == "" || !strings.EqualFold(path.Ext(rel), ".note")) {
			continue
		}
		key := foldName(rel)
		if prior, exists := r.cloudPaths[key]; exists {
			return fmt.Errorf("%w: ambiguous cloud names: %s and %s", port.ErrMirrorRefused, prior.Path, c.Path)
		}
		if c.ID == "" || c.Hash == "" || c.Size == nil {
			return fmt.Errorf("%w: incomplete cloud metadata for %s", port.ErrMirrorRefused, c.Path)
		}
		r.cloudPaths[key], r.docs[c.ID] = c, c
	}
	for key, group := range sidecars {
		c := group[0]
		if len(group) == 1 && c.ID != "" && c.Hash != "" && c.Size != nil {
			r.marks[key] = c
			continue
		}
		identities := make([]string, 0, len(group))
		for _, c := range group {
			identities = append(identities, sidecarIdentity(c))
		}
		slices.Sort(identities)
		r.markProblems[key] = "ambiguous or incomplete .mark sidecar; drawing skipped\n" + strings.Join(identities, "\n")
	}
	return nil
}
func (r *run) relative(c port.Entry) string {
	return strings.TrimPrefix(c.Path, strings.Trim(r.spec.Folder, "/")+"/")
}

func (r *run) guard() {
	up, down := 0, 0
	for id, en := range r.j.Items {
		if !en.countsForDeletion() {
			continue
		}
		_, remote := r.docs[id]
		_, local := r.local.find(en.Path)
		if !remote && !en.Recover {
			down++
		}
		if en.Path != "" && !local && remote && strings.EqualFold(r.docs[id].Hash, en.Record) && !en.RestoreLocal && !r.renamed(en) {
			up++
		}
	}
	tracked, local := r.j.tracked()
	r.refuseUp = guard(up, local, r.local.all == 0, "local directory") != ""
	r.refuseDown = guard(down, tracked, len(r.docs) == 0, "cloud folder") != ""
}
func guard(n, total int, empty bool, side string) string {
	switch {
	case n == 0:
		return ""
	case empty:
		return side + " is empty"
	case n > MaxDeletes:
		return "more than 50 deletions"
	case n >= minShare && n*2 > total:
		return "more than half the tracked documents"
	default:
		return ""
	}
}
func (r *run) renamed(en entry) bool { _, ok := r.findRename(en); return ok }
func (r *run) findRename(en entry) (*localFile, bool) {
	var found *localFile
	for _, p := range r.local.paths() {
		f := r.local.files[p]
		if r.claimed[p] || !f.settled(r.pass.Started) {
			continue
		}
		if hash(r.root, f) == nil && f.sha == en.SHA256 {
			if found != nil {
				return nil, false
			}
			found = f
		}
	}
	return found, found != nil
}
func (r *run) deletion(id string, en entry, down bool) bool {
	if r.spec.NoDelete {
		if !r.opts.DryRun {
			current := r.j.Items[id]
			current.Detached = true
			r.j.Items[id] = current
		}
		r.record(port.MirrorAction{Kind: port.MirrorDetach, Path: en.Path, Document: id, Detail: "--no-delete keeps the other copy"})
		return false
	}
	if !r.opts.ForceDelete && (down && r.refuseDown || !down && r.refuseUp) {
		r.record(port.MirrorAction{Kind: port.MirrorRefuse, Path: en.Path, Document: id, Detail: "mass-deletion guard; --force-delete carries it out"})
		return false
	}
	return true
}

// applied preserves completed writes when their post-write safety check warns.
// Like drawing warnings, these do not add pending work or fail the pass. Other
// errors joined to a warning (including cancellation) keep their normal status.
func (r *run) applied(err error, a *port.MirrorAction) error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		remaining := make([]error, 0, len(joined.Unwrap()))
		for _, part := range joined.Unwrap() {
			remaining = append(remaining, r.applied(part, a))
		}
		return errors.Join(remaining...)
	}
	if !errors.Is(err, port.ErrWriteVerification) {
		return err
	}
	if !verificationOnly(err) {
		// Verification failed after the write. Let the caller record the applied
		// write in its journal, while retaining the failure for the pass result.
		r.failure = errors.Join(r.failure, err)
		return nil
	}
	if a == nil {
		r.record(port.MirrorAction{Kind: port.MirrorWarning, Detail: err.Error()})
	} else {
		a.Warnings = append(a.Warnings, err.Error())
	}
	return nil
}

// verificationOnly stops at the sentinel, whose wrapped conflict is part of the
// finding. Any additional leaf (including a failed listing) remains an error.
func verificationOnly(err error) bool {
	if err == port.ErrWriteVerification {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, part := range joined.Unwrap() {
			if !verificationOnly(part) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return verificationOnly(wrapped.Unwrap())
	}
	return false
}

func (r *run) record(a port.MirrorAction) {
	if a.Err != nil || a.Kind == port.MirrorRefuse || a.Kind == port.MirrorSkip {
		r.pass.Pending++
		if a.Document != "" {
			r.deferredDrawings[a.Document] = true
		}
	}
	r.pass.Actions = append(r.pass.Actions, a)
}
func (r *run) finish(ctx context.Context, err error) port.MirrorPass {
	r.pass.Ended = r.e.timer.Now()
	err = errors.Join(err, r.failure)
	if err == nil {
		for _, a := range r.pass.Actions {
			if a.Err != nil {
				err = fmt.Errorf("%w: %s: %w", errSteps, a.Path, a.Err)
				break
			}
		}
	}
	r.pass.Err = err
	if r.opts.DryRun {
		return r.pass
	}
	r.j.LastPass, r.j.Pending, r.j.LastError = r.pass.Ended, r.pass.Pending, ""
	if err != nil {
		r.j.LastError = err.Error()
	}
	r.pass.Err = errors.Join(err, r.j.write(ctx, r.root))
	return r.pass
}

// Drawing I/O failures are local to a drawing. Journal failures alone stop the pass.
func (r *run) writeJournal(ctx context.Context) error {
	err := r.j.write(ctx, r.root)
	if err != nil {
		r.failure = err
	}
	return err
}

func (r *run) checkpoint(ctx context.Context) {
	if r.opts.DryRun || r.e.timer.Now().Sub(r.saved) < checkpointEvery {
		return
	}
	_ = r.writeJournal(ctx)
	r.saved = r.e.timer.Now()
}

func (r *run) identity(ctx context.Context) error {
	if source, ok := r.e.cloud.(port.OutputSourceReader); ok {
		identity, err := source.OutputSource(ctx)
		if err != nil {
			return err
		}
		if r.j.Endpoint != "" && (r.j.Endpoint != identity.Endpoint || r.j.Account != identity.Account) {
			return fmt.Errorf("%w: journal belongs to another cloud endpoint or account", port.ErrMirrorRefused)
		}
		r.j.Endpoint, r.j.Account = identity.Endpoint, identity.Account
	}
	return nil
}

func (r *run) findFolder(all []port.Entry, folder string) (port.Entry, error) {
	var top port.Entry
	key := foldName(folder)
	for _, c := range all {
		folded := foldName(c.Path)
		if (folded == key || strings.HasPrefix(folded, key+"/")) && (c.Path != path.Clean(c.Path) || !safeRel(c.Path, false)) {
			return top, fmt.Errorf("%w: unsafe cloud path %q", port.ErrMirrorRefused, c.Path)
		}
		if r.j.Folder != "" && c.ID == r.j.Folder && c.Path != r.j.Path {
			return top, fmt.Errorf("%w: folder %s is now at /%s", port.ErrMirrorRefused, r.spec.Folder, c.Path)
		}
		if folded == key {
			if top.ID != "" {
				return top, fmt.Errorf("%w: ambiguous cloud folders: %s and %s", port.ErrMirrorRefused, top.Path, c.Path)
			}
			top = c
		}
	}
	return top, nil
}

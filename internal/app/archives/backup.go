// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package archives keeps whole Supernote files in incremental local backups.
package archives

import (
	"context"
	"crypto/md5" // #nosec G501 -- Supernote's content identifier.
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/altessa-s/go-atlas-core/runtime/concurrency"

	"github.com/altessa-s/snctl/internal/port"
)

const manifestName = "manifest.json"
const formatVersion = 1
const defaultJobs = 4

// Service binds backup storage to the configured cloud and upload service.
type Service struct {
	Cloud             port.Files
	Local             port.BackupLocal
	Upload            port.BackupUploader
	Profile, Endpoint string
	Now               func() time.Time
}

// Backup publishes its manifest after every file and retained removal is whole.
// Content-addressed names keep the previous manifest usable until publication.
func (s Service) Backup(ctx context.Context, dir, query string, o port.BackupOptions) (port.BackupReport, error) {
	r := port.BackupReport{Dir: dir}
	if o.Jobs < 0 {
		return r, fmt.Errorf("%w: --jobs must not be negative", port.ErrArgument)
	}
	if o.All {
		return r, fmt.Errorf("%w: --all is unavailable: the recycle-bin API has no original paths and complete MD5 metadata", port.ErrArgument)
	}
	root, err := s.Local.OpenBackup(ctx, dir, true)
	if err != nil {
		return r, err
	}
	defer func() { _ = root.Close() }()
	old, err := readManifest(root)
	if err != nil {
		return r, err
	}
	if old.Version != 0 && (old.Profile != s.Profile || old.Endpoint != s.Endpoint) && !o.Force {
		return r, fmt.Errorf("%w: directory belongs to another profile or endpoint; use --force", port.ErrRefused)
	}
	docs, live, covered, err := s.plan(ctx, query, old, o)
	if err != nil {
		return r, err
	}
	slices.SortFunc(docs, func(a, b port.Entry) int { return strings.Compare(a.Path, b.Path) })
	next := port.BackupManifest{Version: formatVersion, Profile: s.Profile, Endpoint: s.Endpoint, Created: s.Now().UTC(), Path: covered,
		Files: make([]port.BackupEntry, len(docs))}
	if err = s.writeFiles(ctx, root, docs, old, &next, o, &r); err != nil {
		return r, err
	}
	cleanup, err := setAside(ctx, root, old, next, live, o.Prune && old.Path == covered, &r)
	if err != nil {
		return r, err
	}
	b, err := json.Marshal(next, json.Deterministic(true))
	if err != nil {
		return r, err
	}
	if err := root.Produce(ctx, manifestName, func(w io.Writer) error { _, err := w.Write(b); return err }); err != nil {
		return r, err
	}
	r.Documents = len(next.Files)
	// Publication has succeeded. Cleanup cannot make either generation incomplete.
	for _, name := range cleanup {
		if err := root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return r, err
		}
	}
	return r, nil
}

func under(name, scope string) bool {
	return scope == "/" || name == scope || strings.HasPrefix(name, scope+"/")
}

func validEntry(e port.Entry) error {
	hash, err := hex.DecodeString(e.Hash)
	if err != nil || len(hash) != md5.Size || e.Size == nil || *e.Size < 0 {
		return fmt.Errorf("%s: missing MD5 or size", e.Path)
	}
	if e.ID == "" || strings.ContainsAny(e.ID, "/\\:") || e.ID == "." || e.ID == ".." {
		return errors.New("invalid backup file identifier")
	}
	if strings.ContainsAny(path.Ext(e.Name), "/\\:") {
		return errors.New("invalid file extension")
	}
	return nil
}

func (s Service) backupOne(ctx context.Context, root port.BackupDirectory, entry port.Entry, e port.BackupEntry) error {
	file, err := s.Cloud.Download(ctx, entry, nil)
	if err != nil {
		return err
	}
	defer func() { _ = s.Local.Remove(file) }()
	in, err := s.Local.Open(file)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	return root.Produce(ctx, e.Stored, func(w io.Writer) error { return checkedCopy(ctx, w, in, e) })
}

func checkedCopy(ctx context.Context, w io.Writer, in io.Reader, e port.BackupEntry) error {
	h := md5.New() // #nosec G401 -- Integrity against the cloud's MD5, not authentication.
	n, err := io.Copy(io.MultiWriter(w, h), contextReader{ctx, in})
	if err != nil {
		return err
	}
	if n != e.Size || hex.EncodeToString(h.Sum(nil)) != e.MD5 {
		return fmt.Errorf("%s: backup size or MD5 mismatch", e.Path)
	}
	return ctx.Err()
}

type contextReader struct {
	ctx context.Context
	io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(b)
}

func verify(ctx context.Context, root port.BackupDirectory, e port.BackupEntry) error {
	info, err := root.Info(e.Stored)
	if err != nil {
		return err
	}
	if !info.Regular || info.Size != e.Size {
		return fmt.Errorf("%s: missing or damaged backup file", e.Stored)
	}
	in, err := root.Open(e.Stored)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	return checkedCopy(ctx, io.Discard, in, e)
}

func readManifest(root port.BackupDirectory) (port.BackupManifest, error) {
	var m port.BackupManifest
	in, err := root.Open(manifestName)
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	defer func() { _ = in.Close() }()
	if err := json.UnmarshalRead(in, &m); err != nil {
		return m, fmt.Errorf("invalid backup manifest: %w", err)
	}
	if m.Version != formatVersion || m.Profile == "" || m.Path == "" || m.Path != path.Clean("/"+m.Path) {
		return m, errors.New("unsupported or damaged backup manifest")
	}
	seen := map[string]bool{}
	for _, e := range m.Files {
		if err := validEntry(port.Entry{ID: e.ID, Name: e.Name, Path: e.Path, Size: &e.Size, Hash: e.MD5}); err != nil {
			return m, err
		}
		if e.Path != path.Clean(e.Path) || !under("/"+e.Path, m.Path) || !validStored(e) || seen[e.Stored] {
			return m, errors.New("invalid backup entry path or stored name")
		}
		seen[e.Stored] = true
	}
	return m, nil
}

func setAside(ctx context.Context, root port.BackupDirectory, old, next port.BackupManifest,
	live map[string]bool, prune bool, r *port.BackupReport) ([]string, error) {
	kept := map[string]bool{}
	ids := map[string]bool{}
	for _, e := range next.Files {
		kept[e.Stored] = true
		ids[e.ID] = true
	}
	var cleanup []string
	for _, e := range old.Files {
		if kept[e.Stored] {
			continue
		}
		info, err := root.Info(e.Stored)
		if err != nil {
			return nil, err
		}
		if !info.Exists {
			continue
		}
		if !info.Regular {
			return nil, fmt.Errorf("%s: not a regular backup file", e.Stored)
		}
		cleanup = append(cleanup, e.Stored)
		if ids[e.ID] {
			continue
		}
		if prune && !live[e.ID] {
			r.Pruned++
			continue
		}
		if err = root.MkdirAll("removed"); err != nil {
			return nil, err
		}
		in, err := root.Open(e.Stored)
		if err != nil {
			return nil, err
		}
		err = root.Produce(ctx, "removed/"+path.Base(e.Stored), func(w io.Writer) error { return checkedCopy(ctx, w, in, e) })
		err = errors.Join(err, in.Close())
		if err != nil {
			return nil, err
		}
		r.Moved++
		if live[e.ID] {
			r.Outside++
		}
	}
	return cleanup, nil
}

func (s Service) writeFiles(ctx context.Context, root port.BackupDirectory, docs []port.Entry, old port.BackupManifest,
	next *port.BackupManifest, o port.BackupOptions, r *port.BackupReport) error {
	previous := map[string]port.BackupEntry{}
	for _, e := range old.Files {
		previous[e.ID] = e
	}
	if err := backupNames(docs, next); err != nil {
		return err
	}
	if err := root.MkdirAll("files"); err != nil {
		return err
	}
	jobs := o.Jobs
	if jobs == 0 {
		jobs = defaultJobs
	}
	var mu sync.Mutex
	progress := port.SyncProgress{FilesTotal: len(docs)}
	for _, e := range docs {
		progress.BytesTotal += *e.Size
	}
	indexes := make([]int, len(docs))
	for i := range indexes {
		indexes[i] = i
	}
	err := concurrency.Process(ctx, indexes, func(ctx context.Context, i int) error {
		e := next.Files[i]
		prior, known := previous[e.ID]
		skip := known && prior.MD5 == e.MD5 && prior.Size == e.Size && prior.Stored == e.Stored && verify(ctx, root, e) == nil
		if !skip {
			if err := s.backupOne(ctx, root, docs[i], e); err != nil {
				return err
			}
		}
		mu.Lock()
		defer mu.Unlock()
		if skip {
			r.Skipped++
		} else {
			r.Written++
		}
		progress.Files++
		progress.Bytes += e.Size
		if o.Progress != nil {
			o.Progress(progress)
		}
		return nil
	}, concurrency.WithConcurrency[int](jobs), concurrency.WithStopOnError[int]())
	if err != nil {
		return err
	}
	return nil
}

func backupNames(docs []port.Entry, next *port.BackupManifest) error {
	seen := map[string]bool{}
	for i, e := range docs {
		if err := validEntry(e); err != nil {
			return err
		}
		if seen[e.ID] {
			return fmt.Errorf("duplicate file identifier %s", e.ID)
		}
		seen[e.ID] = true
		stored := "files/" + e.ID + "-" + strings.ToLower(e.Hash) + strings.ToLower(path.Ext(e.Name))
		next.Files[i] = port.BackupEntry{ID: e.ID, Name: e.Name, Path: e.Path, Size: *e.Size, MD5: strings.ToLower(e.Hash), Updated: e.Modified, Stored: stored}
	}
	return nil
}

func (s Service) plan(ctx context.Context, query string, old port.BackupManifest, o port.BackupOptions) ([]port.Entry, map[string]bool, string, error) {
	cloud := s.Cloud
	if snapshot, ok := cloud.(port.FileSnapshot); ok {
		view, err := snapshot.Snapshot(ctx)
		if err != nil {
			return nil, nil, "", err
		}
		cloud = view
	}
	entries, err := cloud.List(ctx, "/", true)
	if err != nil {
		return nil, nil, "", err
	}
	covered := path.Clean("/" + query)
	if covered != "/" {
		e, statErr := cloud.Stat(ctx, covered)
		if statErr != nil {
			return nil, nil, "", statErr
		}
		covered = path.Clean("/" + e.Path)
	}
	if old.Version != 0 && old.Path != covered && !o.Rescope {
		return nil, nil, "", fmt.Errorf("%w: directory covers %s; use --rescope for %s", port.ErrRefused, old.Path, covered)
	}
	docs := []port.Entry{}
	live := map[string]bool{}
	for _, e := range entries {
		live[e.ID] = true
		if !e.Folder && under(path.Clean("/"+e.Path), covered) {
			docs = append(docs, e)
		}
	}
	return docs, live, covered, nil
}

func validStored(e port.BackupEntry) bool {
	return e.Path == strings.TrimPrefix(path.Clean("/"+e.Path), "/") && e.Path != "" && e.Name == path.Base(e.Path) &&
		e.Stored == "files/"+e.ID+"-"+e.MD5+strings.ToLower(path.Ext(e.Name))
}

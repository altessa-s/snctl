// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package cache stores each profile's last listing and MD5-addressed bodies.
package cache

import (
	"context"
	"crypto/md5" // #nosec G501 -- Supernote identifies content by MD5.
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/altessa-s/snctl/internal/adapter/flock"
	"github.com/altessa-s/snctl/internal/adapter/fsdir"
	"github.com/altessa-s/snctl/internal/port"
)

const dirMode = 0o700
const fileMode = 0o600

// Bookkeeping may queue behind other processes, but never waits on body I/O.
const lockWait = 30 * time.Second
const lockPoll = 10 * time.Millisecond

// Store serializes local state changes with an operating-system lock.
// The lock file stays in place so concurrent processes lock the same inode.
type Store struct {
	Dir, Profile, Source string
	Budget               int64
	Now                  func() time.Time
	invalidations        atomic.Uint64
	cleared              atomic.Uint64
	mu                   sync.Mutex
	recognitionMu        sync.Mutex
	recognitionLoaded    bool
	recognitionValues    map[string]bool
}

func (s *Store) locked(ctx context.Context, body func() error) error {
	ctx, cancel := context.WithTimeout(ctx, lockWait)
	defer cancel()
	if err := waitLock(ctx, func() (bool, error) { return !s.mu.TryLock(), nil }); err != nil {
		return err
	}
	defer s.mu.Unlock()
	if err := privateDir(s.Dir); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, "lock"), os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := f.Chmod(fileMode); err != nil {
		return err
	}
	if err := waitLock(ctx, func() (bool, error) { return flock.TryLock(f) }); err != nil {
		return err
	}
	defer func() { _ = flock.Unlock(f) }()
	if generation := s.invalidations.Load(); generation != s.cleared.Load() {
		if err := errors.Join(s.remove("listing"), s.remove("sync")); err != nil {
			return err
		}
		s.cleared.Store(generation)
	}
	return body()
}

func waitLock(ctx context.Context, try func() (bool, error)) error {
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("profile cache is busy: %w", err)
		}
		busy, err := try()
		if err != nil {
			return err
		}
		if !busy {
			return nil
		}
		timer := time.NewTimer(lockPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (s *Store) write(name string, value []byte) error {
	f, err := os.CreateTemp(s.Dir, ".part-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_, werr := f.Write(value)
	if err := errors.Join(werr, f.Sync(), f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(s.Dir, name))
}

func (s *Store) read(name string, value any) error {
	root, err := os.OpenRoot(s.Dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	b, err := root.ReadFile(name)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, value)
}

func (s *Store) save(name string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.write(name, b)
}

// Listing reads without creating directories or changing recency.
func (s *Store) Listing() (port.Listing, error) {
	var l port.Listing
	if err := s.read("listing", &l); err != nil || l.Confirmed.IsZero() || s.invalidations.Load() != s.cleared.Load() {
		return l, errors.New("cached listing is missing or damaged; run snctl sync online")
	}
	return l, nil
}

// SaveListing atomically replaces the last cloud-confirmed listing.
func (s *Store) SaveListing(ctx context.Context, l port.Listing) error {
	err := s.locked(ctx, func() error { return s.save("listing", l) })
	if err != nil {
		s.invalidations.Add(1)
	}
	return err
}

func validHash(hash string) bool {
	h, err := hex.DecodeString(hash)
	return err == nil && len(h) == md5.Size && hash == strings.ToLower(hash)
}

func valid(e port.Entry) bool { return validHash(e.Hash) && e.Size != nil && *e.Size >= 0 }

func (s *Store) open(ctx context.Context, e port.Entry) (*os.File, error) {
	if !valid(e) {
		return nil, errors.New("missing MD5 or size")
	}
	var f *os.File
	err := s.locked(ctx, func() error {
		root, err := os.OpenRoot(filepath.Join(s.Dir, "blobs"))
		if err != nil {
			return err
		}
		defer func() { _ = root.Close() }()
		f, err = root.Open(e.Hash)
		if err == nil {
			now := s.Now()
			_ = root.Chtimes(e.Hash, now, now)
		}
		return err
	})
	return f, err
}

// check hashes and optionally copies through an open descriptor outside the lock.
// Unix keeps an evicted inode readable; Windows eviction skips open bodies.
func (s *Store) check(ctx context.Context, e port.Entry, out io.Writer) (bool, error) {
	f, err := s.open(ctx, e)
	if errors.Is(err, fs.ErrNotExist) || !valid(e) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return false, err
	}
	h := md5.New() // #nosec G401 -- Integrity against the cloud's content identifier, not authentication.
	n, copyErr := io.Copy(io.MultiWriter(out, h), contextReader{ctx, f})
	closeErr := f.Close()
	if copyErr = errors.Join(copyErr, closeErr); copyErr != nil {
		return false, copyErr
	}
	if n == *e.Size && hex.EncodeToString(h.Sum(nil)) == e.Hash {
		return true, nil
	}
	err = s.locked(ctx, func() error {
		current, statErr := os.Stat(filepath.Join(s.Dir, "blobs", e.Hash))
		if errors.Is(statErr, fs.ErrNotExist) {
			return nil
		}
		if statErr != nil {
			return statErr
		}
		if os.SameFile(info, current) {
			return s.drop(e.Hash)
		}
		return nil
	})
	return false, err
}

type contextReader struct {
	ctx context.Context
	io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

// Body verifies size and MD5 before returning an independent temporary copy.
func (s *Store) Body(ctx context.Context, e port.Entry) (string, bool, error) {
	if err := privateDir(s.Dir); err != nil {
		return "", false, err
	}
	out, err := os.CreateTemp(s.Dir, ".read-")
	if err != nil {
		return "", false, err
	}
	found, readErr := s.check(ctx, e, out)
	err = errors.Join(readErr, out.Close())
	if !found || err != nil {
		_ = os.Remove(out.Name())
		return "", false, err
	}
	return out.Name(), true, nil
}

// Holds verifies a body and counts it as recently used, reporting cache failures.
func (s *Store) Holds(ctx context.Context, e port.Entry) (bool, error) {
	return s.check(ctx, e, io.Discard)
}

// Keep installs a body only after checking its advertised bytes outside the lock.
func (s *Store) Keep(ctx context.Context, e port.Entry, source string) error {
	in, err := os.Open(source) // #nosec G304 -- Caller-owned local upload or download staging file.
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	return s.keep(ctx, e, in)
}

func (s *Store) keep(ctx context.Context, e port.Entry, in io.Reader) error {
	if !valid(e) {
		return fmt.Errorf("%s: missing MD5 or size", e.Path)
	}
	if s.Budget > 0 && *e.Size > s.Budget {
		return errors.New("body exceeds cache budget")
	}
	if err := privateDir(s.Dir); err != nil {
		return err
	}
	out, err := os.CreateTemp(s.Dir, ".part-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(out.Name()) }()
	h := md5.New() // #nosec G401 -- The cloud's content identifier is MD5.
	n, copyErr := io.Copy(io.MultiWriter(out, h), contextReader{ctx, in})
	if err := errors.Join(copyErr, out.Sync(), out.Close()); err != nil {
		return err
	}
	if n != *e.Size || hex.EncodeToString(h.Sum(nil)) != e.Hash {
		return fmt.Errorf("%s: body size or MD5 changed", e.Path)
	}
	return s.locked(ctx, func() error {
		dir := filepath.Join(s.Dir, "blobs")
		if err := privateDir(dir); err != nil {
			return err
		}
		if err := os.Rename(out.Name(), filepath.Join(dir, e.Hash)); err != nil {
			if openBodyError(err) {
				return s.reconcile()
			}
			return err
		}
		now := s.Now()
		if err := os.Chtimes(filepath.Join(dir, e.Hash), now, now); err != nil {
			return err
		}
		return s.reconcile()
	})
}

func (s *Store) blobs() ([]fs.FileInfo, int64, error) {
	entries, err := fsdir.Read(filepath.Join(s.Dir, "blobs"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var files []fs.FileInfo
	var total int64
	for _, e := range entries {
		if !validHash(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, 0, err
		}
		if info.Mode().IsRegular() {
			files = append(files, info)
			total += info.Size()
		}
	}
	return files, total, nil
}

func (s *Store) removals() uint64 {
	var n uint64
	if err := s.read("removals", &n); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ^uint64(0)
	}
	return n
}

func (s *Store) drop(hash string) error {
	if err := s.remove("sync"); err != nil {
		return err
	}
	n := s.removals()
	if n == ^uint64(0) {
		n = 0
	}
	n++
	if err := s.save("removals", n); err != nil {
		return err
	}
	return s.remove(filepath.Join("blobs", hash))
}

func (s *Store) remove(name string) error {
	err := os.Remove(filepath.Join(s.Dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) reconcile() error {
	if s.Budget == 0 {
		return nil
	}
	files, total, err := s.blobs()
	if err != nil {
		return err
	}
	slices.SortFunc(files, func(a, b fs.FileInfo) int { return a.ModTime().Compare(b.ModTime()) })
	for _, f := range files {
		if total <= s.Budget {
			break
		}
		if err := s.drop(f.Name()); err != nil {
			if openBodyError(err) {
				continue
			}
			return err
		}
		total -= f.Size()
	}
	return nil
}

// Reconcile applies a lowered budget when the profile cache is opened.
func (s *Store) Reconcile(ctx context.Context) error {
	return s.locked(ctx, func() error {
		if _, err := s.temporaries(true); err != nil {
			return err
		}
		return s.reconcile()
	})
}

// temporaries removes only abandoned staging files, preserving recent readers.
func (s *Store) temporaries(sweep bool) (int, error) {
	entries, err := fsdir.Read(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".part-") && !strings.HasPrefix(e.Name(), ".read-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return count, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if sweep && s.Now().Sub(info.ModTime()) > time.Hour {
			if err := s.remove(e.Name()); err != nil {
				return count, err
			}
			continue
		}
		count++
	}
	return count, nil
}

// Status reports bodies and durable state without cloud access.
func (s *Store) Status() (port.CacheStatus, error) {
	files, total, err := s.blobs()
	temps, tempErr := s.temporaries(false)
	l, _ := s.Listing()
	return port.CacheStatus{Profile: s.Profile, Dir: s.Dir, Bytes: total, Budget: s.Budget, BudgetSource: s.Source,
		Blobs: len(files), Temporaries: temps, Confirmed: l.Confirmed, Removals: s.removals()}, errors.Join(err, tempErr)
}

// Clear removes cache-owned entries and retains the cross-process lock.
func (s *Store) Clear(ctx context.Context) error {
	return s.locked(ctx, func() error {
		files, _, err := s.blobs()
		if err != nil {
			return err
		}
		for _, f := range files {
			if err := s.drop(f.Name()); err != nil {
				return err
			}
		}
		if n := s.removals(); n == ^uint64(0) {
			if err := s.save("removals", uint64(1)); err != nil {
				return err
			}
		}
		s.recognitionMu.Lock()
		s.recognitionLoaded, s.recognitionValues = false, nil
		s.recognitionMu.Unlock()
		for _, name := range []string{"listing", "sync", recognitionFile} {
			if err := s.remove(name); err != nil {
				return err
			}
		}
		return nil
	})
}

// LastSync rejects records from before any eviction or damaged removal state.
func (s *Store) LastSync() (port.SyncMark, bool) {
	var m port.SyncMark
	err := s.read("sync", &m)
	n := s.removals()
	return m, err == nil && n != ^uint64(0) && m.Removals == n
}

// MarkSync commits only while no body has been removed since verification began.
func (s *Store) MarkSync(ctx context.Context, m port.SyncMark) error {
	return s.locked(ctx, func() error {
		if n := s.removals(); n == ^uint64(0) || m.Removals != n {
			return errors.New("cached bodies were removed during sync; run sync again")
		}
		return s.save("sync", m)
	})
}

// Invalidate discards a listing that a cloud write may have changed.
func (s *Store) Invalidate(ctx context.Context) error {
	s.invalidations.Add(1)
	return s.locked(ctx, func() error { return errors.Join(s.remove("listing"), s.remove("sync")) })
}

func privateDir(path string) error {
	if err := os.MkdirAll(path, dirMode); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s: cache directory must not be a link", path)
	}
	return os.Chmod(path, dirMode) // #nosec G302 -- This is a directory; owner search permission is required.
}

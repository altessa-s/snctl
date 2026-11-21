// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cache

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/adapter/flock"
	"github.com/altessa-s/snctl/internal/port"
)

func fixture(t *testing.T) *Store {
	t.Helper()
	return &Store{Dir: filepath.Join(t.TempDir(), "profile"), Profile: "test", Budget: 10, Source: "env", Now: func() time.Time { return time.Unix(100, 0) }}
}
func body(t *testing.T, data string) (port.Entry, string) {
	t.Helper()
	n := int64(len(data))
	h := md5.Sum([]byte(data))
	e := port.Entry{Hash: hex.EncodeToString(h[:]), Size: &n, Path: "NOTE/Note/test"}
	file := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(file, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return e, file
}
func TestLifecycle(t *testing.T) {
	s := fixture(t)
	if _, err := s.Listing(); err == nil {
		t.Fatal("missing listing")
	}
	if _, ok := s.LastSync(); ok {
		t.Fatal("missing sync")
	}
	// A profile that never cached anything reports an empty cache, not an error.
	if status, err := s.Status(); err != nil || status.Blobs != 0 || status.Temporaries != 0 || status.Bytes != 0 {
		t.Fatal("status before first use", status, err)
	}
	if err := s.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	a, af := body(t, "aaaaaa")
	b, bf := body(t, "bbbbbb")
	l := port.Listing{Entries: []port.Entry{a}, Confirmed: s.Now()}
	if err := s.SaveListing(t.Context(), l); err != nil {
		t.Fatal(err)
	}
	if err := s.Keep(t.Context(), a, af); err != nil {
		t.Fatal(err)
	}
	mark := port.SyncMark{Listing: l, Files: true}
	if err := s.MarkSync(t.Context(), mark); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.LastSync(); !ok {
		t.Fatal("mark")
	}
	copy, ok, err := s.Body(t.Context(), a)
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if data, err := os.ReadFile(copy); err != nil || string(data) != "aaaaaa" {
		t.Fatalf("%s %v", data, err)
	}
	if err := os.Remove(copy); err != nil {
		t.Fatal(err)
	}
	if !holds(t, s, a) {
		t.Fatal("caller removed retained body")
	}
	s.Now = func() time.Time { return time.Unix(200, 0) }
	if err := s.Keep(t.Context(), b, bf); err != nil {
		t.Fatal(err)
	}
	if holds(t, s, a) || !holds(t, s, b) {
		t.Fatal("LRU")
	}
	if _, ok := s.LastSync(); ok {
		t.Fatal("eviction retained sync")
	}
	if err := s.MarkSync(t.Context(), mark); err == nil {
		t.Fatal("old removal count")
	}
	st, err := s.Status()
	if err != nil || st.Bytes != 6 || st.Removals != 1 {
		t.Fatalf("%+v %v", st, err)
	}
	if _, err := s.Listing(); err != nil {
		t.Fatal("listing evicted", err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "blobs", b.Hash), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if holds(t, s, b) {
		t.Fatal("damaged MD5 accepted")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "blobs", b.Hash)); !os.IsNotExist(err) {
		t.Fatal("damaged body retained")
	}
	if err := s.Keep(t.Context(), b, bf); err != nil {
		t.Fatal(err)
	}
	wrong := b
	size := int64(7)
	wrong.Size = &size
	if _, ok, err := s.Body(t.Context(), wrong); err != nil || ok {
		t.Fatalf("wrong size: %v %v", ok, err)
	}
	if err := s.Clear(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.Invalidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Listing(); err == nil {
		t.Fatal("clear listing")
	}
	for _, name := range []string{"lock", "removals"} {
		fi, err := os.Stat(filepath.Join(s.Dir, name))
		if err != nil || fi.Mode().Perm() != cacheMode(0600) {
			t.Fatalf("mode %s %v", name, err)
		}
	}
	fi, err := os.Stat(s.Dir)
	if err != nil || fi.Mode().Perm() != cacheMode(0700) {
		t.Fatalf("directory mode %v", err)
	}
}

func TestValidationAndBudget(t *testing.T) {
	s := fixture(t)
	s.Budget = 0
	a, file := body(t, "hello")
	if err := s.Keep(t.Context(), port.Entry{}, file); err == nil {
		t.Fatal("invalid metadata")
	}
	if holds(t, s, port.Entry{}) {
		t.Fatal("invalid hit")
	}
	if err := s.Keep(t.Context(), a, "missing"); err == nil {
		t.Fatal("missing source")
	}
	bad := a
	n := int64(4)
	bad.Size = &n
	if err := s.Keep(t.Context(), bad, file); err == nil {
		t.Fatal("wrong download size")
	}
	if err := s.Keep(t.Context(), a, file); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "blobs", "unrelated"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.Dir, "blobs", "01234567890123456789012345678901"), 0700); err != nil {
		t.Fatal(err)
	}
	s.Budget = 1
	if err := s.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if holds(t, s, a) {
		t.Fatal("lower budget")
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "removals"), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.LastSync(); ok {
		t.Fatal("bad removal count")
	}
	if err := s.MarkSync(t.Context(), port.SyncMark{}); err == nil {
		t.Fatal("bad removal count trusted")
	}
	if err := s.Clear(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "blobs", "unrelated")); err != nil {
		t.Fatal("foreign file removed")
	}
}

func TestDiskFailures(t *testing.T) {
	a, file := body(t, "hello")
	t.Run("root", func(t *testing.T) {
		s := fixture(t)
		if err := os.WriteFile(s.Dir, []byte("file"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := s.Reconcile(t.Context()); err == nil {
			t.Fatal("root failure")
		}
		if _, _, err := s.Body(t.Context(), a); err == nil {
			t.Fatal("body root failure")
		}
		if _, err := s.Status(); err == nil {
			t.Fatal("status root failure")
		}
	})
	for _, target := range []string{"lock", "listing", "blobs", "sync", "removals"} {
		t.Run(target, func(t *testing.T) {
			s := fixture(t)
			if err := s.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := s.Keep(t.Context(), a, file); err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(s.Dir, target)
			if target == "blobs" {
				if err := os.RemoveAll(p); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, nil, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				_ = os.Remove(p)
				if err := os.Mkdir(p, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(p, "child"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			switch target {
			case "lock":
				if err := s.SaveListing(t.Context(), port.Listing{}); err == nil {
					t.Fatal("lock")
				}
			case "listing":
				if err := s.SaveListing(t.Context(), port.Listing{}); err == nil {
					t.Fatal("listing")
				}
				if err := s.Invalidate(t.Context()); err == nil {
					t.Fatal("invalidate")
				}
			case "blobs":
				if err := s.Keep(t.Context(), a, file); err == nil {
					t.Fatal("blobs")
				}
				if err := s.Clear(t.Context()); err == nil {
					t.Fatal("clear")
				}
				if err := s.Reconcile(t.Context()); err == nil {
					t.Fatal("reconcile")
				}
			case "sync", "removals":
				s.Budget = 1
				if err := s.Reconcile(t.Context()); err == nil {
					t.Fatal("durable state")
				}
				if err := s.Clear(t.Context()); err == nil {
					t.Fatal("clear state")
				}
			}
		})
	}
}

func TestPrivateDirectoriesAndLinkedBodies(t *testing.T) {
	s := fixture(t)
	if err := os.MkdirAll(s.Dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := s.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(s.Dir)
	if err != nil || fi.Mode().Perm() != cacheMode(0700) {
		t.Fatal("directory not private", err)
	}
	e, file := body(t, "hello")
	if err := s.Keep(t.Context(), e, file); err != nil {
		t.Fatal(err)
	}
	cached := filepath.Join(s.Dir, "blobs", e.Hash)
	if err := os.Remove(cached); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file, cached); err != nil {
		t.Skip(err)
	}
	if held, err := s.Holds(t.Context(), e); held || err == nil {
		t.Fatal("read body outside cache", held, err)
	}
	other := fixture(t)
	if err := os.Symlink(s.Dir, other.Dir); err != nil {
		t.Fatal(err)
	}
	if err := other.Reconcile(t.Context()); err == nil {
		t.Fatal("linked profile accepted")
	}
}

func holds(t *testing.T, s *Store, e port.Entry) bool {
	t.Helper()
	held, err := s.Holds(t.Context(), e)
	if err != nil {
		t.Fatal(err)
	}
	return held
}

// pausedReader models a large body whose source has not finished transferring.
type pausedReader struct {
	io.Reader
	started chan struct{}
	resume  chan struct{}
}

func (r *pausedReader) Read(b []byte) (int, error) {
	if r.started != nil {
		close(r.started)
		r.started = nil
		<-r.resume
	}
	return r.Reader.Read(b)
}

func TestBodyStagingDoesNotHoldLock(t *testing.T) {
	s := fixture(t)
	s.Budget = 0
	e, source := body(t, "already cached")
	if err := s.Keep(t.Context(), e, source); err != nil {
		t.Fatal(err)
	}
	other := &Store{Dir: s.Dir, Now: s.Now}
	data := strings.Repeat("slow body", 1024)
	slow, _ := body(t, data)
	reader := &pausedReader{Reader: strings.NewReader(data), started: make(chan struct{}), resume: make(chan struct{})}
	started := reader.started
	done := make(chan error, 1)
	go func() { done <- s.keep(t.Context(), slow, reader) }()
	<-started
	// Every operation must finish while the first store is still copying.
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	name, found, err := other.Body(ctx, e)
	if err != nil || !found {
		t.Errorf("read during keep: %v %v", found, err)
	}
	_ = os.Remove(name)
	if err := other.Keep(ctx, e, source); err != nil {
		t.Error("keep during keep:", err)
	}
	if held, err := other.Holds(ctx, e); err != nil || !held {
		t.Errorf("holds during keep: %v %v", held, err)
	}
	if err := other.Invalidate(ctx); err != nil {
		t.Error("invalidate during keep:", err)
	}
	close(reader.resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !holds(t, other, slow) {
		t.Fatal("staged body missing")
	}
}

func TestLockCancellationAndDeferredInvalidation(t *testing.T) {
	s := fixture(t)
	if err := s.SaveListing(t.Context(), port.Listing{Confirmed: s.Now()}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, "lock"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if busy, err := flock.TryLock(f); busy || err != nil {
		t.Fatal(busy, err)
	}
	defer flock.Unlock(f)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := s.Invalidate(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, err := s.Listing(); err == nil {
		t.Fatal("trusted stale listing")
	}
	e, _ := body(t, "hello")
	if held, err := s.Holds(ctx, e); held || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(held, err)
	}
	if _, _, err := s.Body(ctx, e); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := flock.Unlock(f); err != nil {
		t.Fatal(err)
	}
	if err := s.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "listing")); !os.IsNotExist(err) {
		t.Fatal("stale listing retained", err)
	}
}

func TestDamagedRemovalCountRecoversOnRemoval(t *testing.T) {
	s := fixture(t)
	e, source := body(t, "hello")
	if err := s.Keep(t.Context(), e, source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "removals"), []byte("garbage"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "blobs", e.Hash), []byte("HELLO"), 0600); err != nil {
		t.Fatal(err)
	}
	if holds(t, s, e) {
		t.Fatal("damaged body accepted")
	}
	status, err := s.Status()
	if err != nil || status.Removals != 1 {
		t.Fatal(status, err)
	}
	if err := s.MarkSync(t.Context(), port.SyncMark{Removals: status.Removals}); err != nil {
		t.Fatal(err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestBodyIOFailuresAndBudget(t *testing.T) {
	s := fixture(t)
	e, source := body(t, "hello")
	if err := s.keep(t.Context(), e, failingReader{}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Keep(ctx, e, source); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	s.Budget = 1
	if err := s.Keep(t.Context(), e, source); err == nil {
		t.Fatal("retained body larger than budget")
	}
	s.Budget = 0
	if err := s.Keep(t.Context(), e, source); err != nil {
		t.Fatal(err)
	}
	if found, err := s.check(t.Context(), e, failingWriter{}); found || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(found, err)
	}
	if !holds(t, s, e) {
		t.Fatal("copy failure discarded intact body")
	}
	// Removal state must remain invalid when a damaged body's deletion fails.
	if err := os.WriteFile(filepath.Join(s.Dir, "blobs", e.Hash), []byte("HELLO"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.Dir, "sync"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "sync", "child"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Holds(t.Context(), e); err == nil {
		t.Fatal("hidden corruption cleanup failure")
	}
}

func TestOpenBodySurvivesEviction(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix unlinks an open file; Windows defers eviction")
	}
	s := fixture(t)
	e, source := body(t, "hello")
	if err := s.Keep(t.Context(), e, source); err != nil {
		t.Fatal(err)
	}
	f, err := s.open(t.Context(), e)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s.Budget = 1
	other := &Store{Dir: s.Dir, Now: s.Now, Budget: 1}
	if err := other.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	if err != nil || string(data) != "hello" {
		t.Fatal(string(data), err)
	}
	if held, err := s.Holds(t.Context(), e); held || err != nil {
		t.Fatal(held, err)
	}
}

func TestInProcessLockCancellation(t *testing.T) {
	s := fixture(t)
	s.mu.Lock()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	err := s.Reconcile(ctx)
	s.mu.Unlock()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := s.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestTemporarySweepAndClearRecovery(t *testing.T) {
	s := fixture(t)
	if err := s.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".part-old", ".read-old", ".part-recent", ".read-recent", "foreign"} {
		file := filepath.Join(s.Dir, name)
		if err := os.WriteFile(file, []byte("body"), 0600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "old") {
			stamp := s.Now().Add(-2 * time.Hour)
			if err := os.Chtimes(file, stamp, stamp); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.Chtimes(file, s.Now(), s.Now()); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Only regular staging files are temporaries; a directory with the same
	// prefix is neither counted nor swept, however old it is.
	staged := filepath.Join(s.Dir, ".part-directory")
	if err := os.Mkdir(staged, 0700); err != nil {
		t.Fatal(err)
	}
	if stamp := s.Now().Add(-2 * time.Hour); os.Chtimes(staged, stamp, stamp) != nil {
		t.Fatal("chtimes")
	}
	if err := s.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staged); err != nil {
		t.Fatal("swept a staging directory", err)
	}
	status, err := s.Status()
	if err != nil || status.Temporaries != 2 {
		t.Fatal(status, err)
	}
	for _, name := range []string{".part-old", ".read-old"} {
		if _, err := os.Stat(filepath.Join(s.Dir, name)); !os.IsNotExist(err) {
			t.Fatal(name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "removals"), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkSync(t.Context(), port.SyncMark{}); err == nil {
		t.Fatal("damaged generation trusted")
	}
	if err := s.Clear(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, err = s.Status()
	if err != nil || status.Removals == ^uint64(0) {
		t.Fatal(status, err)
	}
	if err := s.MarkSync(t.Context(), port.SyncMark{Removals: status.Removals}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.LastSync(); !ok {
		t.Fatal("clear failed to repair generation")
	}
}

// Windows exposes writable files as 0666 and directories as 0777; chmod
// does not express POSIX owner/group privacy there.
func cacheMode(posix os.FileMode) os.FileMode {
	if runtime.GOOS == "windows" {
		if posix == 0700 {
			return 0777
		}
		return 0666
	}
	return posix
}

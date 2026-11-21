// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

type eventWatcher struct {
	events chan struct{}
	ready  chan struct{}
	fail   error
}

func (w *eventWatcher) Watch(ctx context.Context, _ string, _ func(string) bool, ready, changed func()) error {
	if w.fail != nil {
		return w.fail
	}
	ready()
	select {
	case w.ready <- struct{}{}:
	default:
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-w.events:
			changed()
		}
	}
}
func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("no result")
		var zero T
		return zero
	}
}
func sleepers(t *testing.T, tm *porttest.Timer, n int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if !tm.WaitSleepers(ctx, n) {
		t.Fatal("timer did not sleep")
	}
}
func TestWatchInjectedDirectoryEvents(t *testing.T) {
	t.Parallel()
	f := setup(t)
	tm := porttest.NewTimer(now)
	watcher := &eventWatcher{events: make(chan struct{}, 10), ready: make(chan struct{}, 1)}
	f.e.timer, f.e.dirs = tm, watcher
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	passes := make(chan port.MirrorPass, 20)
	done := make(chan error, 1)
	go func() {
		done <- f.e.Watch(ctx, f.spec, port.MirrorRunOptions{Poll: time.Second, Debounce: time.Millisecond, Rescan: time.Hour}, func(p port.MirrorPass) { passes <- p })
	}()
	receive(t, watcher.ready)
	receive(t, passes)
	f.write("new.pdf", "new")
	watcher.events <- struct{}{}
	sleepers(t, tm, 2)
	tm.Wake()
	if p := receive(t, passes); !has(p, port.MirrorUpload) {
		t.Fatal(p)
	}
	cancel()
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
}
func TestFeedRepeatsAndReconnect(t *testing.T) {
	t.Parallel()
	f := setup(t)
	tm := porttest.NewTimer(now)
	f.e.timer = tm
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	triggered := make(chan struct{}, 20)
	done := make(chan struct{})
	go func() { defer close(done); f.e.listen(ctx, func() { triggered <- struct{}{} }) }()
	event := port.Change{Type: "files", ID: "1", Kind: "update"}
	f.c.feed <- event
	receive(t, triggered)
	f.c.feed <- event
	f.c.feed <- port.Change{Type: "tasks", ID: "2"}
	f.c.feed <- port.Change{Type: "connected"}
	receive(t, triggered)
	select {
	case <-triggered:
		t.Fatal("repeat triggered a pass")
	default:
	}
	tm.Jump(repeatWindow)
	f.c.feed <- event
	receive(t, triggered)
	cancel()
	receive(t, done)
}
func TestPollingFallbackRescanAndSleepJump(t *testing.T) {
	t.Parallel()
	f := setup(t)
	tm := porttest.NewTimer(now)
	f.e.timer = tm
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch := make(chan struct{}, 10)
	done := make(chan struct{})
	watched := &atomic.Bool{}
	go func() {
		defer close(done)
		f.e.poll(ctx, f.dir, f.e.look(f.dir), time.Second, time.Minute, watched, func() { ch <- struct{}{} })
	}()
	sleepers(t, tm, 1)
	f.write("a.pdf", "a")
	tm.Wake()
	receive(t, ch)
	sleepers(t, tm, 1)
	tm.Jump(time.Hour)
	tm.Wake()
	receive(t, ch)
	cancel()
	receive(t, done)
	if f.e.look(filepath.Join(f.dir, "missing")) == "" {
		t.Fatal("missing directory had no signature")
	}
}
func TestWatchFallbackRetriesAndLock(t *testing.T) {
	t.Parallel()
	f := setup(t)
	tm := porttest.NewTimer(now)
	f.e.timer = tm
	f.e.dirs = &eventWatcher{fail: errBoom}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	triggered := make(chan struct{}, 5)
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.e.watchDir(ctx, f.dir, time.Minute, &atomic.Bool{}, func() { triggered <- struct{}{} })
	}()
	receive(t, triggered)
	sleepers(t, tm, 1)
	tm.Wake()
	receive(t, triggered)
	cancel()
	receive(t, done)
	ctx, cancel = context.WithCancel(t.Context())
	root, err := f.e.disk.OpenMirror(ctx, f.dir)
	if err != nil {
		t.Fatal(err)
	}
	passes := make(chan port.MirrorPass, 4)
	locked := make(chan bool, 1)
	go func() {
		held, ok := f.e.waitLock(ctx, f.spec, port.MirrorRunOptions{}, func(p port.MirrorPass) { passes <- p })
		if ok {
			held.Close()
		}
		locked <- ok
	}()
	if p := receive(t, passes); !errors.Is(p.Err, port.ErrMirrorBusy) {
		t.Fatal(p)
	}
	sleepers(t, tm, 1)
	root.Close()
	tm.Wake()
	if !receive(t, locked) {
		t.Fatal("lock was not retried")
	}
	cancel()
}

type advancingTimer struct {
	at    time.Time
	sleep func(context.Context, time.Duration) error
}

func (t *advancingTimer) Now() time.Time { return t.at }
func (t *advancingTimer) Sleep(ctx context.Context, d time.Duration) error {
	t.at = t.at.Add(d)
	if t.sleep != nil {
		return t.sleep(ctx, d)
	}
	return ctx.Err()
}
func TestSettleTimingAndCap(t *testing.T) {
	t.Parallel()
	f := setup(t)
	p, d, r := timing(port.MirrorRunOptions{})
	if p != DefaultPoll || d != DefaultDebounce || r != DefaultRescan {
		t.Fatal(p, d, r)
	}
	_, d, _ = timing(port.MirrorRunOptions{Debounce: -1})
	if d != 0 {
		t.Fatal(d)
	}
	wake := make(chan struct{}, 1)
	wake <- struct{}{}
	tm := &advancingTimer{at: now}
	f.e.timer = tm
	tm.sleep = func(context.Context, time.Duration) error { wake <- struct{}{}; return nil }
	if !f.e.settle(t.Context(), wake, 10*time.Second) || tm.at.Sub(now) != maxQuiet {
		t.Fatal(tm.at)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if f.e.settle(ctx, nil, time.Second) {
		t.Fatal("settled canceled context")
	}
	wake = make(chan struct{}, 1)
	wake <- struct{}{}
	tm.sleep = func(context.Context, time.Duration) error { return context.Canceled }
	if f.e.settle(t.Context(), wake, time.Second) {
		t.Fatal("ignored sleep failure")
	}
}
func TestWatchWholePassFailureRetriesAndCancellation(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.c.fail["list"] = errBoom
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	tm := porttest.NewTimer(now)
	f.e.timer = tm
	passes := make(chan port.MirrorPass, 10)
	done := make(chan error, 1)
	go func() {
		done <- f.e.Watch(ctx, f.spec, port.MirrorRunOptions{}, func(p port.MirrorPass) { passes <- p })
	}()
	if p := receive(t, passes); !errors.Is(p.Err, errBoom) {
		t.Fatal(p)
	}
	sleepers(t, tm, 2)
	tm.Wake()
	if p := receive(t, passes); !errors.Is(p.Err, errBoom) {
		t.Fatal(p)
	}
	cancel()
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
	f.spec.Dir = "relative"
	if err := f.e.Watch(t.Context(), f.spec, port.MirrorRunOptions{}, nil); !errors.Is(err, port.ErrMirrorSpec) {
		t.Fatal(err)
	}
}
func TestWatchWritingSchedulesAnotherPass(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.write("a.pdf", "a")
	if err := os.Chtimes(filepath.Join(f.dir, "a.pdf"), now, now); err != nil {
		t.Fatal(err)
	}
	tm := porttest.NewTimer(now)
	f.e.timer = tm
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	passes := make(chan port.MirrorPass, 10)
	done := make(chan error, 1)
	go func() {
		done <- f.e.Watch(ctx, f.spec, port.MirrorRunOptions{}, func(p port.MirrorPass) { passes <- p })
	}()
	if p := receive(t, passes); !writing(p) {
		t.Fatal(p)
	}
	sleepers(t, tm, 2)
	tm.Wake()
	if p := receive(t, passes); !has(p, port.MirrorUpload) {
		t.Fatal(p)
	}
	cancel()
	receive(t, done)
}

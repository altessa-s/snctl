// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest

import (
	"context"
	"sync"
	"time"

	"github.com/altessa-s/snctl/internal/port"
)

// Timer is a port.Timer a test drives: Sleep waits until Wake lets every
// sleeper go, moving the time on by the longest of their sleeps, or until
// its context ends. Jump moves the time without waking anyone, as a
// machine that slept does.
type Timer struct {
	mu       sync.Mutex
	now      time.Time
	sleepers []sleeper
	changed  chan struct{}
}

type sleeper struct {
	d    time.Duration
	done chan struct{}
}

// NewTimer returns a timer reading now.
func NewTimer(now time.Time) *Timer { return &Timer{now: now, changed: make(chan struct{})} }

var _ port.Timer = (*Timer)(nil)

// Now implements port.Timer.
func (t *Timer) Now() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.now
}

// Sleep implements port.Timer.
func (t *Timer) Sleep(ctx context.Context, d time.Duration) error {
	s := sleeper{d: d, done: make(chan struct{})}
	t.mu.Lock()
	t.sleepers = append(t.sleepers, s)
	t.signal()
	t.mu.Unlock()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		t.mu.Lock()
		for i, o := range t.sleepers {
			if o.done == s.done {
				t.sleepers = append(t.sleepers[:i], t.sleepers[i+1:]...)
				break
			}
		}
		t.signal()
		t.mu.Unlock()
		return ctx.Err()
	}
}

// signal tells WaitSleepers the sleepers changed; t.mu is held.
func (t *Timer) signal() {
	close(t.changed)
	t.changed = make(chan struct{})
}

// Sleepers is how many goroutines sleep now.
func (t *Timer) Sleepers() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sleepers)
}

// WaitSleepers waits until at least n goroutines sleep, or ctx ends.
func (t *Timer) WaitSleepers(ctx context.Context, n int) bool {
	for {
		t.mu.Lock()
		got, changed := len(t.sleepers), t.changed
		t.mu.Unlock()
		if got >= n {
			return true
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return false
		}
	}
}

// Wake lets every sleeper go and moves the time on by the longest sleep.
func (t *Timer) Wake() {
	t.mu.Lock()
	defer t.mu.Unlock()
	var longest time.Duration
	for _, s := range t.sleepers {
		longest = max(longest, s.d)
		close(s.done)
	}
	t.sleepers = nil
	t.now = t.now.Add(longest)
	t.signal()
}

// Jump moves the time on by d and wakes nobody.
func (t *Timer) Jump(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.now = t.now.Add(d)
}

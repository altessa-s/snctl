// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package files

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/altessa-s/snctl/internal/port"
)

// DefaultJobs is how many files a folder transfer moves at once.
const DefaultJobs = 4

// each runs do for every index, jobs at a time. It stops handing out work
// once ctx ends; do reports its own outcome, so one failure does not stop
// the others.
func each(ctx context.Context, indexes []int, jobs int, do func(context.Context, int)) {
	if jobs <= 0 {
		jobs = DefaultJobs
	}
	jobs = min(jobs, max(1, len(indexes)))
	work := make(chan int)
	var wg sync.WaitGroup
	for range jobs {
		wg.Go(func() {
			for i := range work {
				do(ctx, i)
			}
		})
	}
	for _, i := range indexes {
		if ctx.Err() != nil {
			break
		}
		work <- i
	}
	close(work)
	wg.Wait()
}

// tracker adds per-file byte callbacks into one bytes-and-files progress.
type tracker struct {
	mu     sync.Mutex
	report func(port.SyncProgress)
	done   map[int]int64
	state  port.SyncProgress
}

func newTracker(report func(port.SyncProgress), files int, bytes int64) *tracker {
	return &tracker{report: report, done: map[int]int64{}, state: port.SyncProgress{BytesTotal: bytes, FilesTotal: files}}
}

// file is the byte callback of the file at index i, or nil without a report.
func (t *tracker) file(i int) port.Progress {
	if t.report == nil {
		return nil
	}
	return func(n, _ int64) {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.state.Bytes += n - t.done[i]
		t.done[i] = n
		t.report(t.state)
	}
}

// finish counts a file as done, whatever became of it.
func (t *tracker) finish() {
	if t.report == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.Files++
	t.report(t.state)
}

func failed(item port.TransferItem, err error) port.TransferItem {
	item.Status, item.Reason = port.StatusFailed, reason(err)
	if errors.Is(err, port.ErrConflict) {
		item.Status = port.StatusExists
	}
	if strings.HasPrefix(item.Reason, "skipped: ") {
		item.Status = port.StatusSkipped
	}
	return item
}

func reason(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "interrupted"
	case errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	}
	return err.Error()
}

// outcome is the error a report ends with: the interruption first, then
// failures, then files refused because they already exist.
func outcome(ctx context.Context, r port.TransferReport, verb string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var errs []error
	for _, item := range r.Items {
		if item.Status == port.StatusSkipped && strings.HasPrefix(item.Reason, "skipped: ") {
			errs = append(errs, errors.New(item.Reason))
		}
	}
	if n := r.Counts[port.StatusFailed]; n > 0 {
		errs = append(errs, fmt.Errorf("%d of %d %s failed", n, len(r.Items), verb))
	}
	if n := r.Counts[port.StatusExists]; n > 0 {
		errs = append(errs, fmt.Errorf("%w: %d already in the cloud; --replace overwrites them", port.ErrConflict, n))
	}
	return errors.Join(errs...)
}

func size(e port.Entry) int64 {
	if e.Size == nil {
		return 0
	}
	return *e.Size
}

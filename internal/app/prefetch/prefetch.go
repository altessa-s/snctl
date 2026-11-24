// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package prefetch retains verified cloud bodies for offline reads.
package prefetch

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path"
	"reflect"
	"sync"

	"github.com/altessa-s/go-atlas-core/runtime/concurrency"

	"github.com/altessa-s/snctl/internal/port"
)

// Service fills the selected profile's cache after a live listing.
type Service struct {
	Recognition port.RecognitionOperation
	InitError   error
	Inspect     func(context.Context, port.Entry)
	Cloud       port.Files
	Cache       port.FileCache
	Local       port.LocalFiles
}

// Sync lists even when a previous complete sync covers the scope.
func (s Service) Sync(ctx context.Context, query string, opts port.SyncOptions) (port.SyncReport, error) {
	r := port.SyncReport{Path: path.Clean("/" + query)}
	if s.InitError != nil {
		return r, s.InitError
	}
	if s.Cache == nil {
		return r, fmt.Errorf("%w: sync requires the cache; remove --no-cache", port.ErrArgument)
	}
	ctx, flush := s.Recognition.Begin(ctx)
	defer flush()
	entries, err := s.scope(ctx, r.Path)
	if err != nil {
		return r, err
	}
	l, err := s.Cache.Listing()
	if err != nil {
		return r, err
	}
	status, err := s.Cache.Status()
	if err != nil {
		return r, err
	}
	r.Budget, r.BudgetSource = status.Budget, status.BudgetSource
	mark, marked := s.Cache.LastSync()
	covered := marked && !opts.Force && reflect.DeepEqual(mark.Listing.Entries, l.Entries) &&
		mark.Path == r.Path && (mark.Files || !opts.Files)
	bodies, missing, err := s.plan(ctx, entries, opts.Files, &r)
	if err != nil {
		return r, err
	}
	if r.Budget > 0 && r.Needed > r.Budget {
		return r, overBudget(r, status.Profile)
	}
	// A previously complete record never bypasses verification of retained bodies.
	r.Current = len(missing) == 0
	if covered && r.Current {
		r.CacheBytes = status.Bytes
		return r, nil
	}
	err = s.fetch(ctx, missing, opts, &r)
	status, statusErr := s.Cache.Status()
	r.CacheBytes = status.Bytes
	if joined := errors.Join(err, statusErr); joined != nil {
		return r, joined
	}
	for _, e := range bodies {
		held, err := s.Cache.Holds(ctx, e)
		if err != nil {
			return r, err
		}
		if !held {
			return r, fmt.Errorf("%s: cached body was lost during sync; run sync again", e.Path)
		}
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	return r, s.Cache.MarkSync(ctx, port.SyncMark{Listing: l, Path: r.Path, Files: opts.Files, Removals: status.Removals})
}

func overBudget(r port.SyncReport, profile string) error {
	setting := "budget under [profiles." + profile + ".cache]"
	switch r.BudgetSource {
	case "env":
		setting = "SNCTL_CACHE_BUDGET"
	case "config":
		setting = "budget under [cache]"
	}
	return fmt.Errorf("sync needs %d bytes; budget is %d bytes; raise %s (0 is unlimited); no bodies downloaded", r.Needed, r.Budget, setting)
}

func (s Service) plan(ctx context.Context, entries []port.Entry, files bool, r *port.SyncReport) ([]port.Entry, []port.Entry, error) {
	seen := map[string]bool{}
	var bodies, missing []port.Entry
	for _, e := range entries {
		if e.Folder {
			continue
		}
		r.Documents++
		if !files || seen[e.Hash] {
			continue
		}
		if e.Hash == "" || e.Size == nil || *e.Size < 0 {
			return nil, nil, fmt.Errorf("%s: missing MD5 or size", e.Path)
		}
		seen[e.Hash] = true
		bodies = append(bodies, e)
		if *e.Size > math.MaxInt64-r.Needed {
			return nil, nil, errors.New("sync byte count exceeds supported range")
		}
		r.Needed += *e.Size
		held, err := s.Cache.Holds(ctx, e)
		if err != nil {
			return nil, nil, err
		}
		if held {
			if s.Inspect != nil {
				s.Inspect(ctx, e)
			}
			r.Cached++
		} else {
			missing = append(missing, e)
		}
	}
	return bodies, missing, nil
}

func (s Service) fetch(ctx context.Context, missing []port.Entry, opts port.SyncOptions, r *port.SyncReport) error {
	jobs := opts.Jobs
	if jobs == 0 {
		jobs = 4
	}
	if jobs < 0 {
		return fmt.Errorf("%w: --jobs must not be negative", port.ErrArgument)
	}
	var mu sync.Mutex
	progress := port.SyncProgress{FilesTotal: len(missing)}
	for _, e := range missing {
		progress.BytesTotal += *e.Size
	}
	err := concurrency.Process(ctx, missing, func(ctx context.Context, e port.Entry) error {
		file, err := s.Cloud.Download(ctx, e, nil)
		if err != nil {
			return err
		}
		defer func() { _ = s.Local.Remove(file) }()
		if err := s.Cache.Keep(ctx, e, file); err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		r.Fetched++
		r.Bytes += *e.Size
		progress.Files++
		progress.Bytes += *e.Size
		if opts.Progress != nil {
			opts.Progress(progress)
		}
		return nil
	}, concurrency.WithConcurrency[port.Entry](jobs), concurrency.WithStopOnError[port.Entry]())
	return err
}

func (s Service) scope(ctx context.Context, query string) ([]port.Entry, error) {
	if query != "/" {
		e, err := s.Cloud.Stat(ctx, query)
		if err != nil {
			return nil, err
		}
		if !e.Folder {
			return []port.Entry{e}, nil
		}
	}
	return s.Cloud.List(ctx, query, true)
}

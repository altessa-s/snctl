// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"time"
)

// Listing is the last complete tree confirmed by the cloud.
type Listing struct {
	Endpoint  string    `json:"endpoint"`
	Account   string    `json:"account"`
	Entries   []Entry   `json:"entries"`
	Confirmed time.Time `json:"confirmed"`
}

// SyncMark records the listing and scope whose bodies were all retained.
type SyncMark struct {
	Listing  Listing `json:"listing"`
	Path     string  `json:"path"`
	Files    bool    `json:"files"`
	Removals uint64  `json:"removals"`
}

// CacheStatus describes one profile without reading credentials.
type CacheStatus struct {
	Profile      string    `json:"profile"`
	Dir          string    `json:"dir"`
	Bytes        int64     `json:"bytes"`
	Budget       int64     `json:"budget"`
	BudgetSource string    `json:"budget_source"`
	Temporaries  int       `json:"temporaries,omitzero"`
	Blobs        int       `json:"blobs"`
	Confirmed    time.Time `json:"confirmed"`
	Removals     uint64    `json:"removals"`
}

// FileCache keeps listing state and verified content addressed bodies.
// Body returns a caller-owned temporary copy, never the retained file.
type FileCache interface {
	Listing() (Listing, error)
	SaveListing(context.Context, Listing) error
	Invalidate(context.Context) error
	Body(context.Context, Entry) (string, bool, error)
	Holds(context.Context, Entry) (bool, error)
	Keep(context.Context, Entry, string) error
	Status() (CacheStatus, error)
	Clear(context.Context) error
	LastSync() (SyncMark, bool)
	MarkSync(context.Context, SyncMark) error
}

// CacheAdmin operates on the selected profile or every local profile.
type CacheAdmin interface {
	Status(context.Context, bool) ([]CacheStatus, error)
	Clear(context.Context, bool) ([]string, error)
}

// SyncOptions selects the bodies to retain and transfer parallelism.
type SyncOptions struct {
	Files, Force bool
	Jobs         int
	Progress     func(SyncProgress)
}

// SyncReport describes a completed or interrupted prefetch.
type SyncReport struct {
	Current      bool   `json:"current"`
	Path         string `json:"path"`
	Documents    int    `json:"documents"`
	Fetched      int    `json:"fetched"`
	Cached       int    `json:"cached"`
	Bytes        int64  `json:"bytes_fetched"`
	Needed       int64  `json:"bytes_needed"`
	CacheBytes   int64  `json:"cache_bytes"`
	Budget       int64  `json:"budget"`
	BudgetSource string `json:"budget_source"`
}

// Syncer fills the cache after a live listing and a budget check.
type Syncer interface {
	Sync(context.Context, string, SyncOptions) (SyncReport, error)
}

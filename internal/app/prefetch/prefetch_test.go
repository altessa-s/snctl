// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package prefetch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/port"
)

type double struct {
	port.Files
	port.FileCache
	port.LocalFiles
	failure     string
	held        bool
	statusCalls int
	downloads   int
	progress    int
}

func (d *double) err(stage string) error {
	if d.failure == stage {
		return errors.New(stage)
	}
	return nil
}
func (d *double) entry() port.Entry {
	n := int64(10)
	return port.Entry{Path: "test", Hash: "abcd", Size: &n}
}
func (d *double) List(context.Context, string, bool) ([]port.Entry, error) {
	e := d.entry()
	if d.failure == "metadata" {
		e.Size = nil
	}
	return []port.Entry{e}, d.err("scope")
}
func (d *double) Stat(context.Context, string) (port.Entry, error) { return d.entry(), d.err("stat") }
func (d *double) Download(context.Context, port.Entry, port.Progress) (string, error) {
	d.downloads++
	return "file", d.err("download")
}
func (d *double) Listing() (port.Listing, error) {
	return port.Listing{Confirmed: time.Unix(1, 0)}, d.err("listing")
}
func (d *double) Status() (port.CacheStatus, error) {
	d.statusCalls++
	if d.statusCalls > 1 && d.failure == "after" {
		return port.CacheStatus{}, errors.New("after")
	}
	return port.CacheStatus{}, d.err("status")
}
func (d *double) LastSync() (port.SyncMark, bool)                 { return port.SyncMark{}, false }
func (d *double) Holds(context.Context, port.Entry) (bool, error) { return d.held, d.err("holds") }
func (d *double) Keep(context.Context, port.Entry, string) error {
	if d.failure != "lost" {
		d.held = true
	}
	return d.err("keep")
}
func (d *double) Remove(string) error                           { return nil }
func (d *double) MarkSync(context.Context, port.SyncMark) error { return d.err("mark") }

func TestSyncFailures(t *testing.T) {
	if _, err := (Service{InitError: errors.New("busy cache")}).Sync(t.Context(), "/", port.SyncOptions{}); err == nil {
		t.Fatal("ignored cache initialization failure")
	}
	for _, stage := range []string{"scope", "holds", "listing", "status", "metadata", "download", "keep", "after", "lost", "mark"} {
		t.Run(stage, func(t *testing.T) {
			d := &double{failure: stage}
			s := Service{Cloud: d, Cache: d, Local: d}
			if _, err := s.Sync(t.Context(), "/", port.SyncOptions{Files: true}); err == nil {
				t.Fatal("accepted failure")
			}
		})
	}
	s := Service{}
	if _, err := s.Sync(t.Context(), "/", port.SyncOptions{}); err == nil {
		t.Fatal("nil cache")
	}
	d := &double{failure: "stat"}
	s = Service{Cloud: d, Cache: d, Local: d}
	if _, err := s.Sync(t.Context(), "test", port.SyncOptions{}); err == nil {
		t.Fatal("stat failure")
	}
	d.failure = ""
	r, err := s.Sync(t.Context(), "test", port.SyncOptions{Files: true, Jobs: 0, Progress: func(port.SyncProgress) { d.progress++ }})
	if err != nil || r.Fetched != 1 || d.downloads != 1 || d.progress != 1 {
		t.Fatalf("%+v %v %+v", r, err, d)
	}
	if _, err := s.Sync(t.Context(), "/", port.SyncOptions{Jobs: -1}); err == nil {
		t.Fatal("invalid jobs")
	}
	for _, source := range []string{"default", "config", "profile", "env"} {
		if err := overBudget(port.SyncReport{BudgetSource: source}, "test"); err == nil {
			t.Fatal("budget")
		}
	}
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package check

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func setup(f *porttest.Cloud) Service {
	return Service{Session: f, Files: f, Tasks: f, Digests: f, Feed: f, Probe: f, Local: f}
}

func TestReadOnlyAndWrite(t *testing.T) {
	t.Parallel()
	for _, write := range []bool{false, true} {
		f := &porttest.Cloud{Changes: []port.Change{{Type: "connected"}}}
		var steps []port.CheckStep
		err := setup(f).Run(t.Context(), write, func(s port.CheckStep) error { steps = append(steps, s); return nil })
		if err != nil {
			t.Fatal(err)
		}
		if len(steps) < 10 {
			t.Fatal("missing check results")
		}
		for _, step := range steps {
			if step.Status != "pass" && step.Status != "skipped" {
				t.Fatalf("failed %s", step.Name)
			}
		}
		if !write && (slices.Contains(f.Calls, "upload") || slices.Contains(f.Calls, "mkdir") || slices.Contains(f.Calls, "delete")) {
			t.Fatal("read check mutated account")
		}
		if write && !slices.Contains(f.Calls, "delete") {
			t.Fatal("write check omitted cleanup")
		}
		if slices.Contains(f.Calls, "empty") {
			t.Fatal("check emptied recycle bin")
		}
	}
}

func TestFailureCleanup(t *testing.T) {
	t.Parallel()
	for _, failAt := range []string{"info", "list", "web list", "download", "lists", "tasks", "digest list", "watch", "mkdir", "stage", "upload", "open", "move", "delete", "remove local"} {
		f := &porttest.Cloud{Changes: []port.Change{{Type: "connected"}}, Failure: errors.New("synthetic failure"), FailAt: failAt}
		var failed bool
		err := setup(f).Run(t.Context(), true, func(s port.CheckStep) error {
			if s.Status == "fail" {
				failed = true
			}
			return nil
		})
		if err == nil || !failed {
			t.Fatalf("ignored %s", failAt)
		}
		if failAt != "mkdir" && !slices.Contains(f.Calls, "delete") {
			t.Fatalf("no cleanup after %s", failAt)
		}
	}
	f := &porttest.Cloud{}
	if err := setup(f).Run(t.Context(), false, func(port.CheckStep) error { return nil }); err == nil {
		t.Fatal("empty feed passed")
	}
	f = &porttest.Cloud{Changes: []port.Change{{Type: "connected"}}}
	outputErr := errors.New("output failed")
	if err := setup(f).Run(t.Context(), true, func(port.CheckStep) error { return outputErr }); !errors.Is(err, outputErr) {
		t.Fatal("output failure ignored")
	}
	if !slices.Contains(f.Calls, "delete") {
		t.Fatal("output failure prevented cleanup")
	}
}

type foreignEntry struct {
	*porttest.Cloud
	clean bool
}

func (f *foreignEntry) List(ctx context.Context, name string, recursive bool) ([]port.Entry, error) {
	if strings.HasPrefix(name, "NOTE/Note/snctl-check-") {
		f.clean = ctx.Err() == nil
		return []port.Entry{{ID: "foreign"}}, nil
	}
	return f.Cloud.List(ctx, name, recursive)
}
func TestCleanupRetainsForeignData(t *testing.T) {
	t.Parallel()
	base := &porttest.Cloud{Changes: []port.Change{{Type: "connected"}}}
	f := &foreignEntry{Cloud: base}
	svc := setup(base)
	svc.Files = f
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var retained bool
	err := svc.Run(ctx, true, func(s port.CheckStep) error {
		if strings.Contains(s.Detail, "not empty") {
			retained = true
		}
		return nil
	})
	if err == nil || !retained || !f.clean {
		t.Fatal("cleanup did not protect foreign entry with fresh context")
	}
	count := 0
	for _, call := range base.Calls {
		if call == "delete" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("cleanup deleted %d entries, expected only the two created files", count)
	}
}

func TestReadDownload(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{nil, errors.New("download failed")} {
		f := &porttest.Cloud{Entries: []port.Entry{{ID: "10", Folder: true}, {ID: "11"}}, Changes: []port.Change{{Type: "connected"}}, Failure: failure, FailAt: "download"}
		err := setup(f).Run(t.Context(), false, func(port.CheckStep) error { return nil })
		if (err != nil) != (failure != nil) {
			t.Fatal("download check result")
		}
	}
}

func TestFinalScanDetectsCopiesAnywhere(t *testing.T) {
	t.Parallel()
	for _, entry := range []port.Entry{
		{ID: "copy", Path: "NOTE/other/snctl-check-synthetic/file.txt"},
		{ID: "created", Path: "INBOX/renamed.txt"},
	} {
		f := &porttest.Cloud{Entries: []port.Entry{entry}}
		r := &report{emit: func(port.CheckStep) error { return nil }}
		setup(f).verifyCleanup(t.Context(), r, "NOTE/Note/snctl-check-synthetic", []port.Entry{{ID: "created"}})
		if !r.failed {
			t.Fatal("leftover passed final scan")
		}
	}
}

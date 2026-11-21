// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package cloud

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/port"

	sncloud "github.com/altessa-s/sncloud-go"
)

type restoreDevice struct {
	treeDevice
	beforeList func(int)
	beforeStat func()
}

func (d *restoreDevice) List(ctx context.Context, name string, recursive bool) ([]sncloud.DeviceEntry, error) {
	if d.beforeList != nil {
		d.beforeList(d.listCalls + 1)
	}
	return d.treeDevice.List(ctx, name, recursive)
}

func (d *restoreDevice) EntryAt(ctx context.Context, name string) (*sncloud.DeviceEntry, error) {
	if d.beforeStat != nil {
		d.beforeStat()
	}
	return d.treeDevice.EntryAt(ctx, name)
}

type restoreWeb struct {
	fakeWebAPI
	d          *restoreDevice
	bin        []sncloud.RecycledEntry
	locations  map[sncloud.FileID]sncloud.Location
	onLocation func(sncloud.FileID)
	before     func(sncloud.FileID)
	failID     sncloud.FileID
}

func (w *restoreWeb) RecycleBin(context.Context, sncloud.SortOrder, bool) ([]sncloud.RecycledEntry, error) {
	return slices.Clone(w.bin), w.call("RecycleBin")
}

func (w *restoreWeb) Location(_ context.Context, id sncloud.FileID) (sncloud.Location, error) {
	if w.onLocation != nil {
		w.onLocation(id)
	}
	return w.locations[id], w.call("Location")
}

func (w *restoreWeb) Restore(_ context.Context, ids []sncloud.FileID) error {
	if err := w.call("Restore"); err != nil {
		return err
	}
	for _, id := range ids {
		if id == w.failID {
			return sncloud.ErrNotFound
		}
		if w.before != nil {
			w.before(id)
		}
		for _, e := range w.bin {
			if e.ID != id {
				continue
			}
			location := w.locations[id]
			for _, parent := range w.d.entries {
				if parent.ID == location.IDs[len(location.IDs)-1] {
					w.d.entries = append(w.d.entries, sncloud.DeviceEntry{ID: id, Name: e.Name, Path: path.Join(parent.Path, e.Name), IsFolder: e.IsFolder})
					break
				}
			}
		}
	}
	return nil
}

func restoreFixture() (*Client, *restoreDevice, *restoreWeb) {
	d := &restoreDevice{entries: unicodeTree()[:3]}
	w := &restoreWeb{d: d, bin: []sncloud.RecycledEntry{{ID: 20, Name: "й.pdf"}}, locations: map[sncloud.FileID]sncloud.Location{
		20: {IDs: []sncloud.FileID{1, 2, 3}},
		21: {IDs: []sncloud.FileID{1, 2, 3}},
		22: {IDs: []sncloud.FileID{1, 2, 3}},
	}}
	c := synthetic()
	c.device, c.web = d, w
	return c, d, w
}

const restoredPath = "DOCUMENT/Document/и\u0306/й.pdf"

func TestRestorePlanningConflicts(t *testing.T) {
	for _, mode := range []string{"exact", "twin", "both", "folder", "mark occupied", "orphan occupied", "two marks", "document twin", "mark folder"} {
		t.Run(mode, func(t *testing.T) {
			c, d, w := restoreFixture()
			switch mode {
			case "exact", "both":
				d.entries = append(d.entries, sncloud.DeviceEntry{ID: 30, Name: "й.pdf", Path: restoredPath})
			case "folder":
				d.entries = append(d.entries, sncloud.DeviceEntry{ID: 30, Path: restoredPath, IsFolder: true})
			case "mark occupied", "orphan occupied":
				d.entries = append(d.entries, sncloud.DeviceEntry{ID: 30, Path: restoredPath + ".mark"})
			case "two marks", "mark folder":
				w.bin = append(w.bin, sncloud.RecycledEntry{ID: 22, Name: "й.pdf.mark", IsFolder: mode == "mark folder"})
			case "document twin":
				w.bin = append(w.bin, sncloud.RecycledEntry{ID: 22, Name: "и\u0306.pdf"})
			}
			if mode == "twin" || mode == "both" {
				d.entries = append(d.entries, sncloud.DeviceEntry{ID: 31, Path: path.Dir(restoredPath) + "/и\u0306.pdf"})
			}
			if mode != "orphan occupied" {
				w.bin = append(w.bin, sncloud.RecycledEntry{ID: 21, Name: "и\u0306.pdf.mark"})
			}
			_, err := (files.Service{Cloud: c}).RestoreWithMarks(t.Context(), "20", port.ChangeOptions{Confirm: func([]port.FileChange) error {
				t.Fatal("conflict reached confirmation")
				return nil
			}})
			if !errors.Is(err, port.ErrConflict) || slices.Contains(w.calls, "Restore") || !strings.Contains(err.Error(), "ID ") || !strings.Contains(err.Error(), `"`) {
				t.Fatalf("error = %v; calls = %v", err, w.calls)
			}
			if d.listCalls != 1 {
				t.Fatalf("planning lists = %d, want 1", d.listCalls)
			}
		})
	}
}

func TestRestoreFreshChecks(t *testing.T) {
	for _, mode := range []string{"confirmation twin", "confirmation parent", "targeted twin", "targeted parent", "target changed", "late target changed", "late location error", "unrelated"} {
		t.Run(mode, func(t *testing.T) {
			c, d, w := restoreFixture()
			locations := 0
			w.onLocation = func(sncloud.FileID) {
				locations++
				if locations == 3 {
					if mode == "late target changed" {
						w.locations[20] = sncloud.Location{IDs: []sncloud.FileID{1, 2}}
					}
					if mode == "late location error" {
						w.err, w.failAt = sncloud.ErrNotFound, "Location"
					}
				}
			}
			mutate := func() {
				if strings.HasSuffix(mode, "parent") {
					d.entries[2].ID = 40
				} else {
					name := path.Dir(restoredPath) + "/и\u0306.pdf"
					if mode == "unrelated" {
						name = path.Dir(restoredPath) + "/other.pdf"
					}
					d.entries = append(d.entries, sncloud.DeviceEntry{ID: 30, Path: name})
				}
			}
			d.beforeList = func(n int) {
				if n == 3 && strings.HasPrefix(mode, "targeted") {
					mutate()
				}
			}
			_, err := (files.Service{Cloud: c}).RestoreWithMarks(t.Context(), "20", port.ChangeOptions{Confirm: func([]port.FileChange) error {
				if strings.HasPrefix(mode, "confirmation") || mode == "unrelated" {
					mutate()
				}
				if mode == "target changed" {
					w.locations[20] = sncloud.Location{IDs: []sncloud.FileID{1, 2}}
				}
				return nil
			}})
			if mode == "unrelated" {
				if err != nil || !slices.Contains(w.calls, "Restore") {
					t.Fatal(err, w.calls)
				}
			} else if !errors.Is(err, port.ErrConflict) || slices.Contains(w.calls, "Restore") {
				t.Fatal(err, w.calls)
			}
		})
	}
}

func TestRestoreCarryVerificationAndBounds(t *testing.T) {
	for _, count := range []int{1, 100} {
		for _, mode := range []string{"success", "dry", "twin", "mark twin", "partial", "listing failure", "later conflict"} {
			t.Run(fmt.Sprintf("%s/%d", mode, count), func(t *testing.T) {
				c, d, w := restoreFixture()
				for i := range count {
					d.entries = append(d.entries, sncloud.DeviceEntry{ID: sncloud.FileID(100 + i), Path: fmt.Sprintf("DOCUMENT/Document/unrelated%d/file.pdf", i)})
				}
				w.bin = append(w.bin, sncloud.RecycledEntry{ID: 21, Name: "и\u0306.pdf.mark"})
				if mode == "partial" {
					w.failID = 21
				}
				w.before = func(id sncloud.FileID) {
					if mode == "twin" && id == 20 || mode == "mark twin" && id == 21 {
						name := path.Dir(restoredPath) + "/и\u0306.pdf"
						if id == 21 {
							name = restoredPath + ".mark"
						}
						d.entries = append(d.entries, sncloud.DeviceEntry{ID: 50, Path: name})
					}
					if mode == "later conflict" && id == 20 {
						d.entries = append(d.entries, sncloud.DeviceEntry{ID: 50, Path: restoredPath + ".mark"})
					}
				}
				d.beforeList = func(n int) {
					if mode == "listing failure" && n == 5 {
						d.err = errors.New("synthetic verification failure")
					}
				}
				plan, err := (files.Service{Cloud: c}).RestoreWithMarks(t.Context(), "20", port.ChangeOptions{DryRun: mode == "dry", Confirm: func(plan []port.FileChange) error {
					if len(plan) != 2 || plan[0].Destination != restoredPath || plan[1].Entry.ID != "21" {
						t.Fatal(plan)
					}
					return nil
				}})
				wantLists, wantApplied := 5, 2
				switch mode {
				case "dry":
					wantLists, wantApplied = 1, 2
					if slices.Contains(w.calls, "Restore") {
						t.Fatal("dry run wrote", w.calls)
					}
				case "twin", "mark twin", "listing failure":
					if !errors.Is(err, port.ErrWriteVerification) || !strings.Contains(err.Error(), "no rollback") {
						t.Fatal(err)
					}
				case "partial", "later conflict":
					wantApplied = 1
					if err == nil || !strings.Contains(err.Error(), "restored, but") {
						t.Fatal(err)
					}
				default:
					if err != nil {
						t.Fatal(err)
					}
				}
				if len(plan) != wantApplied || d.listCalls != wantLists {
					t.Fatalf("plan = %v; lists = %d, want %d; error = %v", plan, d.listCalls, wantLists, err)
				}
			})
		}
	}
}

func TestRestoreTargetRefusals(t *testing.T) {
	for _, mode := range []string{"offline", "bad ID", "missing location", "missing parent", "duplicate parent", "bad name", "location error", "tree error"} {
		t.Run(mode, func(t *testing.T) {
			c, d, w := restoreFixture()
			e := port.Entry{ID: "20", Name: "Paper.pdf"}
			switch mode {
			case "offline":
				c.offline = true
			case "bad ID":
				e.ID = "bad"
			case "missing location":
				delete(w.locations, 20)
			case "missing parent":
				d.entries = d.entries[:2]
			case "duplicate parent":
				d.entries = append(d.entries, d.entries[2])
			case "bad name":
				e.Name = "../Paper.pdf"
			case "location error":
				w.err, w.failAt = sncloud.ErrNotFound, "Location"
			case "tree error":
				d.err = sncloud.ErrNotFound
			}
			if _, err := c.RestoreTarget(t.Context(), e); err == nil || slices.Contains(w.calls, "Restore") {
				t.Fatal(err, w.calls)
			}
		})
	}
}

func TestRestoreEarlyFailures(t *testing.T) {
	for _, mode := range []string{"trash error", "missing ID", "cancel confirmation", "changed trash", "trash recheck error", "fresh error", "targeted error", "restore error", "folder", "other folder mark"} {
		t.Run(mode, func(t *testing.T) {
			c, d, w := restoreFixture()
			id := "20"
			o := port.ChangeOptions{Confirm: func([]port.FileChange) error {
				switch mode {
				case "cancel confirmation":
					return context.Canceled
				case "changed trash":
					w.bin = append(w.bin, sncloud.RecycledEntry{ID: 21, Name: "й.pdf.mark"})
				case "trash recheck error":
					w.err, w.failAt = sncloud.ErrNotFound, "RecycleBin"
				}
				return nil
			}}
			switch mode {
			case "trash error":
				w.err, w.failAt = sncloud.ErrNotFound, "RecycleBin"
			case "missing ID":
				id = "999"
			case "fresh error", "targeted error":
				d.beforeList = func(n int) {
					if n == 2 && mode == "fresh error" || n == 3 && mode == "targeted error" {
						d.err = sncloud.ErrNotFound
					}
				}
			case "restore error":
				w.failID = 20
			case "folder":
				w.bin[0].IsFolder = true
			case "other folder mark":
				w.bin = append(w.bin, sncloud.RecycledEntry{ID: 21, Name: "й.pdf.mark"})
				w.locations[21] = sncloud.Location{IDs: []sncloud.FileID{1, 2}}
			}
			plan, err := (files.Service{Cloud: c}).RestoreWithMarks(t.Context(), id, o)
			if mode == "folder" || mode == "other folder mark" {
				if err != nil || len(plan) != 1 {
					t.Fatal(plan, err)
				}
			} else if err == nil || len(plan) != 0 {
				t.Fatal(plan, err)
			}
		})
	}
}

func TestRestoreTargetLocationShapes(t *testing.T) {
	for _, mode := range []string{"parent only", "trailing self", "mismatching name", "mismatching lengths", "self only"} {
		t.Run(mode, func(t *testing.T) {
			c, _, w := restoreFixture()
			e := port.Entry{ID: "20", Name: "й.pdf"}
			location := sncloud.Location{
				IDs: []sncloud.FileID{1, 2, 3, 20}, Names: []string{"DOCUMENT", "Document", "и\u0306", e.Name},
			}
			switch mode {
			case "parent only":
				location.IDs, location.Names = location.IDs[:3], location.Names[:3]
			case "mismatching name":
				location.Names[3] = "и\u0306.pdf" // Even an NFC-equivalent name is not the recycled name.
			case "mismatching lengths":
				location.Names = location.Names[:3]
			case "self only":
				location.IDs, location.Names = location.IDs[3:], location.Names[3:]
			}
			w.locations[20] = location
			got, err := c.RestoreTarget(t.Context(), e)
			if mode == "parent only" || mode == "trailing self" {
				if err != nil || got.ID != e.ID || got.Name != e.Name || got.ParentID != "3" || got.Path != restoredPath {
					t.Fatalf("target = %+v; error = %v", got, err)
				}
			} else if !errors.Is(err, port.ErrConflict) {
				t.Fatalf("target = %+v; error = %v, want conflict", got, err)
			}
		})
	}
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package files

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestExactDuplicateBytesAndRootAmbiguity(t *testing.T) {
	for _, exact := range []bool{false, true} {
		c := &porttest.Cloud{Entries: []port.Entry{{ID: "one", Path: "й.txt"}, {ID: "two", Path: "й.txt"}}}
		c.StatFunc = func(string) (port.Entry, error) { return port.Entry{}, port.ErrConflict }
		_, err := changeSource(t.Context(), c, "й.txt", exact)
		if !errors.Is(err, port.ErrConflict) || !strings.Contains(err.Error(), "ID one") || !strings.Contains(err.Error(), "ID two") {
			t.Fatal(err)
		}
	}
}

func TestReplacementRecheckRetainsOldAndUploadedCopy(t *testing.T) {
	for _, scenario := range []string{"old changed", "third twin", "list failed"} {
		disk := &porttest.NotebookFiles{Data: map[string][]byte{"local.pdf": []byte("new")}}
		drive := porttest.NewDrive(disk, map[string]string{"NOTE/Note/й.pdf": "old", "NOTE/Note/copy.pdf": "new"})
		previous, _ := drive.Stat(t.Context(), "NOTE/Note/й.pdf")
		uploaded, _ := drive.Stat(t.Context(), "NOTE/Note/copy.pdf")
		switch scenario {
		case "old changed":
			drive.Seed(previous.Path, "changed")
		case "third twin":
			drive.Seed("NOTE/Note/и\u0306.pdf", "raced")
		case "list failed":
			drive.Fail["list"] = errors.New("synthetic listing failure")
		}
		s := Service{Disk: disk}
		_, err := s.finishReplacement(t.Context(), drive, upload{local: "local.pdf", previous: previous, item: port.TransferItem{Cloud: previous.Path}}, uploaded)
		if err == nil || !strings.Contains(err.Error(), "uploaded copy retained") || drive.Count("delete") != 0 || drive.Count("move") != 0 {
			t.Fatal(scenario, err, drive.Calls)
		}
	}
}

func TestSeparateReplacementRefusesRacedTarget(t *testing.T) {
	disk := &porttest.NotebookFiles{Data: map[string][]byte{"local.pdf": []byte("new")}}
	drive := porttest.NewDrive(disk, map[string]string{"NOTE/Note/й.pdf": "old"})
	previous, _ := drive.Stat(t.Context(), "NOTE/Note/й.pdf")
	drive.Seed("NOTE/Note/и\u0306.pdf", "raced")
	_, err := (Service{Disk: disk}).uploadSeparate(t.Context(), drive, upload{local: "local.pdf", previous: previous, item: port.TransferItem{Status: port.StatusReplace, Cloud: previous.Path}}, nil)
	if !errors.Is(err, port.ErrConflict) || drive.Count("upload") != 0 {
		t.Fatal(err, drive.Calls)
	}
}

func TestRawRemoveRetainsPlannedIdentity(t *testing.T) {
	d := porttest.NewDrive(&porttest.NotebookFiles{}, map[string]string{"NOTE/Note/a.txt": "old"})
	previous, _ := d.Stat(t.Context(), "NOTE/Note/a.txt")
	s := Service{Cloud: d}
	d.Seed(previous.Path, "new")
	if err := s.Remove(t.Context(), previous, false); !errors.Is(err, port.ErrConflict) || d.Count("delete") != 0 {
		t.Fatal(err, d.Calls)
	}
	d.Fail["snapshot"] = errors.New("snapshot failed")
	if err := s.Remove(t.Context(), previous, false); err == nil {
		t.Fatal("ignored failed snapshot")
	}
}

func TestCancelledVerificationSaysWriteHappened(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	d := &porttest.Cloud{}
	err := verifyWritten(ctx, d, []port.Entry{{ID: "written", Path: "folder/file"}})
	if !errors.Is(err, context.Canceled) || !errors.Is(err, port.ErrWriteVerification) || !strings.Contains(err.Error(), "write happened") || len(d.Calls) != 0 {
		t.Fatal(err, d.Calls)
	}
}

func TestEntryIdentityUsesTimeEqual(t *testing.T) {
	modified := time.Now()
	baseline := port.Entry{ID: "one", Path: "folder/file", Hash: "hash", Size: new(int64(4)), Modified: &modified}
	fresh := baseline
	wall := modified.Round(0).In(time.FixedZone("same instant", 3600))
	fresh.Modified = &wall
	fresh.Size = new(int64(4))
	fresh.Name, fresh.ParentID = "display", "derived"
	fresh.Mark = &port.Entry{ID: "display-only"}
	if !sameEntry(baseline, fresh) {
		t.Fatal("equivalent timestamp or display metadata differed")
	}
	for _, mutate := range []func(*port.Entry){
		func(e *port.Entry) { e.ID = "other" }, func(e *port.Entry) { e.Path = "moved" }, func(e *port.Entry) { e.Folder = true },
		func(e *port.Entry) { e.Hash = "changed" }, func(e *port.Entry) { e.Size = nil }, func(e *port.Entry) { e.Size = new(int64(5)) },
		func(e *port.Entry) { later := wall.Add(time.Second); e.Modified = &later },
	} {
		changed := fresh
		mutate(&changed)
		if sameEntry(baseline, changed) {
			t.Fatal("missed identity change", changed)
		}
	}
	if !sameEntry(port.Entry{}, port.Entry{}) {
		t.Fatal("absent metadata differed")
	}
	// The service's folder listing omits the time that its full tree reports.
	unknown := fresh
	unknown.Modified = nil
	if !sameEntry(baseline, unknown) || !sameEntry(unknown, baseline) {
		t.Fatal("a time missing from one listing was taken for a change")
	}
	for _, mutate := range []func(*port.Entry){
		func(e *port.Entry) { e.ID = "other" },
		func(e *port.Entry) { e.Hash = "changed" },
		func(e *port.Entry) { e.Size = new(int64(5)) },
	} {
		changed := unknown
		mutate(&changed)
		if sameEntry(baseline, changed) || sameEntry(changed, baseline) {
			t.Fatal("missed identity change without a folder-listing timestamp", changed)
		}
	}
}

func TestVerificationRequiresIdentityAtPlannedPath(t *testing.T) {
	expected := port.Entry{ID: "restored", Path: "folder/й.pdf"}
	for _, mode := range []string{"present", "absent", "wrong path", "NFC path", "wrong ID"} {
		t.Run(mode, func(t *testing.T) {
			listed := expected
			switch mode {
			case "wrong path":
				listed.Path = "folder/elsewhere.pdf"
			case "NFC path":
				listed.Path = "folder/и\u0306.pdf"
			case "wrong ID":
				listed.ID = "another"
			}
			c := &porttest.Cloud{Entries: []port.Entry{listed}}
			if mode == "absent" {
				c.Entries = nil
			}
			err := verifyRestored(t.Context(), c, []port.Entry{expected})
			if mode == "present" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, port.ErrWriteVerification) || !strings.Contains(err.Error(), "expected ID restored at") ||
				!strings.Contains(err.Error(), expected.Path) || !strings.Contains(err.Error(), "no rollback") {
				t.Fatalf("error = %v, want missing identity and planned path", err)
			}
		})
	}
}

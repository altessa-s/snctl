// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package cloud

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"path"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"

	sncloud "github.com/altessa-s/sncloud-go"
)

func unicodeTree() []sncloud.DeviceEntry {
	return []sncloud.DeviceEntry{
		{ID: 1, Name: "DOCUMENT", Path: "DOCUMENT", IsFolder: true},
		{ID: 2, Name: "Document", Path: "DOCUMENT/Document", IsFolder: true},
		{ID: 3, Name: "и\u0306", Path: "DOCUMENT/Document/и\u0306", IsFolder: true},
		{ID: 4, Name: "и\u0306.pdf", Path: "DOCUMENT/Document/и\u0306/и\u0306.pdf"},
	}
}

func TestResolveUnicodeSegments(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		extra       []sncloud.DeviceEntry
		id          sncloud.FileID
		err         error
	}{
		{name: "exact", query: "DOCUMENT/Document/и\u0306/и\u0306.pdf", id: 4},
		{name: "NFC only", query: "/DOCUMENT/Document/й/й.pdf", id: 4},
		{name: "mixed", query: "DOCUMENT/Document/й/и\u0306.pdf", id: 4},
		{name: "exact wins", query: "DOCUMENT/Document/й/й.pdf", extra: []sncloud.DeviceEntry{
			{ID: 5, Name: "й", Path: "DOCUMENT/Document/й", IsFolder: true},
			{ID: 6, Name: "й.pdf", Path: "DOCUMENT/Document/й/й.pdf"},
		}, id: 6},
		{name: "no fallback across exact parent", query: "DOCUMENT/Document/й/й.pdf", extra: []sncloud.DeviceEntry{
			{ID: 5, Name: "й", Path: "DOCUMENT/Document/й", IsFolder: true},
		}, err: port.ErrNotFound},
		{name: "ambiguous segment", query: "DOCUMENT/Document/ǻ/x.pdf", extra: []sncloud.DeviceEntry{
			{ID: 5, Name: "å\u0301", Path: "DOCUMENT/Document/å\u0301", IsFolder: true},
			{ID: 6, Name: "a\u030a\u0301", Path: "DOCUMENT/Document/a\u030a\u0301", IsFolder: true},
		}, err: port.ErrConflict},
		{name: "ambiguous file", query: "DOCUMENT/Document/й/ǻ.pdf", extra: []sncloud.DeviceEntry{
			{ID: 5, Path: "DOCUMENT/Document/и\u0306/å\u0301.pdf"},
			{ID: 6, Path: "DOCUMENT/Document/и\u0306/a\u030a\u0301.pdf"},
		}, err: port.ErrConflict},
		{name: "duplicate exact", query: "DOCUMENT/Document/й/й.pdf", extra: []sncloud.DeviceEntry{
			{ID: 5, Path: "DOCUMENT/Document/и\u0306/и\u0306.pdf"},
		}, err: port.ErrConflict},
		{name: "case sensitive", query: "document/Document/й/й.pdf", err: port.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries := append(unicodeTree(), tc.extra...)
			before := append([]sncloud.DeviceEntry(nil), entries...)
			got, err := resolve(entries, tc.query)
			if !errors.Is(err, tc.err) || err == nil && got.ID != tc.id {
				t.Fatalf("resolve = %+v, %v; want ID %d, %v", got, err, tc.id, tc.err)
			}
			if !reflect.DeepEqual(entries, before) {
				t.Fatal("mutated device names")
			}
		})
	}
	// Top-level segments use the same rule as every other segment.
	got, err := resolve([]sncloud.DeviceEntry{{ID: 1, Path: "и\u0306", IsFolder: true}}, "й")
	if err != nil || got.Path != "и\u0306" {
		t.Fatal(got, err)
	}
}

type unicodeCache struct {
	cacheDouble
	bodyEntry port.Entry
}

func (c *unicodeCache) Body(_ context.Context, e port.Entry) (string, bool, error) {
	c.bodyEntry = e
	return "synthetic-body", true, nil
}

func TestOfflineUnicodeLookup(t *testing.T) {
	c := synthetic()
	store := &unicodeCache{listing: port.Listing{
		Endpoint: "https://viewer.supernote.com", Account: "synthetic", Confirmed: time.Unix(100, 0),
	}}
	for _, e := range unicodeTree() {
		store.listing.Entries = append(store.listing.Entries, deviceEntry(e))
	}
	c.SetCache(store, true, 0, func() time.Time { return time.Unix(101, 0) }, nil)
	svc := files.Service{Cloud: c}
	e, err := svc.Resolve(t.Context(), "DOCUMENT/Document/й/й.pdf")
	if err != nil || e.Path != unicodeTree()[3].Path || e.Name != "и\u0306.pdf" || e.ParentID != "3" {
		t.Fatal(e, err)
	}
	file, cached, err := c.DownloadSource(t.Context(), e, nil)
	if err != nil || !cached || file != "synthetic-body" || store.bodyEntry.Path != e.Path {
		t.Fatal(file, cached, err, store.bodyEntry)
	}
	rows, err := c.List(t.Context(), "DOCUMENT/Document/й", true)
	if err != nil || len(rows) != 1 || rows[0].Path != e.Path {
		t.Fatal(rows, err)
	}
	c.view.list = append(c.view.list,
		sncloud.DeviceEntry{ID: 5, Path: "DOCUMENT/Document/å\u0301", IsFolder: true},
		sncloud.DeviceEntry{ID: 6, Path: "DOCUMENT/Document/a\u030a\u0301", IsFolder: true})
	if _, err := c.Stat(t.Context(), "DOCUMENT/Document/ǻ"); !errors.Is(err, port.ErrConflict) {
		t.Fatal(err)
	}
	if _, err := c.List(t.Context(), "DOCUMENT/Document/ǻ", false); !errors.Is(err, port.ErrConflict) {
		t.Fatal(err)
	}
	if len(c.device.(*fakeDeviceAPI).calls) != 0 {
		t.Fatal("offline contacted device")
	}
}

func TestUnicodeWriteConflicts(t *testing.T) {
	for _, operation := range []string{"move", "rename", "mkdir", "upload", "put"} {
		t.Run(operation, func(t *testing.T) {
			d := &treeDevice{entries: unicodeTree()}
			d.entries = append(d.entries, sncloud.DeviceEntry{ID: 5, Name: "source.pdf", Path: "DOCUMENT/Document/и\u0306/source.pdf"})
			before := append([]sncloud.DeviceEntry(nil), d.entries...)
			c := synthetic()
			c.device = d
			svc := files.Service{Cloud: c}
			target := "DOCUMENT/Document/й/й.pdf"
			var err error
			switch operation {
			case "move":
				err = svc.Move(t.Context(), d.entries[4].Path, target)
			case "rename":
				err = svc.Rename(t.Context(), d.entries[4].Path, path.Dir(d.entries[4].Path)+"/й.pdf")
			case "mkdir":
				_, err = c.Mkdir(t.Context(), target)
			case "upload":
				_, err = c.Upload(t.Context(), "local", target, nil)
			case "put":
				disk := &porttest.NotebookFiles{Data: map[string][]byte{"local.pdf": []byte("pdf")}}
				svc.Disk = disk
				_, err = svc.Put(t.Context(), port.PutRequest{Sources: []port.PutSource{{Path: "local.pdf"}}, Destination: target}, port.UploadOptions{})
			}
			if !errors.Is(err, port.ErrConflict) {
				t.Fatalf("got %v, want conflict", err)
			}
			if !reflect.DeepEqual(d.entries, before) {
				t.Fatal("wrote despite NFC twin")
			}
		})
	}
}

func TestUnicodeMoveWithMarks(t *testing.T) {
	for _, mode := range []string{"success", "target mark", "duplicate doc", "duplicate mark"} {
		t.Run(mode, func(t *testing.T) {
			entries := unicodeTree()
			doc := entries[3].Path
			mark := path.Dir(doc) + "/й.pdf.mark"
			entries = append(entries, sncloud.DeviceEntry{ID: 5, Name: "й.pdf.mark", Path: mark})
			target := path.Dir(doc) + "/новыи\u0306.pdf"
			switch mode {
			case "target mark":
				entries = append(entries, sncloud.DeviceEntry{ID: 6, Path: path.Dir(doc) + "/новый.pdf.mark"})
			case "duplicate doc":
				entries = append(entries, sncloud.DeviceEntry{ID: 6, Path: path.Dir(doc) + "/й.pdf"})
			case "duplicate mark":
				entries = append(entries, sncloud.DeviceEntry{ID: 6, Path: doc + ".mark"})
			}
			d := &treeDevice{entries: entries}
			c := synthetic()
			c.device = d
			confirmed := false
			plan, err := (files.Service{Cloud: c}).MoveWithMarks(t.Context(), doc, target, true, port.ChangeOptions{Confirm: func([]port.FileChange) error { confirmed = true; return nil }})
			if mode != "success" {
				if !errors.Is(err, port.ErrConflict) || confirmed || d.entries[3].Path != doc {
					t.Fatal(plan, err, confirmed)
				}
				return
			}
			if err != nil || len(plan) != 2 || !confirmed || plan[1].Entry.Path != mark || d.entries[3].Path != target || d.entries[4].Path != target+".mark" {
				t.Fatal(plan, err, d.entries)
			}
		})
	}
}

func TestUnicodeDestinationSpelling(t *testing.T) {
	for _, operation := range []string{"move", "mkdir", "upload"} {
		t.Run(operation, func(t *testing.T) {
			d := &treeDevice{entries: unicodeTree()}
			c := synthetic()
			c.device = d
			target := "DOCUMENT/Document/й/cafe\u0301"
			stored := "DOCUMENT/Document/и\u0306/cafe\u0301"
			var err error
			switch operation {
			case "move":
				err = (files.Service{Cloud: c}).Move(t.Context(), unicodeTree()[3].Path, target)
			case "mkdir":
				_, err = c.Mkdir(t.Context(), target)
			case "upload":
				_, err = c.Upload(t.Context(), "local", target, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.Stat(t.Context(), target)
			if err != nil || got.Path != stored || got.Name != "cafe\u0301" {
				t.Fatal(got, err)
			}
		})
	}
}

func TestUnicodeMoveIntoSelf(t *testing.T) {
	d := &treeDevice{entries: unicodeTree()}
	c := synthetic()
	c.device = d
	err := (files.Service{Cloud: c}).Move(t.Context(), "DOCUMENT/Document/и\u0306", "DOCUMENT/Document/й/child")
	if !errors.Is(err, port.ErrConflict) || d.entries[2].Path != unicodeTree()[2].Path {
		t.Fatal(d.entries, err)
	}
}

func TestOfflineUncachedPlanning(t *testing.T) {
	for _, operation := range []string{"put", "mv", "mkdir -p", "list", "stat"} {
		t.Run(operation, func(t *testing.T) {
			c := synthetic()
			store := &cacheDouble{listing: port.Listing{
				Endpoint: "https://viewer.supernote.com", Account: "synthetic", Confirmed: time.Unix(100, 0),
			}}
			for _, e := range unicodeTree() {
				store.listing.Entries = append(store.listing.Entries, deviceEntry(e))
			}
			c.SetCache(store, true, 0, func() time.Time { return time.Unix(101, 0) }, nil)
			svc := files.Service{Cloud: c, Disk: &porttest.NotebookFiles{Data: map[string][]byte{"local.pdf": []byte("pdf")}}}
			target := "DOCUMENT/Document/й/uncached"
			var err error
			switch operation {
			case "put":
				var report port.TransferReport
				report, err = svc.Put(t.Context(), port.PutRequest{Sources: []port.PutSource{{Path: "local.pdf"}}, Destination: target}, port.UploadOptions{DryRun: true})
				if len(report.Items) != 0 {
					t.Fatal("planned an uncached upload", report)
				}
			case "mv":
				var plan []port.FileChange
				plan, err = svc.MoveWithMarks(t.Context(), unicodeTree()[3].Path, target, false, port.ChangeOptions{DryRun: true})
				if len(plan) != 0 {
					t.Fatal("planned an uncached move", plan)
				}
			case "mkdir -p":
				// Mkdir has no dry-run API; its planning Stat must fail before creation.
				_, err = svc.Mkdir(t.Context(), target+"/child", true)
			case "list":
				_, err = c.List(t.Context(), target, false)
			case "stat":
				_, err = c.Stat(t.Context(), target)
			}
			if err == nil || errors.Is(err, port.ErrNotFound) || !strings.Contains(err.Error(), "missing from cached listing") {
				t.Fatalf("got %v, want unclassified offline cache miss", err)
			}
			if len(c.device.(*fakeDeviceAPI).calls) != 0 || len(c.web.(*fakeWebAPI).calls) != 0 {
				t.Fatal("offline planning contacted an API")
			}
		})
	}
}

func TestRenameUnicodeParent(t *testing.T) {
	for _, withMarks := range []bool{false, true} {
		d := &treeDevice{entries: unicodeTree()}
		c := synthetic()
		c.device = d
		svc := files.Service{Cloud: c}
		target := "DOCUMENT/Document/й/new.pdf"
		var err error
		if withMarks {
			_, err = svc.MoveWithMarks(t.Context(), unicodeTree()[3].Path, target, true, port.ChangeOptions{})
		} else {
			err = svc.Rename(t.Context(), unicodeTree()[3].Path, target)
		}
		if err != nil || d.entries[3].Path != "DOCUMENT/Document/и\u0306/new.pdf" {
			t.Fatal(withMarks, d.entries, err)
		}
	}
}

func TestRenameOwnUnicodeTwin(t *testing.T) {
	for _, operation := range []string{"rename", "move", "rename with marks", "move with marks", "mark already named", "adapter"} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverse=%t", operation, reverse), func(t *testing.T) {
				entries := unicodeTree()
				source := entries[3].Path
				target := path.Dir(source) + "/й.pdf"
				if reverse {
					source, target = target, source
					entries[3].Path, entries[3].Name = source, path.Base(source)
				}
				mark := source + ".mark"
				if operation == "mark already named" {
					mark = target + ".mark"
				}
				entries = append(entries, sncloud.DeviceEntry{ID: 5, Name: path.Base(mark), Path: mark})
				d := &treeDevice{entries: entries}
				c := synthetic()
				c.device = d
				svc := files.Service{Cloud: c}
				var err error
				switch operation {
				case "rename":
					err = svc.Rename(t.Context(), source, target)
				case "move":
					err = svc.Move(t.Context(), source, target)
				case "adapter":
					err = c.Move(t.Context(), deviceEntry(entries[3]), target)
				default:
					_, err = svc.MoveWithMarks(t.Context(), source, target, operation != "move with marks", port.ChangeOptions{})
				}
				if err != nil || d.entries[3].Path != target {
					t.Fatal(d.entries, err)
				}
				if strings.Contains(operation, "marks") || operation == "mark already named" {
					if d.entries[4].Path != target+".mark" {
						t.Fatal("sidecar did not follow spelling", d.entries)
					}
				}
			})
		}
	}
}

func TestExactUnicodeTwinChanges(t *testing.T) {
	for _, operation := range []string{"rm", "mv", "rename", "adapter move"} {
		for _, ext := range []string{".pdf", ".txt", ""} {
			for _, exact := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s%s/exact=%t", operation, ext, exact), func(t *testing.T) {
					entries := unicodeTree()[:3]
					source := "DOCUMENT/Document/и\u0306/й" + ext
					twin := "DOCUMENT/Document/и\u0306/и\u0306" + ext
					entries = append(entries,
						sncloud.DeviceEntry{ID: 4, Name: path.Base(source), Path: source, IsFolder: ext == ""},
						sncloud.DeviceEntry{ID: 5, Name: path.Base(twin), Path: twin, IsFolder: ext == ""})
					d := &treeDevice{entries: entries}
					c := synthetic()
					c.device, c.view = d, &listing{list: slices.Clone(entries)}
					before := slices.Clone(entries)
					svc := files.Service{Cloud: c}
					confirmed := false
					opts := port.ChangeOptions{Exact: exact, Confirm: func([]port.FileChange) error { confirmed = true; return nil }}
					target := path.Dir(source) + "/new" + ext
					var plan []port.FileChange
					var err error
					switch operation {
					case "rm":
						plan, err = svc.RemoveWithMarks(t.Context(), source, false, opts)
					case "mv":
						plan, err = svc.MoveWithMarks(t.Context(), source, target, false, opts)
					case "rename":
						// The equivalent destination is occupied by another entry.
						err = svc.Rename(t.Context(), source, twin)
					case "adapter move":
						err = c.Move(t.Context(), deviceEntry(entries[3]), twin)
					}
					if !exact || ext == ".pdf" || operation == "rename" || operation == "adapter move" {
						if !errors.Is(err, port.ErrConflict) || confirmed || !reflect.DeepEqual(d.entries, before) || len(c.web.(*fakeWebAPI).calls) != 0 {
							t.Fatal(plan, err, confirmed, d.entries)
						}
						return
					}
					if err != nil || !confirmed || len(plan) != 1 || plan[0].Entry.ID != "4" {
						t.Fatal(plan, err, confirmed)
					}
					got, err := c.Stat(t.Context(), twin)
					if err != nil || got.ID != "5" || got.Path != twin {
						t.Fatal("changed equivalent twin", got, err)
					}
					if operation == "rm" {
						if len(c.view.entries()) != len(before)-1 || !slices.Equal(c.web.(*fakeWebAPI).calls, []string{"Delete"}) {
							t.Fatal(c.view.entries(), c.web.(*fakeWebAPI).calls)
						}
					} else if d.entries[3].Path != target || d.entries[4].Path != twin {
						t.Fatal(d.entries)
					}
				})
			}
		}

	}
}

func TestListFreshUnicodeContract(t *testing.T) {
	for _, recursive := range []bool{false, true} {
		for _, query := range []string{"missing", "DOCUMENT/Document/missing", "DOCUMENT/Document/й/й.pdf"} {
			c := synthetic()
			c.device = &treeDevice{entries: unicodeTree()}
			got, err := c.ListFresh(t.Context(), query, recursive)
			if err != nil || got == nil || len(got) != 0 {
				t.Fatal(query, recursive, got, err)
			}
		}
	}
	c := synthetic()
	d := &treeDevice{entries: unicodeTree()}
	c.device = d
	got, err := c.ListFresh(t.Context(), "DOCUMENT/Document/й", false)
	if err != nil || len(got) != 1 || got[0].Path != unicodeTree()[3].Path {
		t.Fatal(got, err)
	}
	d.entries = append(d.entries,
		sncloud.DeviceEntry{ID: 5, Path: "DOCUMENT/Document/å\u0301", IsFolder: true},
		sncloud.DeviceEntry{ID: 6, Path: "DOCUMENT/Document/a\u030a\u0301", IsFolder: true})
	if _, err := c.ListFresh(t.Context(), "DOCUMENT/Document/ǻ", false); !errors.Is(err, port.ErrConflict) {
		t.Fatal(err)
	}
}

func TestUnicodeMoveUnchangedName(t *testing.T) {
	for _, operation := range []string{"rename", "move", "with marks", "adapter"} {
		t.Run(operation, func(t *testing.T) {
			d := &treeDevice{entries: unicodeTree()}
			before := slices.Clone(d.entries)
			c := synthetic()
			c.device = d
			svc := files.Service{Cloud: c}
			source := d.entries[3].Path
			// Only the parent's spelling changes; the resolved destination is
			// still the source's exact name, so there is no filename to change.
			target := "DOCUMENT/Document/й/и\u0306.pdf"
			var err error
			switch operation {
			case "rename":
				err = svc.Rename(t.Context(), source, target)
			case "move":
				err = svc.Move(t.Context(), source, target)
			case "with marks":
				_, err = svc.MoveWithMarks(t.Context(), source, target, true, port.ChangeOptions{DryRun: true})
			case "adapter":
				err = c.Move(t.Context(), deviceEntry(d.entries[3]), target)
			}
			if !errors.Is(err, port.ErrConflict) || !reflect.DeepEqual(d.entries, before) {
				t.Fatal(err, d.entries)
			}
		})
	}
}

func TestFreshParentIdentityAndOfflineWriteChecks(t *testing.T) {
	c := synthetic()
	d := &treeDevice{entries: unicodeTree()}
	c.device = d
	parent, children, err := c.ListParentFresh(t.Context(), "DOCUMENT/Document/й")
	if err != nil || parent.ID != "3" || parent.Path != "DOCUMENT/Document/и\u0306" || len(children) != 1 {
		t.Fatal(parent, children, err)
	}
	// A planning Stat's not-found value carries numeric ID zero. The service
	// must treat that as absent, just like ListParentFresh's zero Entry.
	if _, err = (files.Service{Cloud: c}).Mkdir(t.Context(), "DOCUMENT/Document/й/new/child", true); err != nil {
		t.Fatal(err)
	}
	c.offline = true
	before := len(d.entries)
	if _, _, err = c.ListParentFresh(t.Context(), "DOCUMENT/Document"); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatal(err)
	}
	if len(d.entries) != before {
		t.Fatal("offline write check changed entries")
	}
}

func TestWarmJSONCacheMutation(t *testing.T) {
	for _, op := range []string{"rm", "mv"} {
		t.Run(op, func(t *testing.T) {
			modified := time.Date(2026, 11, 17, 12, 0, 0, 0, time.FixedZone("device location", 0))
			entries := unicodeTree()
			entries[3].Modified = &modified
			d := &treeDevice{entries: entries}
			c := synthetic()
			c.device = d
			listing := port.Listing{Endpoint: "https://viewer.supernote.com", Account: "synthetic", Confirmed: time.Unix(100, 0)}
			for _, e := range entries {
				listing.Entries = append(listing.Entries, deviceEntry(e))
			}
			data, err := json.Marshal(listing)
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(data, &listing); err != nil {
				t.Fatal(err)
			}
			if *listing.Entries[3].Modified == modified {
				t.Fatal("fixture did not change timestamp representation")
			}
			c.SetCache(&cacheDouble{listing: listing}, false, time.Hour, func() time.Time { return time.Unix(101, 0) }, nil)
			s := files.Service{Cloud: c}
			options := port.ChangeOptions{Confirm: func([]port.FileChange) error {
				if d.listCalls != 0 {
					t.Fatal("baseline bypassed warm JSON cache", d.listCalls)
				}
				return nil
			}}
			if op == "rm" {
				_, err = s.RemoveWithMarks(t.Context(), entries[3].Path, false, options)
			} else {
				_, err = s.MoveWithMarks(t.Context(), entries[3].Path, path.Dir(entries[3].Path)+"/moved.pdf", false, options)
			}
			if err != nil {
				t.Fatal("unchanged cached entry falsely conflicted", err)
			}
		})
	}
}

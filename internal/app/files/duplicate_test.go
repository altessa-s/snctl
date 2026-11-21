// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package files

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestSeparateReplacementReusesMatchingCopy(t *testing.T) {
	disk := &porttest.NotebookFiles{Data: map[string][]byte{"x.pdf": []byte("new")}}
	want := porttest.ContentHash([]byte("new"))
	target := fmt.Sprintf("NOTE/Note/x (replacement %s 2).pdf", want)
	drive := porttest.NewDrive(disk, map[string]string{
		fmt.Sprintf("NOTE/Note/x (replacement %s 1).pdf", want): "different",
		target: "new",
	})
	s := Service{Disk: disk}
	u := upload{local: "x.pdf", item: port.TransferItem{Cloud: "NOTE/Note/x.pdf"}}
	for range 3 {
		got, err := s.uploadSeparate(t.Context(), drive, u, nil)
		if err != nil || got.Path != target || drive.Count("upload") != 0 {
			t.Fatalf("replacement = %+v, %v; uploads = %d", got, err, drive.Count("upload"))
		}
	}
}

func TestDuplicateCheckCancellationAndCanonicalPath(t *testing.T) {
	cloud := &porttest.Cloud{Entries: []port.Entry{{ID: "new", Path: "NOTE/Note/Café.note"}, {ID: "old", Path: "NOTE/Note/Café.note"}}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := port.TransferReport{Items: []port.TransferItem{{Cloud: "note/note/café.note", Entry: &port.Entry{ID: "new", Name: "Café.note", Path: "NOTE/Note/Café.note"}}}}
	err := checkDuplicates(ctx, cloud, &r)
	if !errors.Is(err, port.ErrWriteVerification) || !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "write happened") {
		t.Fatal(err)
	}
	if len(cloud.Calls) != 0 || len(r.Warnings) != 1 {
		t.Fatal(cloud.Calls, r.Warnings)
	}
	checkDuplicates(t.Context(), cloud, &r)
	if r.Items[0].Status != port.StatusDuplicate {
		t.Fatal(r)
	}
}

func TestSeparateReplacementUnicodeNames(t *testing.T) {
	for _, mode := range []string{"occupied twin", "matching twin", "ambiguous matching twins", "folder twin"} {
		t.Run(mode, func(t *testing.T) {
			disk := &porttest.NotebookFiles{Data: map[string][]byte{"local.pdf": []byte("new")}}
			want := porttest.ContentHash([]byte("new"))
			twin := fmt.Sprintf("NOTE/Note/и\u0306 (replacement %s 1).pdf", want)
			target := fmt.Sprintf("NOTE/Note/й (replacement %s 2).pdf", want)
			data := "different"
			if mode == "matching twin" || mode == "ambiguous matching twins" {
				data = "new"
			}
			drive := porttest.NewDrive(disk, map[string]string{twin: data})
			if mode == "ambiguous matching twins" {
				drive.Seed(fmt.Sprintf("NOTE/Note/й (replacement %s 1).pdf", want), data)
			}
			if mode == "folder twin" {
				drive.Drop(twin)
				drive.Folders[twin] = true
			}
			s := Service{Disk: disk}
			u := upload{local: "local.pdf", item: port.TransferItem{Cloud: "NOTE/Note/й.pdf"}}
			got, err := s.uploadSeparate(t.Context(), drive, u, nil)
			wantUploads := 1
			if mode == "matching twin" {
				target, wantUploads = twin, 0
			}
			if err != nil || got.Path != target || drive.Count("upload") != wantUploads {
				t.Fatal(got, err, drive.Calls)
			}
		})
	}
}

func TestDuplicateCheckUnicodeTwins(t *testing.T) {
	for _, twin := range []string{"NOTE/Note/и\u0306.pdf", "NOTE/Note/й.pdf"} {
		cloud := &porttest.Cloud{Entries: []port.Entry{
			{ID: "new", Name: "й.pdf", Path: "NOTE/Note/й.pdf"},
			{ID: "old", Path: twin},
		}}
		r := port.TransferReport{Items: []port.TransferItem{{
			Status: port.StatusUploaded, Entry: &cloud.Entries[0], Reason: "stored",
		}}}
		checkDuplicates(t.Context(), cloud, &r)
		if r.Items[0].Status != port.StatusDuplicate || r.Items[0].Reason != "stored; a second entry named й.pdf now exists" {
			t.Fatal(twin, r)
		}
	}
	cloud := &porttest.Cloud{Entries: []port.Entry{
		{ID: "new", Name: "й.pdf", Path: "NOTE/Note/й.pdf"},
		{ID: "other", Path: "NOTE/Note/Й.pdf"},
	}}
	r := port.TransferReport{Items: []port.TransferItem{{Status: port.StatusUploaded, Entry: &cloud.Entries[0]}}}
	checkDuplicates(t.Context(), cloud, &r)
	if r.Items[0].Status != port.StatusUploaded {
		t.Fatal("folded case", r)
	}
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package files_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestTransfers(t *testing.T) {
	for _, op := range []string{"read", "get"} {
		for _, fail := range []string{"", "stat", "download", "open", "remove local", "save"} {
			t.Run(op+fail, func(t *testing.T) {
				if op == "get" && fail == "open" || op == "read" && fail == "save" {
					return
				}
				f := &porttest.Cloud{Entries: []port.Entry{{Name: "n", Path: "n"}}, Data: "data"}
				if fail != "" {
					f.FailAt = fail
					f.Failure = errors.New("failure")
				}
				s := files.Service{Cloud: f, Local: f}
				var err error
				if op == "read" {
					_, b, e := s.Read(t.Context(), "n", nil)
					err = e
					if fail == "" && string(b) != "data" {
						t.Fatal(string(b))
					}
				} else {
					_, err = s.Get(t.Context(), "n", "out", io.Discard, nil)
				}
				if (err != nil) != (fail != "") {
					t.Fatal(err)
				}
			})
		}
	}
	f := &porttest.Cloud{Entries: []port.Entry{{Path: "n", Folder: true}}}
	s := files.Service{Cloud: f, Local: f}
	if _, _, err := s.Read(t.Context(), "n", nil); !errors.Is(err, port.ErrFileRequired) {
		t.Fatal(err)
	}
	f.Entries[0].Folder = false
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Get(ctx, "n", "out", io.Discard, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRoot(t *testing.T) {
	s := files.Service{}
	e, err := s.Resolve(t.Context(), "/")
	if err != nil || !e.Folder || e.Name != "supernote" {
		t.Fatal(e, err)
	}
}

func TestMove(t *testing.T) {
	for _, fail := range []string{"", "stat", "move"} {
		f := &porttest.Cloud{Entries: []port.Entry{{ID: "1", Path: "old"}}, FailAt: fail}
		if fail != "" {
			f.Failure = errors.New("failure")
		}
		err := (files.Service{Cloud: f}).Move(t.Context(), "old", "new")
		if (err != nil) != (fail != "") {
			t.Fatal(fail, err)
		}
	}
	f := &porttest.Cloud{Entries: []port.Entry{{ID: "1", Path: "old"}, {ID: "2", Path: "new"}}}
	if err := (files.Service{Cloud: f}).Move(t.Context(), "old", "new"); !errors.Is(err, port.ErrConflict) {
		t.Fatal(err)
	}
	f.StatFunc = func(name string) (port.Entry, error) {
		if name == "old" {
			return port.Entry{ID: "1"}, nil
		}
		return port.Entry{}, errors.New("unavailable")
	}
	if err := (files.Service{Cloud: f}).Move(t.Context(), "old", "new"); err == nil {
		t.Fatal("ignored destination error")
	}
}

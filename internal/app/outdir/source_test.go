// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package outdir_test

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/app/outdir"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestSourceRecords(t *testing.T) {
	disk := &porttest.NotebookFiles{}
	source := port.OutputSource{Endpoint: "https://cloud.invalid", Account: "reader", FolderID: "1", FolderPath: "NOTE/Note"}
	entries := map[string]outdir.Entry{"a.note": {ID: "2"}, "../outside": {ID: "3"}}
	if err := outdir.Store(t.Context(), disk, "out", outdir.MgetRecord, source, entries); err != nil {
		t.Fatal(err)
	}
	got, err := outdir.Load(disk, "out", outdir.MgetRecord, source, false)
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	for _, foreign := range []port.OutputSource{{Endpoint: "elsewhere"}, {Account: "other"}, {FolderID: "9"}, {FolderPath: "elsewhere"}} {
		if _, err = outdir.Load(disk, "out", outdir.MgetRecord, foreign, false); !errors.Is(err, port.ErrRefused) {
			t.Fatal(err)
		}
		if got, err = outdir.Load(disk, "out", outdir.MgetRecord, foreign, true); err != nil || len(got) != 0 {
			t.Fatal(got, err)
		}
	}
	disk.FailAt, disk.Err = "open", errors.New("disk error")
	if _, err = outdir.Load(disk, "out", outdir.MgetRecord, source, false); err == nil {
		t.Fatal("open error ignored")
	}
	disk.FailAt = ""
	disk.Data[filepath.Join("out", outdir.MgetRecord)] = []byte("damaged")
	if _, err = outdir.Load(disk, "out", outdir.MgetRecord, source, false); err == nil {
		t.Fatal("damaged record accepted")
	}
}

func TestOutputBinding(t *testing.T) {
	disk := &porttest.NotebookFiles{Data: map[string][]byte{"stage": []byte("data")}}
	bound, err := outdir.Open(disk, "out")
	if err != nil {
		t.Fatal(err)
	}
	defer bound.Close()
	if err = bound.Save("stage", "out/file", "", nil); err != nil {
		t.Fatal(err)
	}
	if err = bound.SetModTime("out/file", time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err = bound.ModTime("out/missing"); err == nil {
		t.Fatal("missing file accepted")
	}
	if _, err = bound.ModTime("out"); err == nil {
		t.Fatal("directory accepted")
	}
	if name, err := bound.Destination("out/file", ""); err != nil || name != "out/file" {
		t.Fatal(name, err)
	}
	if _, err = bound.Destination("out", ""); err == nil {
		t.Fatal("directory destination accepted")
	}
	if !bound.IsDir("out") {
		t.Fatal("directory not detected")
	}
	if err = bound.Copy(t.Context(), "absent", "out/file"); err == nil {
		t.Fatal("missing stage accepted")
	}
	for _, name := range []string{"../escape", "elsewhere"} {
		if _, err = bound.Open(name); err == nil {
			t.Fatal(name)
		}
		if _, err = bound.Info(name); err == nil {
			t.Fatal(name)
		}
		if err = bound.Remove(name); err == nil {
			t.Fatal(name)
		}
		if err = bound.MkdirAll(name); err == nil {
			t.Fatal(name)
		}
		if err = bound.Produce(t.Context(), name, func(io.Writer) error { return nil }); err == nil {
			t.Fatal(name)
		}
		if err = bound.SetModTime(name, time.Time{}); err == nil {
			t.Fatal(name)
		}
		if _, err = bound.ModTime(name); err == nil {
			t.Fatal(name)
		}
	}
}

type sourceCloud struct{ port.Files }

func (sourceCloud) OutputSource(context.Context) (port.OutputSource, error) {
	return port.OutputSource{}, errors.New("identity unavailable")
}
func TestSourceFailure(t *testing.T) {
	if _, err := outdir.Source(t.Context(), sourceCloud{}, port.Entry{}); err == nil {
		t.Fatal("identity failure ignored")
	}
}

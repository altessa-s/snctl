// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package notebooks_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

type changingDestination struct {
	port.NotebookFiles
	dir                bool
	observations, pins int
}

func (f *changingDestination) IsDir(string) bool {
	f.observations++
	dir := f.dir
	f.dir = !dir
	return dir
}

func (f *changingDestination) Destination(dest, name string) (string, error) {
	if f.IsDir(dest) {
		return filepath.Join(dest, name), nil
	}
	return dest, nil
}

func (f *changingDestination) OpenRoot(dir string) (port.OutputDirectory, error) {
	f.pins++
	return f.NotebookFiles.OpenRoot(dir)
}

func TestTextExportUsesOneDestinationObservation(t *testing.T) {
	for _, directory := range []bool{true, false} {
		s, disk := setup()
		files := &changingDestination{NotebookFiles: disk, dir: directory}
		s.Files = files
		_, err := s.ExportText(t.Context(), "local.note", "out", textExportOptions())
		if err != nil || files.observations != 1 || (files.pins == 1) != directory {
			t.Errorf("directory=%t observations=%d pins=%d err=%v", directory, files.observations, files.pins, err)
		}
	}
}

func TestSingleRenderUsesOrdinaryDestination(t *testing.T) {
	for _, name := range []string{"local.note", "local.spd"} {
		s, disk := setup()
		disk.Data["local.spd"] = []byte("drawing")
		files := &changingDestination{NotebookFiles: disk, dir: true}
		s.Files = files
		opts := port.RenderOptions{}
		if name == "local.note" {
			opts.Page = 1
			opts.Format = "png"
		}
		report, err := s.Render(t.Context(), name, "out", opts)
		if err != nil || files.pins != 0 || filepath.Dir(report.Path) != "out" || len(disk.Data[report.Path]) == 0 {
			t.Errorf("%s path=%q pins=%d err=%v", name, report.Path, files.pins, err)
		}
	}
}

func TestImageFilenameErrorPrecedesSourceAndParse(t *testing.T) {
	s, _ := setup()
	s.Reader = porttest.NoteReader{Err: port.ErrNotebook}
	_, err := s.Render(t.Context(), "local.note", "output.png", port.RenderOptions{Source: "missing.pdf"})
	if !errors.Is(err, port.ErrOptions) || !strings.Contains(err.Error(), "give --page") {
		t.Fatal(err)
	}
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package notebooks_test

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/altessa-s/snctl/cmd/snctl/commands/clitest"
	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/notebooks"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func fixture() porttest.Notebooks {
	comment := "A useful passage"
	n := port.Notebook{Name: "sample.mark", Kind: "mark", Version: 20260016, Device: "N6", Pages: []port.NotePage{
		{Number: 1, Width: 1404, Height: 1872, Layers: []string{"MAINLAYER"}, Text: []string{"Typed text"}, Recognition: "Recognized words", Strokes: 3},
		{Number: 2, Width: 1404, Height: 1872, Text: []string{}}}, Titles: []port.NoteTitle{{Page: 1, Level: 1}, {Page: 2, Level: 2, Text: "Heading"}},
		Highlights: []port.NoteHighlight{{Page: 1, Color: 2, Comment: &comment, Rects: []*port.Rect{nil, {X: 1, Y: 2, Width: 3, Height: 4}}, DocumentRects: []*port.Rect{{Width: 4}}}, {Page: 2}},
		Header:     []port.MetadataEntry{{Key: "VERSION", Value: "1"}}, Footer: []port.MetadataEntry{{Key: "PAGE1", Value: "20"}}}
	return porttest.Notebooks{Notebook: n, Report: port.RenderReport{Path: "sample.pdf", Pages: 2, SheetsOnly: true}, Tree: port.TreeReport{Dir: "pdfs", Documents: []port.RenderedDocument{{Document: "NOTE/Note/sample.mark", Status: "rendered", Path: "pdfs/sample.pdf", Pages: 2, SheetsOnly: true}}}}
}
func TestGolden(t *testing.T) {
	for _, name := range []string{"text", "highlights", "note", "render", "open"} {
		formats := []string{"text", "table", "json"}
		if name == "text" || name == "highlights" {
			formats = []string{"text", "markdown", "json"}
		}
		for _, format := range formats {
			t.Run(name+"-"+format, func(t *testing.T) {
				args := []string{name, "sample.mark"}
				if name == "render" || name == "open" {
					args = append(args, "--format", "pdf")
					if format == "json" {
						args = append(args, "--json")
					}
				} else {
					args = append(args, "--format", format)
				}
				r := clitest.Exec(t, cliutil.Services{Notebooks: fixture()}, clitest.Options{OutTerminal: format == "table"}, notebooks.Commands, args...)
				if r.Code != 0 {
					t.Fatal(r.Error)
				}
				clitest.Golden(t, name+"-"+format, r.Out+r.Err)
			})
		}
	}
	for _, args := range [][]string{{"note", "sample.mark", "--raw"}, {"render", "folder", "-a", "-j", "2", "--incremental"}, {"render", "file", "--page", "1", "-o", "x.svg"}, {"open", "file", "--page", "1"}, {"render", "file", "--format", "jpeg"}} {
		f := fixture()
		f.IsFolder = args[1] == "folder"
		r := clitest.Exec(t, cliutil.Services{Notebooks: f}, clitest.Options{Terminal: true}, notebooks.Commands, args...)
		if r.Code != 0 {
			t.Fatal(args, r.Error)
		}
	}
}
func TestErrors(t *testing.T) {
	for _, name := range []string{"text", "highlights", "note", "render", "open"} {
		for index, err := range []error{port.ErrNotebookUnavailable, port.ErrRenderUnavailable, port.ErrNotebook, port.ErrPage, port.ErrOptions, io.ErrUnexpectedEOF} {
			f := fixture()
			f.Err = err
			r := clitest.Exec(t, cliutil.Services{Notebooks: f}, clitest.Options{}, notebooks.Commands, name, "file")
			if r.Code == 0 || r.Out != "" {
				t.Fatal(r)
			}
			clitest.Golden(t, fmt.Sprintf("error-%s-%d", name, index), fmt.Sprintf("%d %s\n", r.Code, r.Error))
		}
		r := clitest.Exec(t, cliutil.Services{}, clitest.Options{}, notebooks.Commands, name, "file")
		if r.Code == 0 {
			t.Fatal("missing service")
		}
	}
	cases := [][]string{
		{"render", "x", "--format", "bad"}, {"render", "x", "--page", "0"}, {"render", "x", "--page", "-1"},
		{"render", "x", "--scale", "0"}, {"render", "x", "--scale", "NaN"}, {"render", "x", "--scale", "17"},
		{"render", "x", "--scale", "2"}, {"render", "x", "--quality", "101"}, {"render", "x", "--quality", "1"},
		{"render", "x", "--format", "pdf", "--page", "1"}, {"render", "x", "-j", "-1"}, {"render", "x", "-a"},
		{"text", "x", "--page", "0"}, {"text", "x", "--format", "bad"}, {"text", "x", "--format", "markdown", "--json"},
		{"highlights", "x", "--format", "bad"},
	}
	for _, args := range cases {
		r := clitest.Exec(t, cliutil.Services{Notebooks: fixture()}, clitest.Options{}, notebooks.Commands, args...)
		if r.Code != 2 {
			t.Fatal(args, r)
		}
	}
	f := fixture()
	f.Notebook.Kind = "note"
	if r := clitest.Exec(t, cliutil.Services{Notebooks: f}, clitest.Options{}, notebooks.Commands, "highlights", "x"); r.Code != 2 {
		t.Fatal(r)
	}
	f.IsFolder = true
	if r := clitest.Exec(t, cliutil.Services{Notebooks: f}, clitest.Options{}, notebooks.Commands, "render", "folder", "-o", "x.pdf"); r.Code != 2 {
		t.Fatal(r)
	}
	f.Err = errors.New("failure")
	if r := clitest.Exec(t, cliutil.Services{Notebooks: f}, clitest.Options{}, notebooks.Commands, "open", "file"); r.Code == 0 {
		t.Fatal(r)
	}
}

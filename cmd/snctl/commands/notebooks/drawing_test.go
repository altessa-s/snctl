// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package notebooks_test

import (
	"strings"
	"testing"

	"github.com/altessa-s/snctl/cmd/snctl/commands/clitest"
	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/notebooks"
	"github.com/altessa-s/snctl/internal/port"
)

func TestHighlightPassageFormats(t *testing.T) {
	for _, format := range []string{"text", "plain", "table", "markdown", "json"} {
		f := fixture()
		text := "A quoted passage\nrect 99\t100\\end\r\n# heading ```"
		f.Notebook.Highlights[0].Text = &text
		f.Notebook.Highlights[1].Text = new("")
		f.Notebook.Warnings = []string{"highlight text unavailable on page 2"}
		r := clitest.Exec(t, cliutil.Services{Notebooks: f}, clitest.Options{}, notebooks.Commands, "highlights", "file.mark", "--format", format)
		if r.Code != 0 || !strings.Contains(r.Err, "warning: highlight text unavailable") || strings.Contains(r.Out, "warning:") {
			t.Fatal(format, r)
		}
		if strings.Contains(r.Out, "\nrect 99") || strings.Contains(r.Out, "\t100") || strings.Contains(r.Out, "\n# heading") {
			t.Fatal(r.Out)
		}
		clitest.Golden(t, "highlight-passage-"+format, r.Out+r.Err)
	}
}

func TestDrawingCommandOptions(t *testing.T) {
	for _, args := range [][]string{
		{"render", "sketch.spd", "-o", "sketch.png", "--drawing-page", "canvas", "--scale", ".5"},
		{"render", "/EXPORT/sketch.spd", "-o", "-", "--format", "svg"},
		{"render", "sketch.SPD", "-o", "sketch.pdf", "--scale", "2"},
		{"open", "sketch.spd", "--format", "png", "--drawing-page", "painted"},
	} {
		r := clitest.Exec(t, cliutil.Services{Notebooks: fixture()}, clitest.Options{}, notebooks.Commands, args...)
		if r.Code != 0 {
			t.Fatal(args, r)
		}
	}
	for _, args := range [][]string{
		{"render", "sketch.spd", "--drawing-page", "bad"},
		{"render", "journal.note", "--drawing-page", "canvas"},
	} {
		r := clitest.Exec(t, cliutil.Services{Notebooks: fixture()}, clitest.Options{}, notebooks.Commands, args...)
		if r.Code != 2 {
			t.Fatal(args, r)
		}
	}
}

func TestDrawingInspectionFormats(t *testing.T) {
	f := fixture()
	f.Notebook = port.Notebook{Name: "drawing.spd", Kind: "drawing", Drawing: &port.DrawingInfo{Width: new(256.0), Height: new(128.0), FormatVersion: new("2"), Tiles: 3,
		Layers: []port.DrawingLayer{{ID: 1, Name: "Ink", Tiles: 3}}}}
	for _, format := range []string{"text", "table", "json"} {
		r := clitest.Exec(t, cliutil.Services{Notebooks: f}, clitest.Options{OutTerminal: format == "table"}, notebooks.Commands, "note", "drawing.spd", "--format", format)
		if r.Code != 0 || !strings.Contains(r.Out, "256") || !strings.Contains(r.Out, "Ink") {
			t.Fatal(format, r)
		}
		clitest.Golden(t, "drawing-"+format, r.Out+r.Err)
	}
	f.Notebook.Drawing.Width, f.Notebook.Drawing.Height = nil, nil
	r := clitest.Exec(t, cliutil.Services{Notebooks: f}, clitest.Options{}, notebooks.Commands, "note", "drawing.spd")
	if r.Code != 0 || !strings.Contains(r.Out, "canvas_width\t-") {
		t.Fatal(r)
	}
	r = clitest.Exec(t, cliutil.Services{Notebooks: f}, clitest.Options{}, notebooks.Commands, "text", "drawing.spd")
	if r.Code != 2 {
		t.Fatal(r)
	}
}

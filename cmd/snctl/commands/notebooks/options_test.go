// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package notebooks_test

import (
	"strings"
	"testing"

	"github.com/altessa-s/snctl/cmd/snctl/commands/clitest"
	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/notebooks"
	"github.com/altessa-s/snctl/internal/port"
)

func TestDrawingDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		message string
	}{
		{[]string{"--no-marks"}, "--no-marks is only supported by open"},
		{[]string{"--quality", "50"}, "--quality is for JPEG"},
		{[]string{"--page", "1", "--scale", "9"}, "between 0.015625 and 8"},
		{[]string{"--page", "1", "--scale", "0.001"}, "between 0.015625 and 8"},
		{[]string{"--page", "1", "--format", "pdf"}, "--page requires an image"},
		{[]string{"-j", "-1"}, "--jobs must be nonnegative"},
		{[]string{"--format", "jpg", "--quality", "101"}, "--quality must be between"},
		{[]string{"--format", "png", "-o", "-"}, "-o - requires"},
		{[]string{"-o", "-", "--json"}, "-o - cannot be combined"},
	} {
		args := append([]string{"render", "local.note"}, tc.args...)
		r := clitest.Exec(t, cliutil.Services{Notebooks: fixture()}, clitest.Options{}, notebooks.Commands, args...)
		if r.Code != 2 || r.Error == nil || !strings.Contains(r.Error.Error(), tc.message) {
			t.Fatalf("%v: %+v", args, r)
		}
	}
	for _, args := range [][]string{{"render", "local.note", "-o", "-"}, {"render", "local.note", "--page", "1", "--format", "svg", "--scale", ".5"}} {
		r := clitest.Exec(t, cliutil.Services{Notebooks: fixture()}, clitest.Options{}, notebooks.Commands, args...)
		if r.Code != 0 {
			t.Fatal(r)
		}
	}
	f := fixture()
	f.IsFolder = true
	r := clitest.Exec(t, cliutil.Services{Notebooks: f}, clitest.Options{}, notebooks.Commands, "render", "folder", "-o", "-")
	if r.Code != 2 {
		t.Fatal(r)
	}
}

func TestHighlightHeadingOncePerPage(t *testing.T) {
	f := fixture()
	f.Notebook.Highlights = []port.NoteHighlight{{Page: 22}, {Page: 22}, {Page: 30}, {Page: 22}}
	r := clitest.Exec(t, cliutil.Services{Notebooks: f}, clitest.Options{}, notebooks.Commands, "highlights", "local.mark", "--format", "markdown")
	if r.Code != 0 || strings.Count(r.Out, "## Page 22") != 1 || strings.Count(r.Out, "## Page 30") != 1 {
		t.Fatal(r)
	}
}

func TestUnavailableBeforeOptions(t *testing.T) {
	f := fixture()
	f.Unavailable = port.ErrRenderUnavailable
	for _, name := range []string{"render", "open"} {
		r := clitest.Exec(t, cliutil.Services{Notebooks: f}, clitest.Options{}, notebooks.Commands, name, "x.note", "--page", "1", "--scale", "2")
		if r.Code != 5 || r.Error != port.ErrRenderUnavailable {
			t.Fatal(r)
		}
	}
}

func TestStdoutFormatSource(t *testing.T) {
	r := clitest.Exec(t, cliutil.Services{Notebooks: fixture()}, clitest.Options{Env: map[string]string{"SNCTL_FORMAT": "json"}}, notebooks.Commands, "render", "x.note", "-o", "-")
	if r.Code != 2 || !strings.Contains(r.Error.Error(), "SNCTL_FORMAT=json") || strings.Contains(r.Error.Error(), "--json") {
		t.Fatal(r)
	}
}

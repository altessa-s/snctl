// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package notebooks_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestStoredPagesAndHighlightsWithoutSheets(t *testing.T) {
	s, d := setup()
	s.Reader = porttest.NoteReader{Notebook: port.Notebook{Kind: "mark", Pages: []port.NotePage{{Number: 22, Width: 1404, Height: 1872}}, Highlights: []port.NoteHighlight{{Page: 22}, {Page: 30}, {Page: 30}}, Titles: []port.NoteTitle{{Page: 22}}}}
	s.Renderer = porttest.Renderer{Pages: 1}
	r, err := s.Render(t.Context(), "local.note", "", port.RenderOptions{Page: 22})
	if err != nil || r.Pages != 1 {
		t.Fatal(r, err)
	}
	r, err = s.Render(t.Context(), "local.note", "", port.RenderOptions{Format: "png"})
	if err != nil {
		t.Fatal(r, err)
	}
	if _, ok := d.Data[filepath.Join("local-rendered", "local-p0022.png")]; !ok {
		t.Fatal(d.Data)
	}
	for _, page := range []int{1, 30} {
		_, err = s.Render(t.Context(), "local.note", "", port.RenderOptions{Page: page})
		if !errors.Is(err, port.ErrPage) || !strings.Contains(err.Error(), "available pages: 22") {
			t.Fatal(err)
		}
	}
	n, err := s.ReadHighlights(t.Context(), "local.note", []int{30}, nil)
	if err != nil || len(n.Pages) != 0 || len(n.Highlights) != 2 {
		t.Fatal(n, err)
	}
	_, err = s.ReadHighlights(t.Context(), "local.note", []int{1}, nil)
	if !errors.Is(err, port.ErrPage) || !strings.Contains(err.Error(), "22, 30") {
		t.Fatal(err)
	}
	if _, err = s.Read(t.Context(), "local.note", []int{30}, nil); !errors.Is(err, port.ErrPage) {
		t.Fatal(err)
	}
	var out bytes.Buffer
	r, err = s.Render(t.Context(), "local.note", "-", port.RenderOptions{Writer: &out})
	if err != nil || r.Path != "-" || !bytes.HasPrefix(out.Bytes(), []byte("%PDF")) {
		t.Fatal(r, err, out.String())
	}
	for _, opts := range []port.RenderOptions{{}, {Format: "png"}} {
		if _, err = s.Render(t.Context(), "local.note", "-", opts); !errors.Is(err, port.ErrOptions) {
			t.Fatal(err)
		}
	}
	for _, dest := range []string{"out.png", "out.jpg", "pages"} {
		if _, err = s.Render(t.Context(), "local.note", dest, port.RenderOptions{Format: "png"}); !errors.Is(err, port.ErrOptions) {
			t.Fatal(err)
		}
	}
	if _, err = s.Render(t.Context(), "local.note", "out.png", port.RenderOptions{Format: "pdf"}); !errors.Is(err, port.ErrOptions) {
		t.Fatal(err)
	}
	for _, scale := range []float64{.001, 9, 3} {
		_, err = s.Render(t.Context(), "local.note", "", port.RenderOptions{Page: 22, Scale: scale})
		if !errors.Is(err, port.ErrOptions) {
			t.Fatal(scale, err)
		}
		if scale == 3 && !strings.Contains(err.Error(), "largest scale") {
			t.Fatal(err)
		}
	}
	if l := s.Limits(); l.MaximumScale != 8 {
		t.Fatal(l)
	}
	if _, err = s.Render(t.Context(), "local.note", "", port.RenderOptions{Page: 22, Format: "svg", Scale: .5}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RenderTree(t.Context(), "folder", "-", port.RenderOptions{}, port.TreeOptions{}); !errors.Is(err, port.ErrOptions) {
		t.Fatal(err)
	}
}

func TestAvailablePageRanges(t *testing.T) {
	s, _ := setup()
	n := port.Notebook{}
	for i := 1; i <= 50; i++ {
		n.Pages = append(n.Pages, port.NotePage{Number: i})
	}
	s.Reader = porttest.NoteReader{Notebook: n}
	_, err := s.Read(t.Context(), "local.note", []int{99}, nil)
	if err == nil || !strings.Contains(err.Error(), "1-50") {
		t.Fatal(err)
	}
	// A missing cloud reader must still preserve local filesystem failures.
	s.Fetch = nil
	_, err = s.Read(t.Context(), "absent", nil, nil)
	if err == nil {
		t.Fatal("missing input accepted")
	}
}

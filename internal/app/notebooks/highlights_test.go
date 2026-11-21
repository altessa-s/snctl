// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package notebooks_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/localfs"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

type textRenderer struct {
	porttest.Renderer
	pages  []int
	rects  [][]*port.Rect
	pdfs   []string
	cancel func()
}

func (r *textRenderer) PageText(ctx context.Context, pdf []byte, page int, rects []*port.Rect) (string, error) {
	r.pages = append(r.pages, page)
	r.rects = append(r.rects, rects)
	r.pdfs = append(r.pdfs, string(pdf))
	if r.cancel != nil {
		r.cancel()
	}
	return r.Renderer.PageText(ctx, pdf, page, rects)
}

func TestHighlightTextSources(t *testing.T) {
	for _, tc := range []struct {
		name, sibling    string
		ambiguous, cloud bool
	}{
		{"Report.pdf.mark", "Report.pdf", false, false},
		{"Report.mark", "Report.pdf", false, false},
		{"Report.epub.mark", "", false, false},
		{"Report.mark", "", false, false},
		{"Report.mark", "Report.pdf", true, false},
		{"/cloud/Report.pdf.mark", "", false, true},
	} {
		t.Run(tc.name+tc.sibling, func(t *testing.T) {
			s, disk := setup()
			reader := s.Reader.(porttest.NoteReader)
			reader.Notebook.Highlights = []port.NoteHighlight{{Page: 22, Rects: []*port.Rect{{X: 999}}, DocumentRects: []*port.Rect{nil, {X: 19, Width: 100}}}, {Page: 30}}
			s.Reader = reader
			r := &textRenderer{Text: "Extracted passage"}
			s.Renderer = r
			if tc.cloud {
				s.Fetch = sourceFetch{}
				// This relative namesake must not shadow the canonical cloud source.
				disk.Data["cloud/Report.pdf"] = []byte("wrong PDF")
			} else {
				disk.Data[tc.name] = []byte("mark")
				if tc.sibling != "" {
					disk.Data[tc.sibling] = []byte("PDF")
				}
				if tc.ambiguous {
					disk.Data["Report.epub"] = []byte("EPUB")
				}
			}
			n, err := s.ReadHighlights(t.Context(), tc.name, []int{22}, nil)
			if err != nil || len(n.Highlights) != 1 {
				t.Fatal(n, err)
			}
			available := tc.cloud || tc.sibling != "" && !tc.ambiguous
			h := n.Highlights[0]
			if available {
				if h.Text == nil || *h.Text != "Extracted passage" || len(n.Warnings) != 0 || !slices.Equal(r.pages, []int{22}) || r.rects[0][1].X != 19 {
					t.Fatal(n, r)
				}
				wantPDF := "PDF"
				if tc.cloud {
					wantPDF = "source"
				}
				if r.pdfs[0] != wantPDF {
					t.Fatal(r.pdfs)
				}
			} else if h.Text != nil || len(n.Warnings) == 0 || len(r.pages) != 0 {
				t.Fatal(n, r)
			}
			if tc.ambiguous && strings.Contains(n.Warnings[0], "--source") {
				t.Fatal("highlights suggested an unsupported flag", n.Warnings)
			}
			if reader.Notebook.Highlights[0].Text != nil {
				t.Fatal("reader model mutated")
			}
		})
	}
}

func TestHighlightTextFailures(t *testing.T) {
	for _, which := range []string{"unavailable", "source", "extract", "empty", "cancel-source", "cancel-extract", "no-highlights", "not-mark", "missing-cloud"} {
		t.Run(which, func(t *testing.T) {
			s, disk := setup()
			disk.Data["Report.pdf.mark"], disk.Data["Report.pdf"] = []byte("mark"), []byte("pdf")
			r := &textRenderer{}
			s.Renderer = r
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			query := "Report.pdf.mark"
			switch which {
			case "unavailable":
				r.Unavailable = port.ErrRenderUnavailable
			case "source":
				s.Fetch = sourceOnlyFailure{}
				query = "/cloud/Report.pdf.mark"
			case "extract":
				r.TextErr = io.ErrUnexpectedEOF
			case "empty":
				disk.Data["Report.pdf"] = nil
			case "cancel-source":
				s.Fetch = cancelSourceFetch{cancel: cancel}
				query = "/cloud/Report.pdf.mark"
			case "cancel-extract":
				r.cancel = cancel
			case "no-highlights":
				reader := s.Reader.(porttest.NoteReader)
				reader.Notebook.Highlights = nil
				s.Reader = reader
			case "not-mark":
				reader := s.Reader.(porttest.NoteReader)
				reader.Notebook.Kind = "note"
				s.Reader = reader
			case "missing-cloud":
				s.Fetch = sourceFetch{missing: true}
				query = "/cloud/Report.pdf.mark"
			}
			n, err := s.ReadHighlights(ctx, query, nil, nil)
			if strings.HasPrefix(which, "cancel-") {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if which == "no-highlights" || which == "not-mark" {
				if len(n.Warnings) != 0 {
					t.Fatal(n.Warnings)
				}
				return
			}
			if len(n.Warnings) == 0 || n.Highlights[0].Text != nil {
				t.Fatal(n)
			}
		})
	}
}

type sourceOnlyFailure struct{ sourceFetch }

func (sourceOnlyFailure) Read(_ context.Context, query string, _ port.Progress) (port.Entry, []byte, error) {
	if strings.HasSuffix(query, ".pdf") {
		return port.Entry{}, nil, fs.ErrPermission
	}
	return port.Entry{Name: "Report.pdf.mark"}, []byte("mark"), nil
}

type cancelSourceFetch struct {
	sourceFetch
	cancel func()
}

func (f cancelSourceFetch) Read(ctx context.Context, query string, p port.Progress) (port.Entry, []byte, error) {
	if strings.HasSuffix(query, ".pdf") {
		f.cancel()
		return port.Entry{}, nil, ctx.Err()
	}
	return f.sourceFetch.Read(ctx, query, p)
}

type countedSourceFetch struct {
	sourceFetch
	size  *int64
	body  []byte
	reads int
}

func (f *countedSourceFetch) Resolve(ctx context.Context, query string) (port.Entry, error) {
	e, err := f.sourceFetch.Resolve(ctx, query)
	if strings.HasSuffix(query, ".pdf") {
		e.Size = f.size
	}
	return e, err
}

func (f *countedSourceFetch) Read(ctx context.Context, query string, p port.Progress) (port.Entry, []byte, error) {
	if strings.HasSuffix(query, ".pdf") {
		f.reads++
		return port.Entry{Name: "Report.pdf"}, f.body, nil
	}
	return f.sourceFetch.Read(ctx, query, p)
}

func TestManyHighlightsFetchSourceOnce(t *testing.T) {
	s, _ := setup()
	reader := s.Reader.(porttest.NoteReader)
	const count = 1000
	reader.Notebook.Highlights = make([]port.NoteHighlight, count)
	for i := range reader.Notebook.Highlights {
		reader.Notebook.Highlights[i] = port.NoteHighlight{Page: i%2 + 1, DocumentRects: []*port.Rect{{X: float64(i), Width: 1, Height: 1}}}
	}
	s.Reader = reader
	f := &countedSourceFetch{body: []byte("pdf")}
	s.Fetch = f
	r := &textRenderer{Text: "passage"}
	s.Renderer = r
	n, err := s.ReadHighlights(t.Context(), "/cloud/Report.pdf.mark", []int{1}, nil)
	if err != nil || len(n.Warnings) != 0 || len(n.Highlights) != count/2 || f.reads != 1 || len(r.pages) != count/2 {
		t.Fatalf("highlights=%d, source reads=%d, extraction calls=%d, warnings=%v, err=%v", len(n.Highlights), f.reads, len(r.pages), n.Warnings, err)
	}
	for i, h := range n.Highlights {
		if h.Text == nil || *h.Text != "passage" || r.pages[i] != 1 || r.rects[i][0].X != float64(i*2) {
			t.Fatalf("highlight %d lost its individual passage or rectangles", i)
		}
	}
}

func TestHighlightSourceSizeBudget(t *testing.T) {
	for _, which := range []string{"local-over", "cloud-over", "cloud-renderer-limit", "cloud-unknown-size", "exact-budget", "cache-error"} {
		t.Run(which, func(t *testing.T) {
			s, _ := setup()
			s.Index = &recognitionCache{status: port.CacheStatus{Budget: 4}}
			r := &textRenderer{}
			s.Renderer = r
			f := &countedSourceFetch{size: new(int64(5)), body: []byte("12345")}
			s.Fetch = f
			query := "/cloud/Report.pdf.mark"
			wantReads := 0
			switch which {
			case "local-over":
				dir := t.TempDir()
				query = filepath.Join(dir, "Report.pdf.mark")
				for name, body := range map[string]string{"Report.pdf.mark": "mark", "Report.pdf": "12345678"} {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				s.Files = localfs.Files{}
			case "cloud-renderer-limit":
				s.Index = nil
				f.size = new(int64(512<<20 + 1))
			case "cloud-unknown-size":
				f.size = nil
				wantReads = 1
			case "exact-budget":
				f.size, f.body = new(int64(4)), []byte("1234")
				wantReads = 1
			case "cache-error":
				s.Index = &recognitionCache{err: fs.ErrPermission}
			}
			n, err := s.ReadHighlights(t.Context(), query, nil, nil)
			if err != nil || len(n.Highlights) != 2 || f.reads != wantReads {
				t.Fatalf("reads=%d, warnings=%v, err=%v", f.reads, n.Warnings, err)
			}
			if which == "exact-budget" {
				if len(n.Warnings) != 0 || n.Highlights[0].Text == nil {
					t.Fatal(n)
				}
				return
			}
			if len(n.Warnings) == 0 || n.Highlights[0].Text != nil || n.Highlights[1].Text != nil || len(r.pages) != 0 {
				t.Fatal(n.Warnings, r.pages)
			}
			if which != "cache-error" && !strings.Contains(n.Warnings[0], "source PDF exceeds size budget") {
				t.Fatal(n.Warnings)
			}
		})
	}
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package notebooks_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/localfs"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

type sourceRenderer struct {
	porttest.Renderer
	source []byte
	page   int
}

func (r *sourceRenderer) Render(ctx context.Context, data, source []byte, o port.RenderOptions, w io.Writer) (int, error) {
	r.source, r.page = bytes.Clone(source), o.Page
	if len(source) > 0 && o.Diagnostics != nil {
		o.Diagnostics.SheetsDropped = 1
	}
	return r.Renderer.Render(ctx, data, source, o, w)
}

type sourceFetch struct {
	port.Fetch
	readErr, resolveErr error
	missing             bool
}

func (f sourceFetch) Resolve(_ context.Context, query string) (port.Entry, error) {
	return port.Entry{Name: path.Base(query), Path: "cloud/Report.pdf.mark"}, f.resolveErr
}
func (f sourceFetch) Read(_ context.Context, query string, _ port.Progress) (port.Entry, []byte, error) {
	if f.readErr != nil {
		return port.Entry{}, nil, f.readErr
	}
	if path.Ext(query) == ".pdf" && f.missing {
		return port.Entry{}, nil, port.ErrNotFound
	}
	return port.Entry{Name: path.Base(query)}, []byte("source"), nil
}

func TestSourcePDFSelection(t *testing.T) {
	for _, tc := range []struct {
		query, source, sibling string
		overlay                bool
	}{
		{"Report.pdf.mark", "", "Report.pdf", true},
		{"Report.mark", "", "Report.pdf", true},
		{"Report.mark", "other.pdf", "other.pdf", true},
		{"Report.epub.mark", "", "", false},
		{"Report.mark", "", "", false},
		{"cloud/Report.pdf.mark", "", "", true},
	} {
		t.Run(tc.query+tc.source+tc.sibling, func(t *testing.T) {
			s, d := setup()
			r := &sourceRenderer{Pages: 3}
			s.Renderer = r
			s.Fetch = sourceFetch{}
			if tc.query != "cloud/Report.pdf.mark" {
				d.Data[tc.query] = []byte("mark")
			}
			if tc.sibling != "" {
				d.Data[tc.sibling] = []byte("source")
			}
			report, err := s.Render(t.Context(), tc.query, "out.pdf", port.RenderOptions{Source: tc.source})
			if err != nil || report.SheetsOnly == tc.overlay || (len(r.source) > 0) != tc.overlay {
				t.Fatal(report, err, string(r.source))
			}
			if tc.overlay && len(report.Warnings) != 1 {
				t.Fatal(report)
			}
		})
	}
	s, d := setup()
	d.Data["Report.mark"], d.Data["Report.pdf"] = []byte("mark"), []byte("source")
	s.Reader = porttest.NoteReader{Notebook: port.Notebook{Kind: "mark", Pages: []port.NotePage{{Number: 22}}}}
	r := &sourceRenderer{Pages: 1}
	s.Renderer = r
	var out bytes.Buffer
	report, err := s.Render(t.Context(), "Report.mark", "-", port.RenderOptions{Format: "png", Page: 1, Writer: &out})
	if err != nil || r.page != 1 || report.SheetsOnly || out.Len() == 0 {
		t.Fatal(report, err, r.page)
	}
	for _, opts := range []port.RenderOptions{{Source: "bad.epub"}, {Format: "svg", Page: 1}, {Format: "png"}} {
		if _, err := s.Render(t.Context(), "Report.mark", "", opts); !errors.Is(err, port.ErrOptions) {
			t.Fatal(err)
		}
	}
	d.Data["empty.pdf"] = nil
	if _, err := s.Render(t.Context(), "Report.mark", "", port.RenderOptions{Source: "empty.pdf"}); !errors.Is(err, port.ErrOptions) {
		t.Fatal(err)
	}
	s.Reader = porttest.NoteReader{Notebook: port.Notebook{Kind: "note", Pages: []port.NotePage{{Number: 1}}}}
	if _, err := s.Render(t.Context(), "local.note", "", port.RenderOptions{Source: "Report.pdf"}); !errors.Is(err, port.ErrOptions) {
		t.Fatal(err)
	}
}

func TestCloudSourceFailures(t *testing.T) {
	for _, tc := range []struct {
		f      sourceFetch
		source string
		want   error
	}{
		{f: sourceFetch{missing: true}},
		{f: sourceFetch{resolveErr: fs.ErrPermission}, want: fs.ErrPermission},
		{f: sourceFetch{readErr: fs.ErrPermission}, want: fs.ErrPermission},
		{f: sourceFetch{missing: true}, source: "cloud/source.pdf", want: port.ErrNotFound},
	} {
		s, _ := setup()
		s.Fetch = tc.f
		_, err := s.Render(t.Context(), "cloud/Report.pdf.mark", "out.pdf", port.RenderOptions{Source: tc.source})
		if !errors.Is(err, tc.want) {
			t.Fatal(err, tc.want)
		}
	}
}

func TestRelativeLocalSourcesWithRealFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	for name, body := range map[string]string{"Report.pdf.mark": "mark", "Report.mark": "mark", "Report.pdf": "local PDF", "source-without-extension": "explicit PDF"} {
		if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ query, source, want string }{
		{"Report.pdf.mark", "", "local PDF"},
		{"Report.mark", "", "local PDF"},
		{"Report.pdf.mark", "Report.pdf", "local PDF"},
		{"Report.pdf.mark", "source-without-extension", "explicit PDF"},
		{"Report.pdf.mark", "/cloud/source.pdf", "source"},
	} {
		t.Run(tc.query+tc.source, func(t *testing.T) {
			s, _ := setup()
			s.Files = localfs.Files{}
			s.Fetch = sourceFetch{}
			r := &sourceRenderer{Pages: 1}
			s.Renderer = r
			report, err := s.Render(t.Context(), tc.query, "out.pdf", port.RenderOptions{Source: tc.source})
			if err != nil || report.SheetsOnly || string(r.source) != tc.want {
				t.Fatal(report, err, string(r.source))
			}
		})
	}
	// An existing absolute local source remains usable as documented.
	abs, err := filepath.Abs("Report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := setup()
	s.Files = localfs.Files{}
	if _, err = s.Render(t.Context(), "Report.pdf.mark", "out.pdf", port.RenderOptions{Source: abs}); err != nil {
		t.Fatal(err)
	}
}

func TestAmbiguousLegacySource(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, name := range []string{"Report.mark", "Report.pdf", "Report.epub"} {
		if err := os.WriteFile(name, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s, _ := setup()
	s.Files = localfs.Files{}
	r := &sourceRenderer{Pages: 1}
	s.Renderer = r
	report, err := s.Render(t.Context(), "Report.mark", "out.pdf", port.RenderOptions{})
	if err != nil || !report.SheetsOnly || len(report.Warnings) != 1 || len(r.source) != 0 {
		t.Fatal(report, err, string(r.source))
	}
	if !strings.Contains(report.Warnings[0], "or --source") {
		t.Fatal("render omitted its source override hint", report.Warnings)
	}
	report, err = s.Render(t.Context(), "Report.mark", "out.pdf", port.RenderOptions{Source: "Report.pdf"})
	if err != nil || report.SheetsOnly || string(r.source) != "Report.pdf" {
		t.Fatal(report, err, string(r.source))
	}
}

func TestCloudRenderBypassesRelativeLocalNamesake(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("cloud", 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Report.pdf", "Report.pdf.mark"} {
		if err := os.WriteFile(filepath.Join("cloud", name), []byte("local namesake"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s, _ := setup()
	s.Files = localfs.Files{}
	s.Fetch = sourceFetch{}
	r := &sourceRenderer{Pages: 1}
	s.Renderer = r
	report, err := s.Render(t.Context(), "/cloud/Report.pdf.mark", "out.pdf", port.RenderOptions{})
	if err != nil || report.SheetsOnly || string(r.source) != "source" {
		t.Fatal("cloud render read a relative local namesake", report, err, string(r.source))
	}
}

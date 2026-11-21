// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package notebooks_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

// Reject document bytes even if a future refactor accidentally reaches the parser.
type documentGuardReader struct {
	porttest.NoteReader
	t *testing.T
}

func (r documentGuardReader) Parse(data []byte) (port.Notebook, error) {
	if bytes.HasPrefix(data, []byte("%PDF")) || bytes.HasPrefix(data, []byte("PK")) {
		r.t.Fatal("document reached the notebook reader")
	}
	return r.NoteReader.Parse(data)
}

func TestDocumentRenderAndOpen(t *testing.T) {
	for _, local := range []bool{false, true} {
		for _, ext := range []string{".pdf", ".epub", ".PDF"} {
			for _, opts := range []port.RenderOptions{{}, {Format: "png", Page: 2, NoTemplate: true, NoText: true, StrictImages: true}} {
				s, disk := setup()
				s.Reader = documentGuardReader{NoteReader: s.Reader.(porttest.NoteReader), t: t}
				query := "DOCUMENT/Document/Paper" + ext
				doc := "%PDF-1.4 synthetic"
				if ext == ".epub" {
					doc = "PK synthetic EPUB"
				}
				drive := porttest.NewDrive(disk, map[string]string{query: doc, query + ".mark": "mark"})
				if local {
					query = "./Paper" + ext
					disk.Data[query], disk.Data[query+".mark"] = []byte(doc), []byte("mark")
				} else {
					s.Cloud, s.Fetch = drive, files.Service{Cloud: drive, Local: disk}
					query = "/" + query
				}
				r := &sourceRenderer{Pages: 2}
				s.Renderer = r
				direct, err := s.Render(t.Context(), query+".mark", "direct.out", opts)
				if err != nil {
					t.Fatal(err)
				}
				viaDoc, err := s.Render(t.Context(), query, "document.out", opts)
				if err != nil || direct.Pages != viaDoc.Pages || direct.SheetsOnly != viaDoc.SheetsOnly || !bytes.Equal(disk.Data[direct.Path], disk.Data[viaDoc.Path]) {
					t.Fatal(local, ext, opts, direct, viaDoc, err)
				}
				preview, err := s.Open(t.Context(), query, opts)
				if err != nil || preview.Pages != direct.Pages || preview.SheetsOnly != direct.SheetsOnly || !bytes.Equal(disk.Data[direct.Path], disk.Data[preview.Path]) {
					t.Fatal(preview, err)
				}
				if (len(r.source) != 0) != (ext != ".epub") {
					t.Fatal("document did not preserve sidecar source selection", ext, string(r.source))
				}
				if local && drive.Count("download") != 0 {
					t.Fatal(drive.Calls)
				}
			}
		}
	}
}

func TestOriginalDocumentPreview(t *testing.T) {
	for _, local := range []bool{false, true} {
		for _, ext := range []string{".pdf", ".epub"} {
			for _, noMarks := range []bool{false, true} {
				s, disk := setup()
				// Original previews require neither notebook parsing nor rendering.
				s.Reader = documentGuardReader{t: t}
				s.Renderer = porttest.Renderer{Err: errors.New("must not render an original document")}
				query := "DOCUMENT/Document/Paper" + ext
				body := []byte("%PDF-1.4 synthetic original")
				drive := porttest.NewDrive(disk, map[string]string{query: string(body)})
				if noMarks {
					drive.Seed(query+".mark", "corrupt annotations deliberately skipped")
				}
				if local {
					query = "./Paper" + ext
					disk.Data[query] = body
					if noMarks {
						disk.Data[query+".mark"] = []byte("corrupt annotations deliberately skipped")
					}
				} else {
					s.Cloud, s.Fetch = drive, files.Service{Cloud: drive, Local: disk}
					query = "/" + query
				}
				preview, err := s.Open(t.Context(), query, port.RenderOptions{NoMarks: noMarks})
				if err != nil || filepath.Base(preview.Path) != "Paper"+ext || !bytes.Equal(disk.Data[preview.Path], body) || preview.Pages != 0 {
					t.Fatal(preview, err)
				}
				if s.Opener.(*porttest.Opener).Path != preview.Path || len(preview.Warnings) != 1 {
					t.Fatal(preview)
				}
				message := "has no annotations"
				if noMarks {
					message = "annotations skipped"
				}
				if !strings.Contains(preview.Warnings[0], message) {
					t.Fatal(preview.Warnings)
				}
				if !local && (drive.Count("download") != 1 || drive.Count("download "+strings.TrimPrefix(query, "/")+".mark") != 0) {
					t.Fatal(drive.Calls)
				}
				_, err = s.Render(t.Context(), query, "out.pdf", port.RenderOptions{NoMarks: noMarks})
				want := port.ErrNotFound
				if noMarks {
					want = port.ErrOptions
				}
				if !errors.Is(err, want) || !noMarks && !strings.Contains(err.Error(), "use snctl get") {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestDocumentResolutionFailures(t *testing.T) {
	const doc = "DOCUMENT/Document/Paper.pdf"
	for _, preview := range []bool{false, true} {
		for _, failure := range []string{"conflict", "list", "stat", "download"} {
			s, disk := setup()
			drive := porttest.NewDrive(disk, map[string]string{doc: "%PDF-synthetic", doc + ".mark": "mark"})
			s.Cloud, s.Fetch = drive, files.Service{Cloud: drive, Local: disk}
			want := error(fs.ErrPermission)
			if failure == "conflict" {
				mark := port.Entry{Path: doc + ".mark"}
				s.Cloud = &porttest.Cloud{Entries: []port.Entry{mark, mark}}
				want = port.ErrConflict
			} else {
				key := failure
				if failure != "list" {
					key += " " + doc
					if failure == "download" {
						key += ".mark"
					}
				}
				drive.Fail[key] = want
			}
			var err error
			if preview {
				_, err = s.Open(t.Context(), "/"+doc, port.RenderOptions{})
			} else {
				_, err = s.Render(t.Context(), "/"+doc, "out.pdf", port.RenderOptions{})
			}
			if !errors.Is(err, want) || s.Opener.(*porttest.Opener).Path != "" {
				t.Fatal(preview, failure, err)
			}
		}
	}
}

func TestOriginalPreviewFailures(t *testing.T) {
	for _, failure := range []string{"open", "preview", "produce", "opener", "missing", "cancel"} {
		s, disk := setup()
		disk.Data["Paper.pdf"] = []byte("%PDF-synthetic")
		ctx := t.Context()
		if failure == "opener" {
			s.Opener = &porttest.Opener{Err: fs.ErrPermission}
		} else if failure == "missing" {
			delete(disk.Data, "Paper.pdf")
		} else if failure == "cancel" {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		} else {
			disk.FailAt, disk.Err = failure, fs.ErrPermission
		}
		r, err := s.Open(ctx, "Paper.pdf", port.RenderOptions{NoMarks: true})
		if err == nil {
			t.Fatal(failure, "succeeded")
		}
		if _, exists := disk.Data[r.Path]; exists {
			t.Fatal(failure, "left a failed preview")
		}
	}
	s, _ := setup()
	if _, err := s.Open(t.Context(), "local.note", port.RenderOptions{NoMarks: true}); !errors.Is(err, port.ErrOptions) {
		t.Fatal(err)
	}
	if _, err := s.RenderTree(t.Context(), "folder", "out", port.RenderOptions{NoMarks: true}, port.TreeOptions{}); !errors.Is(err, port.ErrOptions) {
		t.Fatal(err)
	}
}

func TestDocumentFolderExportStaysOnSidecars(t *testing.T) {
	s, disk := setup()
	drive := porttest.NewDrive(disk, map[string]string{
		"folder/Paper.pdf":      "%PDF synthetic document must not be rendered",
		"folder/Paper.pdf.mark": "mark",
		"folder/Book.epub":      "PK synthetic document must not be rendered",
		"folder/Sketch.spd":     "drawing must not enter folder exports",
	})
	s.Cloud, s.Fetch = drive, files.Service{Cloud: drive, Local: disk}
	s.Reader = documentGuardReader{NoteReader: s.Reader.(porttest.NoteReader), t: t}
	r, err := s.RenderTree(t.Context(), "/folder", "exports", port.RenderOptions{}, port.TreeOptions{})
	if err != nil || r.Rendered != 1 || len(r.Documents) != 1 || r.Documents[0].Document != "folder/Paper.pdf.mark" || !r.Documents[0].SheetsOnly {
		t.Fatal(r, err)
	}
}

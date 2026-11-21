// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package notebooks_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/localfs"
	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestHighlightsViaDocument(t *testing.T) {
	for _, local := range []bool{false, true} {
		for _, ext := range []string{".pdf", ".epub"} {
			s, disk := setup()
			source := "DOCUMENT/Document/Paper" + ext
			d := porttest.NewDrive(disk, map[string]string{source: "document", source + ".mark": "annotations"})
			if local {
				source = "./Paper" + ext
				disk.Data[source] = []byte("document")
				disk.Data[source+".mark"] = []byte("annotations")
			} else {
				s.Cloud = d
				s.Fetch = files.Service{Cloud: d, Local: disk}
				source = "/" + source
			}
			n, err := s.ReadHighlights(t.Context(), source, []int{2}, nil)
			if err != nil || n.Name != "Paper"+ext+".mark" || len(n.Highlights) != 1 || n.Highlights[0].Page != 2 {
				t.Fatal(n, err)
			}
			if local && d.Count("download") != 0 {
				t.Fatal(d.Calls)
			}
			if local {
				delete(disk.Data, source+".mark")
			} else {
				d.Drop(strings.TrimPrefix(source, "/") + ".mark")
			}
			if _, err = s.ReadHighlights(t.Context(), source, nil, nil); !errors.Is(err, port.ErrNotFound) || !strings.Contains(err.Error(), source+" has no annotations") {
				t.Fatal(err)
			}
		}
	}
}

func TestCloudAnnotationSiblings(t *testing.T) {
	const source = "DOCUMENT/Document/Paper.pdf"
	mark := port.Entry{Name: "Paper.pdf.mark", Path: source + ".mark"}
	folder := port.Entry{Path: mark.Path, Folder: true}
	for _, tc := range []struct {
		name     string
		siblings []port.Entry
		wantErr  error
	}{
		{name: "ambiguous", siblings: []port.Entry{mark, mark}, wantErr: port.ErrConflict},
		{name: "folder only", siblings: []port.Entry{folder}, wantErr: port.ErrNotFound},
		{name: "file and folder", siblings: []port.Entry{mark, folder}, wantErr: port.ErrConflict},
		{name: "duplicate documents", siblings: []port.Entry{{Path: source}, {Path: source}, mark}, wantErr: port.ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, disk := setup()
			d := porttest.NewDrive(disk, map[string]string{source: "document", mark.Path: "annotations"})
			s.Fetch = files.Service{Cloud: d, Local: disk}
			s.Cloud = &porttest.Cloud{Entries: tc.siblings}
			n, err := s.ReadHighlights(t.Context(), "/"+source, nil, nil)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ReadHighlights error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil && d.Count("download") != 0 {
				t.Fatal("downloaded an unresolved sidecar", d.Calls)
			}
			if tc.wantErr == nil && n.Name != mark.Name {
				t.Fatal(n)
			}
		})
	}
}

func TestHighlightsWithoutCloudListing(t *testing.T) {
	const source = "DOCUMENT/Document/Paper.pdf"
	s, disk := setup()
	d := porttest.NewDrive(disk, map[string]string{source: "document", source + ".mark": "annotations"})
	s.Fetch = files.Service{Cloud: d, Local: disk}
	n, err := s.ReadHighlights(t.Context(), "/"+source, nil, nil)
	if err != nil || n.Name != "Paper.pdf.mark" || d.Count("list") != 0 {
		t.Fatal(n, err, d.Calls)
	}
}

func TestAnnotationResolutionFailures(t *testing.T) {
	s, disk := setup()
	s.Reader = porttest.NoteReader{Unavailable: port.ErrNotebookUnavailable}
	if _, err := s.ReadHighlights(t.Context(), "/cloud/Paper.pdf", nil, nil); !errors.Is(err, port.ErrNotebookUnavailable) {
		t.Fatal("unavailable reader resolved a cloud document", err)
	}
	s.Reader = porttest.NoteReader{}
	disk.Data["Paper.pdf"] = []byte("pdf")
	disk.FailAt, disk.Err = "open", fs.ErrPermission
	if _, err := s.ReadHighlights(t.Context(), "Paper.pdf", nil, nil); !errors.Is(err, fs.ErrPermission) {
		t.Fatal(err)
	}
	disk.FailAt = ""
	if _, err := s.ReadHighlights(t.Context(), "/cloud/Paper.pdf", nil, nil); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	d := porttest.NewDrive(disk, map[string]string{"cloud/Paper.pdf": "pdf", "cloud/Paper.pdf.mark": "mark"})
	s.Cloud = d
	s.Fetch = files.Service{Cloud: d, Local: disk}
	d.Folders["cloud/Folder.pdf"] = true
	d.Seed("cloud/Folder.pdf.mark", "mark")
	if _, err := s.ReadHighlights(t.Context(), "/cloud/Folder.pdf", nil, nil); err == nil || !strings.Contains(err.Error(), "has no annotations") {
		t.Fatal("accepted a folder as a document", err)
	}
	for _, fail := range []string{"stat cloud/Paper.pdf", "list", "download cloud/Paper.pdf.mark"} {
		d.Fail[fail] = fs.ErrPermission
		if _, err := s.ReadHighlights(t.Context(), "/cloud/Paper.pdf", nil, nil); !errors.Is(err, fs.ErrPermission) {
			t.Fatal(fail, err)
		}
		delete(d.Fail, fail)
	}
	// Explicit local inputs never fall back to cloud sidecars.
	dir := t.TempDir()
	doc := filepath.Join(dir, "Paper.pdf")
	if err := os.WriteFile(doc, []byte("pdf"), 0600); err != nil {
		t.Fatal(err)
	}
	s.Files = localfs.Files{}
	if _, err := s.ReadHighlights(t.Context(), doc, nil, nil); err == nil || !strings.Contains(err.Error(), "has no annotations") {
		t.Fatal(err)
	}
	if _, err := s.ReadHighlights(t.Context(), filepath.Join(dir, "Missing.pdf"), nil, nil); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
}

func TestLocalTextSearchFoldsMarks(t *testing.T) {
	s, _ := setup()
	dir := t.TempDir()
	for _, name := range []string{"Paper.pdf", "Paper.pdf.mark", "orphan.epub.mark"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s.Files = localfs.Files{}
	s.Reader = porttest.NoteReader{Notebook: port.Notebook{Kind: "mark", Pages: []port.NotePage{{Number: 1, Text: []string{"search"}}}}}
	for _, raw := range []bool{false, true} {
		found, err := s.FindText(t.Context(), dir, "search", nil, raw)
		if err != nil || len(found) != 2 {
			t.Fatal(found, err)
		}
		for _, match := range found {
			if strings.Contains(match.Entry.Name, "Paper") {
				if raw && match.Entry.Name != "Paper.pdf.mark" {
					t.Fatal(match)
				}
				if !raw && (match.Entry.Name != "Paper.pdf" || match.Entry.Mark == nil || match.Entry.Mark.Size == nil || match.Entry.Mark.Modified == nil) {
					t.Fatal(match)
				}
			}
		}
	}
}

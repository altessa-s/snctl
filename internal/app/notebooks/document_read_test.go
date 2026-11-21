// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package notebooks_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestReadAndSearchViaDocument(t *testing.T) {
	for _, local := range []bool{false, true} {
		for _, ext := range []string{".pdf", ".epub", ".PDF"} {
			t.Run(filepath.Base(ext)+"/"+map[bool]string{true: "local", false: "cloud"}[local], func(t *testing.T) {
				s, disk := setup()
				note := port.Notebook{Kind: "mark", Pages: []port.NotePage{{Number: 1, Text: []string{"annotation text"}}, {Number: 2, Recognition: "recognized"}}}
				s.Reader = documentGuardReader{Notebook: note, t: t}
				query := "DOCUMENT/Document/Paper" + ext
				d := porttest.NewDrive(disk, map[string]string{query: "%PDF or PK original", query + ".mark": "synthetic mark"})
				if local {
					query = filepath.Join("local", "Paper"+ext)
					disk.Data[query], disk.Data[query+".mark"] = []byte("%PDF original"), []byte("synthetic mark")
				} else {
					s.Cloud, s.Fetch = d, files.Service{Cloud: d, Local: disk}
					query = "/" + query
				}
				n, err := s.Read(t.Context(), query, []int{2}, nil)
				if err != nil || n.Name != "Paper"+ext || n.Mark != "Paper"+ext+".mark" || len(n.Pages) != 1 || n.Pages[0].Recognition != "recognized" {
					t.Fatal(n, err)
				}
				for _, raw := range []bool{false, true} {
					matches, findErr := s.FindText(t.Context(), query, "annotation recognized", nil, raw)
					if findErr != nil || len(matches) != 1 || len(matches[0].Pages) != 2 {
						t.Fatal(matches, findErr)
					}
					want := "Paper" + ext
					if raw {
						want += ".mark"
					}
					if matches[0].Entry.Name != want || !raw && matches[0].Entry.Mark == nil {
						t.Fatal(matches)
					}
				}
				paths, err := s.ExportText(t.Context(), query, "export.txt", port.TextOptions{Extension: "txt", Encode: func(n port.Notebook) ([]byte, error) {
					return []byte(n.Text(false)), nil
				}})
				if err != nil || len(paths) != 1 || !bytes.Contains(disk.Data[paths[0]], []byte("Paper"+ext+" (annotations: Paper"+ext+".mark)")) {
					t.Fatal(paths, err, disk.Data)
				}
				if d.Count("download "+strings.TrimPrefix(query, "/")) != 0 {
					t.Fatal("downloaded original document", d.Calls)
				}
				if _, err = s.Read(t.Context(), query, []int{99}, nil); !errors.Is(err, port.ErrPage) {
					t.Fatal(err)
				}
				if local {
					delete(disk.Data, query+".mark")
				} else {
					d.Drop(strings.TrimPrefix(query, "/") + ".mark")
				}
				_, readErr := s.Read(t.Context(), query, nil, nil)
				_, findErr := s.FindText(t.Context(), query, "text", nil, false)
				_, exportErr := s.ExportText(t.Context(), query, "missing.txt", port.TextOptions{Extension: "txt", Encode: func(port.Notebook) ([]byte, error) {
					t.Fatal("encoded missing annotations")
					return nil, nil
				}})
				for _, err := range []error{readErr, findErr, exportErr} {
					if !errors.Is(err, port.ErrNotFound) || !strings.Contains(err.Error(), query+" has no annotations") {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestDocumentReadFailures(t *testing.T) {
	s, disk := setup()
	query := filepath.Join("local", "Paper.epub")
	disk.Data[query], disk.Data[query+".mark"] = []byte("PK original"), []byte("synthetic")
	s.Reader = porttest.NoteReader{Unavailable: port.ErrNotebookUnavailable}
	if _, err := s.Read(t.Context(), query, nil, nil); !errors.Is(err, port.ErrNotebookUnavailable) {
		t.Fatal(err)
	}
	s.Reader = porttest.NoteReader{Err: port.ErrNotebook}
	if _, err := s.Read(t.Context(), query, nil, nil); !errors.Is(err, port.ErrNotebook) {
		t.Fatal(err)
	}
}

func TestEPUBHighlightWarningParity(t *testing.T) {
	s, disk := setup()
	const doc = "DOCUMENT/Document/Paper.epub"
	d := porttest.NewDrive(disk, map[string]string{doc: "PK original", doc + ".mark": "synthetic mark"})
	s.Cloud, s.Fetch = d, files.Service{Cloud: d, Local: disk}
	direct, err := s.ReadHighlights(t.Context(), "/"+doc+".mark", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	viaDoc, err := s.ReadHighlights(t.Context(), "/"+doc, nil, nil)
	if err != nil || !slices.Equal(viaDoc.Warnings, direct.Warnings) || len(viaDoc.Warnings) != 1 ||
		viaDoc.Warnings[0] != "highlight text unavailable: no unambiguous source PDF (EPUB text extraction is unsupported)" {
		t.Fatal(viaDoc, direct, err)
	}
	for _, h := range viaDoc.Highlights {
		if h.Text != nil {
			t.Fatal("invented EPUB passage text", h)
		}
	}
	if d.Count("download "+doc) != 0 {
		t.Fatal("downloaded EPUB for extraction", d.Calls)
	}
}

func TestDocumentReadAmbiguity(t *testing.T) {
	for _, mode := range []string{"NFC sidecar", "two sidecars", "document twins", "folder sidecar"} {
		t.Run(mode, func(t *testing.T) {
			s, disk := setup()
			const doc = "DOCUMENT/Document/й.epub"
			const mark = "DOCUMENT/Document/и\u0306.epub.mark"
			d := porttest.NewDrive(disk, map[string]string{doc: "PK original", mark: "mark"})
			s.Fetch = files.Service{Cloud: d, Local: disk}
			entries, err := d.List(t.Context(), "DOCUMENT/Document", false)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "two sidecars":
				entries = append(entries, port.Entry{ID: "other", Path: doc + ".mark"})
			case "document twins":
				entries = append(entries, port.Entry{ID: "other", Path: "DOCUMENT/Document/и\u0306.epub"})
			case "folder sidecar":
				entries = append(entries, port.Entry{ID: "other", Path: doc + ".mark", Folder: true})
			}
			s.Cloud = &porttest.Cloud{Entries: entries}
			n, err := s.Read(t.Context(), "/"+doc, nil, nil)
			if mode == "NFC sidecar" {
				if err != nil || n.Name != "й.epub" || n.Mark != "и\u0306.epub.mark" {
					t.Fatal(n, err)
				}
			} else if !errors.Is(err, port.ErrConflict) || d.Count("download") != 0 {
				t.Fatal(err, d.Calls)
			}
		})
	}
}

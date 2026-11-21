//go:build (darwin || linux || windows) && cgo && !norender

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package notebooks_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/localfs"
	"github.com/altessa-s/snctl/internal/adapter/notes"
	"github.com/altessa-s/snctl/internal/adapter/renderer"
	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/app/notebooks"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestDocumentOverlayFixtureParity(t *testing.T) {
	document, err := os.ReadFile("../../../testdata/overlay/Report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	mark, err := os.ReadFile("../../../testdata/overlay/Report.pdf.mark")
	if err != nil {
		t.Fatal(err)
	}
	for _, local := range []bool{true, false} {
		for _, format := range []string{"pdf", "png"} {
			t.Run(format+"/"+map[bool]string{true: "local", false: "cloud"}[local], func(t *testing.T) {
				dir := t.TempDir()
				query := filepath.Join(dir, "Report.pdf")
				s := notebooks.Service{Files: localfs.Files{Dir: dir}, Reader: notes.Reader{}, Renderer: renderer.Renderer{}, Opener: &porttest.Opener{}}
				if local {
					for name, data := range map[string][]byte{query: document, query + ".mark": mark} {
						if err := os.WriteFile(name, data, 0600); err != nil {
							t.Fatal(err)
						}
					}
				} else {
					query = "DOCUMENT/Document/Report.pdf"
					disk := &porttest.NotebookFiles{}
					drive := porttest.NewDrive(disk, map[string]string{query: string(document), query + ".mark": string(mark)})
					s.Cloud, s.Fetch = drive, files.Service{Cloud: drive, Local: disk}
					query = "/" + query
				}
				o := port.RenderOptions{Format: format, Scale: .1, NoText: true, StrictImages: true}
				if format == "png" {
					o.Page = 1
				}
				var direct, viaDoc bytes.Buffer
				o.Writer = &direct
				want, err := s.Render(t.Context(), query+".mark", "-", o)
				if err != nil {
					t.Fatal(err)
				}
				o.Writer = &viaDoc
				got, err := s.Render(t.Context(), query, "-", o)
				if err != nil || got.Pages != want.Pages || got.SheetsOnly != want.SheetsOnly || !bytes.Equal(direct.Bytes(), viaDoc.Bytes()) {
					t.Fatalf("document = %+v, sidecar = %+v, bytes equal = %t, error = %v", got, want, bytes.Equal(direct.Bytes(), viaDoc.Bytes()), err)
				}
				preview, err := s.Open(t.Context(), query, o)
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(preview.Path)
				if err != nil || preview.Pages != want.Pages || !bytes.Equal(direct.Bytes(), data) {
					t.Fatal("preview differs from sidecar render", preview, err)
				}
			})
		}
	}
}

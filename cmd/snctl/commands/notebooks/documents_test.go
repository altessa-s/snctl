// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package notebooks_test

import (
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/cmd/snctl/commands/clitest"
	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/notebooks"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"

	app "github.com/altessa-s/snctl/internal/app/notebooks"
)

func TestDocumentReadingOutput(t *testing.T) {
	for _, ext := range []string{".pdf", ".epub"} {
		for _, command := range []string{"text", "note"} {
			for _, format := range []string{"text", "json", "markdown"} {
				if format == "markdown" && command == "note" {
					continue
				}
				t.Run(command+ext+"/"+format, func(t *testing.T) {
					name := "Paper" + ext
					query := filepath.Join("local", name)
					disk := &porttest.NotebookFiles{Data: map[string][]byte{query: []byte("original"), query + ".mark": []byte("mark")}}
					s := app.Service{Files: disk, Reader: porttest.NoteReader{Notebook: port.Notebook{Kind: "mark", Pages: []port.NotePage{{Number: 1, Text: []string{"annotation"}}}}}}
					r := clitest.Exec(t, cliutil.Services{Notebooks: s}, clitest.Options{}, notebooks.Commands, command, query, "--format", format)
					if r.Code != 0 || !strings.Contains(r.Out, name) || !strings.Contains(r.Out, name+".mark") {
						t.Fatal(r)
					}
					if format == "json" {
						var n struct {
							Name string `json:"name"`
							Mark string `json:"mark"`
						}
						if err := json.Unmarshal([]byte(r.Out), &n); err != nil || n.Name != name || n.Mark != name+".mark" {
							t.Fatal(n, err)
						}
					}
					delete(disk.Data, query+".mark")
					r = clitest.Exec(t, cliutil.Services{Notebooks: s}, clitest.Options{}, notebooks.Commands, command, query)
					if r.Code != 1 || !errors.Is(r.Error, port.ErrNotFound) || !strings.Contains(r.Error.Error(), "has no annotations") {
						t.Fatal(r)
					}
				})
			}
		}
	}
}

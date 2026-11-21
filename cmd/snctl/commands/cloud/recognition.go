// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"errors"
	"path"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/port"
)

func recognitionCell(e port.Entry) output.Cell {
	if e.Folder || !strings.EqualFold(path.Ext(e.Name), ".note") {
		return output.Text("-")
	}
	if e.Recognition == nil {
		return output.Text("?")
	}
	if *e.Recognition {
		return output.Text("yes")
	}
	return output.Text("no")
}

func recognize(cmd *cobra.Command, env *cliutil.Env, entries []port.Entry, scan bool) ([]port.Entry, error) {
	services, err := env.Services(cmd)
	if err != nil {
		return nil, err
	}
	svc, ok := services.Notebooks.(port.NotebookText)
	if !ok {
		if scan {
			return nil, cliutil.ErrNoService
		}
		return entries, nil
	}
	p, done := cliutil.Progress(env.ProgressStreams(cmd), "reading recognition")
	defer done()
	return svc.Recognition(cmd.Context(), entries, scan, p)
}

func findText(cmd *cobra.Command, env *cliutil.Env, folder, words string, marks bool) error {
	services, err := env.Services(cmd)
	if err != nil {
		return err
	}
	svc, ok := services.Notebooks.(port.NotebookText)
	if !ok {
		return cliutil.ErrNoService
	}
	p, done := cliutil.Progress(env.ProgressStreams(cmd), "searching text")
	defer done()
	found, err := svc.FindText(cmd.Context(), folder, words, p, marks)
	if err != nil {
		if errors.Is(err, port.ErrOptions) {
			return cliutil.Usage(err)
		}
		return err
	}
	table := output.Table{Columns: []output.Column{{Title: columnFlags}, {Title: columnPath}, {Title: "pages"}}}
	plain := output.Table{Columns: []output.Column{{Title: columnPath}, {Title: "pages"}, {Title: columnRecognition}}}
	views := make([]textMatchView, 0, len(found))
	for _, match := range found {
		pages := make([]string, len(match.Pages))
		for i, p := range match.Pages {
			pages[i] = strconv.Itoa(p)
		}
		table.Rows = append(table.Rows, []output.Cell{flagsCell(match.Entry), output.Path(match.Entry.Path), output.Text(strings.Join(pages, ","))})
		plain.Rows = append(plain.Rows, []output.Cell{output.Path(match.Entry.Path), output.Text(strings.Join(pages, ",")), recognitionCell(match.Entry)})
		views = append(views, textMatchView{viewFile(match.Entry), match.Pages})
	}
	return emitFiles(cmd, env, output.Result{JSON: views, Doc: output.Doc{table}, PlainDoc: output.Doc{plain}}, len(found) > 0)
}

type textMatchView struct {
	Entry fileView `json:"entry"`
	Pages []int    `json:"pages"`
}

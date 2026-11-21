// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package notebooks

import (
	"bytes"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/port"
)

func exportText(cmd *cobra.Command, env *cliutil.Env, svc port.Notebooks, query, dest, format string, pages []int, raw bool, p port.Progress) error {
	text, ok := svc.(port.NotebookText)
	if !ok {
		return cliutil.ErrNoService
	}
	extension := "txt"
	if format == formatMarkdown {
		extension = "md"
	}
	if format == formatJSON {
		extension = "json"
	}
	opts := port.TextOptions{Extension: extension, Pages: pages, Raw: raw, Progress: p, Encode: func(n port.Notebook) ([]byte, error) {
		n.Header, n.Footer = nil, nil
		if format != formatJSON {
			return []byte(n.Text(format == formatMarkdown)), nil
		}
		var b bytes.Buffer
		r, _ := output.Lookup(formatJSON)
		err := r.Render(&b, output.Result{JSON: viewNotebook(n)}, output.Style{})
		return b.Bytes(), err
	}}
	paths, err := text.ExportText(cmd.Context(), query, dest, opts)
	if err != nil {
		return explained(err)
	}
	table := output.Table{Columns: []output.Column{{Title: keyPath}}}
	for _, p := range paths {
		table.Rows = append(table.Rows, []output.Cell{output.Path(p)})
	}
	return env.Emit(cmd, output.Result{JSON: struct {
		Paths []string `json:"paths"`
	}{paths}, Doc: output.Doc{table}})
}

func textFormat(cmd *cobra.Command, env *cliutil.Env, kind, format, dest string, raw bool) (string, error) {
	if kind == textCommand && dest != "" && !cmd.Flags().Changed("format") && !env.JSON(cmd) {
		switch strings.ToLower(filepath.Ext(dest)) {
		case ".md":
			format = formatMarkdown
		case ".json":
			format = formatJSON
		case ".txt":
			format = textCommand
		}
	}
	chosen, err := readingFormat(cmd, env, kind, format)
	if err != nil {
		return "", err
	}
	if kind == textCommand && raw && chosen != formatJSON {
		return "", cliutil.Usagef("--raw requires JSON text output")
	}
	return chosen, nil
}

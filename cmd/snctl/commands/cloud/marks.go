// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package cloud

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/port"
)

func changeOptions(cmd *cobra.Command, env *cliutil.Env, dryRun, exact bool, question string) port.ChangeOptions {
	return port.ChangeOptions{DryRun: dryRun, Exact: exact, Confirm: func(plan []port.FileChange) error {
		if len(plan) > 1 {
			cliutil.Notef(cmd.ErrOrStderr(), "including the document's .mark annotations")
		}
		paths := make([]string, 0, len(plan))
		for _, change := range plan {
			name := change.Entry.Path
			if change.Destination != "" {
				name += " -> " + change.Destination
			}
			paths = append(paths, name)
		}
		return cliutil.Confirm(cmd, env, fmt.Sprintf("%s (%d entries: %s)?", strings.TrimSuffix(question, "?"), len(plan), strings.Join(paths, ", ")))
	}}
}

func emitChangePlan(cmd *cobra.Command, env *cliutil.Env, plan []port.FileChange) error {
	table := output.Table{Columns: []output.Column{{Title: columnPath}, {Title: "destination"}}}
	for _, change := range plan {
		table.Rows = append(table.Rows, []output.Cell{output.Path(change.Entry.Path), output.Path(change.Destination)})
	}
	return env.Emit(cmd, output.Result{JSON: struct {
		Items  []port.FileChange `json:"items"`
		DryRun bool              `json:"dry_run"`
	}{plan, true}, Doc: output.Doc{table}})
}

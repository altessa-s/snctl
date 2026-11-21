// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package debug

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/port"
)

const actionClear = "clear"

func cacheCommands(env *cliutil.Env) *cobra.Command {
	group := &cobra.Command{Use: "cache", Short: "Inspect or clear profile caches",
		Long: `Inspect the selected profile's local cache, or every profile with --all.
status reports bytes, budget, body and temporary counts, confirmation time
and directory on standard output. clear asks before removing cache-owned
data, retaining the lock and removal counter. Neither changes cloud files.`,
		Example: "snctl debug cache status --all\nsnctl debug cache clear --yes"}
	for _, action := range []string{"status", actionClear} {
		var all bool
		cmd := &cobra.Command{Use: action, Short: strings.ToUpper(action[:1]) + action[1:] + " the selected profile cache", Args: cliutil.Args(cobra.NoArgs),
			RunE: func(cmd *cobra.Command, _ []string) error {
				svc, err := cliutil.Need(cmd, env, func(s cliutil.Services) port.CacheAdmin { return s.Cache })
				if err != nil {
					return err
				}
				statuses, err := svc.Status(cmd.Context(), all)
				if err != nil {
					return err
				}
				if action == actionClear {
					names := make([]string, 0, len(statuses))
					for _, s := range statuses {
						names = append(names, s.Profile)
					}
					if err := cliutil.Confirm(cmd, env, "Clear the caches of profiles "+strings.Join(names, ", ")+"?"); err != nil {
						return err
					}
					cleared, err := svc.Clear(cmd.Context(), all)
					if err != nil {
						return err
					}
					return env.Emit(cmd, output.Fields(struct {
						Cleared []string `json:"cleared"`
					}{cleared}, output.Field{Key: "cleared", Value: output.Text(strings.Join(cleared, ", "))}))
				}
				table := output.Table{Columns: []output.Column{{Title: "profile"},
					{Title: "bytes"},
					{Title: "budget"},
					{Title: "budget_source"},
					{Title: "blobs"},
					{Title: "temporaries"},
					{Title: "confirmed"},
					{Title: "dir"}}}
				for _, s := range statuses {
					budget := output.Size(s.Budget)
					if s.Budget == 0 {
						budget = output.Text("unlimited")
					}
					table.Rows = append(table.Rows, []output.Cell{output.Text(s.Profile),
						output.Size(s.Bytes), budget,
						output.Text(s.BudgetSource),
						output.Count(s.Blobs),
						output.Count(s.Temporaries),
						output.Time(s.Confirmed),
						output.Path(s.Dir)})
				}
				return env.Emit(cmd, output.Result{JSON: statuses, Doc: output.Doc{table}})
			}}
		cacheHelp(cmd, action)
		cmd.Flags().BoolVarP(&all, "all", "a", false, "every profile")
		group.AddCommand(cmd)
	}
	return group
}

// cacheHelp describes the local effect of each cache action.
func cacheHelp(cmd *cobra.Command, action string) {
	cmd.Long = `Report the selected profile's local cache on standard output: bytes,
budget, body and temporary counts, last confirmation and directory.
--all includes every profile. This contacts no cloud and prints no
secret values.`
	cmd.Example = "snctl debug cache status\nsnctl debug cache status --all --json"
	if action == actionClear {
		cmd.Long = `Remove cache-owned data for the selected profile, or every profile
with --all, retaining the lock and removal counter. Ask first, naming
the profiles; --yes answers. Standard output names the cleared
profiles. Cloud files and session secrets remain.`
		cmd.Example = "snctl debug cache clear --yes\nsnctl debug cache clear --all --yes"
	}
}

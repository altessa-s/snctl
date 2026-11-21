// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/port"
)

const defaultSyncJobs = 4

func newSync(env *cliutil.Env) *cobra.Command {
	var opts port.SyncOptions
	cmd := &cobra.Command{Use: "sync [path]", Short: "Keep the listing and, with --files, bodies for offline reads", GroupID: cliutil.GroupRead,
		Example: "snctl sync\nsnctl sync NOTE/Note --files -j 8\nsnctl --offline get NOTE/Note/Journal.note -o journal.note",
		Long: `List the live library even when already current. Without --files only the
listing is kept. With --files every body below path is retained by MD5.
Check the budget before downloading; Ctrl-C keeps completed files.
Recycled entries are absent from the device listing. Nothing is changed
in the cloud. Standard output reports the scope, downloaded and cached
bodies, bytes and cache budget.`,
		Args: cliutil.Args(cobra.MaximumNArgs(1)), ValidArgsFunction: complete(env, true),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.Jobs < 0 {
				return cliutil.Usagef("--jobs must not be negative")
			}
			if disabled, _ := cmd.Flags().GetBool("no-cache"); disabled {
				return cliutil.Usagef("sync requires the cache; remove --no-cache")
			}
			svc, err := cliutil.Need(cmd, env, func(s cliutil.Services) port.Syncer { return s.Syncer })
			if err != nil {
				return err
			}
			query := "/"
			if len(args) > 0 {
				query = args[0]
			}
			update, done := cliutil.Transfer(env.ProgressStreams(cmd), "syncing")
			defer done()
			opts.Progress = update
			r, err := svc.Sync(cmd.Context(), query, opts)
			if err != nil {
				return err
			}
			budget := output.Size(r.Budget)
			if r.Budget == 0 {
				budget = output.Text("unlimited")
			}
			return env.Emit(cmd, output.Fields(r,
				output.Field{Key: "current", Value: output.Bool(r.Current)}, output.Field{Key: "path", Value: output.Path(r.Path)},
				output.Field{Key: "documents", Value: output.Count(r.Documents)}, output.Field{Key: "fetched", Value: output.Count(r.Fetched)},
				output.Field{Key: "cached", Value: output.Count(r.Cached)}, output.Field{Key: "bytes_fetched", Value: output.Size(r.Bytes)},
				output.Field{Key: "bytes_needed", Value: output.Size(r.Needed)}, output.Field{Key: "cache_bytes", Value: output.Size(r.CacheBytes)},
				output.Field{Key: "budget", Value: budget}, output.Field{Key: "budget_source", Value: output.Text(r.BudgetSource)}))
		}}
	cmd.Flags().BoolVar(&opts.Files, "files", false, "keep every file body below path")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "check the scope even when a complete sync is recorded")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", defaultSyncJobs, "concurrent downloads (0 uses 4)")
	return cmd
}

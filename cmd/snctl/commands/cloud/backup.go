// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/port"
)

func backup(env *cliutil.Env) *cobra.Command {
	var o port.BackupOptions
	cmd := &cobra.Command{Use: "backup <dir> [path]", Short: "Back up whole files locally; restore writes to the cloud", GroupID: cliutil.GroupRead,
		Example: "snctl backup ~/Supernote-backup\nsnctl backup ~/Work-backup NOTE/Note/Work -j 8",
		Long: `Copies whole files into a locked local directory, with a manifest written last.
Deleted files move to removed/ unless --prune. A directory is bound to its profile
and path; --force and --rescope override those guards separately. Recycled files
cannot be backed up because original paths and full MD5 metadata are unavailable.
Nothing is changed in the cloud and no confirmation is asked. Standard output
reports the directory and document, written, skipped, moved and pruned counts.
backup restore uploads verified files and asks first; it is a separate operation.`,
		Args: cliutil.Args(cobra.RangeArgs(1, pairArgs)),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := cliutil.Need(cmd, env, func(s cliutil.Services) port.Backups { return s.Backups })
			if err != nil {
				return err
			}
			query := "/"
			if len(args) == pairArgs {
				query = args[1]
			}
			progress, done := cliutil.Transfer(env.ProgressStreams(cmd), "backing up")
			o.Progress = progress
			r, err := svc.Backup(cmd.Context(), args[0], query, o)
			done()
			fields := []output.Field{{Key: "dir", Value: output.Path(r.Dir)},
				{Key: "documents", Value: output.Count(r.Documents)}, {Key: "written", Value: output.Count(r.Written)},
				{Key: "skipped", Value: output.Count(r.Skipped)}, {Key: "moved", Value: output.Count(r.Moved)},
				{Key: "pruned", Value: output.Count(r.Pruned)}, {Key: "outside", Value: output.Count(r.Outside)}}
			return errors.Join(env.Emit(cmd, output.Fields(r, fields...)), err)
		}}
	cmd.Flags().BoolVar(&o.Force, "force", false, "allow a different profile or endpoint")
	cmd.Flags().BoolVar(&o.Rescope, "rescope", false, "bind to another path; never prune former files")
	cmd.Flags().BoolVar(&o.Prune, "prune", false, "delete files gone from the library instead of retaining them")
	cmd.Flags().BoolVar(&o.All, "all", false, "include recycled files (unavailable with the current API)")
	cmd.Flags().IntVarP(&o.Jobs, "jobs", "j", 0, "downloads at a time (0: 4)")
	cmd.AddCommand(backupRestore(env))
	cmd.AddGroup(&cobra.Group{ID: cliutil.GroupWrite, Title: cliutil.TitleWrite})
	return cmd
}

func backupRestore(env *cliutil.Env) *cobra.Command {
	var o port.UploadOptions
	var to string
	cmd := &cobra.Command{Use: "restore <dir> [cloud-folder]", GroupID: cliutil.GroupWrite, Short: "Restore verified backup files to their recorded paths",
		Long: `Verify every file recorded in a local backup before planning cloud changes.
With no destination, restore the recorded tablet paths; a destination places
paths relative to the backup scope below that folder. Identical bytes are
skipped; different files require --replace. Show the plan before asking;
--yes answers and --dry-run verifies and prints without writes. Each upload
is a separate change under the account sync lock, so partial successes
remain. Standard output reports every planned or completed item and its
status. removed/ is not restored.`,
		Example: "snctl backup restore ~/Work-backup --dry-run\nsnctl backup restore ~/Work-backup NOTE/Note/Recovered --yes",
		Args:    cliutil.Args(cobra.RangeArgs(1, pairArgs)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == pairArgs {
				if cmd.Flags().Changed("to") {
					return cliutil.Usagef("give the cloud folder once, as an argument or --to")
				}
				to = args[1]
			}
			svc, err := cliutil.Need(cmd, env, func(s cliutil.Services) port.Backups { return s.Backups })
			if err != nil {
				return err
			}
			o.Confirm = confirmPlan(cmd, env, "Restore backup: %s?")
			progress, done := cliutil.Transfer(env.ProgressStreams(cmd), "restoring")
			o.Progress = progress
			r, err := svc.Restore(cmd.Context(), args[0], to, o)
			done()
			return emitTransfer(cmd, env, r, err)
		}}
	cmd.Flags().StringVar(&to, "to", "", "restore paths relative to the backup scope below this cloud folder")
	cmd.Flags().BoolVar(&o.Replace, "replace", false, "replace different files at existing destinations")
	cmd.Flags().BoolVar(&o.DryRun, "dry-run", false, "verify files and print the plan; send no writes")
	cmd.Flags().IntVarP(&o.Jobs, "jobs", "j", 0, "uploads at a time (0: 4)")
	return cmd
}

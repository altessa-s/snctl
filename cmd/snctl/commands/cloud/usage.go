// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/port"
)

func usage(env *cliutil.Env) *cobra.Command {
	var depth int
	var all bool
	cmd := &cobra.Command{Use: "du [path]", Short: "Show folder sizes and account capacity", GroupID: cliutil.GroupRead,
		Long: `Sums the sizes the cloud lists for the files below a folder ("/" by
default) and below each folder in it, down to --depth levels (0: the total
only), with the account's used and total capacity. --all also counts the
recycle bin, separately, and needs "/": recycled entries have no original
paths. No file is downloaded or changed. The folder totals and capacity go
to standard output.`,
		Example: heredoc.Doc(`
			snctl du NOTE/Note -d 2
			snctl du / --all --json
		`),
		Args: cliutil.Args(cobra.MaximumNArgs(1)), ValidArgsFunction: complete(env, true),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := library(cmd, env)
			if err != nil {
				return err
			}
			report, err := svc.Usage(cmd.Context(), queryOr(args), depth, all)
			if err != nil {
				return err
			}
			table := output.Table{Columns: []output.Column{{Title: columnPath}, {Title: columnSize, Role: output.RoleSize}, {Title: "files"}}}
			for _, f := range report.Folders {
				table.Rows = append(table.Rows, []output.Cell{output.Path(f.Path), output.Size(f.Bytes), output.Count(f.Files)})
			}
			capacity := output.Record{Fields: []output.Field{
				{Key: "used", Value: output.Size(report.Used)}, {Key: "capacity", Value: output.Size(report.Capacity)},
				{Key: "recycled", Value: output.Size(report.Recycled)}}}
			return env.Emit(cmd, output.Result{JSON: report, Doc: output.Doc{table, capacity}})
		}}
	cmd.Flags().IntVarP(&depth, "depth", "d", 1, "report folders this many levels down")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "count the recycle bin separately (needs /)")
	return cmd
}

func duplicates(env *cliutil.Env) *cobra.Command {
	var marks bool
	cmd := &cobra.Command{Use: "dupes [path]", Short: "Group nonempty files with identical bytes", GroupID: cliutil.GroupRead,
		Long: `Groups the files below a folder ("/" by default) that have the same
content hash and size: identical bytes, as the cloud lists them. This is
not rmctl's dupes, which groups documents made from the same source: two
exports of one notebook made at different times differ here. Files the
cloud lists without a hash or size are left out. Nothing is downloaded or
changed. Full-name .mark sidecars fold into their documents; --marks includes
raw sidecars. Standard output groups hash, size and file paths.`,
		Example: "snctl dupes DOCUMENT/Document --json",
		Args:    cliutil.Args(cobra.MaximumNArgs(1)), ValidArgsFunction: complete(env, true),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := library(cmd, env)
			if err != nil {
				return err
			}
			groups, err := svc.Duplicates(cmd.Context(), queryOr(args), marks)
			if err != nil {
				return err
			}
			table := output.Table{Columns: []output.Column{{Title: "hash"}, {Title: columnSize, Role: output.RoleSize}, {Title: columnPath}}}
			for _, group := range groups {
				for _, file := range group.Files {
					table.Rows = append(table.Rows, []output.Cell{output.Text(group.Hash), output.Size(group.Size), output.Path(file.Path)})
				}
			}
			if groups == nil {
				groups = []port.DuplicateGroup{}
			}
			views := make([]duplicateGroupView, 0, len(groups))
			for _, group := range groups {
				views = append(views, duplicateGroupView{group.Hash, group.Size, viewFiles(group.Files)})
			}
			return env.Emit(cmd, output.Result{JSON: views, Doc: output.Doc{table}})
		}}
	cmd.Flags().BoolVar(&marks, "marks", false, "include raw .mark files in duplicate groups")
	return cmd
}

// queryOr is the path argument, or "/" without one.
func queryOr(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return "/"
}

type duplicateGroupView struct {
	Hash  string     `json:"hash"`
	Size  int64      `json:"size"`
	Files []fileView `json:"files"`
}

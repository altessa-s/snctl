// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/port"

	"golang.org/x/text/unicode/norm"
)

func files(cmd *cobra.Command, env *cliutil.Env) (port.Files, error) {
	return cliutil.Need(cmd, env, func(s cliutil.Services) port.Files { return s.Files })
}

func fileCommands(env *cliutil.Env) []*cobra.Command {
	return []*cobra.Command{newSync(env), listing(env, "ls", false), listing(env, "tree", true), stat(env), find(env), usage(env), duplicates(env),
		mkdir(env), move(env, false), move(env, true), remove(env), trash(env), restore(env), get(env), put(env), mget(env), mput(env)}
}

// Column and field names shared by several results.
const (
	columnName        = "name"
	columnPath        = "path"
	columnSize        = "size"
	columnRecognition = "recognition"
)

func entriesResult(entries []port.Entry) output.Result {
	return fileResult(entries, "", false, false)
}

func fileResult(entries []port.Entry, folder string, ids, recognition bool) output.Result {
	if entries == nil {
		entries = []port.Entry{}
	}
	table := output.Table{Columns: []output.Column{
		{Title: "type"}, {Title: columnSize, Role: output.RoleSize},
		{Title: "modified", Role: output.RoleTime}, {Title: columnFlags},
		{Title: columnName, Role: output.RoleName},
	}}
	if ids {
		table.Columns = append(table.Columns, output.Column{Title: "id", Role: output.RoleID})
	}
	plain := output.Table{Columns: append(append([]output.Column{}, table.Columns[:3]...), table.Columns[4:]...)}
	if recognition {
		plain.Columns = append(plain.Columns, output.Column{Title: columnRecognition})
	}
	for _, e := range entries {
		name := e.Path
		if name == "" {
			name = e.Name
		}
		if folder != "" {
			name = strings.TrimPrefix(name, strings.Trim(folder, "/")+"/")
		}
		kind := "-"
		if e.Folder {
			kind = "d"
		}
		size, modified := output.Text(""), output.Text("")
		if e.Size != nil && !e.Folder {
			size = output.Size(*e.Size)
		}
		if e.Modified != nil {
			modified = output.Time(*e.Modified)
		}
		row := []output.Cell{output.Text(kind), size, modified, flagsCell(e), output.Path(name)}
		if ids {
			row = append(row, output.ID(e.ID))
		}
		table.Rows = append(table.Rows, row)
		plainRow := append(append([]output.Cell{}, row[:3]...), row[4:]...)
		if recognition {
			plainRow = append(plainRow, recognitionCell(e))
		}
		plain.Rows = append(plain.Rows, plainRow)
	}
	return output.Result{JSON: viewFiles(entries), Doc: output.Doc{table}, PlainDoc: output.Doc{plain}}
}

func treeNodes(entries []port.Entry, folder string, ids, recognition bool) (nodes, plain []output.Node) {
	for _, e := range entries {
		if path.Clean("/"+path.Dir(e.Path)) != path.Clean("/"+folder) {
			continue
		}
		cell := output.Text(e.Name)
		if e.Folder {
			cell = cell.With(output.Folder)
		}
		cells := []output.Cell{flagsCell(e), cell}
		if ids {
			cells = append(cells, output.ID(e.ID))
		}
		plainCells := append([]output.Cell{}, cells[1:]...)
		if recognition {
			plainCells = append(plainCells, recognitionCell(e))
		}
		children, plainChildren := treeNodes(entries, e.Path, ids, recognition)
		nodes = append(nodes, output.Node{Cells: cells, Children: children})
		plain = append(plain, output.Node{Cells: plainCells, Children: plainChildren})
	}
	return nodes, plain
}

func listing(env *cliutil.Env, name string, recursive bool) *cobra.Command {
	var ids, recognition, marks bool
	cmd := &cobra.Command{Use: name + " [path]", Short: "List tablet paths", GroupID: cliutil.GroupRead,
		Long: `List the entries in a tablet folder ("/" by default) on standard output.
Names follow the device tree exactly, including NOTE/Note and
DOCUMENT/Document. --ids adds the identifiers. No file is downloaded or
changed.`,
		Example:           "snctl " + name + " DOCUMENT/Document\nsnctl " + name + " NOTE/Note --ids --json",
		Args:              cliutil.Args(cobra.MaximumNArgs(1)),
		ValidArgsFunction: complete(env, true),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := files(cmd, env)
			if err != nil {
				return err
			}
			folder := "/"
			if len(args) > 0 {
				folder = args[0]
			}
			entries, err := svc.List(cmd.Context(), folder, recursive)
			if err != nil {
				return err
			}
			entries = port.FoldMarks(entries, marks)
			entries, err = recognize(cmd, env, entries, recognition)
			if err != nil && entries == nil {
				return err
			}
			result := fileResult(entries, folder, ids, recognition)
			if recursive {
				top := strings.Trim(path.Clean("/"+folder), "/")
				nodes, plain := treeNodes(entries, top, ids, recognition)
				result.Doc = output.Doc{output.Tree{Nodes: nodes}}
				result.PlainDoc = output.Doc{output.Tree{Nodes: plain}}
			}
			return errors.Join(emitFiles(cmd, env, result, len(entries) > 0), err)
		}}
	if recursive {
		cmd.Short = "Show tablet files as a tree"
		cmd.Long = `Show the entries below a tablet folder ("/" by default) as a tree on
standard output. --ids adds identifiers. JSON answers contain flat,
complete entries instead of a drawn tree. No file is downloaded or
changed.`
	}
	cmd.Flags().BoolVar(&ids, "ids", false, "show entry identifiers")
	cmd.Flags().BoolVar(&marks, "marks", false, "show raw .mark files as separate entries")
	cmd.Flags().BoolVar(&recognition, columnRecognition, false, "read unknown .note bodies to discover recognized handwriting")
	cmd.Long += `
FLAGS shows R for recognized handwriting and H for a sibling .mark.
Documents fold their full-name .mark sidecars; --marks keeps raw sidecars
visible. --recognition reads unknown .note bodies through the cache, with
progress; --offline uses cached bodies only.`
	return cmd
}

func stat(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{Use: "stat <path>", Short: "Describe a tablet entry", GroupID: cliutil.GroupRead,
		Long: `Show what the device listing knows about an exact tablet path: identifier,
name, path, kind, hash and, when supplied, size and modification time. The
answer on standard output is a labeled record, or one object with --json.
A PDF/EPUB includes its full-name .mark metadata when present.
No file is downloaded or changed.`,
		Example:           "snctl stat NOTE/Note/Journal.note\nsnctl stat DOCUMENT/Document/Paper.pdf --json",
		Args:              cliutil.Args(cobra.ExactArgs(1)),
		ValidArgsFunction: complete(env, false),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := files(cmd, env)
			if err != nil {
				return err
			}
			e, warning, err := statWithMark(cmd.Context(), svc, args[0])
			if err != nil {
				return err
			}
			if warning != "" {
				cliutil.Notef(cmd.ErrOrStderr(), "warning: %s", warning)
			}
			known, err := recognize(cmd, env, []port.Entry{e}, false)
			if err != nil {
				return err
			}
			e = known[0]
			fields := []output.Field{
				{Key: "id", Value: output.ID(e.ID)},
				{Key: columnName, Value: output.Text(e.Name)},
				{Key: columnPath, Value: output.Path(e.Path)},
				{Key: "folder", Value: output.Bool(e.Folder)},
				{Key: "hash", Value: output.Text(e.Hash)}}

			if e.Size != nil {
				fields = append(fields, output.Field{Key: columnSize, Value: output.Size(*e.Size)})
			}
			if e.Modified != nil {
				fields = append(fields, output.Field{Key: "modified", Value: output.Time(*e.Modified)})
			}
			plainDoc := output.Fields(viewFile(e), fields...).Doc
			fields = append(fields, output.Field{Key: columnRecognition, Value: recognitionCell(e)})
			fields = append(fields, output.Field{Key: columnFlags, Value: flagsCell(e)})
			if e.Mark != nil {
				fields = append(fields, output.Field{Key: "mark_path", Value: output.Path(e.Mark.Path)})
				if e.Mark.Size != nil {
					fields = append(fields, output.Field{Key: "mark_size", Value: output.Size(*e.Mark.Size)})
				}
				if e.Mark.Modified != nil {
					fields = append(fields, output.Field{Key: "mark_modified", Value: output.Time(*e.Mark.Modified)})
				}
			}
			result := output.Fields(viewFile(e), fields...)
			result.PlainDoc = plainDoc
			return env.Emit(cmd, result)
		}}
}

func find(env *cliutil.Env) *cobra.Command {
	var text string
	var recognition, marks bool
	cmd := &cobra.Command{Use: "find <name> [path] | find --text words [path]", Short: "Find tablet names or recognized text", GroupID: cliutil.GroupRead,
		Long: `Find names containing a substring, ignoring case, below a tablet folder
("/" by default). Full-name .mark sidecars fold into PDF/EPUB documents;
--marks shows raw sidecars. --recognition discovers unknown recognized
handwriting.
With --text, find notebooks containing every whitespace-separated word,
ignoring case, in cleaned recognition and typed text boxes. Report paths
and page numbers. This reads bodies through the cache; --offline requires
cached bodies. Existing local folders are also accepted for --text and need
no login. Nothing is changed in the cloud. Answers go to standard output;
progress goes to standard error.`,
		Example: "snctl find Journal NOTE/Note\nsnctl find --text 'release notes' NOTE/Note --json\nsnctl find --text words ./notes",
		Args: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("text") {
				if strings.TrimSpace(text) == "" {
					return cliutil.Usagef("--text needs at least one word")
				}
				return cliutil.Args(cobra.MaximumNArgs(1))(cmd, args)
			}
			return cliutil.Args(cobra.RangeArgs(1, pairArgs))(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			folder := "/"
			if cmd.Flags().Changed("text") {
				if len(args) > 0 {
					folder = args[0]
				}
				return findText(cmd, env, folder, text, marks)
			}
			svc, err := files(cmd, env)
			if err != nil {
				return err
			}
			if len(args) > 1 {
				folder = args[1]
			}
			entries, err := svc.List(cmd.Context(), folder, true)
			if err != nil {
				return err
			}
			entries = port.FoldMarks(entries, marks)
			found := make([]port.Entry, 0)
			for _, e := range entries {
				needle := norm.NFC.String(strings.ToLower(args[0]))
				if strings.Contains(norm.NFC.String(strings.ToLower(e.Name)), needle) ||
					e.Mark != nil && strings.Contains(norm.NFC.String(strings.ToLower(e.Mark.Name)), needle) {
					found = append(found, e)
				}
			}
			found, err = recognize(cmd, env, found, recognition)
			if err != nil && found == nil {
				return err
			}
			result := fileResult(found, "", false, recognition)
			return errors.Join(emitFiles(cmd, env, result, len(found) > 0), err)
		}}
	cmd.Flags().BoolVar(&marks, "marks", false, "show raw .mark files as separate entries")
	cmd.Flags().StringVar(&text, "text", "", "recognized or typed text contains all these words, ignoring case")
	cmd.Flags().BoolVar(&recognition, columnRecognition, false, "read unknown .note bodies to discover recognized handwriting")
	return cmd
}

func mkdir(env *cliutil.Env) *cobra.Command {
	var parents bool
	cmd := &cobra.Command{Use: "mkdir <path>", Short: "Create a tablet folder", GroupID: cliutil.GroupWrite,
		Long: `Create a folder at a full tablet path. -p creates the missing folders
above it as well and accepts a folder already there. Ask before changing
the cloud; --yes answers. Re-check parents fresh after confirmation; changed
plans fail with exit 4. Verify written names afterward; a twin or failed listing
is exit 6, with the write retained. Standard output describes the resulting folder.`,
		Example: "snctl mkdir NOTE/Note/Projects --yes\nsnctl mkdir -p DOCUMENT/Document/Papers/2026 --yes",
		Args:    cliutil.Args(cobra.ExactArgs(1)), ValidArgsFunction: complete(env, true),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := library(cmd, env)
			if err != nil {
				return err
			}
			e, err := svc.Mkdir(cmd.Context(), args[0], parents, port.ChangeOptions{Confirm: func([]port.FileChange) error {
				return cliutil.Confirm(cmd, env, fmt.Sprintf("Create the folder %s?", args[0]))
			}})
			if err != nil {
				return err
			}
			return emitFiles(cmd, env, entriesResult([]port.Entry{e}), true)
		}}
	cmd.Flags().BoolVarP(&parents, "parents", "p", false, "create missing parent folders and accept existing folders")
	return cmd
}

func move(env *cliutil.Env, rename bool) *cobra.Command {
	var dryRun, exact bool
	name, short := "mv <path> <destination>", "Move an entry into a folder or to a new path"
	if rename {
		name, short = "rename <path> <name>", "Rename an entry within its folder"
	}
	cmd := &cobra.Command{Use: name, Short: short, GroupID: cliutil.GroupWrite,
		Long: `Move an entry into an existing folder, keeping its name, or to a new full
tablet path whose parent exists. rename accepts only a new filename and
keeps the parent folder. A document carries its full-name .mark sidecar.
Occupied destinations, including the target sidecar, are refused before
writing. NFC-equivalent source siblings are refused; --exact selects only the
byte-exact name (absent means not-found). Document/sidecar pairing must stay
unambiguous, including with --exact. These commands take paths, not IDs.
Re-check parents fresh after confirmation (changes: exit 4). Verify
destinations after writing. Twins or a failed listing mean exit 6; the
write is retained, with no rollback.
Ask before changing the cloud; --yes answers. --dry-run lists both paths
without changing anything. Standard output reports the destination.`,
		Example: "snctl mv DOCUMENT/Document/Draft.pdf DOCUMENT/Document/Archive --yes\nsnctl rename NOTE/Note/Journal.note Diary.note --yes",
		Args:    cliutil.Args(cobra.ExactArgs(pairArgs)), ValidArgsFunction: complete(env, false),
		RunE: func(cmd *cobra.Command, args []string) error {
			destination := args[1]
			if rename {
				if destination == "" || destination == "." || destination == ".." || strings.ContainsAny(destination, "/\\") {
					return cliutil.Usagef("rename requires a filename without separators")
				}
				destination = path.Join(path.Dir(args[0]), destination)
			}
			svc, err := library(cmd, env)
			if err != nil {
				return err
			}
			question := fmt.Sprintf("Move %s to %s?", args[0], destination)
			plan, err := svc.MoveWithMarks(cmd.Context(), args[0], destination, rename, changeOptions(cmd, env, dryRun, exact, question))
			if err != nil {
				return err
			}
			if dryRun {
				return emitChangePlan(cmd, env, plan)
			}
			return success(cmd, env, "moved to "+strings.TrimPrefix(path.Clean(destination), "/"))
		}}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list document and sidecar changes without writing")
	cmd.Flags().BoolVar(&exact, "exact", false, "select only the byte-exact source name; document/sidecar pairing must stay unambiguous")
	return cmd
}

func remove(env *cliutil.Env) *cobra.Command {
	var permanent, dryRun, exact bool
	cmd := &cobra.Command{Use: "rm <path>", Short: "Move an entry to the recycle bin", GroupID: cliutil.GroupWrite,
		Long: `Move an entry at an exact tablet path to the recycle bin. A document carries
its full-name .mark sidecar. --permanent bypasses the bin and deletes both for
good. Ask before either change; --yes answers. --dry-run lists both files
without changing anything. Standard output names the path and says whether
it was recycled or permanently deleted. NFC-equivalent source siblings are
refused; --exact selects only the byte-exact name (absent means not-found).
Document/sidecar pairing must stay unambiguous even with --exact. Paths only,
not IDs. Re-check the parent fresh after confirmation; changes abort with
exit 4 before any file changes. Deletes have no post-write verification.`,
		Example: "snctl rm DOCUMENT/Document/Draft.pdf --yes\nsnctl rm NOTE/Note/Old.note --permanent --yes",
		Args:    cliutil.Args(cobra.ExactArgs(1)), ValidArgsFunction: complete(env, false),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := library(cmd, env)
			if err != nil {
				return err
			}
			question := "Recycle entry?"
			if permanent {
				question = "Permanently delete entry?"
			}
			plan, err := svc.RemoveWithMarks(cmd.Context(), args[0], permanent, changeOptions(cmd, env, dryRun, exact, question))
			if err != nil {
				return err
			}
			if dryRun {
				return emitChangePlan(cmd, env, plan)
			}
			if permanent {
				return success(cmd, env, plan[0].Entry.Path+": permanently deleted")
			}
			return success(cmd, env, plan[0].Entry.Path+": moved to the recycle bin")
		}}
	cmd.Flags().BoolVar(&permanent, "permanent", false, "bypass the recycle bin")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list document and sidecar changes without writing")
	cmd.Flags().BoolVar(&exact, "exact", false, "select only the byte-exact source name; document/sidecar pairing must stay unambiguous")
	return cmd
}

func trash(env *cliutil.Env) *cobra.Command {
	list := func(cmd *cobra.Command, _ []string) error {
		svc, err := files(cmd, env)
		if err != nil {
			return err
		}
		entries, err := svc.Trash(cmd.Context())
		if err != nil {
			return err
		}
		return emitFiles(cmd, env, fileResult(entries, "", true, false), len(entries) > 0)
	}
	cmd := &cobra.Command{Use: "trash", Short: "Inspect or empty the recycle bin", GroupID: cliutil.GroupWrite,
		Long: `List recycled entries and their identifiers on standard output; trash alone
is trash ls and changes nothing. Original tablet paths are unavailable in
the recycle bin. restore takes a listed identifier. trash empty permanently
deletes everything in the bin and asks first; --yes answers.`,
		Example: "snctl trash\nsnctl trash ls --json\nsnctl trash empty --yes",
		Args:    cliutil.Args(cobra.NoArgs), RunE: list}
	cmd.AddCommand(&cobra.Command{Use: "ls", Short: "List recycled entries",
		Long: `List recycled entries and their identifiers on standard output without
changing the cloud. The bin has no original tablet paths. Use a listed
identifier with restore to recover an entry.`,
		Example: "snctl trash ls\nsnctl trash ls --json",
		Args:    cliutil.Args(cobra.NoArgs), RunE: list})
	cmd.AddCommand(&cobra.Command{Use: "empty", Short: "Permanently delete every recycled entry",
		Long: `Delete every entry in the selected account's recycle bin for good. Ask
before deleting anything; --yes answers. Standard output reports that the
bin was emptied. This cannot be undone.`,
		Example: "snctl trash ls\nsnctl trash empty --yes",
		Args:    cliutil.Args(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := files(cmd, env)
			if err != nil {
				return err
			}
			return mutate(cmd, env, "Permanently empty the entire recycle bin?", "trash emptied", svc.EmptyTrash)
		}})
	cliutil.GroupReadWrite(cmd, "ls")
	cmd.AddCommand(restore(env))
	return cmd
}

func restore(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{Use: "restore <id>", Short: "Restore a recycled identifier", GroupID: cliutil.GroupWrite,
		Long: `Restore an entry and its unique full-name .mark sibling using a trash ls
identifier. Resolve the original parent and refuse occupied or Unicode-equivalent
names before asking; --yes answers. Recheck before restoring and verify afterward.
Standard output reports the restored identifier.`,
		Example: "snctl trash ls\nsnctl restore 123456789 --yes",
		Args:    cliutil.Args(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := library(cmd, env)
			if err != nil {
				return err
			}
			_, err = svc.RestoreWithMarks(cmd.Context(), args[0], changeOptions(cmd, env, false, false, "Restore from the recycle bin?"))
			if err != nil {
				return err
			}
			return success(cmd, env, "restored "+args[0])
		}}
}

func complete(env *cliutil.Env, folders bool) cobra.CompletionFunc {
	return cliutil.CompleteCloud(env, folders)
}

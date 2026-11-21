// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"errors"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/port"
)

// planLines is how many planned changes a confirmation lists before it
// summarizes the rest.
const planLines = 20

func local(cmd *cobra.Command, env *cliutil.Env) (port.LocalFiles, error) {
	return cliutil.Need(cmd, env, func(s cliutil.Services) port.LocalFiles { return s.Local })
}

func get(env *cliutil.Env) *cobra.Command {
	var destination string
	cmd := &cobra.Command{Use: "get <path>", Short: "Download a file's original", GroupID: cliutil.GroupRead,
		Long: `Download a file's original bytes at an exact tablet path to a local file or
directory (the current directory by default). The body is verified against
the cloud's hash, written to a temporary sibling and renamed into place, so
an existing destination is replaced only by a complete file. Nothing is
changed in the cloud and no confirmation is asked. Standard output reports
cached or downloaded; -o - writes the bytes there instead and refuses JSON.
Progress goes to standard error on a terminal.`,
		Example: `snctl get DOCUMENT/Document/Paper.pdf -o paper.pdf
snctl get NOTE/Note/Journal.note -o ./downloads/
snctl get DOCUMENT/Document/Paper.pdf -o - > paper.pdf`,
		Args: cliutil.Args(cobra.ExactArgs(1)),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, word string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveDefault
			}
			return complete(env, false)(cmd, args, word)
		},
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			svc, err := cliutil.Need(cmd, env, func(s cliutil.Services) port.Fetch { return s.Fetch })
			if err != nil {
				return err
			}
			if destination == "-" && env.JSON(cmd) {
				return cliutil.Usagef("stdout download cannot be combined with JSON")
			}
			p, done := cliutil.TransferBytes(env.ProgressStreams(cmd), "download")
			defer done()
			status, err := svc.Get(cmd.Context(), args[0], destination, env.Streams.Out, p)
			if err != nil {
				return err
			}
			if destination == "-" {
				return nil
			}
			return success(cmd, env, status)
		}}
	cmd.Flags().StringVarP(&destination, "output", "o", ".", "file or directory to write, or - for stdout")
	return cmd
}

// transferResult is a bulk transfer as a table of items and a record of
// counts, or the whole report as JSON.
func transferResult(r port.TransferReport) output.Result {
	table := output.Table{Columns: []output.Column{{Title: "status"}, {Title: "cloud"}, {Title: "local"},
		{Title: columnSize, Role: output.RoleSize}, {Title: "reason"}}}
	for _, item := range r.Items {
		size := output.Size(item.Size)
		if item.Folder {
			size = output.Text("")
		}
		table.Rows = append(table.Rows, []output.Cell{output.Text(item.Status), output.Path(item.Cloud),
			output.Path(item.Local), size, output.Text(item.Reason)})
	}
	var fields []output.Field
	for _, status := range []string{port.StatusDownloaded, port.StatusCached, port.StatusUploaded, port.StatusRenamed, port.StatusDuplicate,
		port.StatusReplaced, port.StatusCreated,
		port.StatusUpload, port.StatusReplace, port.StatusCreate, port.StatusUnchanged, port.StatusDeleted,
		port.StatusKept, port.StatusSkipped, port.StatusExists, port.StatusFailed} {
		if n, ok := r.Counts[status]; ok {
			fields = append(fields, output.Field{Key: status, Value: output.Count(n)})
		}
	}
	if r.Counts == nil {
		r.Counts = map[string]int{}
	}
	if r.Items == nil {
		r.Items = []port.TransferItem{}
	}
	doc := output.Doc{table}
	if len(fields) > 0 {
		doc = append(doc, output.Record{Fields: fields})
	}
	return output.Result{JSON: r, Doc: doc}
}

// emitTransfer prints what a transfer did before the error it ended with:
// a partial failure still reports every file.
func emitTransfer(cmd *cobra.Command, env *cliutil.Env, r port.TransferReport, err error) error {
	if r.Items == nil {
		return err
	}
	warnUploads(env, r)
	return errors.Join(env.Emit(cmd, transferResult(r)), err)
}

// confirmPlan asks before a bulk change, showing its plan first on a
// terminal: the question alone does not tell a person what will change.
func confirmPlan(cmd *cobra.Command, env *cliutil.Env, question string) func(port.TransferReport) error {
	return func(r port.TransferReport) error {
		if env.Streams.IsTerminal && !cliutil.Yes(cmd) {
			shown := 0
			for _, item := range r.Items {
				if item.Status != port.StatusCreate && item.Status != port.StatusUpload && item.Status != port.StatusReplace {
					continue
				}
				if shown++; shown <= planLines {
					_, _ = fmt.Fprintf(env.Streams.Err, "  %-8s %s\n", item.Status, item.Cloud) //nolint:errcheck // a prompt
				}
			}
			if shown > planLines {
				_, _ = fmt.Fprintf(env.Streams.Err, "  ... and %d more\n", shown-planLines) //nolint:errcheck // a prompt
			}
		}
		return cliutil.Confirm(cmd, env, fmt.Sprintf(question, planSummary(r)))
	}
}

// planSummary counts the changes a plan makes, for its question.
func planSummary(r port.TransferReport) string {
	var parts []string
	var bytes int64
	for _, item := range r.Items {
		if item.Status == port.StatusUpload || item.Status == port.StatusReplace {
			bytes += item.Size
		}
	}
	for _, c := range []struct {
		status, one, many string
	}{{port.StatusUpload, "upload 1 file", "upload %d files"}, {port.StatusReplace, "replace 1 file", "replace %d files"},
		{port.StatusCreate, "create 1 folder", "create %d folders"}} {
		switch n := r.Counts[c.status]; n {
		case 0:
		case 1:
			parts = append(parts, c.one)
		default:
			parts = append(parts, fmt.Sprintf(c.many, n))
		}
	}
	return strings.Join(parts, ", ") + " (" + output.HumanSize(bytes) + ")"
}

func library(cmd *cobra.Command, env *cliutil.Env) (port.Library, error) {
	return cliutil.Need(cmd, env, func(s cliutil.Services) port.Library { return s.Library })
}

func put(env *cliutil.Env) *cobra.Command {
	var to, name string
	var marks bool
	var o port.UploadOptions
	cmd := &cobra.Command{Use: "put <file|->... [destination]", Short: "Upload files to a tablet folder or full path", GroupID: cliutil.GroupWrite,
		Long: `Uploads files into a tablet folder. The destination is --to, or else the
last argument: a folder, or for one file a full path including its new
name. --name renames one file. "-" uploads standard input, which then
carries the file, so it needs --yes or --dry-run.

Each file is its own change: a file that fails is reported on standard
error and the others still go up; the exit status is 1 when any failed and
4 when a file of the same name exists. --replace overwrites such a file. A
server may replace it in place; when it allocates another copy, the
verified replacement is kept and the prior entry recycled. --dry-run
checks every file and the destination and sends nothing; it needs no
--yes. Standard output lists created entries, or the whole transfer report
for a dry run or a renamed or duplicate upload. Writes go only below
NOTE/Note, NOTE/MyStyle and DOCUMENT/Document inside the NOTE and DOCUMENT
categories. Parents are re-checked fresh after confirmation; changes abort
with exit 4 before file writes. Every written name is verified afterward;
a twin or failed listing is exit 6, with no rollback.`,
		Example: heredoc.Doc(`
			snctl put notes.note NOTE/Note --yes
			snctl put a.pdf b.epub --to DOCUMENT/Document --yes
			snctl put draft.pdf DOCUMENT/Document --name Final.pdf --replace --yes
			snctl put *.pdf DOCUMENT/Document/Inbox --dry-run
			cat page.pdf | snctl put - DOCUMENT/Document/page.pdf --yes
		`),
		Args: cliutil.Args(cobra.MinimumNArgs(1)),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, word string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 || cmd.Flags().Changed("to") {
				return nil, cobra.ShellCompDirectiveDefault
			}
			return complete(env, false)(cmd, args, word)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			req := port.PutRequest{Destination: to, Folder: to != "", Name: name}
			sources := args
			if to == "" {
				if len(args) < pairArgs {
					return cliutil.Usagef("put needs a destination: a folder, a full path, or --to")
				}
				sources, req.Destination = args[:len(args)-1], args[len(args)-1]
			}
			for _, s := range sources {
				src := port.PutSource{Path: s}
				if s == "-" {
					if !o.DryRun && !cliutil.Yes(cmd) {
						return cliutil.Usagef("put -: standard input carries the file, so it cannot answer a question; pass --yes")
					}
					src.Stdin = env.Streams.In
				}
				req.Sources = append(req.Sources, src)
			}
			svc, err := library(cmd, env)
			if err != nil {
				return err
			}
			question := "Put into " + req.Destination + ": %s?"
			o.Confirm = confirmPlan(cmd, env, question)
			p, done := cliutil.Transfer(env.ProgressStreams(cmd), "uploading")
			o.Progress = p
			r, err := svc.Put(cmd.Context(), req, o)
			done()
			if o.DryRun || r.Items == nil || r.Counts[port.StatusRenamed] > 0 || r.Counts[port.StatusDuplicate] > 0 {
				return emitTransfer(cmd, env, r, err)
			}
			warnUploads(env, r)
			entries := []port.Entry{}
			for _, item := range r.Items {
				switch {
				case item.Entry != nil:
					entries = append(entries, *item.Entry)
				case item.Status == port.StatusFailed || item.Status == port.StatusExists:
					cliutil.Notef(env.Streams.Err, "put: %s: %s", item.Local, item.Reason)
				}
			}
			entries = port.FoldMarks(entries, marks)
			return errors.Join(emitFiles(cmd, env, entriesResult(entries), len(entries) > 0), err)
		}}
	cmd.Flags().BoolVar(&marks, "marks", false, "show raw .mark entries in the uploaded file listing")
	cmd.Flags().StringVar(&to, "to", "", "the folder to upload into (default: the last argument)")
	cmd.Flags().StringVar(&name, "name", "", "the uploaded file's name (one file only)")
	cmd.Flags().BoolVar(&o.Replace, "replace", false, "overwrite a cloud file of the same name")
	cmd.Flags().BoolVar(&o.DryRun, "dry-run", false, "check the files and the destination; upload nothing")
	cliutil.CompleteFlag(cmd, "to", complete(env, true))
	return cmd
}

func mget(env *cliutil.Env) *cobra.Command {
	var dir string
	var o port.MirrorOptions
	cmd := &cobra.Command{Use: "mget <folder> -o <dir>", Short: "Download a cloud folder, keeping the tree", GroupID: cliutil.GroupRead,
		Long: `Downloads every file below a cloud folder ("/" for everything) into a
local directory, keeping the folder tree. Names are made safe for the
local file system; names that collide, also without regard to case, get
the entry's identifier. Each file is checked against the cloud's hash and
appears whole or not at all, with the cloud's modification time.

.snctl-mget.json in the directory records the files snctl wrote. With
--incremental a file is skipped when the cloud entry has the recorded
content hash and time and the local file still has the recorded size and
time. Changed cloud content replaces local edits. --delete removes the
recorded files the folder no longer holds, and nothing else: files snctl
did not write, and files changed since, stay. It refuses when the folder
looks wrong (empty while the record lists files, or more than 50
deletions, or more than half of the recorded files) unless --force. A file
that fails is reported and the others still come down; Ctrl-C stops and
leaves the record matching the directory. Standard output reports each
item and totals, including partial failures. Nothing is changed in the
cloud and no confirmation is asked.`,
		Example: heredoc.Doc(`
			snctl mget NOTE/Note -o ~/Supernote/Note
			snctl mget / -o ~/supernote --incremental --delete -j 8
		`),
		Args:              cliutil.Args(cobra.ExactArgs(1)),
		ValidArgsFunction: complete(env, true),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := library(cmd, env)
			if err != nil {
				return err
			}
			p, done := cliutil.Transfer(env.ProgressStreams(cmd), "downloading")
			o.Progress = p
			r, err := svc.Mirror(cmd.Context(), args[0], dir, o)
			done()
			return emitTransfer(cmd, env, r, err)
		}}
	cmd.Flags().StringVarP(&dir, "output", "o", "", "directory to write into")
	cmd.Flags().BoolVar(&o.Incremental, "incremental", false, "skip files recorded as unchanged")
	cmd.Flags().BoolVar(&o.Delete, "delete", false, "remove recorded files the folder no longer holds")
	cmd.Flags().BoolVar(&o.Force, "force", false, "carry out deletions the guard refuses")
	cmd.Flags().IntVarP(&o.Jobs, "jobs", "j", 0, "downloads at a time (0: 4)")
	cliutil.CompleteFlag(cmd, "output", cliutil.CompleteDirs)
	_ = cmd.MarkFlagRequired("output") //nolint:errcheck // the flag exists
	return cmd
}

func mput(env *cliutil.Env) *cobra.Command {
	var o port.UploadOptions
	cmd := &cobra.Command{Use: "mput <dir> [folder]", Short: "Upload a local tree into a cloud folder", GroupID: cliutil.GroupWrite,
		Long: `Uploads the files below a local directory into a cloud folder, "/" by
default, creating the subfolders the directory has: with "/" a directory
laid out like the cloud (as mget / writes it) goes back to the same
places. It carries .note, .mark, .pdf and .epub files; hidden files and
folders (snctl's records among them), empty files, links and other types
are skipped with the reason, and so is anything outside NOTE/Note,
NOTE/MyStyle and DOCUMENT/Document inside the NOTE and DOCUMENT
categories.

A file already in the cloud is never overwritten without --replace.
--incremental skips files whose size and content hash (the MD5 of the
whole file, the cloud's own definition) match the cloud's. The plan is
made from one listing and shown before the question; the uploads run under
the account sync lock, so a tablet does not sync halfway through. The
cloud has no create-only upload: a client that ignores the lock can still
write between the listing and an upload. Re-check parents fresh after
confirmation; changes abort with exit 4 before file writes. Verify every
written name after releasing the lock; a twin or failed listing is exit 6,
with no rollback. --dry-run prints the plan and adds no fresh listings. Standard output reports each item and totals, including
partial failures.`,
		Example: heredoc.Doc(`
			snctl mput ~/Papers DOCUMENT/Document/Papers --dry-run
			snctl mput ~/supernote --incremental -j 8 --yes
		`),
		Args: cliutil.Args(cobra.RangeArgs(1, pairArgs)),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, word string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return cliutil.CompleteDirs(cmd, args, word)
			}
			return complete(env, true)(cmd, args, word)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := library(cmd, env)
			if err != nil {
				return err
			}
			folder := "/"
			if len(args) == pairArgs {
				folder = args[1]
			}
			o.Confirm = confirmPlan(cmd, env, "Below "+folder+": %s?")
			p, done := cliutil.Transfer(env.ProgressStreams(cmd), "uploading")
			o.Progress = p
			r, err := svc.Upload(cmd.Context(), args[0], folder, o)
			done()
			return emitTransfer(cmd, env, r, err)
		}}
	cmd.Flags().BoolVar(&o.Incremental, "incremental", false, "skip files the cloud holds with the same size and hash")
	cmd.Flags().BoolVar(&o.Replace, "replace", false, "overwrite cloud files of the same name")
	cmd.Flags().BoolVar(&o.DryRun, "dry-run", false, "print the plan; upload nothing")
	cmd.Flags().IntVarP(&o.Jobs, "jobs", "j", 0, "uploads at a time (0: 4)")
	return cmd
}

func warnUploads(env *cliutil.Env, r port.TransferReport) {
	for _, warning := range r.Warnings {
		cliutil.Notef(env.Streams.Err, "warning: %s", warning)
	}
	for _, item := range r.Items {
		if item.Status == port.StatusRenamed || item.Status == port.StatusDuplicate || item.Status == port.StatusUploaded && item.Reason != "" {
			cliutil.Notef(env.Streams.Err, "warning: %s: %s", item.Cloud, item.Reason)
		}
	}
}

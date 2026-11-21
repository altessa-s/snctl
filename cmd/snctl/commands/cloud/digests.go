// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/port"
)

func digestService(cmd *cobra.Command, env *cliutil.Env) (port.Digests, error) {
	return cliutil.Need(cmd, env, func(s cliutil.Services) port.Digests { return s.Digests })
}

func digests(env *cliutil.Env) *cobra.Command {
	cmd := &cobra.Command{Use: "digests", Short: "Read or change digests, groups, tags and handwriting", GroupID: cliutil.GroupWrite,
		Long: `Read or change cloud digests, their groups and tags, and handwritten
comments. ls and show read without changing anything; handwriting get writes
only locally. Adds, edits, removals and handwriting uploads ask first; --yes
answers. Use full identifiers from the relevant listing. Answers go to
standard output; prompts and progress go to standard error.`,
		Example: "snctl digests groups ls\nsnctl digests digests ls --json\nsnctl digests tags add --name Reading --yes"}
	for _, kind := range []port.DigestKind{port.DigestGroups, port.DigestItems, port.DigestTags} {
		group := &cobra.Command{Use: string(kind), Short: "Read or change " + string(kind),
			Long: "Read or change cloud " + string(kind) + `. ls lists records and show describes one by identifier without
changing anything. add, edit and rm ask before writing; --yes answers.
Answers go to standard output.`,
			Example: "snctl digests " + string(kind) + " ls --json"}
		group.AddCommand(digestList(env, kind, false), digestList(env, kind, true),
			digestWrite(env, kind, false), digestWrite(env, kind, true), digestRemove(env, kind))
		cliutil.GroupReadWrite(group, "ls", "show")
		cmd.AddCommand(group)
	}
	handwriting := &cobra.Command{Use: "handwriting", Short: "Read or replace handwritten comments",
		Long: `Transfer a digest's handwritten comment as original bytes. get writes a
local file or raw standard output and changes nothing in the cloud. put
replaces the cloud comment after asking; --yes answers. The identifier is a
digest identifier from digests ls.`,
		Example: "snctl digests handwriting get 123456789 comment.mark\nsnctl digests handwriting put 123456789 comment.mark --yes"}
	handwriting.AddCommand(handwritingCommand(env, false), handwritingCommand(env, true))
	cliutil.GroupReadWrite(handwriting, "get")
	cmd.AddCommand(handwriting)
	return cmd
}

func digestResult(rows []port.DigestRecord) output.Result {
	if rows == nil {
		rows = []port.DigestRecord{}
	}
	table := output.Table{Columns: []output.Column{{Title: "id"}, {Title: columnName}, {Title: "content"}, {Title: "identifier"}}}
	for _, r := range rows {
		content := ""
		if r.Content != nil {
			content = *r.Content
		}
		table.Rows = append(table.Rows, []output.Cell{output.Text(r.ID), output.Text(r.Name), output.Text(content), output.Text(r.Identifier)})
	}
	return output.Result{JSON: rows, Doc: output.Doc{table}}
}

func digestList(env *cliutil.Env, kind port.DigestKind, show bool) *cobra.Command {
	use, n := "ls", 0
	if show {
		use, n = "show <id>", 1
	}
	return &cobra.Command{Use: use, Short: "Read " + string(kind),
		Long: "Read cloud " + string(kind) + ` without changing anything. ls prints the records; show selects one by
its full identifier and includes comment, group and tags. Standard
output carries the answer. No handwritten bytes are downloaded.`,
		Example: "snctl digests " + string(kind) + " ls --json\nsnctl digests " + string(kind) + " show 123456789",
		Args:    cliutil.Args(cobra.ExactArgs(n)),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := digestService(cmd, env)
			if err != nil {
				return err
			}
			rows, err := svc.DigestList(cmd.Context(), kind)
			if err != nil {
				return err
			}
			if show {
				for _, r := range rows {
					if r.ID == args[0] {
						result := digestResult([]port.DigestRecord{r})
						result.JSON = r
						result.Doc = append(result.Doc, output.Record{Fields: []output.Field{
							{Key: "comment", Value: output.Text(optionalText(r.Comment))},
							{Key: "group", Value: output.Text(optionalText(r.Group))},
							{Key: "tags", Value: output.Text(strings.Join(r.Tags, ", "))},
						}})
						return env.Emit(cmd, result)
					}
				}
				return port.ErrNotFound
			}
			return env.Emit(cmd, digestResult(rows))
		}}
}

func digestWrite(env *cliutil.Env, kind port.DigestKind, edit bool) *cobra.Command {
	var name, content, comment, group string
	var tags []string
	use := "add"
	if edit {
		use = "edit <id>"
	}
	n := 0
	if edit {
		n = 1
	}
	cmd := &cobra.Command{Use: use, Short: "Write " + string(kind),
		Long: "Create or edit cloud " + string(kind) + `. Groups and tags require --name; digest items require at least one of
--content, --comment, --group or --tag. edit takes a full identifier.
Ask before writing; --yes answers. Standard output reports the
identifier and supplied name or content.`,
		Args: cliutil.Args(cobra.ExactArgs(n)),
		RunE: func(cmd *cobra.Command, args []string) error {
			e := port.DigestEdit{Name: name}
			if cmd.Flags().Changed("content") {
				e.Content = &content
			}
			if cmd.Flags().Changed("comment") {
				e.Comment = &comment
			}
			if cmd.Flags().Changed("group") {
				e.Group = &group
			}
			if cmd.Flags().Changed("tag") {
				e.Tags = &tags
			}
			if kind != port.DigestItems && name == "" {
				return cliutil.Usagef("--name is required")
			}
			if kind == port.DigestItems && e.Content == nil && e.Comment == nil && e.Group == nil && e.Tags == nil {
				return cliutil.Usagef("provide a digest field to write")
			}
			svc, err := digestService(cmd, env)
			if err != nil {
				return err
			}
			if err = cliutil.Confirm(cmd, env, "Write "+string(kind)+" data?"); err != nil {
				return err
			}
			id := ""
			if edit {
				id = args[0]
				err = svc.DigestEdit(cmd.Context(), kind, id, e)
			} else {
				id, err = svc.DigestAdd(cmd.Context(), kind, e)
			}
			if err != nil {
				return err
			}
			return env.Emit(cmd, digestResult([]port.DigestRecord{{ID: id, Name: name, Content: e.Content}}))
		}}
	exampleFields := " --name Reading"
	if kind == port.DigestItems {
		exampleFields = " --content \"A passage to revisit\""
	}
	cmd.Example = "snctl digests " + string(kind) + " " + strings.Fields(use)[0]
	if edit {
		cmd.Example += " 123456789"
	}
	cmd.Example += exampleFields + " --yes"
	if kind != port.DigestItems {
		cmd.Flags().StringVar(&name, "name", "", "group or tag name")
	}
	if kind != port.DigestTags {
		cmd.Flags().StringVar(&comment, "comment", "", "comment or group description")
	}
	if kind == port.DigestItems {
		cmd.Flags().StringVar(&content, "content", "", "passage text")
		cmd.Flags().StringVar(&group, "group", "", "group unique identifier")
		cmd.Flags().StringArrayVar(&tags, "tag", nil, "tag name (repeatable)")
	}
	return cmd
}

func digestRemove(env *cliutil.Env, kind port.DigestKind) *cobra.Command {
	return &cobra.Command{Use: "rm <id>", Short: "Remove " + string(kind),
		Long: "Remove one cloud record from " + string(kind) + ` by the full
identifier shown by its listing. Ask before removing it;
--yes answers. Standard output reports that it was removed.`,
		Example: "snctl digests " + string(kind) + " rm 123456789 --yes",
		Args:    cliutil.Args(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := digestService(cmd, env)
			if err != nil {
				return err
			}
			if err = cliutil.Confirm(cmd, env, "Remove digest data?"); err != nil {
				return err
			}
			if err = svc.DigestRemove(cmd.Context(), kind, args[0]); err != nil {
				return err
			}
			return success(cmd, env, "removed")
		}}
}

func handwritingCommand(env *cliutil.Env, upload bool) *cobra.Command {
	use := "get <id> <file|->"
	if upload {
		use = "put <id> <file>"
	}
	description := `Download a digest's handwritten comment to a local file, or use - for
raw bytes on standard output. No cloud data is changed and no
confirmation is asked. A file download reports downloaded on standard
output; raw output refuses JSON.`
	example := "snctl digests handwriting get 123456789 comment.mark\nsnctl digests handwriting get 123456789 - > comment.mark"
	if upload {
		description = `Replace a digest's handwritten comment with the bytes of a local file.
Ask before changing the cloud; --yes answers. Standard output reports
uploaded; progress goes to standard error on a terminal.`
		example = "snctl digests handwriting put 123456789 comment.mark --yes"
	}
	return &cobra.Command{Use: use, Short: "Transfer a digest's handwritten comment", Long: description, Example: example,
		Args: cliutil.Args(cobra.ExactArgs(pairArgs)),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			svc, err := digestService(cmd, env)
			if err != nil {
				return err
			}
			p, done := cliutil.Progress(env.ProgressStreams(cmd), "handwriting")
			defer done()
			if upload {
				if err = cliutil.Confirm(cmd, env, "Replace handwritten comment?"); err != nil {
					return err
				}
				if err = svc.HandwritingPut(cmd.Context(), args[0], args[1], p); err != nil {
					return err
				}
				return success(cmd, env, "uploaded")
			}
			if args[1] == "-" && env.JSON(cmd) {
				return cliutil.Usagef("stdout download cannot be combined with JSON")
			}
			disk, err := local(cmd, env)
			if err != nil {
				return err
			}
			file, err := svc.HandwritingGet(cmd.Context(), args[0], p)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, disk.Remove(file)) }()
			if err = disk.Save(file, args[1], "comment.mark", env.Streams.Out); err != nil {
				return err
			}
			if args[1] == "-" {
				return nil
			}
			return success(cmd, env, "downloaded")
		}}
}

func optionalText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

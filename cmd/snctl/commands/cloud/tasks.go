// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/port"
)

func tasksService(cmd *cobra.Command, env *cliutil.Env) (port.Tasks, error) {
	return cliutil.Need(cmd, env, func(s cliutil.Services) port.Tasks { return s.Tasks })
}

func tasks(env *cliutil.Env) *cobra.Command {
	cmd := &cobra.Command{Use: "tasks", Short: "Read or change to-do lists and tasks", GroupID: cliutil.GroupWrite,
		Long: `Read or change the selected profile's cloud to-do lists and tasks. ls reads
without changing anything; lists manages the collections. Every add, edit,
completion change or removal asks first; --yes answers. Task and list
identifiers come from their listings. Answers go to standard output; prompts
go to standard error.`,
		Example: "snctl tasks lists ls\nsnctl tasks ls\nsnctl tasks add \"Read the paper\" --yes"}
	lists := &cobra.Command{Use: "lists", Short: "Read or change to-do lists",
		Long: `List the cloud's to-do lists, or create, rename and remove them by
identifier. ls reads without a write; all changes ask first and --yes
answers. Answers go to standard output. List identifiers are distinct from
task identifiers.`,
		Example: "snctl tasks lists ls\nsnctl tasks lists add Reading --yes\nsnctl tasks lists rename 123456789 Books --yes"}
	lists.AddCommand(&cobra.Command{Use: "ls", Short: "List to-do lists",
		Long: `List cloud to-do list identifiers and titles on standard output without
changing anything. Use a listed identifier with tasks ls, tasks add --list
or a list change.`,
		Example: "snctl tasks lists ls\nsnctl tasks lists ls --json",
		Args:    cliutil.Args(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := tasksService(cmd, env)
			if err != nil {
				return err
			}
			rows, err := svc.Lists(cmd.Context())
			if err != nil {
				return err
			}
			table := output.Table{Columns: []output.Column{{Title: "id"}, {Title: "title"}}}
			for _, r := range rows {
				table.Rows = append(table.Rows, []output.Cell{output.Text(r.ID), output.Text(r.Title)})
			}
			if rows == nil {
				rows = []port.TaskList{}
			}
			return env.Emit(cmd, output.Result{JSON: rows, Doc: output.Doc{table}})
		}})
	lists.AddCommand(taskMutation(env, "add <title>", 1, func(ctx context.Context, s port.Tasks, args []string) error {
		_, err := s.AddList(ctx, args[0])
		return err
	}))
	lists.AddCommand(taskMutation(env, "rename <id> <title>", pairArgs, func(ctx context.Context, s port.Tasks, args []string) error {
		return s.RenameList(ctx, args[0], args[1])
	}))
	lists.AddCommand(taskMutation(env, "rm <id>", 1, func(ctx context.Context, s port.Tasks, args []string) error { return s.RemoveList(ctx, args[0]) }))
	for _, change := range lists.Commands() {
		switch change.Name() {
		case "add":
			change.Short = "Create a to-do list"
			change.Long = `Create a cloud to-do list with the supplied title. Ask before creating
it; --yes answers. Standard output reports that the data was updated;
lists ls shows the assigned identifier.`
			change.Example = "snctl tasks lists add Reading --yes"
		case "rename":
			change.Short = "Rename a to-do list"
			change.Long = `Rename a cloud to-do list by the full identifier shown by lists ls.
Ask before changing it; --yes answers. Standard output reports that
the data was updated.`
			change.Example = "snctl tasks lists rename 123456789 Books --yes"
		case "rm":
			change.Short = "Remove a to-do list"
			change.Long = `Remove a cloud to-do list by the full identifier shown by lists ls.
Ask before removing it; --yes answers. Standard output reports that
the data was updated.`
			change.Example = "snctl tasks lists rm 123456789 --yes"
		}
	}
	cliutil.GroupReadWrite(lists, "ls")
	cmd.AddCommand(lists, taskList(env), taskAdd(env), taskEdit(env), taskDone(env, true), taskDone(env, false),
		taskMutation(env, "rm <id>", 1, func(ctx context.Context, s port.Tasks, args []string) error { return s.RemoveTask(ctx, args[0]) }))
	cliutil.GroupReadWrite(cmd, "ls")
	lists.GroupID = cliutil.GroupMixed
	cmd.AddGroup(&cobra.Group{ID: cliutil.GroupMixed, Title: "Lists — ls reads; changes ask first:"})
	return cmd
}

func taskMutation(env *cliutil.Env, use string, n int, run func(context.Context, port.Tasks, []string) error) *cobra.Command {
	return &cobra.Command{Use: use, Short: "Remove a task",
		Long: `Remove one cloud task by the full identifier shown by tasks ls. Ask before
deleting it; --yes answers. Standard output reports that the data was
updated.`,
		Example: "snctl tasks rm 123456789 --yes",
		Args:    cliutil.Args(cobra.ExactArgs(n)),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := tasksService(cmd, env)
			if err != nil {
				return err
			}
			question := "Change the selected to-do data?"
			if cmd.Name() == "rm" {
				question = "Delete the selected to-do data?"
			}
			if err = cliutil.Confirm(cmd, env, question); err != nil {
				return err
			}
			if err = run(cmd.Context(), svc, args); err != nil {
				return err
			}
			return success(cmd, env, "updated")
		}}
}

func taskResult(rows []port.Task) output.Result {
	if rows == nil {
		rows = []port.Task{}
	}
	table := output.Table{Columns: []output.Column{{Title: "id"}, {Title: "title"}, {Title: "done"}}}
	for _, r := range rows {
		table.Rows = append(table.Rows, []output.Cell{output.Text(r.ID), output.Text(r.Title), output.Bool(r.Done)})
	}
	return output.Result{JSON: rows, Doc: output.Doc{table}}
}

func taskList(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{Use: "ls [list-id]", Short: "List tasks",
		Long: `List cloud tasks, optionally within one to-do list by its identifier.
Standard output shows task identifier, title and completion; JSON includes
the available task fields. Nothing is changed.`,
		Example: "snctl tasks ls\nsnctl tasks ls 123456789 --json",
		Args:    cliutil.Args(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := tasksService(cmd, env)
			if err != nil {
				return err
			}
			list := ""
			if len(args) > 0 {
				list = args[0]
			}
			rows, err := svc.Tasks(cmd.Context(), list)
			if err != nil {
				return err
			}
			return env.Emit(cmd, taskResult(rows))
		}}
}

func taskAdd(env *cliutil.Env) *cobra.Command {
	var list, detail, due string
	cmd := &cobra.Command{Use: "add <title>", Short: "Create a task",
		Long: `Create a cloud task with a title and optional list, detail and RFC3339 due
time. Ask before creating it; --yes answers. Standard output describes the
created task, including its identifier and completion state.`,
		Example: "snctl tasks add \"Read the paper\" --yes\nsnctl tasks add \"Review notes\" --list 123456789 --due 2026-12-01T09:00:00Z --yes",
		Args:    cliutil.Args(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := tasksService(cmd, env)
			if err != nil {
				return err
			}
			t := port.Task{Title: args[0]}
			if list != "" {
				t.ListID = &list
			}
			if cmd.Flags().Changed("detail") {
				t.Detail = &detail
			}
			if due != "" {
				value, parseErr := time.Parse(time.RFC3339, due)
				if parseErr != nil {
					return cliutil.Usagef("due must be RFC3339")
				}
				t.Due = &value
			}
			if err = cliutil.Confirm(cmd, env, "Create the task?"); err != nil {
				return err
			}
			t, err = svc.AddTask(cmd.Context(), t)
			if err != nil {
				return err
			}
			return env.Emit(cmd, taskResult([]port.Task{t}))
		}}
	cmd.Flags().StringVar(&list, "list", "", "list identifier")
	cmd.Flags().StringVar(&detail, "detail", "", "task detail")
	cmd.Flags().StringVar(&due, "due", "", "due date in RFC3339")
	return cmd
}

func taskEdit(env *cliutil.Env) *cobra.Command {
	var title, detail string
	cmd := &cobra.Command{Use: "edit <id>", Short: "Edit explicitly provided task fields",
		Long: `Change only the --title and --detail fields explicitly supplied for a cloud
task. At least one is required; an empty detail clears it. Ask before
changing the task; --yes answers. Standard output describes the updated
task.`,
		Example: "snctl tasks edit 123456789 --title \"Read chapter two\" --yes\nsnctl tasks edit 123456789 --detail \"\" --yes",
		Args:    cliutil.Args(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			edit := port.TaskEdit{}
			if cmd.Flags().Changed("title") {
				edit.Title = &title
			}
			if cmd.Flags().Changed("detail") {
				edit.Detail = &detail
			}
			if edit.Title == nil && edit.Detail == nil {
				return cliutil.Usagef("edit needs --title or --detail")
			}
			return editTask(cmd, env, args[0], edit)
		}}
	cmd.Flags().StringVar(&title, "title", "", "new title")
	cmd.Flags().StringVar(&detail, "detail", "", "new detail")
	return cmd
}

func taskDone(env *cliutil.Env, done bool) *cobra.Command {
	name := "done"
	if !done {
		name = "undone"
	}
	return &cobra.Command{Use: name + " <id>", Short: "Set task completion",
		Long: `Set a cloud task's completion state by its full identifier: done marks it
complete and undone marks it incomplete. Ask before changing it; --yes
answers. Standard output describes the updated task.`,
		Example: "snctl tasks " + name + " 123456789 --yes",
		Args:    cliutil.Args(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return editTask(cmd, env, args[0], port.TaskEdit{Done: &done})
		}}
}

func editTask(cmd *cobra.Command, env *cliutil.Env, id string, edit port.TaskEdit) error {
	svc, err := tasksService(cmd, env)
	if err != nil {
		return err
	}
	if err = cliutil.Confirm(cmd, env, "Change the task?"); err != nil {
		return err
	}
	t, err := svc.EditTask(cmd.Context(), id, edit)
	if err != nil {
		return err
	}
	return env.Emit(cmd, taskResult([]port.Task{t}))
}

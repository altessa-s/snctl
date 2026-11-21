// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"context"

	"github.com/altessa-s/snctl/internal/port"

	sncloud "github.com/altessa-s/sncloud-go"
)

// Lists lists all active to-do lists.
func (c *Client) Lists(ctx context.Context) ([]port.TaskList, error) {
	rows, err := c.tasks.Lists(ctx)
	out := make([]port.TaskList, 0, len(rows))
	for _, r := range rows {
		out = append(out, port.TaskList{ID: r.ID, Title: r.Title})
	}
	return out, c.failure("lists", "", err)
}

// AddList creates a list with a server-assigned identifier.
func (c *Client) AddList(ctx context.Context, title string) (port.TaskList, error) {
	r, err := c.tasks.CreateList(ctx, title, "")
	return port.TaskList{ID: r.ID, Title: r.Title}, c.failure("addlist", "", err)
}

// RenameList changes a list's title.
func (c *Client) RenameList(ctx context.Context, id, title string) error {
	return c.failure("renamelist", "", c.tasks.RenameList(ctx, id, title))
}

// RemoveList permanently removes a list and its tasks.
func (c *Client) RemoveList(ctx context.Context, id string) error {
	return c.failure("removelist", "", c.tasks.DeleteList(ctx, id))
}

func task(r sncloud.Task) port.Task {
	return port.Task{ID: r.ID, ListID: r.ListID, Title: r.Title, Detail: r.Detail, Done: r.IsCompleted, Due: r.Due}
}

// Tasks lists every task or only those in the requested list.
func (c *Client) Tasks(ctx context.Context, list string) ([]port.Task, error) {
	var rows []sncloud.Task
	var err error
	if list == "" {
		rows, err = c.tasks.All(ctx)
	} else {
		rows, err = c.tasks.InList(ctx, list)
	}
	out := make([]port.Task, 0, len(rows))
	for _, r := range rows {
		out = append(out, task(r))
	}
	return out, c.failure("tasks", "", err)
}

// AddTask creates a task.
func (c *Client) AddTask(ctx context.Context, t port.Task) (port.Task, error) {
	r, err := c.tasks.Create(ctx, sncloud.Task{ListID: t.ListID, Title: t.Title, Detail: t.Detail, Due: t.Due})
	return task(r), c.failure("addtask", "", err)
}

// EditTask reads the full server row before updating, preserving recurrence,
// ordering, reminders, links and fields not exposed by this CLI.
func (c *Client) EditTask(ctx context.Context, id string, edit port.TaskEdit) (port.Task, error) {
	rows, err := c.tasks.All(ctx)
	if err != nil {
		return port.Task{}, c.failure("edittask", "", err)
	}
	for _, r := range rows {
		if r.ID != id {
			continue
		}
		if edit.Title != nil {
			r.Title = *edit.Title
		}
		if edit.Detail != nil {
			r.Detail = edit.Detail
		}
		if edit.Done != nil {
			r.IsCompleted = *edit.Done
			if !*edit.Done {
				r.Completed = nil
			}
		}
		if edit.Due != nil {
			r.Due = edit.Due
		}
		updated, err := c.tasks.Update(ctx, r)
		return task(updated), c.failure("edittask", "", err)
	}
	return port.Task{}, port.ErrNotFound
}

// RemoveTask permanently deletes a task.
func (c *Client) RemoveTask(ctx context.Context, id string) error {
	return c.failure("removetask", "", c.tasks.Delete(ctx, id))
}

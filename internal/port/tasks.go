// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"time"
)

// TaskList describes a to-do list.
type TaskList struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Task describes a to-do item; edits preserve fields not explicitly changed.
type Task struct {
	ID     string     `json:"id"`
	ListID *string    `json:"list_id"`
	Title  string     `json:"title"`
	Detail *string    `json:"detail"`
	Done   bool       `json:"done"`
	Due    *time.Time `json:"due"`
}

// TaskEdit changes only explicitly provided fields.
type TaskEdit struct {
	Title, Detail *string
	Done          *bool
	Due           *time.Time
}

// Tasks manages lists and tasks by identifier.
type Tasks interface {
	Lists(context.Context) ([]TaskList, error)
	AddList(context.Context, string) (TaskList, error)
	RenameList(context.Context, string, string) error
	RemoveList(context.Context, string) error
	Tasks(context.Context, string) ([]Task, error)
	AddTask(context.Context, Task) (Task, error)
	EditTask(context.Context, string, TaskEdit) (Task, error)
	RemoveTask(context.Context, string) error
}

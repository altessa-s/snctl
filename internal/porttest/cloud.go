// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest

import (
	"context"
	"io"
	"iter"
	"strings"

	"github.com/altessa-s/snctl/internal/port"
)

// Cloud is an isolated callback double. It never records login input.
type Cloud struct {
	Failure      error
	FailAt       string
	Calls        []string
	Entries      []port.Entry
	Recycled     []port.Entry
	StatFunc     func(string) (port.Entry, error)
	LoginResult  port.LoginResult
	SavedAccount string
	TaskRows     []port.Task
	DigestRows   []port.DigestRecord
	Changes      []port.Change
	CheckSteps   []port.CheckStep
	Data         string
}

func (f *Cloud) call(name string) error {
	f.Calls = append(f.Calls, name)
	if f.FailAt == "" || f.FailAt == name {
		return f.Failure
	}
	return nil
}

// LogIn implements the session double without retaining credentials.
func (f *Cloud) LogIn(context.Context, string, string, ...int) (port.LoginResult, error) {
	return f.LoginResult, f.call("login")
}

// SendVerificationCode implements the challenge double.
func (f *Cloud) SendVerificationCode(context.Context) error { return f.call("send code") }

// Verify implements the challenge double.
func (f *Cloud) Verify(context.Context, string) error { return f.call("verify") }

// LogOut implements the session double.
func (f *Cloud) LogOut(context.Context) error { return f.call("logout") }

// Account implements the saved account double.
func (f *Cloud) Account(context.Context) (string, bool, error) {
	return f.SavedAccount, f.SavedAccount != "", f.call("account")
}

// Info implements the account summary double.
func (f *Cloud) Info(context.Context) (port.AccountInfo, error) {
	return port.AccountInfo{Account: "reader@example.invalid", User: "Reader", Cloud: "official", Used: 1, Total: 1}, f.call("info")
}

// Close implements the lifecycle double.
func (f *Cloud) Close() error { return f.call("close") }

// List implements the file listing double.
func (f *Cloud) List(context.Context, string, bool) ([]port.Entry, error) {
	return f.Entries, f.call("list")
}

// Stat implements the path lookup double.
func (f *Cloud) Stat(_ context.Context, name string) (port.Entry, error) {
	if err := f.call("stat"); err != nil {
		return port.Entry{}, err
	}
	if f.StatFunc != nil {
		return f.StatFunc(name)
	}
	for _, e := range f.Entries {
		if e.Path == name {
			return e, nil
		}
	}
	return port.Entry{}, port.ErrNotFound
}

// Download implements the download double.
func (f *Cloud) Download(_ context.Context, _ port.Entry, p port.Progress) (string, error) {
	if p != nil {
		p(1, 1)
	}
	return "download", f.call("download")
}

// Upload implements the upload double.
func (f *Cloud) Upload(_ context.Context, _, name string, p port.Progress) (port.Entry, error) {
	if p != nil {
		p(1, 1)
	}
	return port.Entry{ID: "10", Name: "sample.txt", Path: name}, f.call("upload")
}

// Mkdir implements the folder creation double.
func (f *Cloud) Mkdir(_ context.Context, name string) (port.Entry, error) {
	return port.Entry{ID: "20", Name: "folder", Path: name, Folder: true}, f.call("mkdir")
}

// Move implements the relocation double.
func (f *Cloud) Move(context.Context, port.Entry, string) error { return f.call("move") }

// Delete implements the deletion double.
func (f *Cloud) Delete(context.Context, port.Entry, bool) error { return f.call("delete") }

// Trash implements the recycle listing double.
func (f *Cloud) Trash(context.Context) ([]port.Entry, error) {
	entries := f.Entries
	if f.Recycled != nil {
		entries = f.Recycled
	}
	return entries, f.call("trash")
}

// RestoreTarget uses explicit synthetic original paths.
func (f *Cloud) RestoreTarget(_ context.Context, e port.Entry) (port.Entry, error) {
	return e, f.call("restore target")
}

// EmptyTrash implements the explicit empty operation double.
func (f *Cloud) EmptyTrash(context.Context) error { return f.call("empty") }

// Restore implements the restore double.
func (f *Cloud) Restore(_ context.Context, id string) error {
	if err := f.call("restore"); err != nil {
		return err
	}
	for _, e := range f.Recycled {
		if e.ID == id {
			f.Entries = append(f.Entries, e)
		}
	}
	return nil
}

// Stage implements local staging without disk writes.
func (f *Cloud) Stage(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	f.Data = string(b)
	return "staged", f.call("stage")
}

// Open implements a staged-file reader.
func (f *Cloud) Open(string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(f.Data)), f.call("open")
}

// Remove implements local staging cleanup.
func (f *Cloud) Remove(string) error { return f.call("remove local") }

// Save implements local output.
func (f *Cloud) Save(_, destination, _ string, w io.Writer) error {
	if destination == "-" {
		_, _ = io.WriteString(w, "synthetic bytes")
	}
	return f.call("save")
}

// Lists implements the task-list double.
func (f *Cloud) Lists(context.Context) ([]port.TaskList, error) {
	return []port.TaskList{{ID: "list", Title: "Reading"}}, f.call("lists")
}

// AddList implements list creation.
func (f *Cloud) AddList(context.Context, string) (port.TaskList, error) {
	return port.TaskList{ID: "list"}, f.call("add list")
}

// RenameList implements list renaming.
func (f *Cloud) RenameList(context.Context, string, string) error { return f.call("rename list") }

// RemoveList implements list deletion.
func (f *Cloud) RemoveList(context.Context, string) error { return f.call("remove list") }

// Tasks implements task listing.
func (f *Cloud) Tasks(context.Context, string) ([]port.Task, error) {
	return f.TaskRows, f.call("tasks")
}

// AddTask implements task creation.
func (f *Cloud) AddTask(_ context.Context, t port.Task) (port.Task, error) {
	t.ID = "task"
	return t, f.call("add task")
}

// EditTask implements task editing.
func (f *Cloud) EditTask(context.Context, string, port.TaskEdit) (port.Task, error) {
	return port.Task{ID: "task"}, f.call("edit task")
}

// RemoveTask implements task deletion.
func (f *Cloud) RemoveTask(context.Context, string) error { return f.call("remove task") }

// DigestList implements digest listing.
func (f *Cloud) DigestList(context.Context, port.DigestKind) ([]port.DigestRecord, error) {
	return f.DigestRows, f.call("digest list")
}

// DigestAdd implements digest creation.
func (f *Cloud) DigestAdd(context.Context, port.DigestKind, port.DigestEdit) (string, error) {
	return "30", f.call("digest add")
}

// DigestEdit implements digest editing.
func (f *Cloud) DigestEdit(context.Context, port.DigestKind, string, port.DigestEdit) error {
	return f.call("digest edit")
}

// DigestRemove implements digest deletion.
func (f *Cloud) DigestRemove(context.Context, port.DigestKind, string) error {
	return f.call("digest remove")
}

// HandwritingGet implements handwriting download.
func (f *Cloud) HandwritingGet(context.Context, string, port.Progress) (string, error) {
	return "handwriting", f.call("handwriting get")
}

// HandwritingPut implements handwriting upload.
func (f *Cloud) HandwritingPut(context.Context, string, string, port.Progress) error {
	return f.call("handwriting put")
}

// Watch implements a finite change stream.
func (f *Cloud) Watch(context.Context) iter.Seq2[port.Change, error] {
	return func(yield func(port.Change, error) bool) {
		if err := f.call("watch"); err != nil {
			yield(port.Change{}, err)
			return
		}
		for _, c := range f.Changes {
			if !yield(c, nil) {
				return
			}
		}
	}
}

// WebList implements the independent web probe.
func (f *Cloud) WebList(context.Context) error { return f.call("web list") }

// Run implements a scripted check report.
func (f *Cloud) Run(_ context.Context, _ bool, emit func(port.CheckStep) error) error {
	for _, step := range f.CheckSteps {
		if err := emit(step); err != nil {
			return err
		}
	}
	return f.call("check")
}

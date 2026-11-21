// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"context"
	"iter"
	"time"

	sncloud "github.com/altessa-s/sncloud-go"
)

type fakeSessionAPI struct {
	err    error
	calls  []string
	failAt string
	empty  bool
	task   sncloud.Task
}

func (f *fakeSessionAPI) call(name string) error {
	f.calls = append(f.calls, name)
	if f.failAt == "" || f.failAt == name {
		return f.err
	}
	return nil
}
func (f *fakeSessionAPI) LogIn(ctx context.Context, account, password string, country *int) (sncloud.LoginOutcome, error) {
	err := f.call("LogIn")
	return sncloud.LoggedIn, err
}
func (f *fakeSessionAPI) SendVerificationCode(ctx context.Context) error {
	err := f.call("SendVerificationCode")
	return err
}
func (f *fakeSessionAPI) Verify(ctx context.Context, code string) error {
	err := f.call("Verify")
	return err
}
func (f *fakeSessionAPI) LogOut(ctx context.Context) error {
	err := f.call("LogOut")
	return err
}
func (f *fakeSessionAPI) Account() (string, error) {
	err := f.call("Account")
	if f.empty {
		var zero string
		return zero, err
	}
	return "synthetic", err
}
func (f *fakeSessionAPI) Expiry() (*time.Time, error) {
	err := f.call("Expiry")
	if f.empty {
		var zero *time.Time
		return zero, err
	}
	return nil, err
}
func (f *fakeSessionAPI) Close() error {
	err := f.call("Close")
	return err
}

type fakeDeviceAPI struct {
	err    error
	calls  []string
	failAt string
	empty  bool
	task   sncloud.Task
}

func (f *fakeDeviceAPI) call(name string) error {
	f.calls = append(f.calls, name)
	if f.failAt == "" || f.failAt == name {
		return f.err
	}
	return nil
}
func (f *fakeDeviceAPI) List(ctx context.Context, name string, recursive bool) ([]sncloud.DeviceEntry, error) {
	err := f.call("List")
	if f.empty {
		var zero []sncloud.DeviceEntry
		return zero, err
	}
	return []sncloud.DeviceEntry{{ID: 1, Name: "Note", Path: "Note", IsFolder: true}, {ID: 10, Name: "sample.txt", Path: "Note/sample.txt"}}, err
}
func (f *fakeDeviceAPI) EntryAt(ctx context.Context, name string) (*sncloud.DeviceEntry, error) {
	err := f.call("EntryAt")
	if f.empty {
		var zero *sncloud.DeviceEntry
		return zero, err
	}
	return &sncloud.DeviceEntry{ID: 10, Name: "sample.txt", Path: "Note/sample.txt"}, err
}
func (f *fakeDeviceAPI) Download(ctx context.Context, id sncloud.FileID, span *sncloud.ByteRange, p sncloud.Progress) (string, error) {
	err := f.call("Download")
	if f.empty {
		var zero string
		return zero, err
	}
	return "synthetic", err
}
func (f *fakeDeviceAPI) Upload(ctx context.Context, file, name string, p sncloud.Progress) (sncloud.DeviceEntry, error) {
	err := f.call("Upload")
	return sncloud.DeviceEntry{ID: 10, Name: "sample.txt", Path: "Note/sample.txt"}, err
}
func (f *fakeDeviceAPI) CreateFolder(ctx context.Context, name string, autorename bool) (sncloud.DeviceEntry, error) {
	err := f.call("CreateFolder")
	return sncloud.DeviceEntry{ID: 10, Name: "sample.txt", Path: "Note/sample.txt"}, err
}
func (f *fakeDeviceAPI) Move(ctx context.Context, id sncloud.FileID, name string, autorename bool) (*sncloud.DeviceEntry, error) {
	err := f.call("Move")
	if f.empty {
		var zero *sncloud.DeviceEntry
		return zero, err
	}
	return &sncloud.DeviceEntry{ID: 10, Name: "sample.txt", Path: "Note/sample.txt"}, err
}
func (f *fakeDeviceAPI) Delete(ctx context.Context, id sncloud.FileID) error {
	err := f.call("Delete")
	return err
}
func (f *fakeDeviceAPI) WithSyncLock(ctx context.Context, body func(context.Context, bool) error) error {
	if err := f.call("WithSyncLock"); err != nil {
		return err
	}
	return body(ctx, true)
}

type fakeWebAPI struct {
	err    error
	calls  []string
	failAt string
	empty  bool
	task   sncloud.Task
}

func (f *fakeWebAPI) call(name string) error {
	f.calls = append(f.calls, name)
	if f.failAt == "" || f.failAt == name {
		return f.err
	}
	return nil
}
func (f *fakeWebAPI) User(ctx context.Context) (sncloud.User, error) {
	err := f.call("User")
	return sncloud.User{Name: "Reader"}, err
}
func (f *fakeWebAPI) Capacity(ctx context.Context) (sncloud.Capacity, error) {
	err := f.call("Capacity")
	return sncloud.Capacity{Used: 1, Total: 100}, err
}
func (f *fakeWebAPI) List(ctx context.Context, id sncloud.FileID, order sncloud.SortOrder, ascending bool) ([]sncloud.Entry, error) {
	err := f.call("List")
	if f.empty {
		var zero []sncloud.Entry
		return zero, err
	}
	return []sncloud.Entry{{ID: 10}}, err
}
func (f *fakeWebAPI) Delete(ctx context.Context, ids []sncloud.FileID, parent sncloud.FileID) error {
	err := f.call("Delete")
	return err
}
func (f *fakeWebAPI) RecycleBin(ctx context.Context, order sncloud.SortOrder, ascending bool) ([]sncloud.RecycledEntry, error) {
	err := f.call("RecycleBin")
	if f.empty {
		var zero []sncloud.RecycledEntry
		return zero, err
	}
	return []sncloud.RecycledEntry{{ID: 10, Name: "sample.txt"}}, err
}
func (f *fakeWebAPI) EmptyRecycleBin(ctx context.Context) error {
	err := f.call("EmptyRecycleBin")
	return err
}
func (f *fakeWebAPI) Location(context.Context, sncloud.FileID) (sncloud.Location, error) {
	return sncloud.Location{Names: []string{"Note"}, IDs: []sncloud.FileID{1}}, f.call("Location")
}
func (f *fakeWebAPI) Restore(ctx context.Context, ids []sncloud.FileID) error {
	err := f.call("Restore")
	return err
}

type fakeTasksAPI struct {
	err    error
	calls  []string
	failAt string
	empty  bool
	task   sncloud.Task
}

func (f *fakeTasksAPI) call(name string) error {
	f.calls = append(f.calls, name)
	if f.failAt == "" || f.failAt == name {
		return f.err
	}
	return nil
}
func (f *fakeTasksAPI) Lists(ctx context.Context) ([]sncloud.TaskList, error) {
	err := f.call("Lists")
	if f.empty {
		var zero []sncloud.TaskList
		return zero, err
	}
	return []sncloud.TaskList{{ID: "list", Title: "Reading"}}, err
}
func (f *fakeTasksAPI) CreateList(ctx context.Context, title, id string) (sncloud.TaskList, error) {
	err := f.call("CreateList")
	return sncloud.TaskList{ID: "list"}, err
}
func (f *fakeTasksAPI) RenameList(ctx context.Context, id, title string) error {
	err := f.call("RenameList")
	return err
}
func (f *fakeTasksAPI) DeleteList(ctx context.Context, id string) error {
	err := f.call("DeleteList")
	return err
}
func (f *fakeTasksAPI) All(ctx context.Context) ([]sncloud.Task, error) {
	err := f.call("All")
	if f.empty {
		var zero []sncloud.Task
		return zero, err
	}
	return []sncloud.Task{{ID: "task", Title: "Read", Recurrence: new("daily"), Links: new("link"), IsReminderOn: true}}, err
}
func (f *fakeTasksAPI) InList(ctx context.Context, id string) ([]sncloud.Task, error) {
	err := f.call("InList")
	if f.empty {
		var zero []sncloud.Task
		return zero, err
	}
	return []sncloud.Task{{ID: "task", Title: "Read", Recurrence: new("daily"), Links: new("link"), IsReminderOn: true}}, err
}
func (f *fakeTasksAPI) Create(ctx context.Context, task sncloud.Task) (sncloud.Task, error) {
	err := f.call("Create")
	return task, err
}
func (f *fakeTasksAPI) Update(ctx context.Context, task sncloud.Task) (sncloud.Task, error) {
	err := f.call("Update")
	f.task = task
	return task, err
}
func (f *fakeTasksAPI) Delete(ctx context.Context, id string) error {
	err := f.call("Delete")
	return err
}

type fakeDigestsAPI struct {
	err    error
	calls  []string
	failAt string
	empty  bool
	task   sncloud.Task
}

func (f *fakeDigestsAPI) call(name string) error {
	f.calls = append(f.calls, name)
	if f.failAt == "" || f.failAt == name {
		return f.err
	}
	return nil
}
func (f *fakeDigestsAPI) Groups(ctx context.Context) ([]sncloud.DigestGroup, error) {
	err := f.call("Groups")
	if f.empty {
		var zero []sncloud.DigestGroup
		return zero, err
	}
	return []sncloud.DigestGroup{{ID: 30, Name: "Reading"}}, err
}
func (f *fakeDigestsAPI) Tags(ctx context.Context) ([]sncloud.DigestTag, error) {
	err := f.call("Tags")
	if f.empty {
		var zero []sncloud.DigestTag
		return zero, err
	}
	return []sncloud.DigestTag{{ID: 30, Name: "tag"}}, err
}
func (f *fakeDigestsAPI) List(ctx context.Context, group string) ([]sncloud.Digest, error) {
	err := f.call("List")
	if f.empty {
		var zero []sncloud.Digest
		return zero, err
	}
	return []sncloud.Digest{{ID: 30, Content: new("passage"), Tags: []string{"tag"}}}, err
}
func (f *fakeDigestsAPI) CreateGroup(ctx context.Context, name string, description *string, identifier string) (sncloud.DigestID, error) {
	err := f.call("CreateGroup")
	return 30, err
}
func (f *fakeDigestsAPI) CreateTag(ctx context.Context, name string) (sncloud.DigestID, error) {
	err := f.call("CreateTag")
	return 30, err
}
func (f *fakeDigestsAPI) Create(ctx context.Context, draft sncloud.DigestDraft) (sncloud.DigestID, error) {
	err := f.call("Create")
	return 30, err
}
func (f *fakeDigestsAPI) UpdateGroup(ctx context.Context, id sncloud.DigestID, name string, description *string) error {
	err := f.call("UpdateGroup")
	return err
}
func (f *fakeDigestsAPI) RenameTag(ctx context.Context, id sncloud.DigestID, name string) error {
	err := f.call("RenameTag")
	return err
}
func (f *fakeDigestsAPI) Update(ctx context.Context, id sncloud.DigestID, draft sncloud.DigestDraft) error {
	err := f.call("Update")
	return err
}
func (f *fakeDigestsAPI) DeleteGroup(ctx context.Context, id sncloud.DigestID) error {
	err := f.call("DeleteGroup")
	return err
}
func (f *fakeDigestsAPI) DeleteTag(ctx context.Context, id sncloud.DigestID) error {
	err := f.call("DeleteTag")
	return err
}
func (f *fakeDigestsAPI) Delete(ctx context.Context, id sncloud.DigestID) error {
	err := f.call("Delete")
	return err
}
func (f *fakeDigestsAPI) ByID(ctx context.Context, ids []sncloud.DigestID) ([]sncloud.Digest, error) {
	err := f.call("ByID")
	if f.empty {
		var zero []sncloud.Digest
		return zero, err
	}
	return []sncloud.Digest{{ID: 30, Content: new("passage"), Tags: []string{"tag"}}}, err
}
func (f *fakeDigestsAPI) DownloadHandwriting(ctx context.Context, digest sncloud.Digest, p sncloud.Progress) (string, error) {
	err := f.call("DownloadHandwriting")
	if f.empty {
		var zero string
		return zero, err
	}
	return "synthetic", err
}
func (f *fakeDigestsAPI) UploadHandwriting(ctx context.Context, file, name string, p sncloud.Progress) (sncloud.DigestHandwriting, error) {
	err := f.call("UploadHandwriting")
	return sncloud.DigestHandwriting{Name: "comment.mark"}, err
}

type fakeFeedAPI struct {
	err    error
	calls  []string
	failAt string
	empty  bool
	task   sncloud.Task
}

func (f *fakeFeedAPI) call(name string) error {
	f.calls = append(f.calls, name)
	if f.failAt == "" || f.failAt == name {
		return f.err
	}
	return nil
}
func (f *fakeFeedAPI) Watch(ctx context.Context) iter.Seq2[sncloud.Change, error] {
	return func(yield func(sncloud.Change, error) bool) {
		yield(sncloud.Change{Type: sncloud.ChangeConnected}, f.err)
	}
}

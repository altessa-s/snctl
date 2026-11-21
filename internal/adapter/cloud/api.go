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

type sessionAPI interface {
	LogIn(ctx context.Context, account, password string, country *int) (sncloud.LoginOutcome, error)
	SendVerificationCode(ctx context.Context) error
	Verify(ctx context.Context, code string) error
	LogOut(ctx context.Context) error
	Account() (string, error)
	Expiry() (*time.Time, error)
	Close() error
}
type deviceAPI interface {
	List(ctx context.Context, name string, recursive bool) ([]sncloud.DeviceEntry, error)
	EntryAt(ctx context.Context, name string) (*sncloud.DeviceEntry, error)
	Download(ctx context.Context, id sncloud.FileID, span *sncloud.ByteRange, p sncloud.Progress) (string, error)
	Upload(ctx context.Context, file, name string, p sncloud.Progress) (sncloud.DeviceEntry, error)
	CreateFolder(ctx context.Context, name string, autorename bool) (sncloud.DeviceEntry, error)
	Move(ctx context.Context, id sncloud.FileID, name string, autorename bool) (*sncloud.DeviceEntry, error)
	Delete(ctx context.Context, id sncloud.FileID) error
	WithSyncLock(ctx context.Context, body func(context.Context, bool) error) error
}
type webAPI interface {
	User(ctx context.Context) (sncloud.User, error)
	Capacity(ctx context.Context) (sncloud.Capacity, error)
	List(ctx context.Context, id sncloud.FileID, order sncloud.SortOrder, ascending bool) ([]sncloud.Entry, error)
	Location(ctx context.Context, id sncloud.FileID) (sncloud.Location, error)
	Delete(ctx context.Context, ids []sncloud.FileID, parent sncloud.FileID) error
	RecycleBin(ctx context.Context, order sncloud.SortOrder, ascending bool) ([]sncloud.RecycledEntry, error)
	EmptyRecycleBin(ctx context.Context) error
	Restore(ctx context.Context, ids []sncloud.FileID) error
}
type tasksAPI interface {
	Lists(ctx context.Context) ([]sncloud.TaskList, error)
	CreateList(ctx context.Context, title, id string) (sncloud.TaskList, error)
	RenameList(ctx context.Context, id, title string) error
	DeleteList(ctx context.Context, id string) error
	All(ctx context.Context) ([]sncloud.Task, error)
	InList(ctx context.Context, id string) ([]sncloud.Task, error)
	Create(ctx context.Context, task sncloud.Task) (sncloud.Task, error)
	Update(ctx context.Context, task sncloud.Task) (sncloud.Task, error)
	Delete(ctx context.Context, id string) error
}
type digestsAPI interface {
	Groups(ctx context.Context) ([]sncloud.DigestGroup, error)
	Tags(ctx context.Context) ([]sncloud.DigestTag, error)
	List(ctx context.Context, group string) ([]sncloud.Digest, error)
	CreateGroup(ctx context.Context, name string, description *string, identifier string) (sncloud.DigestID, error)
	CreateTag(ctx context.Context, name string) (sncloud.DigestID, error)
	Create(ctx context.Context, draft sncloud.DigestDraft) (sncloud.DigestID, error)
	UpdateGroup(ctx context.Context, id sncloud.DigestID, name string, description *string) error
	RenameTag(ctx context.Context, id sncloud.DigestID, name string) error
	Update(ctx context.Context, id sncloud.DigestID, draft sncloud.DigestDraft) error
	DeleteGroup(ctx context.Context, id sncloud.DigestID) error
	DeleteTag(ctx context.Context, id sncloud.DigestID) error
	Delete(ctx context.Context, id sncloud.DigestID) error
	ByID(ctx context.Context, ids []sncloud.DigestID) ([]sncloud.Digest, error)
	DownloadHandwriting(ctx context.Context, digest sncloud.Digest, p sncloud.Progress) (string, error)
	UploadHandwriting(ctx context.Context, file, name string, p sncloud.Progress) (sncloud.DigestHandwriting, error)
}
type feedAPI interface {
	Watch(ctx context.Context) iter.Seq2[sncloud.Change, error]
}

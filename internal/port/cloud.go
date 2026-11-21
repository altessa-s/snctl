// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"errors"
	"time"
)

// Classified cloud failures contain no server-supplied text.
var (
	ErrAmbiguousUpload = errors.New("cloud operation failed")
	ErrSession         = errors.New("not logged in or session expired; run snctl login")
	ErrConflict        = errors.New("destination already exists")
	ErrNotFound        = errors.New("remote entry not found")
)

// Entry is a tablet-path entry or a recycled identifier.
type Entry struct {
	ParentID    string     `json:"-"`
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Path        string     `json:"path"`
	Folder      bool       `json:"folder"`
	Size        *int64     `json:"size"`
	Hash        string     `json:"hash"`
	Modified    *time.Time `json:"modified"`
	Recognition *bool      `json:"recognition"`
	// Mark is display metadata only; file adapters and transfers retain raw entries.
	Mark *Entry `json:"mark,omitempty"`
}

// Progress receives byte counts; nil disables reporting.
type Progress func(done, total int64)

// Files is the remote file boundary. Download returns a caller-owned temporary
// file. Delete recycles by default; permanent deletion must be explicit.
type Files interface {
	List(context.Context, string, bool) ([]Entry, error)
	Stat(context.Context, string) (Entry, error)
	Download(context.Context, Entry, Progress) (string, error)
	Upload(context.Context, string, string, Progress) (Entry, error)
	Mkdir(context.Context, string) (Entry, error)
	Move(context.Context, Entry, string) error
	Delete(context.Context, Entry, bool) error
	Trash(context.Context) ([]Entry, error)
	EmptyTrash(context.Context) error
	Restore(context.Context, string) error
}

// SyncProgress tracks bytes and files for concurrent transfers.
type SyncProgress struct {
	Bytes, BytesTotal int64
	Files, FilesTotal int
}

// FileRefresh verifies one exact destination after an uncertain upload outcome.
// The adapter updates only that path in the invocation-local listing.
type FileRefresh interface {
	Refresh(context.Context, string) (Entry, error)
}

// FileDownloadSource exposes cache provenance for transfer status reporting.
type FileDownloadSource interface {
	DownloadSource(context.Context, Entry, Progress) (string, bool, error)
}

// FileRetention retains local bytes only after an upload succeeds or verifies.
type FileRetention interface {
	RetainUpload(context.Context, Entry, string)
}

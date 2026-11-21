// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// Failures of the library operations that the command line classifies.
var (
	// ErrArgument is a request the use case refuses before contacting the cloud.
	ErrArgument = errors.New("invalid argument")
	// ErrWriteVerification means a completed write could not be verified safely.
	// It wraps ErrConflict; callers must not retry or roll back automatically.
	ErrWriteVerification = fmt.Errorf("post-write verification failed: %w", ErrConflict)
	// ErrRefused is a guard that stopped a change which looks unintended.
	ErrRefused = errors.New("refused")
	// ErrBusy is the account sync lock held by a tablet or another client.
	ErrBusy = errors.New("another device is syncing this account; try again when it has finished")
)

// FileSnapshot bounds reuse of a complete listing to one operation. The view
// it returns answers List and Stat from that listing and keeps it current
// with the changes made through the view. It must not outlive the invocation.
type FileSnapshot interface {
	Snapshot(context.Context) (Files, error)
}

// FileFreshSnapshot bypasses cached and invocation-local listings. Mutations
// through the returned view keep that independent snapshot current.
type FileFreshSnapshot interface {
	FreshSnapshot(context.Context) (Files, error)
}

// FileWriteBatch shares one re-check and one post-write verification across
// sequential library and upload operations using the callback's context.
type FileWriteBatch interface {
	WithWriteBatch(context.Context, func(context.Context) error) error
}

// FileListing reads live entries independently of an invocation's planning snapshot.
type FileListing interface {
	ListFresh(context.Context, string, bool) ([]Entry, error)
}

// FileParentListing returns the live parent identity and children, so write
// checks also detect a replaced or moved parent folder.
type FileParentListing interface {
	ListParentFresh(context.Context, string) (Entry, []Entry, error)
}

// FileRestoreTarget resolves a recycled entry's original parent by identity.
// Missing or unresolvable parents must fail rather than guess a destination.
type FileRestoreTarget interface {
	RestoreTarget(context.Context, Entry) (Entry, error)
}

// AccountReader reads the account summary, including its capacity.
type AccountReader interface {
	Info(context.Context) (AccountInfo, error)
}

// SyncLock runs a batch of changes under the account's sync lock, the lock a
// tablet takes while it synchronizes. It keeps a tablet from syncing halfway
// through the batch; it does not stop clients that write without it.
type SyncLock interface {
	WithSyncLock(context.Context, func(context.Context) error) error
}

// FolderUsage is the aggregate size of files below one tablet folder.
type FolderUsage struct {
	Path  string `json:"path"`
	Depth int    `json:"depth"`
	Bytes int64  `json:"bytes"`
	Files int    `json:"files"`
}

// UsageReport includes live totals and separately attributed recycled bytes.
type UsageReport struct {
	Folders  []FolderUsage `json:"folders"`
	Recycled int64         `json:"recycled"`
	Used     int64         `json:"used"`
	Capacity int64         `json:"capacity"`
}

// DuplicateGroup groups files whose complete bytes have equal size and hash.
type DuplicateGroup struct {
	Hash  string  `json:"hash"`
	Size  int64   `json:"size"`
	Files []Entry `json:"files"`
}

// LocalFile is one entry below a local directory, by its slash-separated
// path relative to that directory.
type LocalFile struct {
	Rel     string
	Size    int64
	Regular bool
	Dir     bool
}

// FileInfo is what a bulk transfer observes of one local path.
type FileInfo struct {
	Exists, Regular, Dir bool
	Size                 int64
	Modified             time.Time
}

// TreeFiles is the local side of folder transfers: atomic output from
// [NotebookFiles], plus observation of whole directories.
type TreeFiles interface {
	NotebookFiles
	Info(string) (FileInfo, error)
	// Walk lists every entry below a directory without following links.
	Walk(string) ([]LocalFile, error)
	// Hash is the lowercase hexadecimal MD5 of a whole file, the definition
	// of the cloud's content hash.
	Hash(string) (string, error)
}

// Transfer statuses of the files of a bulk operation.
const (
	StatusDownloaded = "downloaded"
	StatusCached     = "cached"
	StatusUploaded   = "uploaded"
	StatusRenamed    = "uploaded_as"
	StatusDuplicate  = "duplicate"
	StatusReplaced   = "replaced"
	StatusUnchanged  = "unchanged"
	StatusDeleted    = "deleted"
	StatusKept       = "kept"
	StatusSkipped    = "skipped"
	StatusExists     = "exists"
	StatusFailed     = "failed"
	StatusCreated    = "created"
	// The statuses of a dry run: what would happen.
	StatusUpload  = "upload"
	StatusReplace = "replace"
	StatusCreate  = "create"
)

// TransferItem is one file or folder of a bulk operation and what became of it.
type TransferItem struct {
	Status string `json:"status"`
	Cloud  string `json:"cloud"`
	Local  string `json:"local"`
	Folder bool   `json:"folder,omitzero"`
	Size   int64  `json:"size"`
	Reason string `json:"reason,omitempty"`
	// Entry is the cloud entry put created or replaced.
	Entry *Entry `json:"-"`
}

// TransferReport is the outcome of mget, mput or put: every item in a stable
// order and the count of each status.
type TransferReport struct {
	Items    []TransferItem `json:"items"`
	Counts   map[string]int `json:"counts"`
	DryRun   bool           `json:"dry_run,omitzero"`
	Warnings []string       `json:"-"`
}

// Count tallies the items by status.
func (r *TransferReport) Count() {
	r.Counts = map[string]int{}
	for _, item := range r.Items {
		r.Counts[item.Status]++
	}
}

// MirrorOptions shapes mget.
type MirrorOptions struct {
	Incremental, Delete, Force bool
	Jobs                       int
	Progress                   func(SyncProgress)
}

// UploadOptions shapes mput and put.
type UploadOptions struct {
	Incremental, Replace, DryRun bool
	Jobs                         int
	Progress                     func(SyncProgress)
	// Confirm sees the plan before anything is written; an error stops the
	// operation unchanged. A dry run never calls it.
	Confirm func(TransferReport) error
}

// PutSource is one file put sends: a local path, or "-" for the bytes of
// Stdin, staged locally first so that a failed upload can be retried.
type PutSource struct {
	Path  string
	Stdin io.Reader
}

// PutRequest is the destination of put. Folder means Destination names a
// folder; otherwise a single source goes to Destination as a full path when
// no folder of that name exists. Name renames a single source.
type PutRequest struct {
	Sources     []PutSource
	Destination string
	Folder      bool
	Name        string
}

// Library is the input boundary of the tree operations and bulk transfers.
type Library interface {
	RestoreWithMarks(context.Context, string, ChangeOptions) ([]FileChange, error)
	MoveWithMarks(context.Context, string, string, bool, ChangeOptions) ([]FileChange, error)
	RemoveWithMarks(context.Context, string, bool, ChangeOptions) ([]FileChange, error)
	Move(context.Context, string, string) error
	Remove(context.Context, Entry, bool) error
	Rename(context.Context, string, string) error
	Mkdir(context.Context, string, bool, ...ChangeOptions) (Entry, error)
	Usage(context.Context, string, int, bool) (UsageReport, error)
	Duplicates(context.Context, string, bool) ([]DuplicateGroup, error)
	Mirror(context.Context, string, string, MirrorOptions) (TransferReport, error)
	Upload(context.Context, string, string, UploadOptions) (TransferReport, error)
	Put(context.Context, PutRequest, UploadOptions) (TransferReport, error)
}

// FileChange is one resolved member of a document operation's plan.
type FileChange struct {
	Entry       Entry  `json:"entry"`
	Destination string `json:"destination,omitempty"`
}

// ChangeOptions confirms the complete plan before any member is changed.
// DryRun returns the plan without confirmation or writes.
type ChangeOptions struct {
	Exact   bool
	DryRun  bool
	Confirm func([]FileChange) error
}

// SyncLockState retains uncertain ownership when an acquisition or release
// response is lost, so later invocations can explain the remote refusal.
type SyncLockState interface {
	Pending() (bool, error)
	Mark() error
	Clear() error
}

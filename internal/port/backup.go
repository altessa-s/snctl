// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"time"
)

// BackupEntry records the original identity and verified stored bytes of a file.
type BackupEntry struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Path    string     `json:"path"`
	Size    int64      `json:"size"`
	MD5     string     `json:"md5"`
	Updated *time.Time `json:"updated"`
	Stored  string     `json:"stored"`
}

// BackupManifest publishes a complete backup generation.
type BackupManifest struct {
	Version  int           `json:"version"`
	Profile  string        `json:"profile"`
	Endpoint string        `json:"endpoint"`
	Created  time.Time     `json:"created"`
	Path     string        `json:"path"`
	Files    []BackupEntry `json:"files"`
}

// BackupOptions binds a backup to a profile and path and controls retention.
type BackupOptions struct {
	Force, Rescope, Prune, All bool
	Jobs                       int
	Progress                   func(SyncProgress)
}

// BackupReport counts the files kept and the former files set aside.
type BackupReport struct {
	Dir       string `json:"dir"`
	Documents int    `json:"documents"`
	Written   int    `json:"written"`
	Skipped   int    `json:"skipped"`
	Moved     int    `json:"moved"`
	Pruned    int    `json:"pruned"`
	Outside   int    `json:"outside"`
}

// RestoreFile is a verified local body, its displayed backup name and exact destination.
type RestoreFile struct {
	Local, Display, Path string
	// Expected pins a replacement to the version recorded by its caller.
	Expected *Entry
}

// BackupUploader applies a complete restore plan through the upload machinery.
type BackupUploader interface {
	RestoreFiles(context.Context, []RestoreFile, UploadOptions) (TransferReport, error)
}

// Backups is the command boundary for local backups and cloud restoration.
type Backups interface {
	Backup(context.Context, string, string, BackupOptions) (BackupReport, error)
	Restore(context.Context, string, string, UploadOptions) (TransferReport, error)
}

// BackupDirectory pins a local directory and holds its advisory lock until Close.
// Names are relative to the directory and cannot traverse links.
type BackupDirectory interface {
	OutputDirectory
}

// BackupStorage opens a backup directory exclusively, creating it for a backup.
type BackupStorage interface {
	OpenBackup(context.Context, string, bool) (BackupDirectory, error)
}

// BackupLocal supplies transfer staging alongside confined backup storage.
type BackupLocal interface {
	BackupStorage
	LocalFiles
}

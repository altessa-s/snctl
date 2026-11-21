// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"io"
	"time"
)

// OutputDirectory confines all names to an opened directory for its lifetime.
// Implementations reject symbolic links in every component, including records.
type OutputDirectory interface {
	io.Closer
	Open(string) (io.ReadCloser, error)
	Info(string) (FileInfo, error)
	MkdirAll(string) error
	Produce(context.Context, string, func(io.Writer) error) error
	Remove(string) error
	SetModTime(string, time.Time) error
}

// OutputSource is the nonsecret identity needed before reusing a local record.
type OutputSource struct {
	Endpoint   string `json:"endpoint"`
	Account    string `json:"account"`
	FolderID   string `json:"folder_id"`
	FolderPath string `json:"folder_path"`
}

// OutputSourceReader reads saved session identity without logging in.
type OutputSourceReader interface {
	OutputSource(context.Context) (OutputSource, error)
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package files shares cloud resolution and verified transfer staging.
package files

import (
	"context"
	"errors"
	"io"
	"path"

	"github.com/altessa-s/snctl/internal/port"
)

// Service keeps downloaded temporary files within the lifetime of one
// operation. Disk and Lock serve the folder transfers; without Lock a batch
// runs without the account sync lock.
type Service struct {
	Recognition port.RecognitionOperation
	Account     port.AccountReader
	Cloud       port.Files
	Local       port.LocalFiles
	Disk        port.TreeFiles
	Lock        port.SyncLock
}

// Resolve finds an entry through canonical tablet paths.
func (s Service) Resolve(ctx context.Context, query string) (port.Entry, error) {
	if path.Clean("/"+query) == "/" {
		return port.Entry{Name: "supernote", Folder: true}, nil
	}
	return s.Cloud.Stat(ctx, query)
}

// Read returns bytes only after the cloud adapter has verified their checksum.
func (s Service) Read(ctx context.Context, query string, p port.Progress) (e port.Entry, data []byte, err error) {
	e, file, _, err := s.download(ctx, query, p)
	if err != nil {
		return e, nil, err
	}
	defer func() { err = errors.Join(err, s.Local.Remove(file)) }()
	r, err := s.Local.Open(file)
	if err != nil {
		return e, nil, err
	}
	defer func() { err = errors.Join(err, r.Close()) }()
	data, err = io.ReadAll(r)
	return e, data, err
}

func (s Service) download(ctx context.Context, query string, p port.Progress) (port.Entry, string, bool, error) {
	e, err := s.Resolve(ctx, query)
	if err != nil {
		return e, "", false, err
	}
	if e.Folder {
		return e, "", false, port.ErrFileRequired
	}
	file, cached, err := download(ctx, s.Cloud, e, p)
	return e, file, cached, err
}

// Get installs a verified file using the local adapter's atomic replacement.
func (s Service) Get(ctx context.Context, query, dest string, out io.Writer, p port.Progress) (status string, err error) {
	e, file, cached, err := s.download(ctx, query, p)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, s.Local.Remove(file)) }()
	if err = ctx.Err(); err != nil {
		return "", err
	}
	status = port.StatusDownloaded
	if cached {
		status = port.StatusCached
	}
	return status, s.Local.Save(file, dest, e.Name, out)
}

func download(ctx context.Context, cloud port.Files, e port.Entry, p port.Progress) (string, bool, error) {
	if source, ok := cloud.(port.FileDownloadSource); ok {
		return source.DownloadSource(ctx, e, p)
	}
	file, err := cloud.Download(ctx, e, p)
	return file, false, err
}

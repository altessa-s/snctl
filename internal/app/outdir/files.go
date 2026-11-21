// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package outdir

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/altessa-s/snctl/internal/port"
)

// Files binds output paths to one open directory while retaining the original
// filesystem for staging downloads. Output methods never fall back to it.
type Files struct {
	port.NotebookFiles
	root port.OutputDirectory
	dir  string
}

// Open confines outputs for the lifetime of the returned handle.
func Open(files port.NotebookFiles, dir string) (*Files, error) {
	root, err := files.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &Files{NotebookFiles: files, root: root, dir: dir}, nil
}

// Close releases the directory handle after workers and record writes finish.
func (f *Files) Close() error { return f.root.Close() }

func (f *Files) relative(name string) (string, error) {
	rel, err := filepath.Rel(f.dir, name)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("output %s is outside its directory", name)
	}
	return rel, nil
}

// Open reads only within the output root, including its record.
func (f *Files) Open(name string) (io.ReadCloser, error) {
	rel, err := f.relative(name)
	if err != nil {
		return nil, err
	}
	return f.root.Open(rel)
}

// Info observes every path component through the confined handle.
func (f *Files) Info(name string) (port.FileInfo, error) {
	rel, err := f.relative(name)
	if err != nil {
		return port.FileInfo{}, err
	}
	return f.root.Info(rel)
}

// MkdirAll creates only below the pinned output directory.
func (f *Files) MkdirAll(name string) error {
	rel, err := f.relative(name)
	if err != nil {
		return err
	}
	return f.root.MkdirAll(rel)
}

// Produce atomically installs a completed output within the root.
func (f *Files) Produce(ctx context.Context, name string, write func(io.Writer) error) error {
	rel, err := f.relative(name)
	if err != nil {
		return err
	}
	return f.root.Produce(ctx, rel, write)
}

// Save copies a staged download into a confined atomic output.
func (f *Files) Save(source, dest, _ string, _ io.Writer) error {
	return f.Copy(context.Background(), source, dest)
}

// Copy installs a staged file while respecting cancellation.
func (f *Files) Copy(ctx context.Context, source, dest string) error {
	r, err := f.NotebookFiles.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	return f.Produce(ctx, dest, func(w io.Writer) error { _, err := io.Copy(w, r); return err })
}

// Remove cannot delete through an ancestor link.
func (f *Files) Remove(name string) error {
	rel, err := f.relative(name)
	if err != nil {
		return err
	}
	return f.root.Remove(rel)
}

// ModTime trusts only regular files observed inside the root.
func (f *Files) ModTime(name string) (time.Time, error) {
	info, err := f.Info(name)
	if err != nil {
		return time.Time{}, err
	}
	if !info.Exists {
		return time.Time{}, fs.ErrNotExist
	}
	if !info.Regular {
		return time.Time{}, fmt.Errorf("skipped: %s is not a regular file", name)
	}
	return info.Modified, nil
}

// SetModTime preserves the cloud timestamp without leaving the output root.
func (f *Files) SetModTime(name string, stamp time.Time) error {
	rel, err := f.relative(name)
	if err != nil {
		return err
	}
	return f.root.SetModTime(rel, stamp)
}

// Destination does not reinterpret a tree entry as an output container.
func (f *Files) Destination(name, _ string) (string, error) {
	info, err := f.Info(name)
	if err == nil && info.Exists && !info.Regular {
		err = fmt.Errorf("skipped: %s is not a regular file", name)
	}
	return name, err
}

// IsDir observes directories within the confined root.
func (f *Files) IsDir(name string) bool { info, err := f.Info(name); return err == nil && info.Dir }

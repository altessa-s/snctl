// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/altessa-s/snctl/internal/port"
)

const (
	outputDirectoryMode = 0700
	outputFileMode      = 0600
)

// OpenRoot pins the output directory so renames cannot redirect later writes.
func (Files) OpenRoot(dir string) (port.OutputDirectory, error) {
	if err := os.MkdirAll(dir, outputDirectoryMode); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &outputDirectory{root: r}, nil
}

type outputDirectory struct{ root *os.Root }

func (d *outputDirectory) Close() error { return d.root.Close() }

// check rejects links even when they point inside the root. Root operations
// independently prevent escape if a component changes after this observation.
func (d *outputDirectory) check(name string) error {
	if !filepath.IsLocal(name) {
		return fmt.Errorf("skipped: %s is not a local path", name)
	}
	parts := strings.Split(filepath.Clean(name), string(filepath.Separator))
	for i := range parts {
		part := filepath.Join(parts[:i+1]...)
		info, err := d.root.Lstat(part)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("skipped: %s cannot be accessed: %w", name, err)
		}
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return fmt.Errorf("skipped: %s is a link", part)
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			return fmt.Errorf("skipped: %s is not a regular file or directory", part)
		}
		if i < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("skipped: %s is not a directory", part)
		}
	}
	return nil
}

func (d *outputDirectory) Open(name string) (io.ReadCloser, error) {
	if err := d.check(name); err != nil {
		return nil, err
	}
	return d.root.Open(name)
}

func (d *outputDirectory) Info(name string) (port.FileInfo, error) {
	if err := d.check(name); err != nil {
		return port.FileInfo{}, err
	}
	fi, err := d.root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return port.FileInfo{}, nil
	}
	if err != nil {
		return port.FileInfo{}, err
	}
	return port.FileInfo{Exists: true, Regular: fi.Mode().IsRegular(), Dir: fi.IsDir(), Size: fi.Size(), Modified: fi.ModTime()}, nil
}

func (d *outputDirectory) MkdirAll(name string) error {
	if err := d.check(name); err != nil {
		return err
	}
	return d.root.MkdirAll(name, outputDirectoryMode)
}

func (d *outputDirectory) Remove(name string) error {
	if err := d.check(name); err != nil {
		return err
	}
	return d.root.Remove(name)
}

func (d *outputDirectory) SetModTime(name string, stamp time.Time) error {
	if err := d.check(name); err != nil {
		return err
	}
	return d.root.Chtimes(name, stamp, stamp)
}

func (d *outputDirectory) Produce(ctx context.Context, name string, write func(io.Writer) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := d.check(name); err != nil {
		return err
	}
	parent, err := d.root.OpenRoot(filepath.Dir(name))
	if err != nil {
		return fmt.Errorf("%s: cannot open output directory", name)
	}
	defer func() { _ = parent.Close() }()
	temp := ".snctl-output-" + rand.Text()
	f, err := parent.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, outputFileMode)
	if err != nil {
		return fmt.Errorf("%s: cannot create output: %w", name, errors.Unwrap(err))
	}
	defer func() { _ = f.Close(); _ = parent.Remove(temp) }()
	if err = errors.Join(write(f), f.Sync(), f.Close()); err != nil {
		return fmt.Errorf("%s: cannot write output", name)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = d.check(name); err != nil {
		return err
	}
	if err = parent.Rename(temp, filepath.Base(name)); err != nil {
		return fmt.Errorf("%s: cannot install output: %w", name, errors.Unwrap(err))
	}
	return nil
}

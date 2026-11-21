// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package localfs owns transfer staging and atomic local file replacement.
package localfs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Files stages uploads in Dir. An empty Dir uses the platform temporary directory.
type Files struct{ Dir string }

// Stage copies stdin into a private temporary file for a replayable upload.
func (f Files) Stage(r io.Reader) (string, error) {
	file, err := os.CreateTemp(f.Dir, "snctl-upload-*")
	if err != nil {
		return "", err
	}
	name := file.Name()
	_, copyErr := io.Copy(file, r)
	err = errors.Join(copyErr, file.Close())
	if err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// Remove removes a caller-owned temporary file.
func (Files) Remove(name string) error { return os.Remove(name) }

// Save copies source to stdout or atomically replaces a file. A destination
// directory is a container, never something to remove. Remote names cannot
// escape it. The temporary sibling keeps replacement on one filesystem.
func (Files) Save(source, destination, name string, stdout io.Writer) error {
	in, err := open(source)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if destination == "-" {
		_, err = io.Copy(stdout, in)
		return err
	}
	info, err := os.Stat(destination)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && info.IsDir() {
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
			return errors.New("unsafe remote filename")
		}
		destination = filepath.Join(destination, name)
	}
	out, err := os.CreateTemp(filepath.Dir(destination), ".snctl-download-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(out.Name()) }()
	_, copyErr := io.Copy(out, in)
	err = errors.Join(copyErr, out.Sync(), out.Close())
	if err != nil {
		return err
	}
	return os.Rename(out.Name(), destination)
}

// Open reads a transfer file.
func (Files) Open(name string) (io.ReadCloser, error) { return open(name) }

func open(name string) (*os.File, error) {
	root, err := os.OpenRoot(filepath.Dir(name))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.Open(filepath.Base(name))
}

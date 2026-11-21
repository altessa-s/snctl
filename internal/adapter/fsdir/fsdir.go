// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package fsdir lists directories the same way on every platform.
package fsdir

import (
	"io/fs"
	"os"
)

// Read lists a directory and refuses anything else. On Windows, ReadDir on a
// regular file can report an empty directory; a missing path still reports
// fs.ErrNotExist.
func Read(path string) ([]os.DirEntry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &fs.PathError{Op: "readdir", Path: path, Err: fs.ErrInvalid}
	}
	return os.ReadDir(path)
}

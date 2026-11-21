//go:build !darwin && !linux

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"errors"
	"io/fs"
	"os"
)

// The mirror lock serializes cooperating writers on these platforms.
func installExclusive(root *os.Root, from, to string) error {
	if _, err := root.Lstat(to); !errors.Is(err, fs.ErrNotExist) {
		if err == nil {
			return fs.ErrExist
		}
		return err
	}
	return root.Rename(from, to)
}

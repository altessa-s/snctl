//go:build darwin || linux

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"os"
	"path/filepath"
)

// Both names are siblings. Opening their parent through Root keeps the native
// rename confined even if an ancestor is moved concurrently.
func installExclusive(root *os.Root, from, to string) error {
	parent, err := root.Open(filepath.Dir(to))
	if err != nil {
		return err
	}
	defer func() { _ = parent.Close() }()
	return renameExclusive(int(parent.Fd()), filepath.Base(from), filepath.Base(to))
}

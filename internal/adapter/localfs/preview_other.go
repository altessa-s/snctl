//go:build !windows

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package localfs

import "os"

// PreviewDir gives the viewer a private directory that survives process exit.
func (f Files) PreviewDir() (string, error) { return os.MkdirTemp(f.Dir, "snctl-preview-*") }

//go:build !windows

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"errors"
	"syscall"
)

func unsupportedLink(err error) bool { return errors.Is(err, syscall.ENOSYS) }

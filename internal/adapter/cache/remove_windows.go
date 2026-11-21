//go:build windows

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cache

import (
	"errors"

	"golang.org/x/sys/windows"
)

// Open bodies remain eligible for eviction when a later operation retries.
func openBodyError(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

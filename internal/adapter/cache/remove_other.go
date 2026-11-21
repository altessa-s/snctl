//go:build !windows

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cache

// Unix can unlink open bodies without disturbing readers.
func openBodyError(error) bool { return false }

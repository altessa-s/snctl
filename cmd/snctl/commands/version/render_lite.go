//go:build norender || !((darwin || linux || windows) && cgo)

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package version

// rendering records the compiled capability without loading services.
const rendering = false

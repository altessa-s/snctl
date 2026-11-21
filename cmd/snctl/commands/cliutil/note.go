// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"fmt"
	"io"
)

// Notef writes commentary to standard error.
func Notef(w io.Writer, format string, args ...any) { _, _ = fmt.Fprintf(w, format+"\n", args...) }

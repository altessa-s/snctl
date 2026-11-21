// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import "github.com/altessa-s/snctl/cmd/snctl/commands/output"

// BudgetCell displays an unlimited budget distinctly from an empty cache.
func BudgetCell(bytes int64) output.Cell {
	if bytes == 0 {
		return output.Text("unlimited")
	}
	return output.Size(bytes)
}

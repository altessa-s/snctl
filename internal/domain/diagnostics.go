// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import "strings"

// MaskDiagnostic suppresses lines containing credential or wire payload markers.
// Fixed server explanations remain readable; unknown sensitive lines are withheld whole.
func MaskDiagnostic(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lower := strings.ToLower(line)
		for _, marker := range []string{"token", "password", "cookie", "authorization", "signature", "://", "request body", "request_body"} {
			if strings.Contains(lower, marker) {
				lines[i] = "[redacted]"
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}

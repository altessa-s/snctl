// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import "strings"

// CleanText removes copy/search-breaking format characters and normalizes CRLF/CR to LF.
// Other Unicode characters, including combining marks and whitespace, are preserved.
func CleanText(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	return strings.Map(func(r rune) rune {
		switch r {
		case '\u00ad', '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff':
			return -1
		}
		return r
	}, s)
}

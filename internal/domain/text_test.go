// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain_test

import (
	"testing"

	"github.com/altessa-s/snctl/internal/domain"
)

func TestCleanText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"co\u00adpy\u200b\u200c\u200d\u2060\ufeff\r\nтекст\rnext", "copy\nтекст\nnext"},
		{"é e\u0301\t  \n", "é e\u0301\t  \n"},
		{"", ""},
	} {
		if got := domain.CleanText(tc.in); got != tc.want {
			t.Errorf("CleanText(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

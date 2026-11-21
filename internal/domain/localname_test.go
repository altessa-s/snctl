// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/altessa-s/snctl/internal/domain"
)

func TestPortableNames(t *testing.T) {
	for _, name := range []string{"CON", "con.note", "PRN.pdf", "Aux", "NUL", "COM1", "com9.pdf", "LPT1", "lpt9.note", "COM¹.note", "com²", "LPT³.note", "CONIN$.note", "conout$", "CON .note"} {
		if got := domain.SafeName(name); got != "_"+name {
			t.Fatalf("%q -> %q", name, got)
		}
	}
	for _, name := range []string{"COM0", "LPT10", "normal", ".trash"} {
		if got := domain.SafeName(name); got != name {
			t.Fatalf("%q -> %q", name, got)
		}
	}
	if domain.SafeName("name. ") != "name" || domain.SafeName("..") != "notebook" || domain.SafeName("a\x7fb") != "a-b" {
		t.Fatal("unsafe names")
	}
	a := domain.SafeName(strings.Repeat("л", 200) + ".pdf")
	b := domain.SafeName(strings.Repeat("л", 199) + "я.pdf")
	if len(a) > 240 || !utf8.ValidString(a) || !strings.HasSuffix(a, ".pdf") || a == b {
		t.Fatal(a, b)
	}
	if got := domain.SafeName("x." + strings.Repeat("y", 300)); len(got) > 240 {
		t.Fatal(got)
	}
}

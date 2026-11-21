// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package port_test

import (
	"testing"

	"github.com/altessa-s/snctl/internal/port"
)

func TestOverlayWarnings(t *testing.T) {
	if got := (port.OverlayDiagnostics{}).Warnings(); len(got) != 0 {
		t.Fatal(got)
	}
	got := (port.OverlayDiagnostics{SheetsDropped: 1, HighlightsDropped: 2, AllAnnotationsDropped: true, NoPairsAgree: true, SignatureFields: 1}).Warnings()
	if len(got) != 4 {
		t.Fatal(got)
	}
}

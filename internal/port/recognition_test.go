// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port_test

import (
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/port"
)

func TestNotebookText(t *testing.T) {
	n := port.Notebook{Pages: []port.NotePage{{Number: 1, Text: []string{"type\u00add\rbox"}, Recognition: "word\u200bs"}, {Number: 3, Text: []string{}}},
		Titles: []port.NoteTitle{{Page: 1, Level: 0, Text: ""}, {Page: 3, Level: 99, Text: "ti\u00adtle"}, {Page: 8, Text: "elsewhere"}}}
	cleaned := n.Clean()
	if n.Pages[0].Text[0] != "type\u00add\rbox" || n.Titles[1].Text != "ti\u00adtle" {
		t.Fatal("clean mutated input")
	}
	if !n.HasRecognition() || !strings.Contains(cleaned.Text(true), "###### title") || !strings.Contains(cleaned.Text(true), "# Title") {
		t.Fatal(cleaned.Text(true))
	}
	if got := cleaned.Text(false); got != "Page 1\ntyped\nbox\nwords\nPage 3\n" {
		t.Fatal(got)
	}
	if cleaned.Pages[1].Text == nil || (port.Notebook{}).Clean().Pages != nil {
		t.Fatal("empty collection shape changed")
	}
	for _, rec := range []string{"", " \r\n\u00ad\u200b\u200c\u200d\u2060\ufeff"} {
		blank := port.Notebook{Header: []port.MetadataEntry{{Key: "FILE_RECOGN_TYPE", Value: "1"}}, Pages: []port.NotePage{{Text: []string{"typed only"}, Recognition: rec}}}
		if blank.HasRecognition() {
			t.Fatal("header/typed/blank recognition qualifies")
		}
	}
}

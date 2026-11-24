//go:build cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package notes

import (
	"errors"
	"testing"

	"github.com/altessa-s/snctl/internal/port"

	snnote "github.com/altessa-s/snnote-go"
)

func TestNotebookParseErrorClasses(t *testing.T) {
	_, err := (Reader{}).Parse([]byte("garbage"))
	if !errors.Is(err, snnote.ErrUnsupportedFormat) || !errors.Is(err, port.ErrNotebookContent) || !errors.Is(err, port.ErrNotebook) {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		cause   error
		content bool
	}{
		{snnote.ErrOutOfMemory, false}, {snnote.ErrInvalidArgument, false},
		{&snnote.Error{Status: snnote.StatusLimit, Message: "parse work budget exceeded"}, true},
		{&snnote.Error{Status: snnote.StatusLimit, Message: "Go model copy allocation budget exceeded"}, false},
	} {
		err = notebookParseError(tc.cause)
		if !errors.Is(err, tc.cause) || !errors.Is(err, port.ErrNotebook) || errors.Is(err, port.ErrNotebookContent) != tc.content {
			t.Errorf("Parse(%v) classified as %v", tc.cause, err)
		}
	}
}

//go:build !cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/cmd/snctl/commands"
	"github.com/altessa-s/snctl/internal/adapter/config"
)

func TestLiteNotebookCommands(t *testing.T) {
	dirs := config.BaseDirs{Config: t.TempDir(), Cache: t.TempDir()}
	s := seams{dirs: &dirs, env: map[string]string{}, streams: func(s *commands.Streams) { s.Getenv = func(string) string { return "" } }}
	for _, name := range []string{"render", "open", "text", "highlights", "note"} {
		var out, diag bytes.Buffer
		code := runWith([]string{name, "absent.note"}, nil, &out, &diag, func(int) { t.Fatal("forced exit") }, s)
		if code != 5 || out.Len() != 0 || !strings.Contains(diag.String(), "this build cannot") {
			t.Fatal(name, code, out.String(), diag.String())
		}
	}
}

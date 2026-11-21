//go:build cgo && norender

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/cmd/snctl/commands"
	"github.com/altessa-s/snctl/internal/adapter/config"
)

func TestAtelierLiteReading(t *testing.T) {
	dirs := config.BaseDirs{Config: t.TempDir(), Cache: t.TempDir()}
	s := seams{dirs: &dirs, env: map[string]string{}, streams: func(s *commands.Streams) { s.Getenv = func(string) string { return "" } }}
	for _, tc := range []struct {
		args         []string
		code         int
		out, warning string
	}{
		{[]string{"note", "../../testdata/notebooks/drawing.spd", "--json"}, 0, `"tiles": 6`, ""},
		{[]string{"highlights", "../../testdata/overlay/Report.pdf.mark", "--json"}, 0, `"text": null`, "highlight text unavailable"},
		{[]string{"render", "../../testdata/notebooks/drawing.spd"}, 5, "", "this build cannot render"},
		{[]string{"open", "../../testdata/notebooks/drawing.spd"}, 5, "", "this build cannot render"},
	} {
		args := append([]string{"--offline"}, tc.args...)
		args[2], _ = filepath.Abs(args[2])
		var out, diag bytes.Buffer
		code := runWith(args, nil, &out, &diag, func(int) { t.Fatal("forced exit") }, s)
		if code != tc.code || !strings.Contains(out.String(), tc.out) || !strings.Contains(diag.String(), tc.warning) {
			t.Fatal(args, code, out.String(), diag.String())
		}
	}
}

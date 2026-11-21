// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/internal/port"
)

func TestProgressPolicyPreservesCommentaryAndPrompts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                            string
		args                            []string
		envFormat                       string
		terminal, localFormat, progress bool
	}{
		{name: "terminal", terminal: true, progress: true},
		{name: "pipe"},
		{name: "quiet", terminal: true, args: []string{"--quiet"}},
		{name: "json", terminal: true, args: []string{"--json"}},
		{name: "format", terminal: true, args: []string{"--format", "json"}},
		{name: "environment", terminal: true, envFormat: "json"},
		{name: "local json", terminal: true, localFormat: true, args: []string{"--format", "json"}},
		{name: "local image", terminal: true, localFormat: true, args: []string{"--format", "png"}, progress: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			env := cliutil.NewEnv(cliutil.Streams{In: strings.NewReader("yes\n"), Out: &out, Err: &errOut,
				IsTerminal: tc.terminal, Getenv: func(key string) string {
					if key == cliutil.FormatEnv {
						return tc.envFormat
					}
					return ""
				}}, cliutil.Fixed(cliutil.Services{}))
			root := &cobra.Command{Use: "snctl"}
			cliutil.AddPersistentFlags(root, env)
			cmd := &cobra.Command{Use: "test"}
			root.AddCommand(cmd)
			if tc.localFormat {
				cmd.Flags().String("format", "pdf", "file format")
			}
			if err := cmd.ParseFlags(tc.args); err != nil {
				t.Fatal(err)
			}
			streams := env.ProgressStreams(cmd)
			// All four progress entry points must follow the same policy.
			p, done := cliutil.Progress(streams, "reading")
			if p != nil {
				p(1, 1)
			}
			done()
			p, done = cliutil.CountProgress(streams, "pages")
			if p != nil {
				p(1, 1)
			}
			done()
			p, done = cliutil.TransferBytes(streams, "download")
			if p != nil {
				p(1, 1)
			}
			done()
			transfer, done := cliutil.Transfer(streams, "upload")
			if transfer != nil {
				transfer(port.SyncProgress{Bytes: 1, BytesTotal: 1, Files: 1, FilesTotal: 1})
			}
			done()
			if got := errOut.Len() > 0; got != tc.progress {
				t.Fatalf("progress %v, want %v: %q", got, tc.progress, errOut.String())
			}
			if env.Streams.IsTerminal != tc.terminal {
				t.Fatal("progress policy changed confirmation streams")
			}
			errOut.Reset()
			if _, err := fmt.Fprintln(env.Streams.Err, "warning: keep this"); err != nil {
				t.Fatal(err)
			}
			if tc.terminal {
				if err := cliutil.Confirm(cmd, env, "Make this change?"); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(errOut.String(), "Make this change?") {
					t.Fatal("confirmation prompt hidden")
				}
			}
			if !strings.Contains(errOut.String(), "warning: keep this") {
				t.Fatal("commentary hidden")
			}
			if out.Len() != 0 {
				t.Fatalf("progress polluted stdout: %q", out.String())
			}
		})
	}
}

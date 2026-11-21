// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package commands_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/cmd/snctl/commands"
	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
)

func TestRoot(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--help"}, {"version"}, {"version", "--full", "--format", "json"}, {"completion", "bash"}, {"completion", "zsh"}, {"completion", "fish"}, {"completion", "powershell"}} {
		var out bytes.Buffer
		err := commands.Run(t.Context(), args, commands.Streams{In: strings.NewReader(""), Out: &out, Err: io.Discard, Getenv: func(string) string { return "" }}, func(context.Context, commands.Request) (commands.Services, func() error, error) {
			t.Fatal("help/version/completion built services")
			return commands.Services{}, nil, nil
		})
		if err != nil || out.Len() == 0 {
			t.Fatalf("%v: %v %q", args, err, out.String())
		}
	}
	for _, name := range []string{"login", "logout", "account", "ls", "tree", "stat", "find", "get", "put", "mkdir", "mv", "rename", "rm", "trash", "restore", "tasks", "digests", "watch"} {
		cmd := commands.New(commands.Services{}, commands.Streams{Out: io.Discard, Err: io.Discard})
		found, _, err := cmd.Find([]string{name})
		if err != nil || found == cmd {
			t.Fatalf("cloud command %s is missing", name)
		}
	}
	sentinel := errors.New("close failed")
	err := commands.Run(t.Context(), []string{"debug", "paths"}, commands.Streams{Out: io.Discard, Err: io.Discard}, func(context.Context, commands.Request) (commands.Services, func() error, error) {
		return commands.Services{}, func() error { return sentinel }, nil
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(cliutil.ErrInterrupted)
	err = commands.Run(ctx, []string{"debug", "paths"}, commands.Streams{Out: io.Discard, Err: io.Discard}, func(context.Context, commands.Request) (commands.Services, func() error, error) {
		return commands.Services{}, nil, context.Canceled
	})
	if commands.ExitCode(err) != 130 {
		t.Fatal(err)
	}
}

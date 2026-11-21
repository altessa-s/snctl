// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package clitest runs commands with fake services and isolated streams.
package clitest

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
)

// Result is what a command line printed and how it ended.
type Result struct {
	Out, Err string
	Code     int
	Error    error
}

// Options shape a run.
type Options struct {
	// Stdin is the input stream's content; Terminal makes the run look like a
	// person at a terminal.
	Stdin    string
	Terminal bool
	// OutTerminal makes the output stream look like a terminal of Width
	// columns that shows color; Env is the whole environment the run sees.
	OutTerminal bool
	Width       int
	Env         map[string]string
	// Factory overrides the fixed services, to test a failing build.
	Factory cliutil.Factory
	// Context is the run's context; nil is a background context.
	Context context.Context //nolint:containedctx // a test option
}

// Exec runs args against a root carrying the persistent flags and the
// commands build returns, with svc behind them.
func Exec(t testing.TB, svc cliutil.Services, opts Options, build func(env *cliutil.Env) []*cobra.Command, args ...string) Result {
	t.Helper()
	var out, errOut bytes.Buffer
	streams := cliutil.Streams{
		In: strings.NewReader(opts.Stdin), Out: &out, Err: &errOut, IsTerminal: opts.Terminal,
		OutTerminal: opts.OutTerminal, OutColor: opts.OutTerminal, Width: opts.Width,
		Getenv: func(key string) string { return opts.Env[key] },
	}
	factory := opts.Factory
	if factory == nil {
		factory = cliutil.Fixed(svc)
	}
	env := cliutil.NewEnv(streams, factory)
	root := &cobra.Command{Use: "snctl", SilenceUsage: true, SilenceErrors: true}
	root.SetIn(streams.In)
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetFlagErrorFunc(cliutil.FlagError)
	cliutil.AddPersistentFlags(root, env)
	for _, c := range build(env) {
		if c.GroupID != "" && !root.ContainsGroup(c.GroupID) {
			root.AddGroup(&cobra.Group{ID: c.GroupID, Title: c.GroupID})
		}
		root.AddCommand(c)
	}
	root.SetArgs(args)
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	err := cliutil.Interrupted(ctx, root.ExecuteContext(ctx))
	if closeErr := env.Close(); err == nil {
		err = closeErr
	}
	return Result{Out: out.String(), Err: errOut.String(), Code: cliutil.ExitCode(err), Error: err}
}

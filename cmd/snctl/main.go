// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// snctl manages local Supernote Cloud profiles and secrets.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/altessa-s/snctl/cmd/snctl/commands"
	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"

	"golang.org/x/term"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Exit))
}

// run is main with its streams and exit injected, so a test can drive it.
// The local services are constructed only when a command needs them.
func run(args []string, in *os.File, out, errOut io.Writer, exit func(int)) int {
	return runWith(args, in, out, errOut, exit, seams{})
}

// runWith is run with the parts of the machine a test replaces.
func runWith(args []string, in *os.File, out, errOut io.Writer, exit func(int), s seams) int {
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	ctx, stop := cliutil.Interrupt(context.Background(), sigs, errOut, exit)
	defer stop()

	streams := commands.Streams{
		In:         in,
		Out:        out,
		Err:        errOut,
		IsTerminal: isTerminal(in) && isTerminal(errOut),
	}
	if f, ok := out.(*os.File); ok && isTerminal(f) {
		streams.OutTerminal = true
		streams.OutColor = enableColor(f)
		if w, _, err := term.GetSize(int(f.Fd())); err == nil { //nolint:gosec // a file descriptor fits an int
			streams.Width = w
		}
	}
	if s.streams != nil {
		s.streams(&streams)
	}
	factory := func(ctx context.Context, req commands.Request) (commands.Services, func() error, error) {
		return build(ctx, req, errOut, s)
	}
	err := commands.Run(ctx, args, streams, factory)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "snctl: %v\n", err) //nolint:errcheck // the exit code still says it
		if commands.ExitCode(err) == cliutil.CodeUsage {
			_, _ = fmt.Fprintln(errOut, "Run 'snctl --help' for usage.") //nolint:errcheck // see above
		}
	}
	return commands.ExitCode(err)
}

// isTerminal reports whether v is a terminal.
func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	return ok && term.IsTerminal(int(f.Fd())) //nolint:gosec // a file descriptor fits an int
}

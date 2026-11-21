// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"context"
	"fmt"
	"io"
	"os"
	"syscall"
)

// InterruptNotice is printed on the first Ctrl-C.
const InterruptNotice = "interrupted — stopping; Ctrl-C again to quit at once"

// TerminateNotice is printed on the first SIGTERM, which a service manager or
// kill sends, not a person at the keyboard.
const TerminateNotice = "terminated — stopping; a second signal quits at once"

// Interrupt makes Ctrl-C cancel rather than kill. The first signal on sigs
// prints [InterruptNotice] to errOut and cancels the returned context with
// [ErrInterrupted], so a download stops and cleans up after itself; the
// second calls exit with [CodeInterrupted]. stop ends the watch.
func Interrupt(
	parent context.Context, sigs <-chan os.Signal, errOut io.Writer, exit func(int),
) (ctx context.Context, stop func()) {
	ctx, cancel := context.WithCancelCause(parent)
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		count := 0
		for {
			select {
			case <-done:
				return
			case sig := <-sigs:
				cause, code := ErrInterrupted, CodeInterrupted
				notice := InterruptNotice
				if sig == syscall.SIGTERM {
					cause, code = ErrTerminated, CodeTerminated
					notice = TerminateNotice
				}
				count++
				if count == 1 {
					_, _ = fmt.Fprintln(errOut, notice) //nolint:errcheck // a notice
					cancel(cause)
					continue
				}
				exit(code)
				return
			}
		}
	}()
	return ctx, func() {
		close(done)
		<-finished
		cancel(nil)
	}
}

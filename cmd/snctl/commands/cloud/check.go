// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/port"
)

func check(env *cliutil.Env) *cobra.Command {
	var write bool
	cmd := &cobra.Command{Use: "check", Short: "Run live read checks; --write tests transfers in a new folder", GroupID: "debug",
		Long: `Check the selected cloud session and live read operations without changing
cloud data. --write itself authorizes a transfer self-test in a unique new
folder under NOTE/Note; no extra --yes is needed. Cleanup touches only
entries created by the self-test. Each measured step goes to standard output
with status, duration and detail; failed steps and cleanup failures make the
command fail.`,
		Example: "snctl check\nsnctl -P private check --json\nsnctl check --write",
		Args:    cliutil.Args(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := cliutil.Need(cmd, env, func(s cliutil.Services) port.Check { return s.Check })
			if err != nil {
				return err
			}
			// One column takes what the terminal has left, so a step's name is
			// not cut to share the line with an empty detail: the detail of a
			// failed or skipped step follows its name.
			stream := env.Stream(cmd, []output.Column{{Title: "status", Role: output.RoleKind},
				{Title: "time", Role: output.RoleCount}, {Title: "step"}})
			err = svc.Run(cmd.Context(), write, func(step port.CheckStep) error {
				name := step.Name
				if step.Detail != "" {
					name += ": " + step.Detail
				}
				return stream.Row(output.Row{JSON: step, Cells: []output.Cell{output.Text(step.Status),
					output.Text(elapsed(time.Duration(step.Duration))), output.Text(name)}})
			})
			return errors.Join(err, stream.End(nil))
		}}
	cmd.Flags().BoolVar(&write, "write", false, "create a unique folder under Note, test transfers, and clean up only created entries")
	return cmd
}

// elapsed is a step's duration as a person reads it: whole milliseconds under
// a second, tenths of a second under a minute, minutes and seconds beyond.
// The exact nanoseconds stay in the JSON answer.
func elapsed(d time.Duration) string {
	switch {
	case d <= 0:
		return "-"
	case d < time.Millisecond:
		return "<1ms"
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		d = d.Round(time.Second)
		minutes := d.Truncate(time.Minute)
		return fmt.Sprintf("%dm%02ds", int(minutes/time.Minute), int((d-minutes)/time.Second))
	}
}

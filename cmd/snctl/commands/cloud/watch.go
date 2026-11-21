// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"path"
	"strings"

	"github.com/spf13/cobra"

	"github.com/altessa-s/go-atlas-core/time/clock"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/port"
)

func watch(env *cliutil.Env) *cobra.Command {
	var program string
	var shell bool
	o := execOptions{grace: execGrace}
	cmd := &cobra.Command{Use: "watch [-- args...]", Short: "Follow announcements and optionally run a program", GroupID: cliutil.GroupRead,
		Long: `Follow the selected account's change announcements until interrupted.
Standard output carries type, kind, identifier and path; --json writes one
compact object per line. Watching changes no cloud data. --exec runs a
program with each debounced batch as JSON on its standard input; program
arguments follow --. Program output and diagnostics go to standard error.
Filters and time limits apply to execution, not the announcement stream. A
program you supply can make its own changes.`,
		Example: "snctl watch --json\nsnctl watch --exec ~/bin/sn-on-change.sh --only file --under /NOTE/Note\nsnctl watch --exec cat -- --number",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && (program == "" || cmd.ArgsLenAtDash() != 0) {
				return cliutil.Usagef("program arguments must follow --exec PROGRAM --")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateWatch(cmd, program, shell, o); err != nil {
				return err
			}
			svc, err := env.Services(cmd)
			if err != nil {
				return err
			}
			if svc.Feed == nil {
				return cliutil.ErrNoService
			}
			var receive func(port.Change)
			if program != "" {
				d := watchDispatcher(cmd, env, svc.Files, program, shell, args, o)
				receive = d.add
				go d.work(cmd.Context())
				defer func() { d.close(); <-d.done }()
			}
			return watchStream(cmd, env, svc.Feed, receive)
		}}
	cmd.Flags().StringVar(&program, "exec", "", "program to run with each batch as JSON on stdin")
	cmd.Flags().BoolVar(&shell, "shell", false, "run --exec through sh -c")
	cmd.Flags().DurationVar(&o.debounce, "debounce", DefaultDebounce, "gather events for this long from the first event")
	cmd.Flags().DurationVar(&o.timeout, "exec-timeout", DefaultExecTimeout, "stop a run after this duration")
	cmd.Flags().DurationVar(&o.repeatWindow, "repeat-window", DefaultRepeatWindow, "suppress identical execution announcements for this long; 0 disables")
	cmd.Flags().StringVar(&o.only, "only", "", "keep only file or folder events")
	cmd.Flags().StringVar(&o.under, "under", "", "keep events at or below this cloud folder")
	return cmd
}

func validateWatch(cmd *cobra.Command, program string, shell bool, o execOptions) error {
	if o.debounce < 0 || o.timeout <= 0 {
		return cliutil.Usagef("--debounce must not be negative and --exec-timeout must be positive")
	}
	if o.repeatWindow < 0 {
		return cliutil.Usagef("--repeat-window must not be negative")
	}
	if o.only != "" && o.only != onlyFile && o.only != onlyFolder {
		return cliutil.Usagef("--only must be file or folder")
	}
	if o.under != "" && !strings.HasPrefix(o.under, "/") {
		return cliutil.Usagef("--under must be an absolute cloud path")
	}
	if program == "" {
		flags := []string{"only", "under", "debounce", "exec-timeout", "repeat-window"}
		for _, flag := range flags {
			if cmd.Flags().Changed(flag) {
				return cliutil.Usagef("execution options require --exec")
			}
		}
		if shell {
			return cliutil.Usagef("--shell requires --exec")
		}
	}
	return nil
}

func watchDispatcher(cmd *cobra.Command, env *cliutil.Env, files port.Files, program string, shell bool, args []string, o execOptions) *dispatcher {
	o.argv = append([]string{program}, args...)
	if shell {
		o.argv = append([]string{"sh", "-c", program}, args...)
	}
	o.under = path.Clean(o.under)
	if o.under == "." {
		o.under = ""
	}
	o.profile = env.EffectiveProfile(cmd)
	now := env.Streams.Now
	if now == nil {
		now = clock.Real().Now
	}
	return newDispatcher(o, execRunner{}, func(ctx context.Context) (map[string]port.Entry, error) {
		if files == nil {
			return nil, cliutil.ErrNoService
		}
		list := files.List
		if fresh, ok := files.(port.FileListing); ok {
			list = fresh.ListFresh
		}
		entries, err := list(ctx, "/", true)
		if err != nil {
			return nil, err
		}
		byID := make(map[string]port.Entry, len(entries))
		for _, e := range entries {
			byID[e.ID] = e
		}
		return byID, nil
	}, &lockedWriter{w: env.Streams.Err}, now)
}

func watchStream(cmd *cobra.Command, env *cliutil.Env, feed port.Feed, receive func(port.Change)) error {
	stream := env.Stream(cmd, []output.Column{{Title: "type"}, {Title: "kind"}, {Title: "id"}, {Title: columnPath}})
	for event, err := range feed.Watch(cmd.Context()) {
		if errors.Is(err, context.Canceled) {
			return cmd.Context().Err()
		}
		if err != nil {
			return err
		}
		// The dispatcher is fed independently of formatting, including JSON lines.
		if receive != nil {
			receive(event)
		}
		if env.JSON(cmd) {
			if err = json.MarshalWrite(env.Streams.Out, event); err != nil {
				return err
			}
			if _, err = io.WriteString(env.Streams.Out, "\n"); err != nil {
				return err
			}
			continue
		}
		row := output.Row{JSON: event, Cells: []output.Cell{output.Text(event.Type), output.Text(event.Kind),
			output.Text(event.ID), output.Path(event.Path)}}
		if err = stream.Row(row); err != nil {
			return err
		}
	}
	return errors.Join(cmd.Context().Err(), stream.End(nil))
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/port"
)

// defaultLogLines is how many lines mirror service logs prints.
const defaultLogLines = 50

func newService(env *cliutil.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:     commandService,
		Short:   "Start every mirror with the session: install, uninstall, status, logs",
		Example: "snctl mirror service install\nsnctl mirror service status\nsnctl mirror service logs -n 50 -f",
		Long: heredoc.Doc(`
			The mirror service is one snctl process, snctl mirror run --service, that
			keeps every mirror of config.toml in step, whatever its profile, and reads
			config.toml again for mirrors added and removed: installing it once is
			enough. It is the user's own, never the system's:
			  macOS    a launchd agent, ~/Library/LaunchAgents/io.altessa.snctl.mirror.plist,
			           started at login and again after a failure
			  Linux    a systemd user unit, ~/.config/systemd/user/snctl-mirror.service,
			           started with the session and again after a failure;
			           loginctl enable-linger keeps it running after logout
			  Windows  a Task Scheduler task, snctl-mirror, started at logon without a
			           console window, and again after a failure
			It writes its log to logs/mirror.log in snctl's configuration directory,
			rotated at 5 MiB, three kept; mirror service logs shows it.
			Status and action reports go to standard output; manager notes go to
			standard error. install starts the authorized cloud mirrors without
			another confirmation; uninstall stops them. status and logs only read.
		`),
	}
	cmd.AddCommand(
		newServiceAction(env, commandInstall, "Install and start the mirror service", install),
		newServiceAction(env, commandUninstall, "Stop and remove the mirror service; library files stay", uninstall),
		newServiceAction(env, "status", "Show whether the mirror service is installed and running", status),
		newLogs(env),
	)
	cmd.AddGroup(
		&cobra.Group{ID: cliutil.GroupRead, Title: cliutil.TitleRead},
		&cobra.Group{ID: cliutil.GroupLocal, Title: "Service settings — install starts authorized mirrors; uninstall stops them:"},
	)
	for _, child := range cmd.Commands() {
		child.GroupID = cliutil.GroupRead
		if child.Name() == commandInstall || child.Name() == commandUninstall {
			child.GroupID = cliutil.GroupLocal
		}
	}
	return cmd
}

func service(cmd *cobra.Command, env *cliutil.Env) (port.MirrorService, error) {
	return cliutil.Need(cmd, env, func(s cliutil.Services) port.MirrorService { return s.MirrorService })
}

func install(ctx context.Context, s port.MirrorService) (port.ServiceInfo, error) {
	return s.Install(ctx)
}
func uninstall(ctx context.Context, s port.MirrorService) (port.ServiceInfo, error) {
	return s.Uninstall(ctx)
}
func status(ctx context.Context, s port.MirrorService) (port.ServiceInfo, error) {
	return s.Status(ctx)
}

func newServiceAction(env *cliutil.Env, use, short string, do func(context.Context, port.MirrorService) (port.ServiceInfo, error)) *cobra.Command {
	description := `Report whether the user's mirror service is installed and running,
with its manager, definition and log paths, on standard output. No
cloud files or service settings are changed.`
	switch use {
	case commandInstall:
		description = `Install and start the current user's mirror service. It runs the
saved, authorized mirrors with the session and writes a rotated log
under snctl's configuration directory. This starts cloud changes
without another prompt. Standard output reports the service state and
paths; manager notes go to standard error.`
	case commandUninstall:
		description = `Stop and remove the current user's mirror service definition without a
confirmation prompt. Library files, local originals and mirror
definitions remain. Standard output reports the service state and
paths; manager notes go to standard error.`
	}
	return &cobra.Command{
		Long:         description,
		Example:      "snctl mirror service " + use,
		Use:          use,
		Short:        short,
		Args:         cliutil.Args(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := service(cmd, env)
			if err != nil {
				return err
			}
			info, err := do(cmd.Context(), s)
			if err != nil {
				return fmt.Errorf("mirror service %s: %w", use, err)
			}
			for _, n := range info.Notes {
				cliutil.Notef(cmd.ErrOrStderr(), "%s", n)
			}
			return env.Emit(cmd, serviceAnswer(info))
		},
	}
}

// serviceView is the service as --json prints it.
type serviceView struct {
	Manager   string   `json:"manager"`
	Name      string   `json:"name"`
	File      string   `json:"file"`
	Installed bool     `json:"installed"`
	Running   bool     `json:"running"`
	Log       string   `json:"log"`
	Notes     []string `json:"notes"`
}

func serviceAnswer(i port.ServiceInfo) output.Result {
	notes := i.Notes
	if notes == nil {
		notes = []string{}
	}
	return output.Fields(serviceView{
		Manager: i.Manager, Name: i.Name, File: i.File, Installed: i.Installed, Running: i.Running, Log: i.Log, Notes: notes,
	},
		output.Field{Key: "manager", Value: output.Text(i.Manager)},
		output.Field{Key: keyName, Value: output.Text(i.Name)},
		output.Field{Key: "file", Value: output.Path(i.File)},
		output.Field{Key: "installed", Value: output.Bool(i.Installed)},
		output.Field{Key: "running", Value: output.Bool(i.Running)},
		output.Field{Key: "log", Value: output.Path(i.Log)})
}

func newLogs(env *cliutil.Env) *cobra.Command {
	var lines int
	var follow bool
	cmd := &cobra.Command{
		Use:     "logs [-n N] [-f]",
		Short:   "Print the mirror service's log",
		Example: "snctl mirror service logs -n 20\nsnctl mirror service logs -f",
		Long: `Print the last lines of the mirror service's log as they were written;
-f prints the lines written after them too, until Ctrl-C. Standard output
carries the log, or an object with lines in JSON without -f. Follow mode
always streams raw lines. Nothing is changed.`,
		Args:         cliutil.Args(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if lines < 1 {
				return cliutil.Usagef("%s: -n is 1 or more", cmd.CommandPath())
			}
			s, err := service(cmd, env)
			if err != nil {
				return err
			}
			if !follow {
				var got []string
				if err = s.Log(cmd.Context(), lines, false, func(l string) { got = append(got, l) }); err != nil {
					return fmt.Errorf("mirror service logs: %w", err)
				}
				text := strings.Join(got, "\n")
				if text != "" {
					text += "\n"
				}
				if got == nil {
					got = []string{}
				}
				return env.Emit(cmd, output.Result{JSON: struct {
					Lines []string `json:"lines"`
				}{got}, Doc: output.Doc{output.Body{Text: text}}})
			}
			out := cmd.OutOrStdout()
			err = s.Log(cmd.Context(), lines, true, func(l string) { _, _ = io.WriteString(out, l+"\n") }) //nolint:errcheck // a closed pipe ends the next write too
			if cmd.Context().Err() != nil {
				return context.Cause(cmd.Context())
			}
			if err != nil {
				return fmt.Errorf("mirror service logs: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&lines, "lines", "n", defaultLogLines, "how many of the last lines")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "print the lines written after them too, until Ctrl-C")

	return cmd
}

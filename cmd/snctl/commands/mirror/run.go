// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/port"
)

// runFlags are the flags of mirror run.
type runFlags struct {
	all, service bool
	opts         port.MirrorRunOptions
	adHoc        port.MirrorSpec
}

func newRun(env *cliutil.Env) *cobra.Command {
	var f runFlags
	cmd := &cobra.Command{
		Use:   "run [name...] [--all] [--watch]",
		Short: "Make a pass of mirrors, or keep them in step",
		Long: `Compare the journal, local files and cloud folder. PDFs and EPUBs move
both ways; local originals are retained. Cloud replacements follow the
server's overwrite behavior. --dry-run changes no content or
journal. --watch listens for changes, falls back to polling and rescans
periodically. --all rereads config.toml every 30 seconds. The mass-
deletion guard refuses more than 50 deletions, more than half of the
tracked files (two at least), or deletions from an empty source; --force-
delete overrides it. --service runs every mirror, watched, writing the
rotated service log. Saved definitions authorize their passes; ad-hoc
--dir/--folder runs ask first unless --yes or --dry-run. Standard output
reports each action and a final pass line; failures can leave completed
actions in place. This is not one atomic cloud transaction.`,
		Example: heredoc.Doc(`
			snctl mirror run papers --dry-run
			snctl mirror run --all
			snctl mirror run --all --watch
			snctl mirror run --dir ~/Inbox --folder /DOCUMENT/Document/Inbox --yes
		`),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			sel, err := f.selection(cmd, args, env)
			if err != nil {
				return err
			}
			m, err := mirrors(cmd, env)
			if err != nil {
				return err
			}
			if sel.AdHoc != nil && !f.opts.DryRun {
				if err = cliutil.Confirm(cmd, env, fmt.Sprintf("Mirror %s and %s, both ways?", sel.AdHoc.Dir, sel.AdHoc.Folder)); err != nil {
					return err
				}
			}
			if f.service {
				closeLog, lerr := toServiceLog(cmd, env)
				if lerr != nil {
					return lerr
				}
				defer closeLog()
			}
			em := env.Stream(cmd, columns)
			err = m.Run(cmd.Context(), sel, f.opts, func(p port.MirrorPass) {
				if p.TextAdded > 0 {
					_, _ = fmt.Fprintf(env.Streams.Err, "Added recognized text to %d current notebooks\n", p.TextAdded)
				}
				emitPass(em, p)
			})
			if endErr := em.End(nil); err == nil {
				err = endErr
			}
			if f.opts.Watch && cmd.Context().Err() != nil {
				return context.Cause(cmd.Context())
			}
			if err != nil {
				return fmt.Errorf("mirror run: %w", err)
			}
			return nil
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&f.all, "all", false, "every mirror of config.toml")
	fl.BoolVar(&f.opts.Watch, "watch", false, "keep the mirrors in step until Ctrl-C")
	fl.BoolVar(&f.opts.DryRun, "dry-run", false, "work the passes out and change nothing")
	fl.BoolVar(&f.opts.ForceDelete, "force-delete", false, "carry out the deletions the mass-deletion guard refuses")
	fl.StringVar(&f.adHoc.Dir, "dir", "", "the directory of a mirror config.toml does not define (with --folder)")
	fl.StringVar(&f.adHoc.Folder, "folder", "", "the cloud folder of a mirror config.toml does not define (with --dir)")
	fl.BoolVar(&f.adHoc.PullNew, "pull-new", false, "with --dir: draw the documents created on the tablet too")
	fl.BoolVar(&f.adHoc.NoDelete, "no-delete", false, "with --dir: carry no deletion from one side to the other")
	fl.StringArray("service-env", nil, "environment saved by the service definition")
	_ = fl.MarkHidden("service-env")
	fl.BoolVar(&f.service, commandService, false, "run as the mirror service: --all --watch, into the service's log")
	fl.DurationVar(&f.opts.Poll, "poll", 0, "with --watch: how often the directory is looked at when the file watcher cannot watch it (0: 3s)")
	fl.DurationVar(&f.opts.Debounce, "debounce", 0, "with --watch: how long changes must stop before a pass (0: 2s)")
	fl.DurationVar(&f.opts.Rescan, "rescan", 0, "with --watch: how often a full pass is made in any case (0: 15m)")
	return cmd
}

// selection checks the flags and names the mirrors the run takes.
func (f *runFlags) selection(cmd *cobra.Command, args []string, env *cliutil.Env) (port.MirrorSelection, error) {
	path := cmd.CommandPath()
	if f.service {
		if len(args) > 0 || f.adHoc.Dir != "" || f.adHoc.Folder != "" || f.opts.DryRun {
			return port.MirrorSelection{}, cliutil.Usagef("%s: --service runs every mirror, watched, for real", path)
		}
		f.all, f.opts.Watch = true, true
	}
	for name, d := range map[string]time.Duration{"poll": f.opts.Poll, "debounce": f.opts.Debounce, "rescan": f.opts.Rescan} {
		if d < 0 {
			return port.MirrorSelection{}, cliutil.Usagef("%s: --%s is a duration of zero or more", path, name)
		}
		if cmd.Flags().Changed(name) && !f.opts.Watch {
			return port.MirrorSelection{}, cliutil.Usagef("%s: --%s goes with --watch", path, name)
		}
	}
	adHoc := f.adHoc.Dir != "" || f.adHoc.Folder != ""
	if !adHoc && (f.adHoc.PullNew || f.adHoc.NoDelete) {
		return port.MirrorSelection{}, cliutil.Usagef("%s: --pull-new and --no-delete go with --dir; a mirror of config.toml has its own", path)
	}
	switch {
	case adHoc && (len(args) > 0 || f.all):
		return port.MirrorSelection{}, cliutil.Usagef("%s: --dir and --folder make a pass of one mirror config.toml does not define: no name, no --all", path)
	case adHoc && (f.adHoc.Dir == "" || f.adHoc.Folder == ""):
		return port.MirrorSelection{}, cliutil.Usagef("%s: --dir and --folder go together", path)
	case adHoc:
		dir, err := filepath.Abs(f.adHoc.Dir)
		if err != nil {
			return port.MirrorSelection{}, err
		}
		spec := f.adHoc
		spec.Dir, spec.Profile = dir, selectedProfile(cmd, env)
		return port.MirrorSelection{AdHoc: &spec}, nil
	case f.all && len(args) > 0:
		return port.MirrorSelection{}, cliutil.Usagef("%s: name mirrors or pass --all, not both", path)
	case !f.all && len(args) == 0:
		return port.MirrorSelection{}, cliutil.Usagef("%s: name the mirrors to run, or pass --all", path)
	}
	return port.MirrorSelection{Names: args, All: f.all}, nil
}

// toServiceLog points the command's output and error streams at the
// service's log, as plain lines.
func toServiceLog(cmd *cobra.Command, env *cliutil.Env) (func(), error) {
	svc, err := cliutil.Need(cmd, env, func(s cliutil.Services) port.MirrorService { return s.MirrorService })
	if err != nil {
		return nil, err
	}
	log, err := svc.OpenLog()
	if err != nil {
		return nil, fmt.Errorf("mirror run: open the service's log: %w", err)
	}
	if f := cmd.Flags().Lookup(cliutil.FlagFormat); f != nil {
		_ = f.Value.Set(output.FormatPlain) //nolint:errcheck // a known format
		f.Changed = true
	}
	cmd.SetOut(log)
	cmd.SetErr(log)
	return func() { _ = log.Close() }, nil
}

// columns are mirror run's: the time, the mirror, the action, the local
// path, where it went, and what more there is to say.
var columns = []output.Column{ //nolint:gochecknoglobals // a fixed layout
	{Title: "time", Role: output.RoleTime},
	{Title: "mirror", Role: output.RoleName},
	{Title: "action", Role: output.RoleKind},
	{Title: "path", Role: output.RolePath},
	{Title: "to", Role: output.RolePath},
	{Title: "detail"},
}

// actionView is one line of mirror run as --json prints it: an action, or,
// with action "pass", the end of a pass.
type actionView struct {
	Warnings []string `json:"warnings,omitempty"`
	Time     string   `json:"time"`
	Mirror   string   `json:"mirror"`
	Action   string   `json:"action"`
	Path     string   `json:"path"`
	To       string   `json:"to"`
	Document string   `json:"document"`
	Detail   string   `json:"detail"`
	Error    string   `json:"error"`
	DryRun   bool     `json:"dry_run"`
	Pending  int      `json:"pending,omitzero"`
}

// emitPass writes a pass: a line per action, then one that ends the pass.
func emitPass(em output.Emitter, p port.MirrorPass) {
	name := p.Mirror
	if name == "" {
		name = p.Dir
	}
	row := func(v actionView, when time.Time) {
		v.Time, v.Mirror, v.DryRun = stamp(when), name, p.DryRun
		detail := v.Detail
		if len(v.Warnings) != 0 {
			detail += "warning: " + strings.Join(v.Warnings, "; ")
		}
		if v.Error != "" {
			detail = "failed: " + v.Error
		}
		_ = em.Row(output.Row{JSON: v, Cells: []output.Cell{ //nolint:errcheck // a closed pipe ends the next write too
			output.Time(when), output.Text(name), output.Text(v.Action), output.Path(v.Path), output.Path(v.To), output.Text(detail),
		}})
	}
	for _, a := range p.Actions {
		v := actionView{Action: string(a.Kind), Path: a.Path, To: a.To, Document: a.Document, Detail: a.Detail, Warnings: a.Warnings}
		if a.Err != nil {
			v.Error = a.Err.Error()
		}
		row(v, p.Ended)
	}
	end := actionView{Action: "pass", Detail: summary(p), Pending: p.Pending}
	if p.Err != nil {
		end.Error = p.Err.Error()
	}
	row(end, p.Ended)
}

// summary says what a pass came to.
func summary(p port.MirrorPass) string {
	done := len(p.Actions)
	var parts []string
	switch {
	case p.DryRun:
		parts = append(parts, fmt.Sprintf("dry run: %d actions", done))
	case done == 0:
		parts = append(parts, "in step")
	default:
		parts = append(parts, fmt.Sprintf("%d actions", done))
	}
	if p.Pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", p.Pending))
		for _, a := range p.Actions {
			if a.Kind == port.MirrorSkip && a.Detail == "still being written" {
				parts = append(parts, "still being written; run again in a few seconds")
				break
			}
		}
	}
	return strings.Join(parts, "; ")
}

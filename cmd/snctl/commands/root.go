// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package commands builds snctl's command tree over input ports.
package commands

import (
	"context"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/go-atlas-core/runtime/appinfo"

	"github.com/altessa-s/snctl/cmd/snctl/commands/account"
	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/cloud"
	"github.com/altessa-s/snctl/cmd/snctl/commands/debug"
	"github.com/altessa-s/snctl/cmd/snctl/commands/mirror"
	"github.com/altessa-s/snctl/cmd/snctl/commands/notebooks"
	"github.com/altessa-s/snctl/cmd/snctl/commands/version"
)

// Services are the command line's input ports.
type Services = cliutil.Services

// Streams are the command line's streams and terminal properties.
type Streams = cliutil.Streams

// Request selects the services for a run.
type Request = cliutil.Request

// Factory builds services after flags have been parsed.
type Factory = cliutil.Factory

// ExitCode translates a command failure into a process status.
func ExitCode(err error) int { return cliutil.ExitCode(err) }

// New builds a command tree over fixed services, for tests and embedding.
func New(svc Services, streams Streams) *cobra.Command {
	return newRoot(cliutil.NewEnv(streams, cliutil.Fixed(svc)))
}

func newRoot(env *cliutil.Env) *cobra.Command {
	root := &cobra.Command{Use: "snctl",
		Short: "Manage Supernote cloud files and read notebooks",
		Long: heredoc.Doc(`
            snctl reads and changes a Supernote cloud library: list tablet files,
            find entries by name, download and upload, create folders, move, recycle and
            restore, and follow changes live. Read text and highlights or draw
            notebooks from the cloud or local .note and .mark files.

            Items are named by exact tablet path ("DOCUMENT/Document/Paper.pdf"
            or "NOTE/Note/Journal.note"), with an optional leading slash. Recycled
            items, tasks and digests use the identifiers their listings show.
            Answers go to standard output and commentary to standard error, so a
            pipe gets the answer. On a terminal an answer is a table; in a pipe it
            is plain lines of TAB-separated fields; --format table, plain or json
            (or SNCTL_FORMAT) chooses, and --json is --format json. --quiet hides
            progress lines; JSON hides them too.

            Cloud file, task and digest writes ask first; --yes answers for them.
            --dry-run, where offered, prints or checks the plan and sends no
            changes. Downloads and local notebook output need no confirmation.
            Login and logout act on the session directly; check is read-only
            unless --write authorizes its self-test in a new folder. Defining a
            mirror authorizes its later runs; ad-hoc runs ask first.

            Each profile selects the official or a private cloud and its secrets.
            Existing local notebooks need no cloud login.
        `),
		Example: heredoc.Doc(`
            # Log in to a profile
            snctl profile add work --endpoint official --yes
            snctl -P work login --account reader@example.com

            # List a folder and show the notebooks as a tree
            snctl ls DOCUMENT/Document
            snctl tree NOTE/Note

            # Download a document and upload one
            snctl get DOCUMENT/Document/Paper.pdf -o paper.pdf
            snctl put paper.pdf DOCUMENT/Document --yes

            # Draw a local notebook as PDF
            snctl render Journal.note -o journal.pdf

            # Everything in a folder as JSON for a script
            snctl ls DOCUMENT/Document --json
        `),
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       appinfo.Version}
	root.SetIn(env.Streams.In)
	root.SetOut(env.Streams.Out)
	root.SetErr(env.Streams.Err)
	root.SetFlagErrorFunc(cliutil.FlagError)
	cliutil.AddPersistentFlags(root, env)
	root.AddGroup(
		&cobra.Group{ID: account.GroupID, Title: "Account:"},
		&cobra.Group{ID: cliutil.GroupRead, Title: cliutil.TitleRead},
		&cobra.Group{ID: cliutil.GroupWrite, Title: cliutil.TitleWrite},
		&cobra.Group{ID: debug.GroupID, Title: "Diagnostics:"},
	)
	root.AddCommand(notebooks.Commands(env)...)
	root.AddCommand(cloud.Commands(env)...)
	root.AddCommand(account.Commands(env)...)
	root.AddCommand(debug.New(env), version.New(env), mirror.New(env))
	root.SetHelpCommandGroupID(account.GroupID)
	root.SetCompletionCommandGroupID(account.GroupID)
	initBuiltinHelp(root)
	return root
}

// Run executes args and releases any services built along the way.
func Run(ctx context.Context, args []string, streams Streams, factory Factory) (err error) {
	env := cliutil.NewEnv(streams, factory)
	defer func() {
		if closeErr := env.Close(); err == nil {
			err = closeErr
		}
	}()
	root := newRoot(env) //nolint:contextcheck // handlers obtain the execution context from Cobra after ExecuteContext
	root.SetArgs(args)
	return cliutil.Interrupted(ctx, root.ExecuteContext(ctx))
}

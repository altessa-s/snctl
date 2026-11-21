// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package debug exposes local diagnostic commands.
package debug

import (
	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
)

// GroupID groups diagnostics in root help.
const GroupID = "debug"

// New returns the debug group, whose commands never read secrets.
func New(env *cliutil.Env) *cobra.Command {
	cmd := &cobra.Command{Use: "debug",
		Short: "Inspect local paths", GroupID: GroupID,
		Long: `Inspect local configuration and cache paths without printing secret values
or contacting the cloud. paths and cache status report on standard output.
cache clear removes local cached data after confirmation; it changes no
cloud files.`,
		Example: "snctl debug paths\nsnctl debug cache status --all --json"}
	cmd.AddCommand(&cobra.Command{Use: "paths",
		Short: "Show configuration, profile and cache paths",
		Long: `Show the resolved configuration file, configuration root, profile directory,
token-file path and cache paths on standard output. This locates files
without reading secret values or contacting the cloud.`,
		Example: "snctl debug paths\nsnctl -P work debug paths --json",
		Args:    cliutil.Args(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := env.Services(cmd)
			if err != nil {
				return err
			}
			p := svc.Paths
			return env.Emit(cmd, output.Result{JSON: p, Doc: output.Doc{output.Record{Fields: []output.Field{
				{Key: "config_file", Value: output.Path(p.ConfigFile)}, {Key: "config_root", Value: output.Path(p.ConfigRoot)},
				{Key: "profile_dir", Value: output.Path(p.ProfileDir)}, {Key: "token_file", Value: output.Path(p.TokenFile)},
				{Key: "cache_dir", Value: output.Path(p.CacheDir)}, {Key: "profile_cache", Value: output.Path(p.ProfileCache)},
			}}}})
		}})
	cmd.AddCommand(cacheCommands(env))
	return cmd
}

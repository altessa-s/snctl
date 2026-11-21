// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package account manages local profiles and their secrets.
package account

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/port"
)

// GroupID groups configuration commands in root help.
const GroupID = cliutil.GroupAccount
const (
	keyProfile    = "profile"
	keyBackend    = "backend"
	keyConfigFile = "config_file"
)

// Commands returns the local profile and secrets groups.
func Commands(env *cliutil.Env) []*cobra.Command {
	return []*cobra.Command{newProfile(env), newSecrets(env)}
}

func profiles(cmd *cobra.Command, env *cliutil.Env) (port.Profiles, error) {
	return cliutil.Need(cmd, env, func(s cliutil.Services) port.Profiles { return s.Profiles })
}

func newProfile(env *cliutil.Env) *cobra.Command {
	cmd := &cobra.Command{Use: "profile",
		Short: "List, describe, add and remove local profiles", GroupID: GroupID,
		Long: `Keep named local profiles for the official cloud or a private HTTP(S)
endpoint. Each profile selects its own session and secret store. ls and info
read local settings; add and del ask before changing them, and --yes
answers. Answers go to standard output and never include secret values.`,
		Example: "snctl profile ls\nsnctl profile add work --endpoint official --yes\nsnctl profile info work"}
	cmd.AddCommand(&cobra.Command{Use: "ls",
		Short: "List saved profiles",
		Long: `List local profiles, including profiles known only from cached data, on
standard output. The answer includes endpoint, secret backend, whether the
profile is current, cache bytes and budget. No cloud request is made and no
secrets are printed.`,
		Example: "snctl profile ls\nsnctl profile ls --json",
		Args:    cliutil.Args(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := profiles(cmd, env)
			if err != nil {
				return err
			}
			ps, err := svc.List(cmd.Context())
			if err != nil {
				return err
			}
			table := output.Table{Columns: []output.Column{{Title: "name"},
				{Title: "endpoint"},
				{Title: keyBackend},
				{Title: "current"},
				{Title: "cache_bytes"},
				{Title: "cache_budget"},
				{Title: "cache_budget_source"}}}
			for _, p := range ps {
				table.Rows = append(table.Rows, []output.Cell{output.Text(p.Name),
					output.Text(p.Endpoint),
					output.Text(string(p.Backend)),
					output.Bool(p.Current),
					output.Size(p.CacheBytes),
					cliutil.BudgetCell(p.CacheBudget),
					output.Text(p.CacheBudgetSource)})
			}
			if ps == nil {
				ps = []port.ProfileSettings{}
			}
			return env.Emit(cmd, output.Result{JSON: ps, Doc: output.Doc{table}})
		}})
	cmd.AddCommand(&cobra.Command{Use: "info [name]",
		Short: "Describe a profile without reading its secrets",
		Long: `Describe a named profile, or the selected profile when no name is given.
Standard output shows settings, secret-store location and cache budget and
usage; it does not read or print secret values and makes no cloud request.`,
		Example: "snctl profile info work\nsnctl -P private profile info --json",
		Args:    cliutil.Args(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := profiles(cmd, env)
			if err != nil {
				return err
			}
			var name string
			if len(args) > 0 {
				name = args[0]
			}
			p, err := svc.Info(cmd.Context(), name)
			if err != nil {
				return err
			}
			return env.Emit(cmd, profileResult(p))
		}})
	var endpoint, backend string
	add := &cobra.Command{Use: "add <name>",
		Short: "Save a cloud profile; use login to authenticate",
		Long: `Save a named local profile for the official cloud or a private HTTP(S)
endpoint. --secrets records a backend; otherwise configuration supplies it.
Ask before saving; --yes answers. Standard output describes the profile.
This saves settings only: login authenticates it.`,
		Example: "snctl profile add work --endpoint official --yes\nsnctl profile add private --endpoint https://cloud.example.com --secrets file --yes",
		Args:    cliutil.Args(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := profiles(cmd, env)
			if err != nil {
				return err
			}
			if err = cliutil.Confirm(cmd, env, fmt.Sprintf("Add profile %q?", args[0])); err != nil {
				return err
			}
			if err = svc.Add(cmd.Context(), args[0], endpoint, backend); err != nil {
				return err
			}
			p, err := svc.Info(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return env.Emit(cmd, profileResult(p))
		}}
	add.Flags().StringVar(&endpoint, "endpoint", "official", "official or a private cloud HTTP(S) URL")
	add.Flags().StringVar(&backend, "secrets", "", "file or keychain (default: inherit configuration)")
	cmd.AddCommand(add)
	cmd.AddCommand(&cobra.Command{Use: "del <name>",
		Short: "Remove local profile settings and secrets",
		Long: `Remove a named profile's local settings, secrets and cached data. Ask before
removing them; --yes answers. No library file is deleted in the cloud.
Standard output names the removed profile.`,
		Example: "snctl profile del private --yes",
		Args:    cliutil.Args(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := profiles(cmd, env)
			if err != nil {
				return err
			}
			if err = cliutil.Confirm(cmd, env, fmt.Sprintf("Remove profile %q and its secrets?", args[0])); err != nil {
				return err
			}
			if err = svc.Remove(cmd.Context(), args[0]); err != nil {
				return err
			}
			return env.Emit(cmd, output.Result{JSON: struct {
				Name string `json:"removed"`
			}{args[0]}, Doc: output.Doc{output.Body{Text: "removed " + args[0]}}})
		}})
	return cmd
}

func profileResult(p port.ProfileSettings) output.Result {
	return output.Result{JSON: p, Doc: output.Doc{output.Record{Fields: []output.Field{
		{Key: "name", Value: output.Text(p.Name)}, {Key: "endpoint", Value: output.Text(p.Endpoint)},
		{Key: keyBackend, Value: output.Text(string(p.Backend))}, {Key: "backend_source", Value: output.Text(p.BackendSource)},
		{Key: keyConfigFile, Value: output.Path(p.ConfigFile)}, {Key: "config_dir", Value: output.Path(p.ConfigDir)},
		{Key: "cache_bytes", Value: output.Size(p.CacheBytes)}, {Key: "cache_budget", Value: cliutil.BudgetCell(p.CacheBudget)},
		{Key: "cache_budget_source", Value: output.Text(p.CacheBudgetSource)},
		{Key: "cache_dir", Value: output.Path(p.CacheDir)}, {Key: "token_path", Value: output.Path(p.TokenPath)},
		{Key: "exists", Value: output.Bool(p.Exists)}, {Key: "current", Value: output.Bool(p.Current)},
	}}}}
}

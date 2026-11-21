// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package version reports build information without constructing services.
package version

import (
	"github.com/spf13/cobra"

	"github.com/altessa-s/go-atlas-core/runtime/appinfo"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
)

// New returns version with optional full build metadata.
func New(env *cliutil.Env) *cobra.Command {
	var full bool
	cmd := &cobra.Command{Use: "version",
		Short: "Display version information",
		Long: `Print the version on standard output without constructing services or
contacting the cloud. --full adds build time, commit, Go version, platform
and the notebook reading and rendering capabilities compiled into this
binary. --json writes the same information for a script.`,
		Example: "snctl version\nsnctl version --full --json",
		Args:    cliutil.Args(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !full {
				return env.Emit(cmd, output.Result{JSON: struct {
					Version string `json:"version"`
				}{appinfo.Version}, Doc: output.Doc{output.Body{Text: appinfo.Version}}})
			}
			v := struct {
				Version   string `json:"version"`
				BuildTime string `json:"build_time"`
				Commit    string `json:"commit"`
				GoVersion string `json:"go_version"`
				Platform  string `json:"platform"`
				Rendering bool   `json:"render"`
				Reading   bool   `json:"notes"`
			}{appinfo.Version, appinfo.BuildTime, appinfo.Commit, appinfo.BuildGoVersion(), appinfo.BuildPlatform(), rendering, reading}
			return env.Emit(cmd, output.Result{JSON: v, Doc: output.Doc{output.Record{Fields: []output.Field{

				{Key: "version", Value: output.Text(v.Version)}, {Key: "build_time", Value: output.Text(v.BuildTime)},
				{Key: "commit", Value: output.ID(v.Commit)}, {Key: "go_version", Value: output.Text(v.GoVersion)}, {Key: "platform", Value: output.Text(v.Platform)},
				{Key: "render", Value: output.Text(yesNo(v.Rendering))}, {Key: "notes", Value: output.Text(yesNo(v.Reading))},
			}}}})
		}}
	cmd.Flags().BoolVarP(&full, "full", "f", false, "include full build information")
	return cmd
}

func yesNo(on bool) string {
	if on {
		return "yes"
	}
	return "no"
}

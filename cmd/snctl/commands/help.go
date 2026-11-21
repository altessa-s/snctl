// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package commands

import "github.com/spf13/cobra"

// initBuiltinHelp gives Cobra's generated commands the same examples as ours.
// Initialize them before testing or showing the tree so help is complete there too.
func initBuiltinHelp(root *cobra.Command) {
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	for _, cmd := range root.Commands() {
		switch cmd.Name() {
		case "help":
			cmd.Long = `Show usage, flags, examples and subcommands on standard output.
Name a command path to read its help. No settings are loaded and nothing
is changed.`
			cmd.Example = "snctl help\nsnctl help get\nsnctl help digests handwriting put"
		case "completion":
			cmd.Example = "snctl completion zsh > _snctl\nsnctl completion bash > snctl.bash"
			for _, shell := range cmd.Commands() {
				shell.Example = "snctl completion " + shell.Name() + " > snctl." + shell.Name()
			}
		}
	}
}

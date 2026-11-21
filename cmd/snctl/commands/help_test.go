// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package commands_test

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands"
	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
)

func TestHelpCompleteness(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	root := commands.New(commands.Services{}, commands.Streams{Out: &out, Err: io.Discard})
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		if cmd.Hidden {
			return
		}
		t.Run(cmd.CommandPath(), func(t *testing.T) {
			first, _ := utf8.DecodeRuneInString(cmd.Short)
			if !unicode.IsUpper(first) {
				t.Errorf("Short must start with a capital letter: %q", cmd.Short)
			}
			if strings.TrimSpace(cmd.Long) == "" {
				t.Error("missing Long description")
			}
			if strings.TrimSpace(cmd.Example) == "" {
				t.Error("missing Example invocations")
			}
			if cmd.Parent() == root && cmd.Name() != "version" && cmd.GroupID == "" {
				t.Error("top-level command has no help group")
			}
			for _, group := range cmd.Groups() {
				hasMember := false
				for _, child := range cmd.Commands() {
					hasMember = hasMember || child.GroupID == group.ID
				}
				if !hasMember {
					t.Errorf("empty help group %q", group.Title)
				}
			}
			out.Reset()
			if err := cmd.Help(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "Examples:") {
				t.Error("help does not show examples")
			}
		})
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
}

func TestBuiltinShortDescriptions(t *testing.T) {
	t.Parallel()
	root := commands.New(commands.Services{}, commands.Streams{Out: io.Discard, Err: io.Discard})
	want := map[string]string{
		"help":       "Help about any command",
		"completion": "Generate the autocompletion script for the specified shell",
		"version":    "Display version information",
	}
	// Compare the shell subcommands with Cobra's generated descriptions too.
	defaults := &cobra.Command{Use: "snctl"}
	defaults.InitDefaultCompletionCmd()
	for _, completion := range defaults.Commands() {
		for _, shell := range completion.Commands() {
			want["completion "+shell.Name()] = shell.Short
		}
	}
	for path, short := range want {
		cmd, _, err := root.Find(strings.Fields(path))
		if err != nil {
			t.Fatal(err)
		}
		if cmd.Short != short {
			t.Errorf("%s: Short = %q, want %q", path, cmd.Short, short)
		}
	}
}

func TestHelpGroupsReflectBehavior(t *testing.T) {
	t.Parallel()
	root := commands.New(commands.Services{}, commands.Streams{Out: io.Discard, Err: io.Discard})
	groups := map[string][]string{
		"account": {"login", "logout", "account", "profile", "secrets", "completion", "help"},
		cliutil.GroupRead: {"ls", "tree", "stat", "find", "du", "dupes", "get", "mget", "backup", "sync", "watch",
			"text", "highlights", "note", "render", "open", "tasks ls", "tasks lists ls", "trash ls", "digests handwriting get"},
		cliutil.GroupWrite: {"put", "mput", "mkdir", "mv", "rename", "rm", "restore", "trash", "mirror", "tasks", "digests",
			"backup restore", "trash empty", "tasks add", "tasks lists rm", "digests handwriting put"},
		"debug": {"check", "debug"},
	}
	for group, paths := range groups {
		for _, path := range paths {
			cmd, _, err := root.Find(strings.Fields(path))
			if err != nil {
				t.Fatal(err)
			}
			if cmd.GroupID != group {
				t.Errorf("%s: group %q, want %q", path, cmd.GroupID, group)
			}
		}
	}
	version, _, err := root.Find([]string{"version"})
	if err != nil || version.GroupID != "" {
		t.Fatalf("version should keep Cobra's default group: %v", err)
	}
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Help(); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Account:", "Reading — these change nothing:", "Writing — these change the cloud and ask first:", "Diagnostics:", "Additional Commands:"} {
		if !strings.Contains(out.String(), title) {
			t.Errorf("root help lacks %q", title)
		}
	}
}

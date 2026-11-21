// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"

	"golang.org/x/text/unicode/norm"
)

// CompleteCloud shares canonical remote path completion across command families.
func CompleteCloud(env *Env, folders bool) cobra.CompletionFunc {
	return func(cmd *cobra.Command, _ []string, word string) ([]string, cobra.ShellCompDirective) {
		env.completing = true
		svc, err := Need(cmd, env, func(s Services) port.Files { return s.Completion })
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		cut := strings.LastIndex(word, "/") + 1
		dir, base := word[:cut], strings.ToLower(word[cut:])
		entries, err := svc.List(cmd.Context(), dir, false)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		out := make([]string, 0, len(entries))
		for _, e := range entries {
			if (folders && !e.Folder) || !strings.HasPrefix(strings.ToLower(e.Name), base) {
				continue
			}
			name := dir + e.Name
			if e.Folder {
				name += "/"
			}
			out = append(out, name)
		}
		return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
	}
}

// CompleteDirs completes local directories only.
func CompleteDirs(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveFilterDirs
}

// CompleteFlag registers the completion of a flag's value. The flag exists
// by construction, so registration cannot fail.
func CompleteFlag(cmd *cobra.Command, flag string, complete cobra.CompletionFunc) {
	_ = cmd.RegisterFlagCompletionFunc(flag, complete) //nolint:errcheck // the flag exists
}

// CompleteNotebook combines local notebook files with cloud paths.
func CompleteNotebook(env *Env) cobra.CompletionFunc {
	return completeNotebook(env, completionNotebook)
}

// CompleteHighlights offers raw marks and documents with full-name sidecars.
func CompleteHighlights(env *Env) cobra.CompletionFunc {
	return completeNotebook(env, completionHighlights)
}

// CompleteRender offers notebooks, drawings and documents with full-name sidecars.
func CompleteRender(env *Env) cobra.CompletionFunc {
	return completeNotebook(env, completionRender)
}

type notebookCompletion int

const (
	completionNotebook notebookCompletion = iota
	completionHighlights
	completionRender
)

func completeNotebook(env *Env, kind notebookCompletion) cobra.CompletionFunc {
	remote := CompleteCloud(env, false)
	return func(cmd *cobra.Command, args []string, word string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		cut := strings.LastIndex(word, "/") + 1
		out, _ := remote(cmd, args, word[:cut])
		out = notebookCandidates(out, kind)
		filtered := out[:0]
		for _, name := range out {
			if strings.HasPrefix(norm.NFC.String(strings.ToLower(name)), norm.NFC.String(strings.ToLower(word))) {
				filtered = append(filtered, name)
			}
		}
		out = filtered
		matches, _ := filepath.Glob(word + "*")
		for _, name := range matches {
			fi, err := os.Stat(name)
			if err != nil {
				continue
			}
			if fi.IsDir() {
				out = append(out, name+string(filepath.Separator))
				continue
			}
			if localNotebook(name, kind) {
				out = append(out, name)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
	}
}

func notebookCandidates(names []string, kind notebookCompletion) []string {
	entries := make([]port.Entry, 0, len(names))
	for _, name := range names {
		entries = append(entries, port.Entry{Name: path.Base(name), Path: name, Folder: strings.HasSuffix(name, "/")})
	}
	out := make([]string, 0, len(names))
	for _, e := range port.FoldMarks(entries, true) {
		if e.Folder || notebookExtension(e.Path, kind) || e.Mark != nil {
			out = append(out, e.Path)
		}
	}
	return out
}

func notebookExtension(name string, kind notebookCompletion) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".mark" || kind != completionHighlights && (ext == ".note" || ext == ".spd")
}

func localNotebook(name string, kind notebookCompletion) bool {
	if notebookExtension(name, kind) {
		return true
	}
	if !domain.DocumentName(name) {
		return false
	}
	mark, err := os.Stat(name + ".mark")
	return err == nil && mark.Mode().IsRegular()
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package cliutil

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestNotebookDocumentCompletion(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"paired.epub", "paired.epub.mark", "bare.pdf", "other.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cloud := &porttest.Cloud{Entries: []port.Entry{
		{Name: "й.pdf"}, {Name: "и\u0306.pdf.mark"}, {Name: "bare.pdf"},
		{Name: "ambiguous.pdf"}, {Name: "ambiguous.pdf.mark"}, {Name: "ambiguous.pdf.mark"},
	}}
	cmd := &cobra.Command{Use: "test"}
	cmd.SetContext(t.Context())
	env := NewEnv(Streams{Getenv: func(string) string { return "" }}, Fixed(Services{Completion: cloud}))
	for _, complete := range []cobra.CompletionFunc{CompleteNotebook(env), CompleteHighlights(env), CompleteRender(env)} {
		got, _ := complete(cmd, nil, "remote/й")
		if !slices.Contains(got, "remote/й.pdf") || cloud.Calls[len(cloud.Calls)-1] != "list" {
			t.Fatal(got, cloud.Calls)
		}
		got, _ = complete(cmd, nil, "remote/")
		if slices.Contains(got, "remote/ambiguous.pdf") || slices.Contains(got, "remote/bare.pdf") {
			t.Fatal(got)
		}
	}
	env = NewEnv(Streams{Getenv: func(string) string { return "" }}, Fixed(Services{}))
	got, _ := CompleteNotebook(env)(cmd, nil, filepath.Join(dir, "paired.e"))
	if !slices.Contains(got, filepath.Join(dir, "paired.epub")) {
		t.Fatal(got)
	}
}

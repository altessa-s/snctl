// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud_test

import (
	"bytes"
	"context"
	"path"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/cmd/snctl/commands"
	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

type commandTree struct{ *porttest.Cloud }

func (f *commandTree) Move(_ context.Context, e port.Entry, destination string) error {
	for i := range f.Entries {
		if f.Entries[i].ID == e.ID {
			f.Entries[i].Path = destination
			f.Entries[i].Name = path.Base(destination)
			return nil
		}
	}
	return port.ErrNotFound
}
func (f *commandTree) List(_ context.Context, folder string, recursive bool) ([]port.Entry, error) {
	var rows []port.Entry
	for _, e := range f.Entries {
		if folder == "/" || path.Dir(e.Path) == folder || (recursive && strings.HasPrefix(e.Path, folder+"/")) {
			rows = append(rows, e)
		}
	}
	return rows, nil
}
func TestCommandsKeepDistinctTreePaths(t *testing.T) {
	t.Parallel()
	f := &commandTree{Cloud: &porttest.Cloud{Entries: []port.Entry{
		{ID: "1", Name: "NOTE", Path: "NOTE", Folder: true},
		{ID: "2", Name: "Note", Path: "NOTE/Note", Folder: true},
		{ID: "3", Name: "x", Path: "NOTE/x", Folder: true},
		{ID: "4", Name: "x", Path: "NOTE/Note/x", Folder: true},
		{ID: "5", Name: "c.txt", Path: "NOTE/x/c.txt"},
		{ID: "6", Name: "c.txt", Path: "NOTE/Note/x/c.txt"},
	}}}
	for _, format := range []string{"text", "table", "json"} {
		for _, args := range [][]string{{"ls", "NOTE/Note/x", "--ids"}, {"tree", "NOTE", "--ids"}, {"stat", "NOTE/Note/x/c.txt"}} {
			var out bytes.Buffer
			svc := services(f.Cloud)
			svc.Files = f
			svc.Fetch = files.Service{Cloud: f, Local: f}
			svc.Library = files.Service{Cloud: f, Account: f}
			err := commands.Run(t.Context(), append(args, "--format", format), commands.Streams{Out: &out, Err: &out}, cliutil.Fixed(svc))
			if err != nil || out.Len() == 0 {
				t.Fatalf("%v: %v", args, err)
			}
			if args[0] == "ls" && strings.Contains(out.String(), "NOTE/x/c.txt") {
				t.Fatal("listed sibling file")
			}
		}
	}
	var out bytes.Buffer
	svc := services(f.Cloud)
	svc.Files = f
	svc.Fetch = files.Service{Cloud: f, Local: f}
	svc.Library = files.Service{Cloud: f, Account: f}
	err := commands.Run(t.Context(), []string{"rename", "NOTE/Note/x/c.txt", "renamed.txt", "--yes"}, commands.Streams{Out: &out, Err: &out}, cliutil.Fixed(svc))
	if err != nil {
		t.Fatal(err)
	}
	if f.Entries[4].Path != "NOTE/x/c.txt" || f.Entries[5].Path != "NOTE/Note/x/renamed.txt" {
		t.Fatal("renamed wrong entry")
	}
	if !strings.Contains(out.String(), "NOTE/Note/x/renamed.txt") {
		t.Fatal(out.String())
	}
}

// One rule: a command that changes the cloud asks, or needs --yes outside a
// terminal; reading, local output and a dry run do not.
func TestCloudChangesNeedConfirmation(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"mkdir", "Note/new"}, {"restore", "10"}, {"mv", "Note/sample.txt", "Note/new.txt"}, {"rename", "Note/sample.txt", "new.txt"},
		{"put", "local.txt", "Note/new.txt"}, {"tasks", "lists", "add", "Read"}, {"tasks", "lists", "rename", "list", "Read"},
		{"tasks", "add", "Read"}, {"tasks", "done", "task"}, {"tasks", "edit", "task", "--title", "T"},
		{"digests", "groups", "add", "--name", "Read"}, {"digests", "digests", "edit", "30", "--content", "x"},
	} {
		f := fixture()
		if _, _, err := run(t, f, "", args...); commands.ExitCode(err) != 2 {
			t.Fatalf("%v unconfirmed: %v", args, err)
		}
		for _, call := range f.Calls {
			readOnly := call == "stat" || call == "list"
			if args[0] == "restore" && (call == "trash" || call == "restore target") {
				readOnly = true
			}
			if !readOnly {
				t.Fatalf("%v changed the cloud before confirming: %v", args, f.Calls)
			}
		}
		if _, _, err := run(t, fixture(), "", append(args, "--yes")...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	for _, args := range [][]string{{"logout"}, {"ls"}, {"get", "Note/sample.txt", "-o", "-"}, {"put", "local.txt", "Note/new.txt", "--dry-run"}} {
		if _, _, err := run(t, fixture(), "", args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/cmd/snctl/commands"
	"github.com/altessa-s/snctl/cmd/snctl/commands/clitest"
	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func services(f *porttest.Cloud) commands.Services {
	disk := &porttest.NotebookFiles{Data: map[string][]byte{"local.txt": []byte("abc"), "download": []byte("abc")}}
	return commands.Services{Completion: f, Library: files.Service{Cloud: f, Account: f, Disk: disk}, Fetch: files.Service{Cloud: f, Local: f},
		Session: f, Files: f, Local: f, Tasks: f, Digests: f, Feed: f, Check: f}
}
func fixture() *porttest.Cloud {
	size := int64(3)
	return &porttest.Cloud{SavedAccount: "reader@example.invalid", Entries: []port.Entry{{ID: "10", Name: "sample.txt", Path: "Note/sample.txt", Size: &size}, {ID: "1", Name: "Note", Path: "Note", Folder: true}},
		TaskRows: []port.Task{{ID: "task", Title: "Reading"}}, DigestRows: []port.DigestRecord{{ID: "30", Name: "Reading"}},
		Changes: []port.Change{{Type: "connected"}}, CheckSteps: []port.CheckStep{{Name: "session", Status: "pass"}}}
}
func run(t *testing.T, f *porttest.Cloud, input string, args ...string) (string, string, error) {
	t.Helper()
	if len(args) > 0 && args[0] == "restore" && f.Recycled == nil {
		f.Recycled = []port.Entry{{ID: "10", Name: "restored.txt", Path: "Note/restored.txt"}}
	}
	var out, errout bytes.Buffer
	err := commands.Run(t.Context(), args, commands.Streams{In: strings.NewReader(input), Out: &out, Err: &errout, Getenv: func(string) string { return "" }}, cliutil.Fixed(services(f)))
	return out.String(), errout.String(), err
}

func TestCloudCommands(t *testing.T) {
	t.Parallel()
	cases := [][]string{
		{"login", "--password-stdin"}, {"logout"}, {"account"}, {"ls"}, {"ls", "Note"}, {"tree"}, {"stat", "Note/sample.txt"},
		{"find", "sample"}, {"find", "sample", "Note"}, {"get", "Note/sample.txt"}, {"get", "Note/sample.txt", "-o", "download.txt"},
		{"put", "local.txt", "Note/new.txt"}, {"put", "-", "Note/new.txt"}, {"mkdir", "Note/new"}, {"mv", "Note/sample.txt", "Note/new.txt"},
		{"rename", "Note/sample.txt", "new.txt"}, {"rm", "Note/sample.txt"}, {"rm", "Note/sample.txt", "--permanent"},
		{"trash"}, {"trash", "ls"}, {"trash", "empty"}, {"restore", "10"},
		{"tasks", "lists", "ls"}, {"tasks", "lists", "add", "Reading"}, {"tasks", "lists", "rename", "list", "Reading"}, {"tasks", "lists", "rm", "list"},
		{"tasks", "ls"}, {"tasks", "ls", "list"}, {"tasks", "add", "Read"}, {"tasks", "add", "Read", "--list", "list", "--detail", "detail", "--due", "2026-01-01T00:00:00Z"},
		{"tasks", "edit", "task", "--title", "Read", "--detail", "detail"}, {"tasks", "done", "task"}, {"tasks", "undone", "task"}, {"tasks", "rm", "task"},
		{"watch"}, {"check"}, {"check", "--write"},
		{"digests", "handwriting", "get", "30", "comment.mark"}, {"digests", "handwriting", "put", "30", "comment.mark"},
	}
	for _, kind := range []string{"groups", "tags", "digests"} {
		cases = append(cases, []string{"digests", kind, "ls"}, []string{"digests", kind, "show", "30"}, []string{"digests", kind, "rm", "30"})
		field := "--name"
		if kind == "digests" {
			field = "--content"
		}
		cases = append(cases, []string{"digests", kind, "add", field, "Reading"}, []string{"digests", kind, "edit", "30", field, "Reading"})
	}
	cases = append(cases, []string{"digests", "digests", "add", "--content", "passage", "--comment", "comment", "--group", "group", "--tag", "tag"})
	cases = append(cases, []string{"du"}, []string{"du", "Note", "-d", "0"}, []string{"du", "--all"},
		[]string{"mkdir", "-p", "Note/new/deeper"}, []string{"put", "local.txt", "--to", "Note"}, []string{"put", "local.txt", "Note/x.txt", "--dry-run"},
		[]string{"put", "local.txt", "Note", "--name", "named.txt"})
	for index, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			for _, format := range []string{"text", "json"} {
				args := append(append([]string{}, args...), "--yes", "--format", format)
				f := fixture()
				out, _, err := run(t, f, "synthetic-input\n", args...)
				if err != nil || out == "" {
					t.Fatalf("%s: output=%q err=%v", format, out, err)
				}
				clitest.Golden(t, fmt.Sprintf("%02d-%s-%s", index, args[0], format), out)
				calls := append([]string{}, f.Calls...)
				for _, call := range calls {
					f = fixture()
					f.FailAt = call
					f.Failure = errors.New("synthetic failure")
					_, _, runErr := run(t, f, "synthetic-input\n", args...)
					err = runErr

					if err == nil {
						t.Fatalf("%s failure ignored", call)
					}
				}
				var outbuf bytes.Buffer
				err = commands.Run(t.Context(), args, commands.Streams{In: strings.NewReader("input\n"), Out: &outbuf, Err: io.Discard}, cliutil.Fixed(commands.Services{}))
				if err == nil {
					t.Fatal("missing service accepted")
				}
			}
		})
	}
}

func TestRefusals(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"login"}, {"login", "--country-code", "-1", "--password-stdin"}, {"get", "Note/sample.txt", "-o", "-", "--json"},
		{"rename", "Note/sample.txt", "../outside"}, {"mv", "Note/sample.txt", "Note/sample.txt"}, {"put", "x", "Note/sample.txt"},
		{"tasks", "edit", "task"}, {"tasks", "add", "Read", "--due", "bad"}, {"digests", "groups", "add"}, {"digests", "digests", "add"},
		{"digests", "groups", "show", "404"}, {"digests", "handwriting", "get", "30", "-", "--json"}, {"rm", "absent"},
	} {
		_, _, err := run(t, fixture(), "", args...)
		if err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	for _, args := range [][]string{{"rm", "Note/sample.txt"}, {"trash", "empty"},
		{"put", "local.txt", "Note/sample.txt", "--replace"}, {"tasks", "lists", "rm", "list"}, {"tasks", "rm", "task"},
		{"digests", "groups", "rm", "30"}, {"digests", "handwriting", "put", "30", "file"}} {
		_, _, err := run(t, fixture(), "", args...)
		if commands.ExitCode(err) != 2 {
			t.Fatalf("unconfirmed %v: %v", args, err)
		}
	}
	f := fixture()
	f.SavedAccount = ""
	if _, _, err := run(t, f, "input\n", "login", "--password-stdin"); commands.ExitCode(err) != 2 {
		t.Fatal("missing account accepted")
	}
	f = fixture()
	f.Entries[0].Folder = true
	if _, _, err := run(t, f, "", "get", "Note/sample.txt"); commands.ExitCode(err) != 2 {
		t.Fatal("folder download accepted")
	}
	for _, sentinel := range []error{port.ErrSession, port.ErrConflict} {
		f = fixture()
		f.Failure = sentinel
		_, _, err := run(t, f, "", "account")
		want := 3
		if sentinel == port.ErrConflict {
			want = 4
		}
		if commands.ExitCode(err) != want {
			t.Fatal("wrong classified exit")
		}
	}
}

func TestLoginChallengeAndStreams(t *testing.T) {
	t.Parallel()
	for _, failAt := range []string{"", "send code", "verify"} {
		f := fixture()
		f.LoginResult.VerificationRequired = true
		f.FailAt = failAt
		if failAt != "" {
			f.Failure = errors.New("synthetic failure")
		}
		out, stderr, err := run(t, f, "synthetic-password\n", "login", "--account", "reader@example.invalid", "--password-stdin", "--code", "123456", "--country-code", "1")
		if (err != nil) != (failAt != "") {
			t.Fatalf("challenge %s: %v", failAt, err)
		}
		if strings.Contains(out+stderr, "synthetic-password") {
			t.Fatal("password leaked")
		}
	}
	f := fixture()
	f.LoginResult.VerificationRequired = true
	if _, _, err := run(t, f, "input\n", "login", "--password-stdin"); commands.ExitCode(err) != 2 {
		t.Fatal("missing code accepted")
	}
	for _, input := range []string{"", strings.Repeat("x", 65536)} {
		if _, _, err := run(t, fixture(), input, "login", "--password-stdin"); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	if _, _, err := run(t, fixture(), "input", "login", "--password-stdin"); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"get", "Note/sample.txt", "-o", "-"}, {"digests", "handwriting", "get", "30", "-"}} {
		out, _, err := run(t, fixture(), "", args...)
		if err != nil || out != "synthetic bytes" {
			t.Fatalf("stdout: %q %v", out, err)
		}
	}
	f = fixture()
	f.Failure = context.Canceled
	if _, _, err := run(t, f, "", "watch"); err != nil {
		t.Fatal("watch cancellation was not clean")
	}
	f = fixture()
	f.Changes = append(f.Changes, port.Change{Type: "files", ID: "10"})
	out, _, err := run(t, f, "", "watch", "--json")
	if err != nil || strings.Count(out, "\n") != 2 {
		t.Fatalf("not JSON lines: %q %v", out, err)
	}
}

func TestCompletionAndEmpty(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"__complete", "ls", "No"}, {"__complete", "get", "No"}, {"__complete", "get", "Note/sample.txt", ""},
		{"__complete", "put", ""}, {"__complete", "put", "local.txt", "No"}} {
		if _, _, err := run(t, fixture(), "", args...); err != nil {
			t.Fatal(err)
		}
	}
	// The new commands complete cloud folders, local directories and flag values.
	for args, want := range map[string]string{
		"mget|No":               "Note/",
		"mget|Note|-o|":         ":16",
		"mput|":                 ":16",
		"mput|dir|No":           "Note/",
		"put|a|--to|No":         "Note/",
		"put|a|--to|Note|":      ":0",
		"du|No":                 "Note/",
		"dupes|No":              "Note/",
		"mkdir|-p|No":           "Note/",
		"put|local.txt|Note|-x": "",
	} {
		out, _, err := run(t, fixture(), "", append([]string{"__complete"}, strings.Split(args, "|")...)...)
		if err != nil || !strings.Contains(out, want) {
			t.Fatalf("%s: %q %v", args, out, err)
		}
	}
	for _, args := range [][]string{{"ls"}, {"tasks", "ls"}, {"digests", "groups", "ls"}} {
		f := fixture()
		f.Entries = nil
		f.TaskRows = nil
		f.DigestRows = nil
		out, _, err := run(t, f, "", append(args, "--json")...)
		if err != nil || strings.TrimSpace(out) != "[]" {
			t.Fatalf("empty: %q %v", out, err)
		}
	}
	f := fixture()
	f.Entries[0].Folder = true
	if _, _, err := run(t, f, "", "put", "local.txt", "Note/sample.txt", "--yes"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run(t, fixture(), "", "put", "local.txt", "Note/sample.txt", "--replace", "--yes"); err != nil {
		t.Fatal(err)
	}
}

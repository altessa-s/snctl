// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/clitest"
	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestWatchExecProgram(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "handler.go")
	body := `package main
import("os";"io";"fmt";"time")
func main(){if len(os.Args)>1 && os.Args[1]=="wait" {time.Sleep(time.Hour)}
fmt.Printf("argv:%q\n",os.Args[1:])
for _,s:=range os.Environ(){fmt.Println(s)}
io.Copy(os.Stdout,os.Stdin)
}`
	if err := os.WriteFile(source, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	prog := filepath.Join(dir, "handler")
	if runtime.GOOS == "windows" {
		prog += ".exe"
	}
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", prog, source)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	t.Setenv("SNCTL_EVENT", "stale")
	t.Setenv("SNCTL_PROFILE", "stale")
	feed := &porttest.Cloud{Changes: []port.Change{{Type: "files", Kind: "MODIFYFILE", ID: "a"}}}
	files := &porttest.Cloud{Entries: []port.Entry{{ID: "a", Name: "$(touch sentinel).note", Path: "NOTE/Note/A/a.note"}}}
	svc := cliutil.Services{Feed: feed, Files: files}
	build := func(env *cliutil.Env) []*cobra.Command { return []*cobra.Command{watch(env)} }
	res := clitest.Exec(t, svc, clitest.Options{Env: map[string]string{cliutil.ProfileEnv: "work"}}, build,
		"watch", "--exec", prog, "--debounce", "1h", "--only", "file", "--under", "/NOTE/Note/A", "--", "a b", "$SNCTL_NAME", "$(touch sentinel)")
	if res.Code != 0 || !strings.Contains(res.Err, `argv:["a b" "$SNCTL_NAME" "$(touch sentinel)"]`) ||
		!strings.Contains(res.Err, "SNCTL_EVENT=MODIFYFILE\n") || strings.Contains(res.Err, "SNCTL_EVENT=stale") ||
		!strings.Contains(res.Err, "SNCTL_PROFILE=work\n") || !strings.Contains(res.Err, `"path":"/NOTE/Note/A/a.note"`) {
		t.Fatal(res.Code, res.Err, res.Error)
	}
	if len(files.Calls) != 1 || files.Calls[0] != "list" {
		t.Fatal(files.Calls)
	}
	res = clitest.Exec(t, svc, clitest.Options{}, build, "watch", "--exec", prog, "--exec-timeout", "20ms", "--", "wait")
	if res.Code != 0 || !strings.Contains(res.Err, "timed out") {
		t.Fatal(res)
	}
	res = clitest.Exec(t, svc, clitest.Options{}, build, "watch", "--exec", filepath.Join(dir, "absent"))
	if res.Code != 0 || !strings.Contains(res.Err, "exec:") {
		t.Fatal(res)
	}
	for _, args := range [][]string{
		{"--shell"}, {"--only", "file"}, {"--under", "/x"}, {"--debounce", "1s"}, {"--exec-timeout", "1s"}, {"--repeat-window", "0"},
		{"--", "x"}, {"x"}, {"--exec", prog, "x"}, {"--exec", prog, "--only", "page"},
		{"--exec", prog, "--under", "NOTE"}, {"--exec", prog, "--debounce", "-1s"}, {"--exec", prog, "--exec-timeout", "0s"},
		{"--exec", prog, "--repeat-window", "-1s"},
	} {
		r := clitest.Exec(t, svc, clitest.Options{}, build, append([]string{"watch"}, args...)...)
		if r.Code != 2 {
			t.Fatal(args, r)
		}
	}
	if runtime.GOOS != "windows" {
		res = clitest.Exec(t, svc, clitest.Options{}, build, "watch", "--shell", "--exec", `printf 'shell:%s\n' "$1"`, "--", "zero", "one")
		if res.Code != 0 || !strings.Contains(res.Err, "shell:one") {
			t.Fatal(res)
		}
	}
}

func TestWatchRawRepeatedAnnouncements(t *testing.T) {
	e := port.Change{Type: "files", Kind: "DOWNLOADFILE", ID: "a", Path: "NOTE/Note/"}
	feed := &porttest.Cloud{Changes: []port.Change{e, e, {Type: "connected"}, {Type: "connected"}}}
	build := func(env *cliutil.Env) []*cobra.Command { return []*cobra.Command{watch(env)} }
	res := clitest.Exec(t, cliutil.Services{Feed: feed}, clitest.Options{}, build, "watch", "--json")
	if res.Code != 0 || strings.Count(res.Out, `"kind":"DOWNLOADFILE"`) != 2 || strings.Count(res.Out, `"type":"connected"`) != 2 {
		t.Fatal(res)
	}
	if len(feed.Calls) != 1 || feed.Calls[0] != "watch" {
		t.Fatal(feed.Calls)
	}
	cmd := watch(&cliutil.Env{})
	if got := cmd.Flags().Lookup("repeat-window").DefValue; got != "5m0s" {
		t.Fatalf("repeat-window default = %q", got)
	}
}

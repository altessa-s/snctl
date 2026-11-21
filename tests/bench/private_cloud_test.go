//go:build bench

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package bench_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
)

// The isolated process environment prevents an opt-in run from reading the
// owner's usual profile or credential backend.
type bench struct {
	t       *testing.T
	binary  string
	env     []string
	secrets []string
}

func (b bench) command(ctx context.Context, input string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, b.binary, args...)
	cmd.Env = b.env
	cmd.Stdin = strings.NewReader(input)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	if err != nil {
		b.t.Error(b.failure(args, err, stderr.String()))
	}
	return out.String(), err
}

func (b bench) mask(text string) string {
	for _, secret := range b.secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
	}
	return domain.MaskDiagnostic(text)
}

func (b bench) failure(args []string, err error, stderr string) string {
	code := -1
	if e, ok := errors.AsType[*exec.ExitError](err); ok {
		code = e.ExitCode()
	}
	quoted := []string{strconv.Quote(filepath.Base(b.binary))}
	for i, arg := range args {
		if i > 0 && (args[i-1] == "--account" || args[i-1] == "--password" || args[i-1] == "--endpoint") {
			arg = "[redacted]"
		} else if arg != "--password-stdin" {
			arg = b.mask(arg)
		}
		quoted = append(quoted, strconv.Quote(arg))
	}
	return fmt.Sprintf("command: %s\nexit code: %d\nstderr:\n%s", strings.Join(quoted, " "), code, b.mask(stderr))
}

func (b bench) run(args ...string) string {
	b.t.Helper()
	out, err := b.command(b.t.Context(), "", args...)
	if err != nil {
		b.t.FailNow()
	}
	return out
}

func (b bench) entry(name string) port.Entry {
	b.t.Helper()
	var e port.Entry
	if err := json.Unmarshal([]byte(b.run("stat", name, "--json")), &e); err != nil {
		b.t.Fatal("invalid stat response")
	}
	return e
}

func (b bench) counts(args ...string) map[string]int {
	b.t.Helper()
	var r port.TransferReport
	if err := json.Unmarshal([]byte(b.run(append(args, "--json")...)), &r); err != nil {
		b.t.Fatal("invalid transfer response")
	}
	return r.Counts
}

func TestPrivateCloud(t *testing.T) {
	endpoint, account, passwordFile := os.Getenv("SNCTL_BENCH_ENDPOINT"), os.Getenv("SNCTL_BENCH_ACCOUNT"), os.Getenv("SNCTL_BENCH_PASSWORD_FILE")
	if endpoint == "" || account == "" || passwordFile == "" {
		t.Skip("set all three SNCTL_BENCH variables to opt in")
	}
	raw, err := os.ReadFile(passwordFile)
	if err != nil {
		t.Fatal("cannot read password file")
	}
	password := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSuffix(line, "\r"), "password:"); ok {
			password = value
			break
		}
	}
	if password == "" {
		t.Fatal("password file needs a nonempty password: line")
	}
	root := t.TempDir()
	binary := filepath.Join(root, "snctl")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "../../cmd/snctl")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatal((bench{binary: "go", secrets: []string{account, password}}).failure(build.Args[1:], err, string(output)))
	}
	b := bench{t: t, binary: binary, secrets: []string{account, password}, env: []string{"PATH=" + os.Getenv("PATH"), "TMPDIR=" + root, "TMP=" + root, "TEMP=" + root,
		"SNCTL_CONFIG_DIR=" + filepath.Join(root, "config"), "SNCTL_CACHE_DIR=" + filepath.Join(root, "cache"), "SNCTL_SECRETS=file", "SNCTL_PROFILE=bench"}}
	b.run("profile", "add", "bench", "--endpoint", endpoint, "--yes")
	if _, err := b.command(t.Context(), password+"\n", "login", "--account", account, "--password-stdin"); err != nil {
		t.FailNow()
	}
	folder := "NOTE/Note/snctl-bench-" + time.Now().UTC().Format("20060102T150405.000000000") + "-" + rand.Text()
	b.run("mkdir", folder, "--yes")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if _, err := b.command(ctx, "", "rm", folder, "--yes"); err != nil {
			t.Errorf("cleanup failed; retained folder %s", folder)
		}
	})

	local := filepath.Join(root, "one.pdf")
	write := func(t *testing.T, name, data string) {
		t.Helper()
		if err := os.WriteFile(name, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	target := folder + "/one.pdf"
	t.Run("replacement", func(t *testing.T) {
		b := b
		b.t = t
		write(t, local, "original bytes")
		b.run("put", local, target, "--yes")
		old := b.entry(target)
		write(t, local, "replacement bytes")
		b.run("put", local, target, "--replace", "--yes")
		fresh := b.entry(target)
		if fresh.ID != old.ID {
			t.Fatal("replacement changed identifier")
		}
		downloaded := filepath.Join(root, "download.pdf")
		b.run("get", target, "-o", downloaded)
		data, err := os.ReadFile(downloaded)
		if err != nil || string(data) != "replacement bytes" {
			t.Fatal("replacement download differs")
		}
	})
	t.Run("bulk", func(t *testing.T) {
		b := b
		b.t = t
		src := filepath.Join(root, "src")
		if err := os.Mkdir(src, 0700); err != nil {
			t.Fatal(err)
		}
		for i := range 5 {
			write(t, filepath.Join(src, fmt.Sprintf("file%d.pdf", i)), fmt.Sprintf("file %d", i))
		}
		counts := b.counts("mput", src, folder+"/bulk", "--yes")
		if counts[port.StatusUploaded] != 5 {
			t.Fatal("parallel upload incomplete")
		}
		counts = b.counts("mput", src, folder+"/bulk", "--incremental", "--yes")
		if counts[port.StatusUnchanged] != 5 {
			t.Fatal("incremental upload differs")
		}
		out := filepath.Join(root, "mirror")
		// mput keeps what it uploads in the cache, so these bodies may come
		// from there rather than over the network.
		got := b.counts("mget", folder+"/bulk", "-o", out)
		if got[port.StatusDownloaded]+got[port.StatusCached] != 5 {
			t.Fatal("download incomplete")
		}
		for i := range 5 {
			data, err := os.ReadFile(filepath.Join(out, fmt.Sprintf("file%d.pdf", i)))
			if err != nil || string(data) != fmt.Sprintf("file %d", i) {
				t.Fatal("bulk download differs")
			}
		}
		b.run("rm", folder+"/bulk/file0.pdf", "--yes")
		counts = b.counts("mget", folder+"/bulk", "-o", out, "--incremental", "--delete")
		if counts[port.StatusUnchanged] != 4 || counts[port.StatusDeleted] != 1 {
			t.Fatal("incremental mirror differs")
		}
	})
	t.Run("moves", func(t *testing.T) {
		b := b
		b.t = t
		b.run("mkdir", "-p", folder+"/nested/leaf", "--yes")
		write(t, local, "move bytes")
		target := folder + "/move.pdf"
		b.run("put", local, target, "--yes")
		b.run("mv", target, folder+"/nested", "--yes")
		b.run("mv", folder+"/nested/move.pdf", folder+"/nested/two.pdf", "--yes")
		b.run("rename", folder+"/nested/two.pdf", "three.pdf", "--yes")
		restored := b.entry(folder + "/nested/three.pdf")
		b.run("rm", restored.Path, "--yes")
		b.run("restore", restored.ID, "--yes")
		if b.entry(restored.Path).ID != restored.ID {
			t.Fatal("restore changed identifier")
		}
	})
	for _, command := range []string{"du", "dupes"} {
		t.Run(command, func(t *testing.T) {
			b := b
			b.t = t
			b.run(command, folder, "--json")
		})
	}
}

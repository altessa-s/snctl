//go:build integration && unix

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/altessa-s/snctl/internal/clouddouble"
)

func TestWatchExecBinaryBatchAndInterrupt(t *testing.T) {
	e := newE2E(t)
	id := e.cloud.Seed("NOTE/Note/changed.note", false, []byte("notebook"))
	binary := os.Getenv("SNCTL_TEST_BINARY")
	if binary == "" {
		binary = e.path("snctl")
		if out, err := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	source := e.path("handler.go")
	helper := `package main
import("io";"os";"time")
func main(){b,_:=io.ReadAll(os.Stdin)
f,_:=os.Create(os.Args[1]);f.Write(b);f.WriteString("\nprofile="+os.Getenv("SNCTL_PROFILE")+"\n");f.Close()
if len(os.Args)>2{time.Sleep(time.Hour)}}`
	if err := os.WriteFile(source, []byte(helper), 0600); err != nil {
		t.Fatal(err)
	}
	program := e.path("handler")
	if out, err := exec.CommandContext(t.Context(), "go", "build", "-o", program, source).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	script := e.path("receive-batch.sh")
	scriptBody := "#!/bin/sh\ncat > \"$1\"\nprintf '\\nprofile=%s\\n' \"$SNCTL_PROFILE\" >> \"$1\"\n"
	if err := os.WriteFile(script, []byte(scriptBody), 0700); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{"code": "200", "data": []map[string]any{{"messageType": "MODIFYFILE", "fileType": "FILE", "id": id, "path": "NOTE/Note/changed.note"}}})
	if err != nil {
		t.Fatal(err)
	}
	event, err := json.Marshal([]string{"ServerMessage", string(payload)})
	if err != nil {
		t.Fatal(err)
	}
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for _, frame := range [][]byte{[]byte(`0{"sid":"fixture","upgrades":[],"pingInterval":25000,"pingTimeout":20000}`), []byte("40"), append([]byte("42"), event...)} {
			if err := conn.Write(r.Context(), websocket.MessageText, frame); err != nil {
				return
			}
		}
		for {
			if _, _, err := conn.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer feed.Close()
	for _, wait := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "grace"}[wait], func(t *testing.T) {
			output := e.path("batch-" + t.Name()[strings.LastIndex(t.Name(), "/")+1:] + ".json")
			handler := program
			if !wait {
				handler = script
			}
			args := []string{"watch", "--exec", handler, "--debounce", "20ms", "--only", "file", "--under", "/NOTE/Note", "--", output}
			if wait {
				args = append(args, "wait")
			}
			cmd := exec.CommandContext(t.Context(), binary, args...)
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "TMPDIR=" + os.Getenv("TMPDIR"), "SNCTL_CONFIG_DIR=" + e.path("config/snctl"),
				"SNCTL_CACHE_DIR=" + e.path("cache/snctl"), "SNCTL_SECRETS=file", "SNCTL_PROFILE=e2e", "SNCTL_SOCKET_URL=" + feed.URL}
			var diag bytes.Buffer
			cmd.Stderr = &diag
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cmd.Process.Kill() }()
			deadline := time.Now().Add(20 * time.Second)
			for {
				body, _ := os.ReadFile(output)
				if strings.Contains(string(body), "profile=e2e") {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("handler did not receive batch")
				}
				time.Sleep(10 * time.Millisecond)
			}
			start := time.Now()
			if err := cmd.Process.Signal(os.Interrupt); err != nil {
				t.Fatal(err)
			}
			err := cmd.Wait()
			exit, ok := errors.AsType[*exec.ExitError](err)
			if !ok || exit.ExitCode() != 130 {
				t.Fatal(err, diag.String())
			}
			elapsed := time.Since(start)
			if wait && (elapsed < 4500*time.Millisecond || elapsed > 8*time.Second || !strings.Contains(diag.String(), "stopped at interrupt")) {
				t.Fatal(elapsed, diag.String())
			}
			body, err := os.ReadFile(output)
			if err != nil || !bytes.Contains(body, []byte(`"event":"MODIFYFILE"`)) || !bytes.Contains(body, []byte(`"path":"/NOTE/Note/changed.note"`)) {
				t.Fatal(string(body), err)
			}
			t.Logf("watch exit=130; batch=%s; grace=%s", body, elapsed.Round(time.Millisecond))
		})
	}
}

func TestBackupBinaryInterruptKeepsManifest(t *testing.T) {
	e := newE2E(t)
	e.cloud.Seed("NOTE/Note/a.note", false, []byte("alpha"))
	dir := e.path("backup")
	e.must("backup", dir)
	previous, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	binary := os.Getenv("SNCTL_TEST_BINARY")
	if binary == "" {
		binary = e.path("snctl")
		if out, err := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	e.cloud.Seed("NOTE/Note/a.note", false, []byte("ALPHA"))
	e.cloud.Seed("NOTE/Note/z.note", false, []byte("zeta"))
	started := make(chan struct{}, 1)
	e.cloud.Inject(clouddouble.Fault{Path: "NOTE/Note/z.note", Count: 1, Delay: time.Minute, Before: func() { started <- struct{}{} }})
	cmd := exec.CommandContext(t.Context(), binary, "backup", dir, "-j", "1")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "TMPDIR=" + os.Getenv("TMPDIR"), "SNCTL_CONFIG_DIR=" + e.path("config/snctl"),
		"SNCTL_CACHE_DIR=" + e.path("cache/snctl"), "SNCTL_SECRETS=file", "SNCTL_PROFILE=e2e"}
	var diag bytes.Buffer
	cmd.Stderr = &diag
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	select {
	case <-started:
	case <-time.After(20 * time.Second):
		t.Fatal("second download did not start")
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	exit, ok := errors.AsType[*exec.ExitError](err)
	if !ok || exit.ExitCode() != 130 {
		t.Fatal(err, diag.String())
	}
	current, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil || !bytes.Equal(current, previous) {
		t.Fatal("interrupted manifest changed", err)
	}
	e.must("backup", "restore", dir, "NOTE/Note/Recover", "--dry-run")
	e.must("backup", dir)
}

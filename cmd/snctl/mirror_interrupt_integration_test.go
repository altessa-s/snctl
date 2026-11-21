// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build integration

package main

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/clouddouble"
)

func TestMirrorRecoveryAfterInterrupt(t *testing.T) {
	e, dir, oldID := mirrorReplacementFixture(t)
	binary := os.Getenv("SNCTL_TEST_BINARY")
	if binary == "" {
		binary = e.path("snctl")
		if runtime.GOOS == "windows" {
			binary += ".exe"
		}
		if out, err := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	started := make(chan struct{}, 1)
	e.cloud.Inject(clouddouble.Fault{Path: "/storage/upload", Count: 1, Delay: time.Minute, Before: func() { started <- struct{}{} }})
	cmd := exec.CommandContext(t.Context(), binary, "mirror", "run", "papers")
	prepareInterrupt(cmd)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "SystemRoot=" + os.Getenv("SystemRoot"), "TMPDIR=" + os.Getenv("TMPDIR"), "SNCTL_CONFIG_DIR=" + e.path("config/snctl"), "SNCTL_CACHE_DIR=" + e.path("cache/snctl"), "SNCTL_SECRETS=file", "SNCTL_PROFILE=e2e"}
	var diag bytes.Buffer
	cmd.Stderr = &diag
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	select {
	case <-started:
	case <-time.After(20 * time.Second):
		t.Fatal("upload did not start")
	}
	if items := mirrorJournal(t, dir+"/.snctl-mirror.json"); !items[oldID].Recover {
		t.Fatal("interrupt boundary has no intent", items)
	}
	if err := interruptProcess(cmd.Process); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil || cmd.ProcessState.ExitCode() != 130 {
		t.Fatal("interrupt not acknowledged", err, diag.String())
	}
	e.cloud.Seed("DOCUMENT/Document/Papers/a.pdf", false, []byte("tablet edit"))
	assertMirrorTabletConflict(t, e, dir, oldID)
}

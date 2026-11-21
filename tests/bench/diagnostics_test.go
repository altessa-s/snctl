//go:build bench

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package bench_test

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestBenchFailureDiagnostics(t *testing.T) {
	b := bench{binary: "snctl", secrets: []string{"synthetic-account", "synthetic-password"}}
	got := b.failure([]string{"login", "--account", "synthetic-account", "--password-stdin"}, errors.New("private"),
		"fixed server explanation\nsynthetic-account\nsynthetic-password\nCookie: sid=private\nhttps://storage.invalid/?signature=private\nrequest body: private")
	for _, secret := range []string{"synthetic-account", "synthetic-password", "sid=private", "storage.invalid", "signature=private", "body: private"} {
		if strings.Contains(got, secret) {
			t.Fatal("secret leaked")
		}
	}
	for _, want := range []string{`command: "snctl" "login"`, "exit code: -1", "stderr:", "fixed server explanation"} {
		if !strings.Contains(got, want) {
			t.Fatal(got)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestBenchExitHelper$")
	cmd.Env = []string{"SNCTL_TEST_EXIT_HELPER=1"}
	err = cmd.Run()
	if err == nil {
		t.Fatal("helper succeeded")
	}
	got = b.failure([]string{"login", "--account", "synthetic-account", "--password-stdin"}, err, "login failed: synthetic-account synthetic-password")
	if !strings.Contains(got, "exit code: 7") || !strings.Contains(got, "login failed:") || strings.Contains(got, "synthetic-account") || strings.Contains(got, "synthetic-password") {
		t.Fatal(got)
	}
}

func TestBenchExitHelper(t *testing.T) {
	if os.Getenv("SNCTL_TEST_EXIT_HELPER") == "1" {
		os.Exit(7)
	}
}

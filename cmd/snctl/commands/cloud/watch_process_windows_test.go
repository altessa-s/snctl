// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestExecJobDescendants(t *testing.T) {
	const helper = "SNCTL_TEST_JOB_HELPER"
	if role := os.Getenv(helper); role != "" {
		if role == "child" {
			if err := os.WriteFile(os.Getenv("SNCTL_TEST_JOB_PID"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
				t.Fatal(err)
			}
			time.Sleep(time.Minute)
			return
		}
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestExecJobDescendants$")
		cmd.Env = childEnv(os.Environ(), []string{helper + "=child"}, true)
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Parallel()
	for _, timeout := range []bool{false, true} {
		t.Run(strconv.FormatBool(timeout), func(t *testing.T) {
			pidFile := filepath.Join(t.TempDir(), "pid")
			ctx, cancel := context.WithCancel(t.Context())
			if timeout {
				ctx, cancel = context.WithTimeout(t.Context(), 3*time.Second)
			}
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := (execRunner{}).Run(ctx, []string{os.Args[0], "-test.run=^TestExecJobDescendants$"},
					[]string{helper + "=parent", "SNCTL_TEST_JOB_PID=" + pidFile}, nil, io.Discard)
				done <- err
			}()
			var pid uint64
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				data, _ := os.ReadFile(pidFile)
				pid, _ = strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32)
				if pid != 0 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if pid == 0 {
				t.Fatal("descendant did not start")
			}
			process, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
			if err != nil {
				t.Fatal(err)
			}
			defer windows.CloseHandle(process)
			defer windows.TerminateProcess(process, 1)
			if !timeout {
				cancel()
			}
			select {
			case err = <-done:
				if err == nil {
					t.Fatal("canceled command succeeded")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("command did not stop")
			}
			const waitMillis = 5000
			if status, err := windows.WaitForSingleObject(process, waitMillis); err != nil || status != windows.WAIT_OBJECT_0 {
				t.Fatal("descendant survived", status, err)
			}
		})
	}
}

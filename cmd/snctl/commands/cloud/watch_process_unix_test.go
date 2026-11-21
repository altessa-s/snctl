//go:build unix

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
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExecStopsDescendants(t *testing.T) {
	t.Parallel()
	for _, timeout := range []bool{false, true} {
		t.Run(strconv.FormatBool(timeout), func(t *testing.T) {
			dir := t.TempDir()
			pidFile := filepath.Join(dir, "pid")
			ctx, cancel := context.WithCancel(t.Context())
			if timeout {
				ctx, cancel = context.WithTimeout(t.Context(), time.Second)
			}
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := (execRunner{}).Run(ctx, []string{"sh", "-c", `sleep 30 & echo $! > "$1"; wait`, "sh", pidFile}, nil, nil, io.Discard)
				done <- err
			}()
			deadline := time.Now().Add(5 * time.Second)
			var pid int
			for time.Now().Before(deadline) {
				data, _ := os.ReadFile(pidFile)
				pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
				if pid != 0 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if pid == 0 {
				t.Fatal("descendant did not start")
			}
			defer syscall.Kill(pid, syscall.SIGKILL)
			if !timeout {
				cancel()
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("canceled command succeeded")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("command did not stop")
			}
			deadline = time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if syscall.Kill(pid, 0) == syscall.ESRCH {
					return
				}
				// A reparented zombie has stopped but awaits the system reaper.
				if runtime.GOOS == "linux" {
					// Minimal CI containers may have neither ps nor a reaping PID 1.
					stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
					if err == nil && procStatIsZombie(string(stat)) {
						return
					}
				} else {
					stat, _ := exec.CommandContext(t.Context(), "ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
					if strings.HasPrefix(strings.TrimSpace(string(stat)), "Z") {
						return
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("descendant survived cancellation")
		})
	}
}

func procStatIsZombie(stat string) bool {
	// The comm field is parenthesized and may itself contain spaces or ')'.
	end := strings.LastIndexByte(stat, ')')
	if end == -1 {
		return false
	}
	fields := strings.Fields(stat[end+1:])
	return len(fields) > 0 && fields[0] == "Z"
}

func TestProcStatIsZombie(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		stat string
		want bool
	}{
		{name: "zombie", stat: "123 (sleep) Z 1 123 0", want: true},
		{name: "complex comm", stat: "123 (a name ) with (parens)) Z 1 123 0", want: true},
		{name: "sleeping", stat: "123 (sleep) S 1 123 0"},
		{name: "running", stat: "123 (sleep) R 1 123 0"},
		{name: "stopped", stat: "123 (sleep) T 1 123 0"},
		{name: "zombie in comm", stat: "123 (Z) S 1 123 0"},
		{name: "empty"},
		{name: "missing comm", stat: "123 Z 1 123 0"},
		{name: "missing state", stat: "123 (sleep)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := procStatIsZombie(tt.stat); got != tt.want {
				t.Errorf("procStatIsZombie(%q) = %v, want %v", tt.stat, got, tt.want)
			}
		})
	}
}

func TestExecCancellationSendsTERM(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ready, terminated := filepath.Join(dir, "ready"), filepath.Join(dir, "terminated")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", `trap 'echo terminated > "$2"; exit 0' TERM; echo ready > "$1"; while :; do sleep 0.05; done`, "sh", ready, terminated)
	cmd.WaitDelay = time.Second
	done := make(chan error, 1)
	go func() { done <- runCommand(cmd) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("command did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("command did not stop")
	}
	if data, err := os.ReadFile(terminated); err != nil || strings.TrimSpace(string(data)) != "terminated" {
		t.Fatalf("TERM trap did not run: %q, %v", data, err)
	}
}

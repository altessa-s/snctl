// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package secretfile

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/adapter/flock"
	"github.com/altessa-s/snctl/internal/port"
)

func TestTokenLockProcess(t *testing.T) {
	if path := os.Getenv("SNCTL_TEST_TOKEN_LOCK"); path != "" {
		s := New(path)
		for _, update := range []func(context.Context) error{
			func(ctx context.Context) error { return s.Save(ctx, port.SecretAccessToken, "new") },
			func(ctx context.Context) error { return s.Forget(ctx, port.SecretAccount) },
		} {
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			err := update(ctx)
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("write bypassed process lock: %v", err)
			}
		}
		return
	}
	t.Parallel()
	path := filepath.Join(t.TempDir(), "token.json")
	s := New(path)
	if err := s.Save(t.Context(), port.SecretAccount, "keep"); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if busy, err := flock.TryLock(f); busy || err != nil {
		t.Fatal(busy, err)
	}
	defer flock.Unlock(f)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	err = New(path).Save(ctx, port.SecretClientID, "blocked")
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTokenLockProcess$")
	cmd.Env = append(os.Environ(), "SNCTL_TEST_TOKEN_LOCK="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if err = flock.Unlock(f); err != nil {
		t.Fatal(err)
	}
	if err = New(path).Save(t.Context(), port.SecretAccessToken, "new"); err != nil {
		t.Fatal(err)
	}
	if v, _, err := s.Secret(t.Context(), port.SecretAccount); v != "keep" || err != nil {
		t.Fatal(v, err)
	}
}

func TestTokenLockFailures(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "token.json")
	if err := os.Mkdir(path+".lock", dirMode); err != nil {
		t.Fatal(err)
	}
	if err := New(path).Save(t.Context(), port.SecretAccount, "x"); err == nil {
		t.Fatal("directory lock accepted")
	}
}

func TestForgetAbsentDoesNotCreateFiles(t *testing.T) {
	t.Parallel()
	for _, missingDir := range []bool{false, true} {
		dir := t.TempDir()
		path := filepath.Join(dir, "token.json")
		if missingDir {
			path = filepath.Join(dir, "missing", "token.json")
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := New(path).Forget(ctx, port.SecretAccount); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("absent logout created files: %v, %v", entries, err)
		}
	}
}

func TestUncontendedCanceledUpdate(t *testing.T) {
	t.Parallel()
	s := New(filepath.Join(t.TempDir(), "token.json"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Save(ctx, port.SecretAccount, "synthetic"); err != nil {
		t.Fatal(err)
	}
	if err := s.Forget(ctx, port.SecretAccount); err != nil {
		t.Fatal(err)
	}
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wiring

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/adapter/config"
)

func TestMirrorTimerCancellationAndWake(t *testing.T) {
	t.Parallel()
	tm := mirrorTimer{}
	if tm.Now().IsZero() {
		t.Fatal("clock has no time")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := tm.Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := tm.Sleep(t.Context(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
}
func TestMirrorCompositionRejectsBrokenSettings(t *testing.T) {
	t.Parallel()
	readErr := errors.New("unreadable configuration")
	cfg := mirrorConfig{writer: config.Writer{Path: "unused", Read: func(string) ([]byte, bool, error) { return nil, false, readErr }}}
	if _, err := cfg.Mirrors(); !errors.Is(err, readErr) {
		t.Fatal(err)
	}
	e := engines{opts: Options{Dirs: config.BaseDirs{Config: t.TempDir(), Cache: t.TempDir()}}}
	if _, _, err := e.Open("Bad profile"); !errors.Is(err, config.ErrProfile) {
		t.Fatal(err)
	}
}

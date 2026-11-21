// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// SyncLockState keeps uncertain lock ownership across process exits.
type SyncLockState struct{ Path string }

// Pending reports whether a previous attempt may still own the remote lock.
func (s SyncLockState) Pending() (bool, error) {
	_, err := os.Lstat(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// Mark persists a timestamp before acquisition can reach the server.
func (s SyncLockState) Mark() error {
	if err := (Files{}).MkdirAll(filepath.Dir(s.Path)); err != nil {
		return err
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano) //nolint:forbidigo // diagnostic wall-clock timestamp persisted across processes
	return (Files{}).Produce(context.Background(), s.Path, func(w io.Writer) error { _, err := io.WriteString(w, stamp+"\n"); return err })
}

// Clear forgets ownership only when release is known to have succeeded.
func (s SyncLockState) Clear() error {
	err := os.Remove(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

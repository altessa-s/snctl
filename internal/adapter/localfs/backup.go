// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"context"
	"errors"
	"os"

	"github.com/altessa-s/snctl/internal/adapter/flock"
	"github.com/altessa-s/snctl/internal/port"
)

// OpenBackup pins a directory and takes its persistent exclusive advisory lock.
func (f Files) OpenBackup(ctx context.Context, dir string, create bool) (port.BackupDirectory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if create {
		if err := os.MkdirAll(dir, outputDirectoryMode); err != nil {
			return nil, err
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	d := &backupDirectory{outputDirectory: &outputDirectory{root: root}}
	if err = d.check(".snctl-backup.lock"); err == nil {
		d.lock, err = root.OpenFile(".snctl-backup.lock", os.O_CREATE|os.O_RDWR, outputFileMode)
	}
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	busy, err := flock.TryLock(d.lock)
	if busy {
		err = errors.New("backup directory is busy")
	}
	if err != nil {
		_ = d.lock.Close()
		_ = root.Close()
		return nil, err
	}
	return d, nil
}

type backupDirectory struct {
	*outputDirectory
	lock *os.File
}

func (d *backupDirectory) Close() error {
	return errors.Join(flock.Unlock(d.lock), d.lock.Close(), d.outputDirectory.Close())
}

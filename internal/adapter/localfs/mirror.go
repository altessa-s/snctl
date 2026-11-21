// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/altessa-s/snctl/internal/adapter/flock"
	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
)

// Scan lists regular files without traversing symbolic links.
func (Files) Scan(dir string, prune func(string) bool) ([]port.LocalEntry, error) {
	var out []port.LocalEntry
	err := filepath.WalkDir(dir, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, name)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if prune != nil && prune(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, port.LocalEntry{Path: rel, Size: info.Size(), ModTime: info.ModTime()})
		return nil
	})
	return out, err
}

// OpenMirror pins an existing directory and holds its nonblocking advisory lock.
func (Files) OpenMirror(ctx context.Context, dir string) (port.MirrorDirectory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	d := &mirrorDirectory{outputDirectory: &outputDirectory{root: root}}
	if err = d.check(domain.MirrorLockName); err == nil {
		d.lock, err = root.OpenFile(domain.MirrorLockName, os.O_CREATE|os.O_RDWR, outputFileMode)
	}
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	busy, err := flock.TryLock(d.lock)
	if busy {
		err = port.ErrMirrorBusy
	}
	if err != nil {
		_ = d.lock.Close()
		_ = root.Close()
		return nil, err
	}
	return d, nil
}

type mirrorDirectory struct {
	*outputDirectory
	lock    *os.File
	link    func(string, string) error
	install func(string, string) error
}

func (d *mirrorDirectory) RootEntries() ([]port.LocalEntry, error) {
	f, err := d.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	children, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	var entries []port.LocalEntry
	for _, child := range children {
		if child.Type().IsRegular() {
			entries = append(entries, port.LocalEntry{Path: child.Name()})
		}
	}
	return entries, nil
}

func (d *mirrorDirectory) Close() error {
	return errors.Join(flock.Unlock(d.lock), d.lock.Close(), d.outputDirectory.Close())
}

// SameFile distinguishes a case-only alias from distinct names on this filesystem.
func (d *mirrorDirectory) SameFile(from, to string) (bool, error) {
	for _, name := range []string{from, to} {
		if err := d.check(name); err != nil {
			return false, err
		}
	}
	a, err := d.root.Stat(from)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	b, err := d.root.Stat(to)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(a, b), nil
}

// Move installs a regular file into an unoccupied name before removing its old name.
func (d *mirrorDirectory) Move(from, to string) error {
	for _, name := range []string{from, to} {
		if err := d.check(name); err != nil {
			return err
		}
	}
	info, err := d.root.Stat(from)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fs.ErrInvalid
	}
	if err = d.MkdirAll(filepath.Dir(to)); err != nil {
		return err
	}
	if target, statErr := d.root.Stat(to); statErr == nil && os.SameFile(info, target) {
		return d.root.Rename(from, to)
	}
	link := d.link
	if link == nil {
		link = d.root.Link
	}
	if err = link(from, to); err != nil {
		if !errors.Is(err, syscall.ENOTSUP) && !errors.Is(err, syscall.EPERM) && !errors.Is(err, syscall.EXDEV) && !unsupportedLink(err) {
			return err
		}
		return d.copyMove(from, to, info)
	}
	return d.root.Remove(from)
}

// Publish installs new bytes without replacing a destination created meanwhile.
func (d *mirrorDirectory) Publish(ctx context.Context, to string, write func(io.Writer) error) error {
	if err := d.check(to); err != nil {
		return err
	}
	if err := d.MkdirAll(filepath.Dir(to)); err != nil {
		return err
	}
	temp := ".snctl-mirror-" + rand.Text()
	if err := d.Produce(ctx, temp, write); err != nil {
		return err
	}
	defer func() { _ = d.Remove(temp) }()
	return d.Move(temp, to)
}

// copyMove stages complete bytes beside the destination before installing them.
// The source survives every unsuccessful copy or installation.
func (d *mirrorDirectory) copyMove(from, to string, info fs.FileInfo) error {
	src, err := d.root.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	temp := filepath.Join(filepath.Dir(to), ".snctl-mirror-"+rand.Text())
	dst, err := d.root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer func() { _ = d.root.Remove(temp) }()
	_, copyErr := io.Copy(dst, src)
	if err = errors.Join(copyErr, dst.Sync(), dst.Close()); err != nil {
		return err
	}
	if err = d.root.Chtimes(temp, info.ModTime(), info.ModTime()); err != nil {
		return err
	}
	install := d.install
	if install == nil {
		install = func(from, to string) error { return installExclusive(d.root, from, to) }
	}
	if err = install(temp, to); err != nil {
		return err
	}
	return d.root.Remove(from)
}

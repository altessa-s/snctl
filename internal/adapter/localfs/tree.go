// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"crypto/md5" // #nosec G501 -- the cloud's content hash is MD5; it is compared, never trusted for security.
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/altessa-s/snctl/internal/port"
)

// Info observes a path without following a final symbolic link, so that a
// link is never mistaken for the file a transfer recorded.
func (Files) Info(path string) (port.FileInfo, error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return port.FileInfo{}, nil
	}
	if err != nil {
		return port.FileInfo{}, err
	}
	return port.FileInfo{Exists: true, Regular: fi.Mode().IsRegular(), Dir: fi.IsDir(), Size: fi.Size(), Modified: fi.ModTime()}, nil
}

// Walk lists the entries below dir in lexical order. Links are reported as
// neither files nor directories and are not followed, so an upload never
// leaves the directory it was given.
func (Files) Walk(dir string) ([]port.LocalFile, error) {
	var out []port.LocalFile
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		f := port.LocalFile{Rel: filepath.ToSlash(rel), Dir: d.IsDir(), Regular: d.Type().IsRegular()}
		if f.Regular {
			info, infoErr := d.Info()
			if infoErr != nil {
				return infoErr
			}
			f.Size = info.Size()
		}
		out = append(out, f)
		return nil
	})
	return out, err
}

// Hash is the lowercase hexadecimal MD5 of the whole file: the value
// sncloud-go sends as content_hash on upload and the listing reports back.
func (Files) Hash(path string) (string, error) {
	f, err := open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := md5.New() // #nosec G401 -- the protocol's content hash, compared for equality only.
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

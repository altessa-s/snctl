// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package archives

import (
	"context"
	"crypto/md5" // #nosec G501 -- Supernote content identifiers.
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"path"
	"path/filepath"
	"strings"

	"github.com/altessa-s/snctl/internal/port"
)

// Restore verifies and stages every file before the cloud upload plan can run.
// With a destination, paths relative to the backup scope go below that folder.
func (s Service) Restore(ctx context.Context, dir, to string, o port.UploadOptions) (port.TransferReport, error) {
	root, err := s.Local.OpenBackup(ctx, dir, false)
	if err != nil {
		return port.TransferReport{}, err
	}
	defer func() { _ = root.Close() }()
	m, err := readManifest(root)
	if err != nil {
		return port.TransferReport{}, err
	}
	if m.Version == 0 {
		return port.TransferReport{}, errors.New("no manifest.json: not a backup directory")
	}
	if len(m.Files) == 0 {
		return port.TransferReport{}, errors.New("the backup holds no files")
	}
	work := make([]port.RestoreFile, 0, len(m.Files))
	defer func() {
		for _, f := range work {
			_ = s.Local.Remove(f.Local)
		}
	}()
	for _, e := range m.Files {
		target, err := restorePath(m.Path, e.Path, to)
		if err != nil {
			return port.TransferReport{}, err
		}
		if err = verify(ctx, root, e); err != nil {
			return port.TransferReport{}, err
		}
		in, err := root.Open(e.Stored)
		if err != nil {
			return port.TransferReport{}, err
		}
		digest := md5.New() // #nosec G401 -- Verify the exact bytes staged for upload.
		staged, stageErr := s.Local.Stage(&verifiedReader{contextReader: contextReader{ctx, in}, entry: e, digest: digest})
		err = errors.Join(stageErr, in.Close())
		if err != nil {
			if staged != "" {
				_ = s.Local.Remove(staged)
			}
			return port.TransferReport{}, fmt.Errorf("%s: %w", e.Path, err)
		}
		work = append(work, port.RestoreFile{Local: staged, Display: filepath.Join(dir, filepath.FromSlash(e.Stored)), Path: target})
	}
	return s.Upload.RestoreFiles(ctx, work, o)
}

func restorePath(scope, recorded, to string) (string, error) {
	if to == "" {
		return path.Join("/", recorded), nil
	}
	full := "/" + recorded
	if scope != path.Clean("/"+scope) || !under(full, scope) {
		return "", fmt.Errorf("%s: invalid backup scope %q", recorded, scope)
	}
	relative := strings.TrimPrefix(full, strings.TrimSuffix(scope, "/")+"/")
	// A backup bound to one file restores that file's name into the folder.
	if full == scope {
		relative = path.Base(recorded)
	}
	return path.Join("/", to, relative), nil
}

// verifiedReader checks the same stream staging consumes, closing the gap between
// initial local validation and a later read if another process edits the file.
type verifiedReader struct {
	contextReader
	entry  port.BackupEntry
	digest hash.Hash
	count  int64
}

func (r *verifiedReader) Read(b []byte) (int, error) {
	n, err := r.contextReader.Read(b)
	_, _ = r.digest.Write(b[:n])
	r.count += int64(n)
	if errors.Is(err, io.EOF) && (r.count != r.entry.Size || hex.EncodeToString(r.digest.Sum(nil)) != r.entry.MD5) {
		return n, fmt.Errorf("%s: backup changed during staging", r.entry.Path)
	}
	return n, err
}

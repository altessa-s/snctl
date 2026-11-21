// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"crypto/md5" // #nosec G501 -- Supernote content identifiers.
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"time"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const settle = 2 * time.Second
const stillWriting = "still being written"

type localFile struct {
	port.LocalEntry
	sha, md5 string
}

func (f *localFile) sameStat(e entry) bool { return f.Size == e.Size && f.ModTime.Equal(e.ModTime) }
func (f *localFile) settled(now time.Time) bool {
	d := now.Sub(f.ModTime)
	return d >= settle || d < -time.Hour
}

type local struct {
	files map[string]*localFile
	forms map[string]string
	all   int
}

func foldName(s string) string { return cases.Fold().String(norm.NFC.String(s)) }
func (l local) find(rel string) (*localFile, bool) {
	if f, ok := l.files[rel]; ok {
		return f, true
	}
	if p := l.forms[foldName(rel)]; p != "" {
		return l.files[p], true
	}
	return nil, false
}
func (l local) paths() []string {
	out := make([]string, 0, len(l.files))
	for p := range l.files {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}
func scan(disk port.MirrorDisk, dir string) (local, error) {
	entries, err := disk.Scan(dir, pruned)
	if err != nil {
		return local{}, err
	}
	out := local{files: map[string]*localFile{}, forms: map[string]string{}}
	for _, e := range entries {
		if domain.MirrorIgnored(e.Path) {
			continue
		}
		out.all++
		if !domain.MirrorUploadable(e.Path) {
			continue
		}
		key := foldName(e.Path)
		if _, ok := out.forms[key]; ok {
			return local{}, fmt.Errorf("%w: ambiguous local names: %s and %s", port.ErrMirrorRefused, out.forms[key], e.Path)
		}
		out.files[e.Path] = &localFile{LocalEntry: e}
		out.forms[key] = e.Path
	}
	return out, nil
}
func pruned(rel string) bool { return domain.MirrorIgnored(rel + "/x") }
func signature(l local) string {
	h := sha256.New()
	for _, p := range l.paths() {
		f := l.files[p]
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00%d\n", p, f.Size, f.ModTime.UnixNano())
	}
	_, _ = fmt.Fprintf(h, "%d", l.all)
	return hex.EncodeToString(h.Sum(nil))
}
func hash(root port.OutputDirectory, f *localFile) error {
	if f.sha != "" {
		return nil
	}
	in, err := root.Open(f.Path)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	sha, m := sha256.New(), md5.New() // #nosec G401 -- Supernote content identifiers.
	n, err := io.Copy(io.MultiWriter(sha, m), in)
	if err != nil {
		return err
	}
	info, err := root.Info(f.Path)
	if err != nil {
		return err
	}
	if !info.Regular || n != f.Size || info.Size != f.Size || !info.Modified.Equal(f.ModTime) {
		return errors.New(stillWriting)
	}
	f.sha, f.md5 = hex.EncodeToString(sha.Sum(nil)), hex.EncodeToString(m.Sum(nil))
	return nil
}
func abs(dir, rel string) string { return filepath.Join(dir, filepath.FromSlash(rel)) }
func checkDir(files port.TreeFiles, dir string) error {
	info, err := files.Info(dir)
	if err != nil {
		return err
	}
	if !info.Dir {
		return fmt.Errorf("%s: %w: the directory is not there; is its drive mounted?", dir, fs.ErrNotExist)
	}
	return nil
}

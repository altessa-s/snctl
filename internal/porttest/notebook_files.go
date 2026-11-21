// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest

import (
	"bytes"
	"context"
	"crypto/md5" // #nosec G501 -- the cloud's content hash is MD5.
	"encoding/hex"
	"io"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/altessa-s/snctl/internal/port"
)

// NotebookFiles is an in-memory disk with atomic writes and injectable failures.
type NotebookFiles struct {
	mu     sync.Mutex
	Data   map[string][]byte
	Times  map[string]time.Time
	FailAt string
	Err    error
}

func (f *NotebookFiles) failure(op string) error {
	if f.FailAt == op {
		return f.Err
	}
	return nil
}

// Open reads a stable copy so concurrent output cannot change an active reader.
func (f *NotebookFiles) Open(name string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure("open"); err != nil {
		return nil, err
	}
	b, ok := f.Data[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(bytes.Clone(b))), nil
}

// Stage retains bytes under a predictable temporary name.
func (f *NotebookFiles) Stage(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Data == nil {
		f.Data = map[string][]byte{}
	}
	f.Data["stage"] = b
	return "stage", nil
}

// Remove forgets a staged file.
func (f *NotebookFiles) Remove(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.Data, name)
	return f.failure("remove")
}

// Save copies bytes to the supplied destination or stdout.
func (f *NotebookFiles) Save(source, dest, _ string, w io.Writer) error {
	r, err := f.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	if dest == "-" {
		_, err = io.Copy(w, r)
		return err
	}
	return f.Produce(context.Background(), dest, func(w io.Writer) error { _, err := io.Copy(w, r); return err })
}

// Produce commits bytes only if the writer succeeds and the context remains active.
func (f *NotebookFiles) Produce(ctx context.Context, name string, write func(io.Writer) error) error {
	if err := f.failure("produce"); err != nil {
		return err
	}
	var b bytes.Buffer
	if err := write(&b); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Data == nil {
		f.Data = map[string][]byte{}
	}
	f.Data[name] = bytes.Clone(b.Bytes())
	return nil
}

// Destination models the current directory default.
func (f *NotebookFiles) Destination(dest, name string) (string, error) {
	if dest == "" {
		dest = name
	}
	return dest, f.failure("destination")
}

// MkdirAll records no directories because names are independent map keys.
func (f *NotebookFiles) MkdirAll(string) error { return f.failure("mkdir") }

// ModTime returns the timestamp of an existing output.
func (f *NotebookFiles) ModTime(name string) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.Data[name]; !ok {
		return time.Time{}, fs.ErrNotExist
	}
	return f.Times[name], f.failure("mtime")
}

// SetModTime records a source timestamp.
func (f *NotebookFiles) SetModTime(name string, t time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Times == nil {
		f.Times = map[string]time.Time{}
	}
	f.Times[name] = t
	return f.failure("settime")
}

// PreviewDir returns a deterministic private preview path.
func (f *NotebookFiles) PreviewDir() (string, error) {
	return filepath.Join("preview", "private"), f.failure("preview")
}

// LocalInput models existing files and injected read failures.
func (f *NotebookFiles) LocalInput(name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure("open"); err != nil {
		return false, err
	}
	_, ok := f.Data[name]
	return ok, nil
}

// IsDir models explicit output containers.
func (*NotebookFiles) IsDir(string) bool { return false }

// Info reports a stored file; directories exist implicitly as prefixes.
func (f *NotebookFiles) Info(name string) (port.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure("info"); err != nil {
		return port.FileInfo{}, err
	}
	if b, ok := f.Data[name]; ok {
		return port.FileInfo{Exists: true, Regular: true, Size: int64(len(b)), Modified: f.Times[name]}, nil
	}
	for key := range f.Data {
		if strings.HasPrefix(key, name+string(filepath.Separator)) {
			return port.FileInfo{Exists: true, Dir: true}, nil
		}
	}
	return port.FileInfo{}, nil
}

// Walk lists the stored files below dir in lexical order.
func (f *NotebookFiles) Walk(dir string) ([]port.LocalFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure("walk"); err != nil {
		return nil, err
	}
	var out []port.LocalFile
	for _, key := range slices.Sorted(maps.Keys(f.Data)) {
		if rel, ok := strings.CutPrefix(key, dir+string(filepath.Separator)); ok {
			out = append(out, port.LocalFile{Rel: filepath.ToSlash(rel), Regular: true, Size: int64(len(f.Data[key]))})
		}
	}
	return out, nil
}

// Hash is the content hash definition the cloud uses.
func (f *NotebookFiles) Hash(name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure("hash"); err != nil {
		return "", err
	}
	b, ok := f.Data[name]
	if !ok {
		return "", fs.ErrNotExist
	}
	return ContentHash(b), nil
}

// ContentHash is the lowercase hexadecimal MD5 the cloud lists for data.
func ContentHash(data []byte) string {
	sum := md5.Sum(data) // #nosec G401 -- the protocol's content hash, compared for equality only.
	return hex.EncodeToString(sum[:])
}

// OpenRoot models a directory handle with names relative to its prefix.
func (f *NotebookFiles) OpenRoot(dir string) (port.OutputDirectory, error) {
	if err := f.MkdirAll(dir); err != nil {
		return nil, err
	}
	return &outputDirectory{files: f, dir: dir}, nil
}

type outputDirectory struct {
	files *NotebookFiles
	dir   string
}

func (d *outputDirectory) Close() error { return nil }
func (d *outputDirectory) Open(n string) (io.ReadCloser, error) {
	return d.files.Open(filepath.Join(d.dir, n))
}
func (d *outputDirectory) Info(n string) (port.FileInfo, error) {
	return d.files.Info(filepath.Join(d.dir, n))
}
func (d *outputDirectory) MkdirAll(n string) error { return d.files.MkdirAll(filepath.Join(d.dir, n)) }
func (d *outputDirectory) Remove(n string) error   { return d.files.Remove(filepath.Join(d.dir, n)) }
func (d *outputDirectory) SetModTime(n string, t time.Time) error {
	return d.files.SetModTime(filepath.Join(d.dir, n), t)
}
func (d *outputDirectory) Produce(ctx context.Context, n string, w func(io.Writer) error) error {
	return d.files.Produce(ctx, filepath.Join(d.dir, n), w)
}

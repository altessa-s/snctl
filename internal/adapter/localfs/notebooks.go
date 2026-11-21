// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/altessa-s/snctl/internal/port"
)

// Produce installs an output only after its writer and the final cancellation check succeed.
func (Files) Produce(ctx context.Context, path string, write func(io.Writer) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".snctl-render-*")
	if err != nil {
		return fmt.Errorf("destination directory %s: %w", filepath.Dir(path), errors.Unwrap(err))
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	err = errors.Join(write(f), f.Sync(), f.Close())
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Destination interprets an existing directory as a container for a safe basename.
func (Files) Destination(dest, name string) (string, error) {
	if dest == "" {
		dest = "."
	}
	fi, err := os.Stat(dest)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if errors.Is(err, os.ErrNotExist) {
		// Windows reports a missing path even when an ancestor is a file.
		// A destination may have missing parents, but no non-directory parent.
		for parent := filepath.Dir(dest); ; parent = filepath.Dir(parent) {
			info, parentErr := os.Stat(parent)
			if parentErr == nil {
				if !info.IsDir() {
					return "", fmt.Errorf("%s: not a directory", parent)
				}
				break
			}
			if !errors.Is(parentErr, os.ErrNotExist) {
				return "", parentErr
			}
			if filepath.Dir(parent) == parent {
				break
			}
		}
	}
	if err == nil && fi.IsDir() {
		dest = filepath.Join(dest, filepath.Base(name))
	}
	return dest, nil
}

// MkdirAll creates private export directories.
func (Files) MkdirAll(path string) error {
	const privateDirectory = 0700
	return os.MkdirAll(path, privateDirectory)
}

// ModTime observes whether a prior export still exists and is unchanged.
func (Files) ModTime(path string) (time.Time, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	if !fi.Mode().IsRegular() {
		return time.Time{}, errors.New("output is not a regular file")
	}
	return fi.ModTime(), nil
}

// SetModTime records the source timestamp on a completed export.
func (Files) SetModTime(path string, t time.Time) error { return os.Chtimes(path, t, t) }

// impossibleLocalName reports a query no local file can have: on Windows a
// name with : * ? " < > | after the volume fails Stat with a syntax error
// rather than "not found", yet it is still a valid cloud name.
func impossibleLocalName(query string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	return strings.ContainsAny(strings.TrimPrefix(query, filepath.VolumeName(query)), `:*?"<>|`)
}

// LocalInput gives existing regular files precedence over ambiguous cloud names.
func (Files) LocalInput(query string) (bool, error) {
	info, err := os.Stat(query)
	explicit := strings.HasPrefix(query, "./") || strings.HasPrefix(query, "../") ||
		strings.HasPrefix(query, "."+string(filepath.Separator)) || strings.HasPrefix(query, ".."+string(filepath.Separator))
	ext := strings.ToLower(filepath.Ext(query))
	notebook := ext == ".note" || ext == ".mark" || ext == ".spd"
	if err == nil {
		if info.IsDir() {
			if explicit {
				return false, fmt.Errorf("%w: a notebook file is needed; local folders are not accepted", port.ErrOptions)
			}
			return false, nil
		}
		return info.Mode().IsRegular(), nil
	}
	if !errors.Is(err, os.ErrNotExist) && !impossibleLocalName(query) {
		return false, err
	}
	if explicit || filepath.IsAbs(query) && !strings.HasPrefix(query, "/") {
		return false, fmt.Errorf("%s: %w", query, os.ErrNotExist)
	}
	if strings.HasPrefix(query, "/") {
		// An existing parent distinguishes an absolute local typo from a cloud root.
		if parent, e := os.Stat(filepath.Dir(query)); e == nil && parent.IsDir() && filepath.Dir(query) != filepath.Clean("/") {
			return false, fmt.Errorf("%s: %w", query, os.ErrNotExist)
		}
		return false, nil
	}
	if notebook {
		if parent, e := os.Stat(filepath.Dir(query)); e == nil && parent.IsDir() {
			return false, missingNotebook(query)
		}
	}
	return false, nil
}

// IsDir distinguishes an output container from an output filename.
func (Files) IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func missingNotebook(query string) error {
	top, _, _ := strings.Cut(filepath.ToSlash(query), "/")
	hint := top == "NOTE" || top == "DOCUMENT" || top == "Note" || top == "Document" || top == "MyStyle"
	for dir := filepath.Dir(query); !hint && dir != "." && dir != string(filepath.Separator); dir = filepath.Dir(dir) {
		for _, record := range []string{".snctl-render.json", ".snctl-mget.json"} {
			if _, err := os.Stat(filepath.Join(dir, record)); err == nil {
				hint = true
			}
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	if hint {
		return fmt.Errorf("%s: %w; a leading / addresses the cloud", query, os.ErrNotExist)
	}
	return fmt.Errorf("%s: %w", query, os.ErrNotExist)
}

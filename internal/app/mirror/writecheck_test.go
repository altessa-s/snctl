// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package mirror

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/port"
)

type warningLibrary struct {
	port.Library
	mkdir, move error
}

func (l warningLibrary) Mkdir(ctx context.Context, p string, parents bool, opts ...port.ChangeOptions) (port.Entry, error) {
	e, err := l.Library.Mkdir(ctx, p, parents, opts...)
	return e, errors.Join(err, l.mkdir)
}
func (l warningLibrary) Move(ctx context.Context, from, to string) error {
	return errors.Join(l.Library.Move(ctx, from, to), l.move)
}

type warningUpload struct{ port.BackupUploader }

func (u warningUpload) RestoreFiles(ctx context.Context, f []port.RestoreFile, o port.UploadOptions) (port.TransferReport, error) {
	r, err := u.BackupUploader.RestoreFiles(ctx, f, o)
	return r, errors.Join(err, port.ErrWriteVerification)
}

func TestAppliedVerificationWarnings(t *testing.T) {
	for _, scenario := range []string{"mkdir-root", "mkdir-move", "move", "upload"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t)
			if scenario == "mkdir-root" {
				f.spec.Folder += "/new"
			}
			if scenario == "mkdir-move" || scenario == "move" {
				f.write("old.pdf", "same")
				f.pass(port.MirrorRunOptions{})
				if err := os.Rename(filepath.Join(f.dir, "old.pdf"), filepath.Join(f.dir, "new.pdf")); err != nil {
					t.Fatal(err)
				}
			} else {
				f.write("new.pdf", "same")
			}
			f.write("z.pdf", "continue")
			lib := warningLibrary{Library: f.e.library}
			if scenario == "move" {
				lib.move = port.ErrWriteVerification
			} else if strings.HasPrefix(scenario, "mkdir") {
				lib.mkdir = port.ErrWriteVerification
			}
			f.e.library = lib
			if scenario == "upload" {
				f.e.upload = warningUpload{f.e.upload}
			}
			p := f.pass(port.MirrorRunOptions{})
			if p.Pending != 0 {
				t.Fatal(p)
			}
			warnings := 0
			for _, a := range p.Actions {
				warnings += len(a.Warnings)
				if a.Kind == port.MirrorWarning {
					warnings++
				}
				if a.Err != nil {
					t.Fatal(a)
				}
			}
			if warnings == 0 {
				t.Fatal("lost warning", p)
			}
			root, err := f.e.disk.OpenMirror(t.Context(), f.dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			j, err := readJournal(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(j.Items) != 2 {
				t.Fatal(j, p)
			}
			for _, en := range j.Items {
				if en.Recover || en.Path == "old.pdf" {
					t.Fatal(en)
				}
			}
		})
	}
}

func TestVerificationWarningPreservesOtherFailures(t *testing.T) {
	r := &run{}
	a := port.MirrorAction{}
	err := r.applied(errors.Join(errBoom, port.ErrWriteVerification), &a)
	if !errors.Is(err, errBoom) || len(a.Warnings) != 1 {
		t.Fatal(err, a)
	}
	err = r.applied(errors.Join(context.Canceled, port.ErrWriteVerification), &a)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

type changedRemove struct {
	port.Library
	before func()
}

func (l changedRemove) Remove(ctx context.Context, e port.Entry, permanent bool) error {
	l.before()
	return l.Library.Remove(ctx, e, permanent)
}

func TestTrashReportsChangedHash(t *testing.T) {
	f := setup(t)
	f.write("a.pdf", "initial")
	f.write("keep.pdf", "keep")
	f.pass(port.MirrorRunOptions{})
	if err := os.Remove(filepath.Join(f.dir, "a.pdf")); err != nil {
		t.Fatal(err)
	}
	f.e.library = changedRemove{Library: f.e.library, before: func() { f.remoteChange("a.pdf", "tablet edit") }}
	p, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{})
	if !errors.Is(err, port.ErrConflict) {
		t.Fatal(err, p)
	}
	for _, a := range p.Actions {
		if a.Kind == port.MirrorTrash {
			if !strings.Contains(a.Detail, "hash") || !strings.Contains(a.Detail, "next pass will re-plan") || !strings.Contains(a.Detail, "source changed") {
				t.Fatal(a)
			}
			if len(f.c.trash) != 0 {
				t.Fatal("deleted changed cloud original")
			}
			return
		}
	}
	t.Fatal("missing trash conflict", p)
}

func TestAppliedWrappedVerificationFailures(t *testing.T) {
	for _, cause := range []error{errBoom, port.ErrBusy, port.ErrSession, context.Canceled} {
		t.Run(cause.Error(), func(t *testing.T) {
			r := &run{}
			a := port.MirrorAction{}
			err := fmt.Errorf("write happened; cannot verify destinations: %w; no rollback", errors.Join(port.ErrWriteVerification, cause))
			if got := r.applied(err, &a); got != nil || !errors.Is(r.failure, cause) || len(a.Warnings) != 0 {
				t.Fatal(got, r.failure, a)
			}
			wrapped := fmt.Errorf("outer: %w", errors.Join(fmt.Errorf("twins: %w", port.ErrWriteVerification), cause))
			if verificationOnly(wrapped) {
				t.Fatal("classified failed listing as pure verification")
			}
		})
	}
	if !verificationOnly(fmt.Errorf("twins: %w", port.ErrWriteVerification)) {
		t.Fatal("lost pure twin warning")
	}
}

type failedVerificationUpload struct {
	port.BackupUploader
	cause error
}

func (u failedVerificationUpload) RestoreFiles(ctx context.Context, f []port.RestoreFile, o port.UploadOptions) (port.TransferReport, error) {
	r, err := u.BackupUploader.RestoreFiles(ctx, f, o)
	return r, errors.Join(err, fmt.Errorf("write happened; verification listing failed: %w", errors.Join(port.ErrWriteVerification, u.cause)))
}

func TestFailedVerificationRecordsAppliedUpload(t *testing.T) {
	for _, cause := range []error{errBoom, port.ErrBusy} {
		t.Run(cause.Error(), func(t *testing.T) {
			f := setup(t)
			f.write("new.pdf", "uploaded")
			f.e.upload = failedVerificationUpload{BackupUploader: f.e.upload, cause: cause}
			p, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{})
			if !errors.Is(err, cause) || p.Pending != 0 {
				t.Fatal(err, p)
			}
			root, err := f.e.disk.OpenMirror(t.Context(), f.dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			j, err := readJournal(root)
			if err != nil || len(j.Items) != 1 || j.LastError == "" {
				t.Fatal(j, err)
			}
			for id, en := range j.Items {
				if en.Path != "new.pdf" || en.Recover || f.c.data[id] != "uploaded" {
					t.Fatal(id, en)
				}
			}
		})
	}
}

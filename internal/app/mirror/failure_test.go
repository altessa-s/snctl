// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/adapter/localfs"
	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
)

type faultDisk struct {
	port.MirrorDisk
	scanErr error
	root    func(port.MirrorDirectory) port.MirrorDirectory
}

func (d faultDisk) Scan(dir string, prune func(string) bool) ([]port.LocalEntry, error) {
	if d.scanErr != nil {
		return nil, d.scanErr
	}
	return d.MirrorDisk.Scan(dir, prune)
}
func (d faultDisk) OpenMirror(ctx context.Context, dir string) (port.MirrorDirectory, error) {
	r, err := d.MirrorDisk.OpenMirror(ctx, dir)
	if err == nil && d.root != nil {
		return d.root(r), nil
	}
	return r, err
}

type faultRoot struct {
	port.MirrorDirectory
	fail string
}

func (r faultRoot) Open(p string) (io.ReadCloser, error) {
	if r.fail == "open" || r.fail == "journal" && p == domain.MirrorJournalName {
		return nil, errBoom
	}
	return r.MirrorDirectory.Open(p)
}
func (r faultRoot) Info(p string) (port.FileInfo, error) {
	if r.fail == "info" {
		return port.FileInfo{}, errBoom
	}
	return r.MirrorDirectory.Info(p)
}
func (r faultRoot) Produce(ctx context.Context, p string, fn func(io.Writer) error) error {
	if r.fail == "produce" || r.fail == "write-journal" && p == domain.MirrorJournalName {
		return errBoom
	}
	return r.MirrorDirectory.Produce(ctx, p, fn)
}
func (r faultRoot) Publish(ctx context.Context, p string, fn func(io.Writer) error) error {
	if r.fail == "publish" {
		return errBoom
	}
	return r.MirrorDirectory.Publish(ctx, p, fn)
}
func (r faultRoot) Move(a, b string) error {
	if r.fail == "move" {
		return errBoom
	}
	return r.MirrorDirectory.Move(a, b)
}
func (r faultRoot) MkdirAll(p string) error {
	if r.fail == "mkdir" {
		return errBoom
	}
	return r.MirrorDirectory.MkdirAll(p)
}

type faultFiles struct {
	port.TreeFiles
	fail string
}

func (f faultFiles) Stage(r io.Reader) (string, error) {
	if f.fail == "stage" {
		return "", errBoom
	}
	return f.TreeFiles.Stage(r)
}
func (f faultFiles) Hash(p string) (string, error) {
	if f.fail == "hash" {
		return "", errBoom
	}
	if f.fail == "mismatch" {
		return strings.Repeat("0", 32), nil
	}
	return f.TreeFiles.Hash(p)
}
func (f faultFiles) Info(p string) (port.FileInfo, error) {
	if f.fail == "info" {
		return port.FileInfo{}, errBoom
	}
	return f.TreeFiles.Info(p)
}
func (f faultFiles) Open(p string) (io.ReadCloser, error) {
	if f.fail == "open" {
		return nil, errBoom
	}
	return f.TreeFiles.Open(p)
}

type connection struct {
	value port.MirrorConnection
	err   error
}

func (c connection) OpenMirrorConnection(context.Context) (port.MirrorConnection, error) {
	return c.value, c.err
}

type uploadResult struct {
	report port.TransferReport
	err    error
}

func (u uploadResult) RestoreFiles(context.Context, []port.RestoreFile, port.UploadOptions) (port.TransferReport, error) {
	return u.report, u.err
}

func TestPassFailureBoundaries(t *testing.T) {
	t.Parallel()
	for _, which := range []string{"list", "scan", "journal", "write-journal", "stage", "hash", "mismatch", "upload", "connection"} {
		t.Run(which, func(t *testing.T) {
			f := setup(t)
			f.write("a.pdf", "a")
			switch which {
			case "list", "upload":
				f.c.fail[which] = errBoom
			case "scan":
				f.e.disk = faultDisk{MirrorDisk: f.e.disk, scanErr: errBoom}
			case "journal", "write-journal":
				f.e.disk = faultDisk{MirrorDisk: f.e.disk, root: func(r port.MirrorDirectory) port.MirrorDirectory { return faultRoot{MirrorDirectory: r, fail: which} }}
			case "connection":
				f.e.connections = connection{err: errBoom}
			default:
				f.e.files = faultFiles{TreeFiles: f.e.files, fail: which}
			}
			if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); err == nil {
				t.Fatal("failure ignored")
			}
		})
	}
	f := setup(t)
	f.e.connections = connection{value: port.MirrorConnection{Cloud: f.c, Upload: f.e.upload, Library: f.e.library, Notes: f.e.notes}}
	f.pass(port.MirrorRunOptions{})
	f.spec.Dir = "relative"
	if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, port.ErrMirrorSpec) {
		t.Fatal(err)
	}
}
func TestCloudIdentityAndNamesRefused(t *testing.T) {
	t.Parallel()
	for _, which := range []string{"gone", "replaced", "profile", "path", "unsafe", "duplicate", "metadata", "file", "mkdir"} {
		t.Run(which, func(t *testing.T) {
			f := setup(t)
			if which == "gone" || which == "replaced" || which == "profile" || which == "path" {
				f.pass(port.MirrorRunOptions{})
			}
			switch which {
			case "gone":
				delete(f.c.entries, f.remoteFolder().ID)
			case "replaced":
				delete(f.c.entries, f.remoteFolder().ID)
				f.c.seed("DOCUMENT/Document/Papers", "<folder>")
			case "profile":
				f.spec.Profile = "other"
			case "path":
				f.spec.Folder = "/DOCUMENT/Document/Other"
			case "unsafe":
				e := f.c.seed("DOCUMENT/Document/Papers/a.pdf", "a")
				e.Path = "DOCUMENT/Document/Papers/../a.pdf"
				f.c.entries[e.ID] = e
			case "duplicate":
				f.c.seed("DOCUMENT/Document/Papers/A.pdf", "a")
				f.c.seed("DOCUMENT/Document/Papers/a.pdf", "b")
			case "metadata":
				e := f.c.seed("DOCUMENT/Document/Papers/a.pdf", "a")
				e.Hash = ""
				f.c.entries[e.ID] = e
			case "file":
				e := f.remoteFolder()
				e.Folder = false
				f.c.entries[e.ID] = e
			case "mkdir":
				delete(f.c.entries, f.remoteFolder().ID)
				f.c.fail["mkdir"] = errBoom
			}
			if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); err == nil {
				t.Fatal("accepted invalid remote state")
			}
		})
	}
}
func (f *fixture) remoteFolder() port.Entry {
	f.t.Helper()
	e, err := f.c.Stat(f.t.Context(), "DOCUMENT/Document/Papers")
	if err != nil {
		f.t.Fatal(err)
	}
	return e
}

func TestDownloadAndDrawingFailures(t *testing.T) {
	t.Parallel()
	for _, which := range []string{"download", "hash", "mismatch", "info", "open", "publish", "write-journal", "move"} {
		t.Run(which, func(t *testing.T) {
			f := setup(t)
			f.c.seed("DOCUMENT/Document/Papers/a.pdf", "cloud")
			switch which {
			case "download":
				f.c.fail[which] = errBoom
			case "publish", "write-journal", "move":
				f.e.disk = faultDisk{MirrorDisk: f.e.disk, root: func(r port.MirrorDirectory) port.MirrorDirectory { return faultRoot{MirrorDirectory: r, fail: which} }}
			default:
				f.e.files = faultFiles{TreeFiles: f.e.files, fail: which}
			}
			if which == "move" {
				f.write("a.pdf", "local")
				f.pass(port.MirrorRunOptions{DryRun: true})
			}
			if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); err == nil {
				t.Fatal("download failure ignored")
			}
		})
	}
	for _, which := range []string{"render", "stage", "open", "mkdir", "produce"} {
		t.Run("draw-"+which, func(t *testing.T) {
			f := setup(t)
			f.spec.PullNew = true
			f.c.seed("DOCUMENT/Document/Papers/a.note", "note")
			switch which {
			case "render":
				f.e.notes = noteRender{err: errBoom}
			case "mkdir", "produce":
				f.e.disk = faultDisk{MirrorDisk: f.e.disk, root: func(r port.MirrorDirectory) port.MirrorDirectory { return faultRoot{MirrorDirectory: r, fail: which} }}
			default:
				f.e.files = faultFiles{TreeFiles: f.e.files, fail: which}
			}
			if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); err == nil {
				t.Fatal("draw failure ignored")
			}
		})
	}
}
func TestPendingAndRecoveryCases(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.write("a.pdf", "initial")
	f.pass(port.MirrorRunOptions{})
	f.write("a.pdf", "changed")
	if err := os.Chtimes(filepath.Join(f.dir, "a.pdf"), now, now); err != nil {
		t.Fatal(err)
	}
	if p := f.pass(port.MirrorRunOptions{}); !has(p, port.MirrorSkip) {
		t.Fatal(p)
	}
	f = setup(t)
	f.write("a.pdf", "a")
	f.write("b.pdf", "b")
	f.pass(port.MirrorRunOptions{})
	if err := os.Rename(filepath.Join(f.dir, "a.pdf"), filepath.Join(f.dir, "renamed.pdf")); err != nil {
		t.Fatal(err)
	}
	if err := f.c.Move(t.Context(), f.remote("a.pdf"), "DOCUMENT/Document/Papers/tablet.pdf"); err != nil {
		t.Fatal(err)
	}
	if p := f.pass(port.MirrorRunOptions{}); !has(p, port.MirrorSkip) {
		t.Fatal(p)
	}
	for _, which := range []string{"missing", "changed-destination", "old-present", "completed"} {
		t.Run(which, func(t *testing.T) {
			f := setup(t)
			f.write("a.pdf", "old")
			f.pass(port.MirrorRunOptions{})
			old := f.remote("a.pdf")
			f.write("a.pdf", "new")
			root, err := f.e.disk.OpenMirror(t.Context(), f.dir)
			if err != nil {
				t.Fatal(err)
			}
			j, err := readJournal(root)
			if err != nil {
				t.Fatal(err)
			}
			en := j.Items[old.ID]
			en.Recover = true
			j.Items[old.ID] = en
			if err = j.write(t.Context(), root); err != nil {
				t.Fatal(err)
			}
			root.Close()
			switch which {
			case "missing":
				if err = os.Remove(filepath.Join(f.dir, "a.pdf")); err != nil {
					t.Fatal(err)
				}
			case "changed-destination":
				delete(f.c.entries, old.ID)
				f.c.seed(old.Path, "unexpected")
			case "completed":
				delete(f.c.entries, old.ID)
				f.c.seed(old.Path, "new")
			}
			p := f.pass(port.MirrorRunOptions{})
			if which == "missing" || which == "changed-destination" {
				if !has(p, port.MirrorSkip) {
					t.Fatal(p)
				}
			} else {
				if has(p, port.MirrorSkip) {
					t.Fatal(p)
				}
			}
		})
	}
}
func TestSafeLocalFailuresAndAsideNumbering(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.write("a.pdf", "a")
	f.pass(port.MirrorRunOptions{})
	f.write(".snctl-removed/a.pdf", "retained")
	if err := f.c.Delete(t.Context(), f.remote("a.pdf"), false); err != nil {
		t.Fatal(err)
	}
	f.pass(port.MirrorRunOptions{ForceDelete: true})
	if f.read(".snctl-removed/a (2).pdf") != "a" {
		t.Fatal("retention replaced")
	}
	f = setup(t)
	f.write("A.pdf", "a")
	f.write("a.pdf", "b")
	// A case-insensitive volume has only one file; a case-sensitive one must refuse ambiguity.
	if entries, err := f.e.disk.Scan(f.dir, nil); err == nil && len(entries) == 2 {
		if _, err = f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, port.ErrMirrorRefused) {
			t.Fatal(err)
		}
	}
	f = setup(t)
	root, err := f.e.disk.OpenMirror(t.Context(), f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = readJournal(faultRoot{MirrorDirectory: root, fail: "journal"}); !errors.Is(err, ErrJournal) {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(f.dir, "a.pdf"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := &localFile{Path: "a.pdf", Size: 2, ModTime: time.Time{}}
	if err = hash(root, file); err == nil {
		t.Fatal("changed file was accepted")
	}
	if err = hash(faultRoot{MirrorDirectory: root, fail: "open"}, file); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	if err = hash(faultRoot{MirrorDirectory: root, fail: "info"}, file); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	root.Close()
	if err = checkDir(localfs.Files{}, "\x00"); err == nil {
		t.Fatal("invalid path accepted")
	}
}
func TestUploadResultMustVerify(t *testing.T) {
	t.Parallel()
	for _, result := range []uploadResult{{}, {report: port.TransferReport{Items: []port.TransferItem{{Entry: &port.Entry{ID: "new", Path: "elsewhere", Hash: "bad"}}}}}} {
		f := setup(t)
		f.write("a.pdf", "a")
		f.e.upload = result
		if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); err == nil {
			t.Fatal("unverified result accepted")
		}
	}
}

type identityCloud struct {
	*cloud
	source port.OutputSource
	err    error
}

func (c identityCloud) OutputSource(context.Context) (port.OutputSource, error) {
	return c.source, c.err
}
func TestJournalBindsEndpointAndAccount(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.write("a.pdf", "a")
	source := port.OutputSource{Endpoint: "http://one.invalid", Account: "one"}
	f.e.cloud = identityCloud{cloud: f.c, source: source}
	f.pass(port.MirrorRunOptions{})
	for _, which := range []string{"endpoint", "account", "error"} {
		t.Run(which, func(t *testing.T) {
			changed := source
			wrapped := identityCloud{cloud: f.c, source: changed}
			switch which {
			case "endpoint":
				wrapped.source.Endpoint = "http://two.invalid"
			case "account":
				wrapped.source.Account = "two"
			case "error":
				wrapped.err = errBoom
			}
			f.e.cloud = wrapped
			if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); err == nil {
				t.Fatal("different identity accepted")
			}
		})
	}
	if f.read("a.pdf") != "a" || len(f.c.trash) != 0 {
		t.Fatal("identity refusal touched originals")
	}
}
func TestStructurallyDamagedJournalRefused(t *testing.T) {
	t.Parallel()
	for _, which := range []string{"path", "cloud-scope", "digest", "output", "duplicate"} {
		t.Run(which, func(t *testing.T) {
			f := setup(t)
			f.write("a.pdf", "a")
			f.pass(port.MirrorRunOptions{})
			root, err := f.e.disk.OpenMirror(t.Context(), f.dir)
			if err != nil {
				t.Fatal(err)
			}
			j, err := readJournal(root)
			if err != nil {
				t.Fatal(err)
			}
			id := f.remote("a.pdf").ID
			en := j.Items[id]
			switch which {
			case "path":
				en.Path = "sub/../a.pdf"
			case "cloud-scope":
				en.CloudPath = "NOTE/Note/a.pdf"
			case "digest":
				en.Record = "bad"
			case "output":
				en.Output = "outside.pdf"
			case "duplicate":
				j.Items["other"] = en
			}
			j.Items[id] = en
			if err = j.write(t.Context(), root); err != nil {
				t.Fatal(err)
			}
			root.Close()
			if _, err = f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, ErrJournal) {
				t.Fatal(err)
			}
		})
	}
}

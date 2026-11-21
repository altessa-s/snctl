// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package archives

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/adapter/localfs"
	"github.com/altessa-s/snctl/internal/port"
)

type backupCloud struct {
	port.Files
	entries                       []port.Entry
	data                          map[string]string
	listErr, statErr, downloadErr error
}

func (c *backupCloud) List(context.Context, string, bool) ([]port.Entry, error) {
	return c.entries, c.listErr
}
func (c *backupCloud) Stat(_ context.Context, name string) (port.Entry, error) {
	if c.statErr != nil {
		return port.Entry{}, c.statErr
	}
	for _, e := range c.entries {
		if e.Path == strings.Trim(name, "/") {
			return e, nil
		}
	}
	return port.Entry{}, port.ErrNotFound
}
func (c *backupCloud) Download(_ context.Context, e port.Entry, _ port.Progress) (string, error) {
	if c.downloadErr != nil {
		return "", c.downloadErr
	}
	return localfs.Files{}.Stage(strings.NewReader(c.data[e.ID]))
}
func (c *backupCloud) seed(id, name, data string) {
	size := int64(len(data))
	hash := md5.Sum([]byte(data))
	c.entries = append(c.entries, port.Entry{ID: id, Name: path.Base(name), Path: name, Size: &size, Hash: hex.EncodeToString(hash[:])})
	c.data[id] = data
}

type backupUpload struct {
	called int
	work   []port.RestoreFile
	err    error
}

func (u *backupUpload) RestoreFiles(_ context.Context, work []port.RestoreFile, _ port.UploadOptions) (port.TransferReport, error) {
	u.called++
	u.work = append([]port.RestoreFile{}, work...)
	for _, f := range work {
		if _, err := os.ReadFile(f.Local); err != nil {
			return port.TransferReport{}, err
		}
	}
	return port.TransferReport{Items: []port.TransferItem{}}, u.err
}

func setup(t *testing.T) (Service, *backupCloud, string) {
	t.Helper()
	c := &backupCloud{data: map[string]string{}}
	c.seed("1", "NOTE/Note/A/a.note", "alpha")
	c.seed("2", "NOTE/Note/B/b.note", "beta")
	c.entries = append(c.entries, port.Entry{ID: "a", Name: "A", Path: "NOTE/Note/A", Folder: true}, port.Entry{ID: "b", Name: "B", Path: "NOTE/Note/B", Folder: true})
	return Service{Cloud: c, Local: localfs.Files{}, Upload: &backupUpload{}, Profile: "default", Endpoint: "http://cloud.invalid", Now: func() time.Time { return time.Unix(1700000000, 0) }}, c, filepath.Join(t.TempDir(), "backup")
}
func load(t *testing.T, dir string) port.BackupManifest {
	t.Helper()
	r, err := (localfs.Files{}).OpenBackup(t.Context(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	m, err := readManifest(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range m.Files {
		if err := verify(t.Context(), r, e); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func TestBackupGenerations(t *testing.T) {
	s, c, dir := setup(t)
	var updates int
	o := port.BackupOptions{Jobs: 1, Progress: func(port.SyncProgress) { updates++ }}
	r, err := s.Backup(t.Context(), dir, "/", o)
	if err != nil || r.Written != 2 || updates != 2 {
		t.Fatal(r, err, updates)
	}
	first := load(t, dir)
	r, err = s.Backup(t.Context(), dir, "/", o)
	if err != nil || r.Skipped != 2 {
		t.Fatal(r, err)
	}
	c.entries[0].Path = "NOTE/Note/B/renamed.note"
	c.entries[0].Name = "renamed.note"
	r, err = s.Backup(t.Context(), dir, "/", o)
	if err != nil || r.Skipped != 2 {
		t.Fatal(r, err)
	}
	c.data["1"] = "ALPHA"
	h := md5.Sum([]byte("ALPHA"))
	c.entries[0].Hash = hex.EncodeToString(h[:])
	ctx, cancel := context.WithCancel(t.Context())
	o.Progress = func(port.SyncProgress) { cancel() }
	if _, err = s.Backup(ctx, dir, "/", o); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	current := load(t, dir)
	if current.Files[1].MD5 != first.Files[0].MD5 {
		t.Fatal("interrupted run published", current)
	}
	o.Progress = nil
	r, err = s.Backup(t.Context(), dir, "/", o)
	if err != nil || r.Written != 1 || r.Skipped != 1 {
		t.Fatal(r, err)
	}
	if _, err := os.Stat(filepath.Join(dir, first.Files[0].Stored)); !os.IsNotExist(err) {
		t.Fatal("obsolete generation retained", err)
	}
	r, err = s.Backup(t.Context(), dir, "/NOTE/Note/A", port.BackupOptions{Rescope: true, Prune: true})
	if err != nil || r.Moved != 2 || r.Outside != 2 || r.Pruned != 0 {
		t.Fatal(r, err)
	}
	load(t, dir)
}

func TestBackupInvalidInputs(t *testing.T) {
	for _, scenario := range []string{"jobs", "all", "open", "manifest", "profile", "endpoint", "scope", "list", "stat", "id", "hash", "size", "duplicate", "extension", "download", "bytes"} {
		t.Run(scenario, func(t *testing.T) {
			s, c, dir := setup(t)
			o := port.BackupOptions{}
			query := "/"
			switch scenario {
			case "jobs":
				o.Jobs = -1
			case "all":
				o.All = true
			case "open":
				if err := os.WriteFile(dir, []byte("x"), 0600); err != nil {
					t.Fatal(err)
				}
			case "manifest":
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, manifestName), []byte("bad"), 0600); err != nil {
					t.Fatal(err)
				}
			case "profile", "endpoint", "scope":
				if _, err := s.Backup(t.Context(), dir, "/", o); err != nil {
					t.Fatal(err)
				}
				if scenario == "profile" {
					s.Profile = "work"
				}
				if scenario == "endpoint" {
					s.Endpoint = "http://other.invalid"
				}
				if scenario == "scope" {
					query = "/NOTE/Note/A"
				}
			case "list":
				c.listErr = io.ErrUnexpectedEOF
			case "stat":
				query = "missing"
				c.statErr = io.ErrUnexpectedEOF
			case "id":
				c.entries[0].ID = "../bad"
			case "hash":
				c.entries[0].Hash = "bad"
			case "size":
				c.entries[0].Size = nil
			case "duplicate":
				c.entries = append(c.entries, c.entries[0])
			case "extension":
				c.entries[0].Name = "x.bad:ext"
			case "download":
				c.downloadErr = io.ErrUnexpectedEOF
			case "bytes":
				c.data["1"] = "bad"
			}
			if _, err := s.Backup(t.Context(), dir, query, o); err == nil {
				t.Fatal("accepted", scenario)
			}
		})
	}
}

func TestRestoreValidation(t *testing.T) {
	for _, scenario := range []string{"good", "open", "missing", "empty", "manifest", "version", "profile", "path", "stored", "hash", "duplicate", "missing-body", "damaged-body", "uploader"} {
		t.Run(scenario, func(t *testing.T) {
			s, _, dir := setup(t)
			if scenario == "open" {
				if _, err := s.Restore(t.Context(), dir, "", port.UploadOptions{}); err == nil {
					t.Fatal("missing directory")
				}
				return
			}
			if _, err := s.Backup(t.Context(), dir, "/", port.BackupOptions{}); err != nil {
				t.Fatal(err)
			}
			m := load(t, dir)
			switch scenario {
			case "missing":
				if err := os.Remove(filepath.Join(dir, manifestName)); err != nil {
					t.Fatal(err)
				}
			case "manifest":
				if err := os.WriteFile(filepath.Join(dir, manifestName), []byte("bad"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-body":
				if err := os.Remove(filepath.Join(dir, m.Files[0].Stored)); err != nil {
					t.Fatal(err)
				}
			case "damaged-body":
				if err := os.WriteFile(filepath.Join(dir, m.Files[0].Stored), []byte("ALPHA"), 0600); err != nil {
					t.Fatal(err)
				}
			case "uploader":
				s.Upload.(*backupUpload).err = io.ErrClosedPipe
			default:
				switch scenario {
				case "empty":
					m.Files = nil
				case "version":
					m.Version = 2
				case "profile":
					m.Profile = ""
				case "path":
					m.Files[0].Path = "../outside.note"
				case "stored":
					m.Files[0].Stored = "../outside.note"
				case "hash":
					m.Files[0].MD5 = "bad"
				case "duplicate":
					m.Files = append(m.Files, m.Files[0])
				}
				data, err := json.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, manifestName), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := s.Restore(t.Context(), dir, "NOTE/Note/New", port.UploadOptions{DryRun: true})
			if scenario == "good" {
				if err != nil {
					t.Fatal(err)
				}
				u := s.Upload.(*backupUpload)
				if u.called != 1 || len(u.work) != 2 || u.work[0].Path != "/NOTE/Note/New/NOTE/Note/A/a.note" {
					t.Fatal(u)
				}
				for _, f := range u.work {
					if _, err := os.Stat(f.Local); !os.IsNotExist(err) {
						t.Fatal("staging retained", err)
					}
				}
			} else if err == nil {
				t.Fatal("accepted", scenario)
			}
		})
	}
}

func TestRestoreDestinations(t *testing.T) {
	for _, tc := range []struct {
		name, scope, to, want string
	}{
		{"original", "NOTE/Note/A", "", "/NOTE/Note/A/a.note"},
		{"folder", "NOTE/Note/A", "NOTE/Note/R", "/NOTE/Note/R/a.note"},
		{"subfolder", "NOTE/Note", "NOTE/Note/R", "/NOTE/Note/R/A/a.note"},
		{"library", "/", "NOTE/Note/R", "/NOTE/Note/R/NOTE/Note/A/a.note"},
		{"file", "NOTE/Note/A/a.note", "NOTE/Note/R", "/NOTE/Note/R/a.note"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, c, dir := setup(t)
			c.entries = append(c.entries, port.Entry{ID: "notes", Name: "Note", Path: "NOTE/Note", Folder: true})
			if _, err := s.Backup(t.Context(), dir, tc.scope, port.BackupOptions{}); err != nil {
				t.Fatal(err)
			}
			m := load(t, dir)
			if _, err := s.Restore(t.Context(), dir, tc.to, port.UploadOptions{DryRun: true}); err != nil {
				t.Fatal(err)
			}
			u := s.Upload.(*backupUpload)
			if u.work[0].Path != tc.want || u.work[0].Display != filepath.Join(dir, m.Files[0].Stored) {
				t.Fatalf("restore input = %+v, want %s", u.work[0], tc.want)
			}
		})
	}
	for _, scope := range []string{"NOTE/Note/A", "/NOTE/Note/A/", "/NOTE/Note/Other"} {
		if _, err := restorePath(scope, "NOTE/Note/A/a.note", "NOTE/Note/R"); err == nil {
			t.Fatalf("accepted invalid scope %q", scope)
		}
	}
}

func TestBrokenStoredFileIsRepaired(t *testing.T) {
	s, _, dir := setup(t)
	if _, err := s.Backup(t.Context(), dir, "/", port.BackupOptions{}); err != nil {
		t.Fatal(err)
	}
	m := load(t, dir)
	if err := os.WriteFile(filepath.Join(dir, m.Files[0].Stored), []byte("ALPHA"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := s.Backup(t.Context(), dir, "/", port.BackupOptions{})
	if err != nil || r.Written != 1 || r.Skipped != 1 {
		t.Fatal(r, err)
	}
	load(t, dir)
}

type brokenBackupLocal struct {
	port.BackupLocal
	openErr, stageErr error
}

func (l brokenBackupLocal) Open(name string) (io.ReadCloser, error) {
	if l.openErr != nil {
		return nil, l.openErr
	}
	return l.BackupLocal.Open(name)
}
func (l brokenBackupLocal) Stage(r io.Reader) (string, error) {
	if l.stageErr != nil {
		return "failed-stage", l.stageErr
	}
	return l.BackupLocal.Stage(r)
}

func TestBackupLocalFailures(t *testing.T) {
	s, _, dir := setup(t)
	s.Local = brokenBackupLocal{BackupLocal: s.Local, openErr: io.ErrClosedPipe}
	if _, err := s.Backup(t.Context(), dir, "/", port.BackupOptions{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	s.Local = localfs.Files{}
	if _, err := s.Backup(t.Context(), dir, "/", port.BackupOptions{}); err != nil {
		t.Fatal(err)
	}
	s.Local = brokenBackupLocal{BackupLocal: s.Local, stageErr: io.ErrClosedPipe}
	if _, err := s.Restore(t.Context(), dir, "", port.UploadOptions{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	b := make([]byte, 1)
	if _, err := (contextReader{ctx, strings.NewReader("a")}).Read(b); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRemovedStorageFailures(t *testing.T) {
	for _, scenario := range []string{"missing", "directory", "link", "removed-file", "damaged", "manifest-write", "cleanup"} {
		t.Run(scenario, func(t *testing.T) {
			s, c, dir := setup(t)
			if _, err := s.Backup(t.Context(), dir, "/", port.BackupOptions{}); err != nil {
				t.Fatal(err)
			}
			m := load(t, dir)
			file := filepath.Join(dir, m.Files[0].Stored)
			c.entries = c.entries[1:]
			switch scenario {
			case "missing":
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(file, 0700); err != nil {
					t.Fatal(err)
				}
			case "link":
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), file); err != nil {
					t.Fatal(err)
				}
			case "removed-file":
				if err := os.WriteFile(filepath.Join(dir, "removed"), []byte("x"), 0600); err != nil {
					t.Fatal(err)
				}
			case "damaged":
				if err := os.WriteFile(file, []byte("ALPHA"), 0600); err != nil {
					t.Fatal(err)
				}
			case "manifest-write":
				s.Local = injectStorage{BackupLocal: s.Local, produce: manifestName}
			case "cleanup":
				s.Local = injectStorage{BackupLocal: s.Local, remove: true}
			}
			_, err := s.Backup(t.Context(), dir, "/", port.BackupOptions{})
			if scenario == "missing" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("accepted", scenario)
			}
		})
	}
}

type injectStorage struct {
	port.BackupLocal
	produce string
	remove  bool
}

func (l injectStorage) OpenBackup(ctx context.Context, name string, create bool) (port.BackupDirectory, error) {
	root, err := l.BackupLocal.OpenBackup(ctx, name, create)
	if err != nil {
		return nil, err
	}
	return injectedRoot{BackupDirectory: root, produce: l.produce, remove: l.remove}, nil
}

type injectedRoot struct {
	port.BackupDirectory
	produce string
	remove  bool
}

func (r injectedRoot) Produce(ctx context.Context, name string, write func(io.Writer) error) error {
	if name == r.produce {
		return io.ErrClosedPipe
	}
	return r.BackupDirectory.Produce(ctx, name, write)
}
func (r injectedRoot) Remove(name string) error {
	if r.remove {
		return io.ErrClosedPipe
	}
	return r.BackupDirectory.Remove(name)
}

func TestStagingVerifiesConsumedBytes(t *testing.T) {
	s, _, dir := setup(t)
	if _, err := s.Backup(t.Context(), dir, "/", port.BackupOptions{}); err != nil {
		t.Fatal(err)
	}
	m := load(t, dir)
	ctx := t.Context()
	for _, data := range []string{"wrong", "short", "too long"} {
		r := &verifiedReader{contextReader: contextReader{ctx, strings.NewReader(data)}, entry: m.Files[0], digest: md5.New()}
		if _, err := io.ReadAll(r); err == nil {
			t.Fatal("staged changed bytes accepted", data)
		}
	}
}

func TestRestoreRefusesNoncanonicalManifestPaths(t *testing.T) {
	for _, recorded := range []string{"NOTE/Note/A/../B/a.note", "NOTE/Note/A/sub/../a.note", "NOTE/Note/A//a.note", "NOTE/Note/B/a.note"} {
		t.Run(recorded, func(t *testing.T) {
			s, _, dir := setup(t)
			if _, err := s.Backup(t.Context(), dir, "/NOTE/Note/A", port.BackupOptions{}); err != nil {
				t.Fatal(err)
			}
			m := load(t, dir)
			m.Files[0].Path = recorded
			data, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err = s.Restore(t.Context(), dir, "NOTE/Note/R", port.UploadOptions{}); err == nil {
				t.Fatal("accepted damaged manifest")
			}
			if s.Upload.(*backupUpload).called != 0 {
				t.Fatal("upload called")
			}
		})
	}
}

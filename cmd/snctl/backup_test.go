// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/clouddouble"
	"github.com/altessa-s/snctl/internal/port"
)

func backupReport(t *testing.T, out string) port.BackupReport {
	t.Helper()
	var r port.BackupReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err, out)
	}
	return r
}

func TestBackupRoundTrip(t *testing.T) {
	e := newE2E(t)
	e.cloud.Seed("NOTE/Note/B/a.note", false, []byte("alpha"))
	e.cloud.Seed("NOTE/Note/B/sub/b.note", false, []byte("beta"))
	dir := e.path("backup")
	r := backupReport(t, e.must("backup", dir, "NOTE/Note/B", "--json"))
	if r.Documents != 2 || r.Written != 2 {
		t.Fatal(r)
	}
	n := e.cloud.Count("/api/file/3/files/download_v3")
	r = backupReport(t, e.must("backup", dir, "NOTE/Note/B", "--json"))
	if r.Skipped != 2 || r.Written != 0 || e.cloud.Count("/api/file/3/files/download_v3") != n {
		t.Fatal(r)
	}
	e.cloud.Remove("NOTE/Note/B/a.note")
	r = backupReport(t, e.must("backup", dir, "NOTE/Note/B", "--json"))
	if r.Moved != 1 || r.Documents != 1 {
		t.Fatal(r)
	}
	removed, err := os.ReadDir(filepath.Join(dir, "removed"))
	if err != nil || len(removed) != 1 {
		t.Fatal(removed, err)
	}
	before := len(e.cloud.Writes())
	plan := report(t, e.must("backup", "restore", dir, "NOTE/Note/Restored", "--dry-run", "--json"))
	if plan.Counts[port.StatusUpload] != 1 || len(e.cloud.Writes()) != before {
		t.Fatal(plan, e.cloud.Writes()[before:])
	}
	if _, _, code := e.run("backup", "restore", dir, "NOTE/Note/Restored"); code != 2 || len(e.cloud.Writes()) != before {
		t.Fatal(code)
	}
	restored := report(t, e.must("backup", "restore", dir, "NOTE/Note/Restored", "--yes", "--json"))
	if restored.Counts[port.StatusUploaded] != 1 {
		t.Fatal(restored)
	}
	if b, ok := e.cloud.File("NOTE/Note/Restored/sub/b.note"); !ok || string(b) != "beta" {
		t.Fatal(e.cloud.Paths())
	}
	before = len(e.cloud.Writes())
	restored = report(t, e.must("backup", "restore", dir, "NOTE/Note/Restored", "--yes", "--json"))
	if restored.Counts[port.StatusUnchanged] != 1 || len(e.cloud.Writes()) != before {
		t.Fatal(restored)
	}
}

func TestBackupRestoreLayout(t *testing.T) {
	for _, format := range []string{"plain", "table", "json"} {
		t.Run(format, func(t *testing.T) {
			e := newE2E(t)
			t.Chdir(e.root)
			e.cloud.Seed("NOTE/Note/X/a.note", false, []byte("alpha"))
			e.cloud.Seed("NOTE/Note/X/sub/b.note", false, []byte("beta"))
			dir := "backup"
			e.must("backup", dir, "NOTE/Note/X")
			e.golden("backup-restore-"+format, e.must("backup", "restore", dir, "NOTE/Note/R", "--dry-run", "--format", format))
			e.must("backup", "restore", dir, "--to", "NOTE/Note/R", "--yes")
			for name, want := range map[string]string{"a.note": "alpha", "sub/b.note": "beta"} {
				target := "NOTE/Note/R/" + name
				if got, ok := e.cloud.File(target); !ok || string(got) != want {
					t.Fatalf("%s = %q, exists %v; paths %v", target, got, ok, e.cloud.Paths())
				}
				t.Logf("restored %s = %q", target, want)
			}
			if _, ok := e.cloud.File("NOTE/Note/R/NOTE/Note/X/a.note"); ok {
				t.Fatal("destination contains the original category path")
			}
		})
	}
}

func TestBackupGuardsOfflineAndPrune(t *testing.T) {
	e := newE2E(t)
	e.cloud.Seed("NOTE/Note/A/a.note", false, []byte("alpha"))
	e.cloud.Seed("NOTE/Note/B/b.note", false, []byte("beta"))
	dir := e.path("backup")
	e.must("backup", dir, "NOTE/Note/A")
	if _, _, code := e.run("backup", dir, "NOTE/Note/B"); code != 4 {
		t.Fatal(code)
	}
	r := backupReport(t, e.must("backup", dir, "NOTE/Note/B", "--rescope", "--prune", "--json"))
	if r.Moved != 1 || r.Outside != 1 || r.Pruned != 0 {
		t.Fatal(r)
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m port.BackupManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	m.Profile = "other"
	data, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, code := e.run("backup", dir, "NOTE/Note/B"); code != 4 {
		t.Fatal(code)
	}
	e.must("backup", dir, "NOTE/Note/B", "--force")
	e.cloud.Remove("NOTE/Note/B/b.note")
	r = backupReport(t, e.must("backup", dir, "NOTE/Note/B", "--prune", "--json"))
	if r.Pruned != 1 {
		t.Fatal(r)
	}
	e.must("sync", "--files")
	before := len(e.cloud.Requests())
	r = backupReport(t, e.must("--offline", "backup", e.path("offline"), "--json"))
	if r.Written != 1 || len(e.cloud.Requests()) != before {
		t.Fatal(r, len(e.cloud.Requests())-before)
	}
	if _, diag, code := e.run("--offline", "backup", "restore", dir, "--yes"); code != 2 || !strings.Contains(diag, "offline") {
		t.Fatal(code, diag)
	}
}

func TestBackupRestoreAmbiguousFinish(t *testing.T) {
	e := newE2E(t)
	e.cloud.Seed("NOTE/Note/a.note", false, []byte("alpha"))
	dir := e.path("backup")
	e.must("backup", dir)
	e.cloud.SetVisibilityMode(clouddouble.HiddenUntilUnlock)
	e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/upload/finish", Count: 1, FinishStoredRejected: true})
	r := report(t, e.must("backup", "restore", dir, "NOTE/Note/New", "--yes", "--json"))
	if r.Counts[port.StatusUploaded] != 1 || !strings.Contains(r.Items[len(r.Items)-1].Reason, "after storing") {
		t.Fatal(r)
	}
	assertVerificationAfterUnlock(t, e.cloud)
	writes := e.cloud.Writes()
	end := -1
	for i, w := range writes {
		if w.Path == "/api/file/2/files/synchronous/end" {
			end = i
		}
	}
	if end < 0 {
		t.Fatal(writes)
	}
}

func TestBackupRestoreUploadModes(t *testing.T) {
	for _, mode := range []clouddouble.UploadMode{clouddouble.Overwrite, clouddouble.Duplicate, clouddouble.Autorename} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			e := newE2E(t)
			e.cloud.SetUploadMode(mode)
			e.cloud.Seed("NOTE/Note/a.note", false, []byte("original"))
			dir := e.path("backup")
			e.must("backup", dir)
			e.cloud.Seed("NOTE/Note/a.note", false, []byte("changed"))
			before := len(e.cloud.Writes())
			if _, _, code := e.run("backup", "restore", dir, "--yes"); code != 4 || len(e.cloud.Writes()) != before {
				t.Fatal(code)
			}
			r := report(t, e.must("backup", "restore", dir, "--replace", "--yes", "--json"))
			if r.Counts[port.StatusReplaced] != 1 {
				t.Fatal(r)
			}
			if data, ok := e.cloud.File("NOTE/Note/a.note"); !ok || string(data) != "original" {
				t.Fatal(e.cloud.Paths())
			}
		})
	}
}

func TestBackupRestoreValidatesBeforeWriting(t *testing.T) {
	e := newE2E(t)
	e.cloud.Seed("NOTE/Note/a.note", false, []byte("alpha"))
	e.cloud.Seed("NOTE/Note/b.note", false, []byte("beta"))
	dir := e.path("backup")
	e.must("backup", dir)
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m port.BackupManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, m.Files[1].Stored), []byte("BETA"), 0600); err != nil {
		t.Fatal(err)
	}
	before := len(e.cloud.Writes())
	if _, _, code := e.run("backup", "restore", dir, "NOTE/Note/New", "--yes"); code != 1 || len(e.cloud.Writes()) != before {
		t.Fatal(code)
	}
}

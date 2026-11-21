// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/snctl/cmd/snctl/commands"
	"github.com/altessa-s/snctl/internal/clouddouble"
	"github.com/altessa-s/snctl/internal/port"
)

func TestMirrorReplacementModes(t *testing.T) {
	for _, mode := range []clouddouble.UploadMode{clouddouble.Overwrite, clouddouble.Duplicate, clouddouble.Autorename, clouddouble.RefuseOverwrite} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			e := newE2E(t)
			e.cloud.SetUploadMode(mode)
			oldID := e.cloud.Seed("DOCUMENT/Document/Papers/a.pdf", false, []byte("before"))
			dir := e.path("mirror")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			e.must("mirror", "add", "papers", dir, "/DOCUMENT/Document/Papers")
			e.must("mirror", "run", "papers")
			writeMirrorFile(t, dir+"/a.pdf", "after")
			out := e.must("mirror", "run", "papers")
			t.Log(out)
			var current port.Entry
			if err := json.Unmarshal([]byte(e.must("stat", "DOCUMENT/Document/Papers/a.pdf", "--json")), &current); err != nil {
				t.Fatal(err)
			}
			var trash []port.Entry
			if err := json.Unmarshal([]byte(e.must("trash", "ls", "--json")), &trash); err != nil {
				t.Fatal(err)
			}
			if mode == clouddouble.Overwrite {
				if current.ID != oldID || len(trash) != 0 || !strings.Contains(out, "replaced in place") {
					t.Fatal("in-place replacement lost identity or recycled", current, trash, out)
				}
			} else if current.ID == oldID || len(trash) != 1 || trash[0].ID != oldID || !strings.Contains(out, "recycled prior cloud copy") {
				t.Fatal("replacement fallback did not retain prior copy", current, trash, out)
			}
			if data, ok := e.cloud.File(current.Path); !ok || string(data) != "after" {
				t.Fatal("wrong replacement bytes", string(data), ok)
			}
			items := mirrorJournal(t, dir+"/.snctl-mirror.json")
			if en, ok := items[current.ID]; !ok || en.Recover || len(items) != 1 {
				t.Fatal("completed replacement intent was not cleared", items)
			}
			if out = e.must("mirror", "run", "papers"); !strings.Contains(out, "in step") {
				t.Fatal(out)
			}
		})
	}
}

type mirrorSavedEntry struct {
	Path      string `json:"path"`
	CloudPath string `json:"cloud_path"`
	Recover   bool   `json:"recover"`
}

func mirrorJournal(t *testing.T, name string) map[string]mirrorSavedEntry {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Items map[string]mirrorSavedEntry `json:"items"`
	}
	if err = json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	return saved.Items
}

func writeMirrorFile(t *testing.T, name, body string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(name, old, old); err != nil {
		t.Fatal(err)
	}
}

func TestMirrorBinaryWorkflow(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		t.Run(fmt.Sprintf("refuse-locked-moves=%t", refuse), func(t *testing.T) {
			testMirrorBinaryWorkflow(t, refuse)
		})
	}
}

func testMirrorBinaryWorkflow(t *testing.T, refuse bool) {
	t.Helper()
	e := newE2E(t)
	e.cloud.RefuseLockedMoves(refuse)
	dir := e.path("mirror")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	local := dir + "/local.pdf"
	if err := os.WriteFile(local, []byte("local original"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(local, old, old); err != nil {
		t.Fatal(err)
	}
	e.cloud.Seed("DOCUMENT/Document/X/cloud.epub", false, []byte("cloud original"))
	t.Log(e.must("mirror", "add", "papers", dir, "DOCUMENT/Document/X"))
	t.Log(e.must("mirror", "run", "papers"))
	if data, ok := e.cloud.File("DOCUMENT/Document/X/local.pdf"); !ok || string(data) != "local original" {
		t.Fatalf("upload: %q %v", data, ok)
	}
	data, err := os.ReadFile(dir + "/cloud.epub")
	if err != nil || string(data) != "cloud original" {
		t.Fatal(string(data), err)
	}
	writes := len(e.cloud.Writes())
	quiet := e.must("mirror", "run", "papers")
	t.Log(quiet)
	if !strings.Contains(quiet, "in step") || len(e.cloud.Writes()) != writes {
		t.Fatal("second pass did work", quiet)
	}
	if err = os.Remove(local); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(dir+"/renamed", 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(dir+"/cloud.epub", old, old); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(dir+"/cloud.epub", dir+"/renamed/cloud.epub"); err != nil {
		t.Fatal(err)
	}
	t.Log(e.must("mirror", "run", "papers"))
	if _, ok := e.cloud.File("DOCUMENT/Document/X/local.pdf"); ok {
		t.Fatal("local deletion was not recycled")
	}
	if data, ok := e.cloud.File("DOCUMENT/Document/X/renamed/cloud.epub"); !ok || string(data) != "cloud original" {
		t.Fatal("local rename did not reach cloud", string(data), ok)
	}
	if _, ok := e.cloud.File("DOCUMENT/Document/X/cloud.epub"); ok {
		t.Fatal("old cloud name remains")
	}
	if _, err = os.Stat(local); !os.IsNotExist(err) {
		t.Fatal("deleted local file returned", err)
	}
	trash := e.must("trash", "ls", "--json")
	if !strings.Contains(trash, "local.pdf") {
		t.Fatal(trash)
	}
	before := len(e.cloud.Requests())
	t.Log(e.must("mirror", "ls"))
	if len(e.cloud.Requests()) != before {
		t.Fatal("mirror ls contacted cloud")
	}
	e.must("mirror", "rm", "papers")
	if _, err = os.Stat(dir + "/.snctl-mirror.json"); err != nil {
		t.Fatal("rm touched journal", err)
	}
}

func TestMirrorAmbiguousFinishAndReplacement(t *testing.T) {
	e := newE2E(t)
	dir := e.path("mirror")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	local := dir + "/a.pdf"
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(local, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-time.Hour)
		if err := os.Chtimes(local, old, old); err != nil {
			t.Fatal(err)
		}
	}
	write("one")
	e.must("mirror", "add", "papers", dir, "/DOCUMENT/Document/Papers")
	e.cloud.SetVisibilityMode(clouddouble.HiddenUntilUnlock)
	e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/upload/finish", Count: 1, FinishStoredRejected: true})
	out, diag, code := e.run("mirror", "run", "papers")
	if code != 0 {
		t.Fatalf("%s %s paths=%v writes=%+v", out, diag, e.cloud.Paths(), e.cloud.Writes())
	}
	assertVerificationAfterUnlock(t, e.cloud)
	write("two")
	e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/upload/finish", Count: 1, FinishStoredRejected: true})
	e.must("mirror", "run", "papers")
	assertVerificationAfterUnlock(t, e.cloud)
	if data, ok := e.cloud.File("DOCUMENT/Document/Papers/a.pdf"); !ok || string(data) != "two" {
		t.Fatal(string(data), ok)
	}
	if out := e.must("trash", "ls", "--json"); strings.Contains(out, "a.pdf") {
		t.Fatal("in-place replacement recycled the original", out)
	}
	if out := e.must("mirror", "run", "papers"); !strings.Contains(out, "in step") {
		t.Fatal(out)
	}
}

func TestMirrorRestorationAndCompletedMove(t *testing.T) {
	e := newE2E(t)
	name := "DOCUMENT/Document/Papers/a.pdf"
	oldID := e.cloud.Seed(name, false, []byte("before"))
	dir := e.path("mirror")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	e.must("mirror", "add", "papers", dir, "/DOCUMENT/Document/Papers")
	e.must("mirror", "run", "papers")
	if err := os.Remove(dir + "/a.pdf"); err != nil {
		t.Fatal(err)
	}
	if id := e.cloud.Seed(name, false, []byte("tablet edit")); id != oldID {
		t.Fatal("tablet edit changed the identifier", oldID, id)
	}
	out := e.must("mirror", "run", "papers")
	t.Log(out)
	if !strings.Contains(out, "deleted here, changed there: restored") {
		t.Fatal(out)
	}
	if data, err := os.ReadFile(dir + "/a.pdf"); err != nil || string(data) != "tablet edit" {
		t.Fatal("cloud edit not restored", string(data), err)
	}
	if out = e.must("trash", "ls", "--json"); strings.Contains(out, "a.pdf") {
		t.Fatal("cloud edit recycled", out)
	}
	journalPath := dir + "/.snctl-mirror.json"
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	writeMirrorFile(t, dir+"/a.pdf", "tablet edit")
	if err = os.Rename(dir+"/a.pdf", dir+"/b.pdf"); err != nil {
		t.Fatal(err)
	}
	t.Log(e.must("mirror", "run", "papers"))
	if data, ok := e.cloud.File("DOCUMENT/Document/Papers/b.pdf"); !ok || string(data) != "tablet edit" {
		t.Fatal("move did not finish", string(data), ok)
	}
	// Keeping the old journal recreates an interruption after the cloud move committed.
	if err = os.WriteFile(journalPath, journal, 0o600); err != nil {
		t.Fatal(err)
	}
	writes := len(e.cloud.Writes())
	out = e.must("mirror", "run", "papers")
	t.Log(out)
	if !strings.Contains(out, "in step") || len(e.cloud.Writes()) != writes {
		t.Fatal("completed move did not converge in one pass", out)
	}
	items := mirrorJournal(t, journalPath)
	if items[oldID].Path != "b.pdf" || items[oldID].CloudPath != "DOCUMENT/Document/Papers/b.pdf" {
		t.Fatal("recovered journal did not record the pairing", items)
	}
}

func TestMirrorFailedReplacementKeepsIntent(t *testing.T) {
	for _, mode := range []clouddouble.UploadMode{clouddouble.Overwrite, clouddouble.Autorename} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			e := newE2E(t)
			e.cloud.SetUploadMode(mode)
			oldID := e.cloud.Seed("DOCUMENT/Document/Papers/a.pdf", false, []byte("before"))
			dir := e.path("mirror")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			e.must("mirror", "add", "papers", dir, "/DOCUMENT/Document/Papers")
			e.must("mirror", "run", "papers")
			writeMirrorFile(t, dir+"/a.pdf", "after")
			e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/upload/finish", Count: 1, Code: "500"})
			if _, _, code := e.run("mirror", "run", "papers"); code == 0 {
				t.Fatal("refused upload succeeded")
			}
			if items := mirrorJournal(t, dir+"/.snctl-mirror.json"); !items[oldID].Recover {
				t.Fatal("failed replacement lost its intent", items)
			}
			e.must("mirror", "run", "papers")
			for _, en := range mirrorJournal(t, dir+"/.snctl-mirror.json") {
				if en.Recover {
					t.Fatal("resumed replacement kept its intent", en)
				}
			}
			if out := e.must("mirror", "run", "papers"); !strings.Contains(out, "in step") {
				t.Fatal(out)
			}
		})
	}
}

func TestMirrorFolderAliasPreservesConfig(t *testing.T) {
	e := newE2E(t)
	e.cloud.Seed("DOCUMENT/Document/Caf\u00e9/a.pdf", false, []byte("cloud"))
	dir := e.path("mirror")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	e.must("mirror", "add", "papers", dir, "/document/document/Cafe\u0301")
	configPath := e.path("config/snctl/config.toml")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	e.must("mirror", "run", "papers")
	writeMirrorFile(t, dir+"/b.pdf", "local")
	e.must("mirror", "run", "papers")
	if data, ok := e.cloud.File("DOCUMENT/Document/Caf\u00e9/b.pdf"); !ok || string(data) != "local" {
		t.Fatal("upload did not use actual folder path", string(data), ok)
	}
	after, err := os.ReadFile(configPath)
	if err != nil || string(before) != string(after) || !strings.Contains(string(after), "/document/document/Cafe\u0301") {
		t.Fatal("configured folder spelling changed", string(after), err)
	}
}

func TestMirrorServiceEnvironmentAndProfile(t *testing.T) {
	e := newE2E(t)
	if err := os.Mkdir(e.path("mirror"), 0o700); err != nil {
		t.Fatal(err)
	}
	e.s.env["SNCTL_PROFILE"] = "work"
	e.s.streams = func(s *commands.Streams) { s.Getenv = func(k string) string { return e.s.env[k] } }
	e.must("profile", "add", "work", "--endpoint", e.cloud.URL(), "--yes")
	if out := e.must("mirror", "add", "papers", e.path("mirror"), "/DOCUMENT/Document/Papers", "--json"); !strings.Contains(out, `"profile": "work"`) {
		t.Fatal(out)
	}
	for _, args := range [][]string{
		{"mirror", "run", "--all", "--service-env=SNCTL_CONFIG_DIR=x"},
		{"mirror", "run", "--service", "--service-env=SECRET=value"},
		{"mirror", "run", "--service", "--service-env=missing-equals"},
	} {
		if _, _, code := e.run(args...); code != 2 {
			t.Fatal(args, code)
		}
	}
}

func TestSavedServiceEnvironmentOverridesCurrentProfile(t *testing.T) {
	e := newE2E(t)
	current := e.s
	current.env["SNCTL_PROFILE"] = "current"
	services, close, err := build(t.Context(), commands.Request{ServiceEnv: []string{
		"SNCTL_PROFILE=saved", "SNCTL_SECRETS=file", "SNCTL_CACHE_TTL=0", "SNCTL_CACHE_BUDGET=0",
	}}, io.Discard, current)
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	if !strings.HasSuffix(services.Paths.ProfileDir, string(os.PathSeparator)+"saved") {
		t.Fatal(services.Paths)
	}
}

func TestMirrorRecoveryPreservesTabletEdit(t *testing.T) {
	for _, boundary := range []string{"failed upload", "sync lock"} {
		t.Run(boundary, func(t *testing.T) {
			e, dir, oldID := mirrorReplacementFixture(t)
			if boundary == "failed upload" {
				e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/upload/finish", Count: 1, Code: "500"})
			} else {
				e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/synchronous/start", Count: 1, Before: func() {
					e.cloud.Seed("DOCUMENT/Document/Papers/a.pdf", false, []byte("tablet edit"))
				}})
			}
			out, diag, code := e.run("mirror", "run", "papers")
			if code == 0 {
				t.Fatal("failed replacement succeeded", out, diag)
			}
			if items := mirrorJournal(t, dir+"/.snctl-mirror.json"); !items[oldID].Recover {
				t.Fatal("failed replacement lost intent", items)
			}
			e.cloud.Seed("DOCUMENT/Document/Papers/a.pdf", false, []byte("tablet edit"))
			assertMirrorTabletConflict(t, e, dir, oldID)
		})
	}
}

func mirrorReplacementFixture(t *testing.T) (*e2e, string, string) {
	t.Helper()
	e := newE2E(t)
	oldID := e.cloud.Seed("DOCUMENT/Document/Papers/a.pdf", false, []byte("before"))
	dir := e.path("mirror")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	e.must("mirror", "add", "papers", dir, "/DOCUMENT/Document/Papers")
	e.must("mirror", "run", "papers")
	writeMirrorFile(t, dir+"/a.pdf", "local edit")
	return e, dir, oldID
}

func assertMirrorTabletConflict(t *testing.T, e *e2e, dir, oldID string) {
	t.Helper()
	out := e.must("mirror", "run", "papers")
	t.Log(out)
	if !strings.Contains(out, "conflict") {
		t.Fatal("expected conflict", out)
	}
	var current port.Entry
	if err := json.Unmarshal([]byte(e.must("stat", "DOCUMENT/Document/Papers/a.pdf", "--json")), &current); err != nil {
		t.Fatal(err)
	}
	if current.ID != oldID || current.Hash != clouddouble.Sum([]byte("tablet edit")) {
		t.Fatal("tablet version overwritten", current)
	}
	if data, err := os.ReadFile(dir + "/a.pdf"); err != nil || string(data) != "tablet edit" {
		t.Fatal("tablet version not downloaded", string(data), err)
	}
	var trash []port.Entry
	if err := json.Unmarshal([]byte(e.must("trash", "ls", "--json")), &trash); err != nil || len(trash) != 0 {
		t.Fatal("tablet version recycled", trash, err)
	}
	items := mirrorJournal(t, dir+"/.snctl-mirror.json")
	if len(items) != 2 {
		t.Fatal("both versions not journaled", items)
	}
	for _, en := range items {
		if en.Recover {
			t.Fatal("recovery intent kept", items)
		}
		if en.Path == "a.pdf" {
			continue
		}
		if data, err := os.ReadFile(dir + "/" + en.Path); err != nil || string(data) != "local edit" {
			t.Fatal("local conflict lost", string(data), err)
		}
		if data, ok := e.cloud.File(en.CloudPath); !ok || string(data) != "local edit" {
			t.Fatal("cloud conflict lost", string(data), ok)
		}
	}
	if out = e.must("mirror", "run", "papers"); !strings.Contains(out, "in step") {
		t.Fatal(out)
	}
}

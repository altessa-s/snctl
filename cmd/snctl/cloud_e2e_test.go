// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/snctl/cmd/snctl/commands"
	"github.com/altessa-s/snctl/cmd/snctl/commands/clitest"
	"github.com/altessa-s/snctl/internal/adapter/config"
	"github.com/altessa-s/snctl/internal/adapter/secretfile"
	"github.com/altessa-s/snctl/internal/clouddouble"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

// e2e runs the whole program, configuration and wiring included, against the
// fake cloud through the real sncloud-go client.
type e2e struct {
	t     *testing.T
	cloud *clouddouble.Cloud
	s     seams
	root  string
}

func newE2E(t *testing.T) *e2e {
	t.Helper()
	c := clouddouble.New()
	c.RefuseLockedMoves(os.Getenv("SNCTL_TEST_REFUSE_LOCKED_MOVES") == "1")
	t.Cleanup(c.Close)
	root := t.TempDir()
	dirs := config.BaseDirs{Config: filepath.Join(root, "config"), Cache: filepath.Join(root, "cache")}
	e := &e2e{t: t, cloud: c, root: root, s: seams{keychain: &porttest.Items{}, dirs: &dirs,
		env:     map[string]string{config.EnvSecrets: "file"},
		streams: func(s *commands.Streams) { s.Getenv = func(string) string { return "" } }}}
	if _, errOut, code := e.run("profile", "add", "e2e", "--endpoint", c.URL(), "--yes"); code != 0 {
		t.Fatal(errOut)
	}
	e.s.env[config.EnvProfile] = "e2e"
	store := secretfile.New(filepath.Join(dirs.Config, "snctl", "e2e", "token.json"))
	for name, value := range map[port.SecretName]string{port.SecretAccessToken: clouddouble.Token, port.SecretClientID: "synthetic-device"} {
		if err := store.Save(t.Context(), name, value); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"NOTE/Note", "NOTE/MyStyle", "DOCUMENT/Document", "EXPORT"} {
		c.Seed(name, true, nil)
	}
	return e
}

// run returns standard output and error with the temporary root shown as $ROOT.
func (e *e2e) run(args ...string) (string, string, int) {
	e.t.Helper()
	if binary := os.Getenv("SNCTL_TEST_BINARY"); binary != "" {
		return e.binaryRun(binary, args)
	}
	in, err := os.Open(os.DevNull)
	if err != nil {
		e.t.Fatal(err)
	}
	defer in.Close()
	var out, errOut bytes.Buffer
	code := runWith(args, in, &out, &errOut, func(int) { e.t.Fatal("forced exit") }, e.s)
	clean := func(s string) string { return strings.ReplaceAll(s, e.root, "$ROOT") }
	return clean(out.String()), clean(errOut.String()), code
}

// must runs a command that has to succeed.
func (e *e2e) must(args ...string) string {
	e.t.Helper()
	out, errOut, code := e.run(args...)
	if code != 0 {
		e.t.Fatalf("%v: exit %d: %s", args, code, errOut)
	}
	return out
}

func (e *e2e) path(rel string) string { return filepath.Join(e.root, filepath.FromSlash(rel)) }

func TestE2EUploadLocalSeparators(t *testing.T) {
	for _, spelling := range []string{"native", "slashes", "trailing separator"} {
		t.Run(spelling, func(t *testing.T) {
			e := newE2E(t)
			dir := e.path("input tree")
			file := filepath.Join(dir, "nested", "й.pdf")
			if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("local bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			arg := dir
			switch spelling {
			case "slashes":
				arg = filepath.ToSlash(dir)
			case "trailing separator":
				arg += string(filepath.Separator)
			}
			r := report(t, e.must("mput", arg, "DOCUMENT/Document", "--yes", "--json"))
			if r.Counts[port.StatusUploaded] != 1 || r.Counts[port.StatusCreated] != 1 {
				t.Fatal(r)
			}
			data, ok := e.cloud.File("DOCUMENT/Document/nested/й.pdf")
			if !ok || string(data) != "local bytes" {
				t.Fatal("local separators changed the upload destination or bytes", string(data), ok)
			}
		})
	}
}

// stamp is an RFC 3339 time in JSON, which carries the machine's zone.
var stamp = regexp.MustCompile(`"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(Z|[+-]\d\d:\d\d)"`)

// testdata is resolved before any test changes the working directory.
var testdata, _ = filepath.Abs("testdata")

func (e *e2e) golden(name, got string) {
	e.t.Helper()
	if runtime.GOOS == "windows" {
		return // local paths in the reports use the platform's separator
	}
	got = stamp.ReplaceAllStringFunc(got, func(s string) string {
		t, err := time.Parse(time.RFC3339, strings.Trim(s, `"`))
		if err != nil {
			return s
		}
		return `"` + t.UTC().Format(time.RFC3339) + `"`
	})
	file := filepath.Join(testdata, "e2e-"+name+".golden")
	if os.Getenv(clitest.UpdateEnv) == "1" {
		if err := os.WriteFile(file, []byte(got), 0o600); err != nil {
			e.t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(file) // #nosec G304 -- a golden file of the test
	if err != nil {
		e.t.Fatalf("%v (run with %s=1 to create it)", err, clitest.UpdateEnv)
	}
	if got != string(want) {
		e.t.Errorf("%s differs from the golden file:\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

// listings counts folder listings since a request log position.
func (e *e2e) listings(since int) int {
	n := 0
	for _, r := range e.cloud.Requests()[since:] {
		if r.Path == "/api/file/2/files/list_folder" {
			n++
		}
	}
	return n
}

func (e *e2e) seedTree(top string, n int) {
	for i := range n {
		dir := []string{"", "a/", "a/b/", "c/"}[i%4]
		e.cloud.Seed(fmt.Sprintf("%s/%sfile%02d.note", top, dir, i), false, []byte(strings.Repeat(string(rune('a'+i%26)), 100+i)))
	}
}

func report(t *testing.T, out string) port.TransferReport {
	t.Helper()
	var r port.TransferReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err, out)
	}
	return r
}

func TestE2EFileCommands(t *testing.T) {
	e := newE2E(t)
	e.cloud.Seed("NOTE/Note/a.note", false, []byte("alpha"))
	e.cloud.Seed("NOTE/Note/copy.note", false, []byte("alpha"))
	if err := os.WriteFile(e.path("one.pdf"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.path("two.pdf"), []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"table", "plain", "json"} {
		e.golden("ls-"+format, e.must("ls", "NOTE/Note", "--format", format))
		e.golden("du-"+format, e.must("du", "/", "-d", "2", "--format", format))
		e.golden("dupes-"+format, e.must("dupes", "--format", format))
	}
	// put: several files, a dry run that writes nothing, a conflict, a replacement.
	start := len(e.cloud.Writes())
	e.golden("put-dry-run", e.must("put", e.path("one.pdf"), e.path("two.pdf"), "DOCUMENT/Document", "--dry-run", "--json"))
	if len(e.cloud.Writes()) != start {
		t.Fatal("dry run wrote", e.cloud.Writes()[start:])
	}
	e.golden("put", e.must("put", e.path("one.pdf"), e.path("two.pdf"), "--to", "DOCUMENT/Document", "--yes", "--json"))
	if _, errOut, code := e.run("put", e.path("one.pdf"), "DOCUMENT/Document", "--yes"); code != 4 || !strings.Contains(errOut, "--replace") {
		t.Fatal(code, errOut)
	}
	e.must("put", e.path("two.pdf"), "DOCUMENT/Document/one.pdf", "--replace", "--yes")
	if data, _ := e.cloud.File("DOCUMENT/Document/one.pdf"); string(data) != "two" {
		t.Fatal(string(data))
	}
	e.must("put", e.path("one.pdf"), "DOCUMENT/Document", "--name", "renamed.pdf", "--yes")
	if _, _, code := e.run("put", e.path("one.pdf"), "DOCUMENT/x.pdf", "--yes"); code != 1 {
		t.Fatal("write into a virtual category accepted", code)
	}
	// One planning snapshot, one re-check and one verification per command.
	for _, tc := range []struct {
		args     []string
		listings int
	}{
		{[]string{"mkdir", "-p", "DOCUMENT/Document/x/y/z", "--yes"}, 3},
		{[]string{"mkdir", "-p", "DOCUMENT/Document/x/y", "--yes"}, 1},
		{[]string{"mv", "DOCUMENT/Document/one.pdf", "DOCUMENT/Document/x/one.pdf", "--yes"}, 5},
		{[]string{"rename", "DOCUMENT/Document/x/one.pdf", "uno.pdf", "--yes"}, 4},
		{[]string{"rm", "DOCUMENT/Document/x/uno.pdf", "--yes"}, 3},
	} {
		since := len(e.cloud.Requests())
		e.must(tc.args...)
		if n := e.listings(since); n != tc.listings {
			t.Fatalf("%v listed the tree %d times", tc.args, n)
		}
	}
	trash := e.must("trash", "--json")
	var recycled []port.Entry
	if err := json.Unmarshal([]byte(trash), &recycled); err != nil || len(recycled) != 1 {
		t.Fatal(trash, err)
	}
	since := len(e.cloud.Requests())
	for _, entry := range recycled {
		if entry.Name == "uno.pdf" {
			e.must("restore", entry.ID, "--yes")
		}
	}
	if e.listings(since) != 4 {
		t.Fatalf("restore listings = %d, want planning, recheck, targeted parent and verification", e.listings(since))
	}
	if _, ok := e.cloud.File("DOCUMENT/Document/x/uno.pdf"); !ok {
		t.Fatal(e.cloud.Paths())
	}
	// Unconfirmed changes are refused before any request that writes.
	start = len(e.cloud.Writes())
	for _, args := range [][]string{{"mkdir", "DOCUMENT/Document/q"}, {"mv", "DOCUMENT/Document/two.pdf", "DOCUMENT/Document/q.pdf"},
		{"rm", "DOCUMENT/Document/two.pdf"}, {"put", e.path("one.pdf"), "DOCUMENT/Document/q.pdf"}} {
		if _, _, code := e.run(args...); code != 2 {
			t.Fatalf("%v: %d", args, code)
		}
	}
	if len(e.cloud.Writes()) != start {
		t.Fatal(e.cloud.Writes()[start:])
	}
	// get and stat through the same client.
	e.must("get", "NOTE/Note/a.note", "-o", e.path("a.note"))
	if data, _ := os.ReadFile(e.path("a.note")); string(data) != "alpha" {
		t.Fatal(string(data))
	}
	e.golden("stat", e.must("stat", "NOTE/Note/a.note", "--json"))
}

func TestE2EMget(t *testing.T) {
	e := newE2E(t)
	e.seedTree("NOTE/Note/Tree", 36)
	e.cloud.Seed("NOTE/Note/Tree/a/Same.note", false, []byte("upper"))
	e.cloud.Seed("NOTE/Note/Tree/a/same.note", false, []byte("lower"))
	out := e.path("out")
	r := report(t, e.must("mget", "NOTE/Note/Tree", "-o", out, "-j", "8", "--json"))
	if r.Counts[port.StatusDownloaded] != 38 {
		t.Fatal(r.Counts)
	}
	// Names that differ only in case both land, told apart by identifier.
	for _, name := range []string{"a/Same (47).note", "a/same (48).note", "a/b/file02.note"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(name))); err != nil {
			t.Fatal(err)
		}
	}
	// Incremental: nothing to download.
	since := e.cloud.Count("/api/file/3/files/download_v3")
	r = report(t, e.must("mget", "NOTE/Note/Tree", "-o", out, "--incremental", "--json"))
	if r.Counts[port.StatusUnchanged] != 38 || e.cloud.Count("/api/file/3/files/download_v3") != since {
		t.Fatal(r.Counts)
	}
	// A file removed in the cloud goes with --delete; a local file snctl did not write stays.
	e.cloud.Remove("NOTE/Note/Tree/file00.note")
	if err := os.WriteFile(filepath.Join(out, "mine.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	r = report(t, e.must("mget", "NOTE/Note/Tree", "-o", out, "--incremental", "--delete", "--json"))
	if r.Counts[port.StatusDeleted] != 1 || r.Counts[port.StatusUnchanged] != 37 {
		t.Fatal(r.Counts)
	}
	if _, err := os.Stat(filepath.Join(out, "file00.note")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "mine.txt")); err != nil {
		t.Fatal(err)
	}
	// The guard: an emptied folder deletes nothing without --force.
	e.cloud.Remove("NOTE/Note/Tree")
	e.cloud.Seed("NOTE/Note/Tree", true, nil)
	if _, errOut, code := e.run("mget", "NOTE/Note/Tree", "-o", out, "--delete"); code != 4 || !strings.Contains(errOut, "--force") {
		t.Fatal(code, errOut)
	}
	if _, err := os.Stat(filepath.Join(out, "file04.note")); err != nil {
		t.Fatal("the guard deleted", err)
	}
	e.must("mget", "NOTE/Note/Tree", "-o", out, "--delete", "--force")
	if _, err := os.Stat(filepath.Join(out, "file04.note")); err != nil {
		t.Fatal(err)
	}
}

func TestE2EMgetFaults(t *testing.T) {
	e := newE2E(t)
	e.seedTree("DOCUMENT/Document/T", 12)
	out := e.path("out")
	// A checksum lie and a dropped connection mid-tree: those files fail, the others land.
	e.cloud.Inject(clouddouble.Fault{Path: "DOCUMENT/Document/T/a/file01.note", Count: 1, Checksum: true})
	e.cloud.Inject(clouddouble.Fault{Path: "/api/oss/download", Count: 3, Drop: true})
	stdout, errOut, code := e.run("mget", "DOCUMENT/Document/T", "-o", out, "-j", "1", "--json")
	r := report(t, stdout)
	if code != 1 || r.Counts[port.StatusFailed] != 2 || r.Counts[port.StatusDownloaded] != 10 || !strings.Contains(errOut, "2 of 12") {
		t.Fatal(code, r.Counts, errOut)
	}
	var record struct {
		Entries map[string]any `json:"entries"`
	}
	data, err := os.ReadFile(filepath.Join(out, ".snctl-mget.json"))
	if err != nil || json.Unmarshal(data, &record) != nil || len(record.Entries) != 10 {
		t.Fatal(len(record.Entries), err)
	}
	noTemporaryFiles(t, out)
	// The next incremental run fetches only what failed.
	r = report(t, e.must("mget", "DOCUMENT/Document/T", "-o", out, "--incremental", "--json"))
	if r.Counts[port.StatusDownloaded] != 2 || r.Counts[port.StatusUnchanged] != 10 {
		t.Fatal(r.Counts)
	}
	// An expired session is exit 3, before anything is written.
	e.cloud.Inject(clouddouble.Fault{Path: "/api/file/2/files/list_folder", Count: 1, Expired: true})
	if _, errOut, code := e.run("mget", "DOCUMENT/Document/T", "-o", e.path("other")); code != 3 || !strings.Contains(errOut, "login") {
		t.Fatal(code, errOut)
	}
	if _, err := os.Stat(e.path("other")); !os.IsNotExist(err) {
		t.Fatal("an expired session created the output", err)
	}
}

func noTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(p string, _ os.DirEntry, err error) error {
		if strings.HasPrefix(filepath.Base(p), ".snctl-") && filepath.Base(p) != ".snctl-mget.json" {
			t.Errorf("temporary file left: %s", p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestE2EMput(t *testing.T) {
	e := newE2E(t)
	src := e.path("src")
	for name, data := range map[string]string{"a.note": "alpha", "sub/b.pdf": "beta", "sub/deep/c.epub": "gamma",
		"skip.txt": "text", ".hidden/x.pdf": "x", "empty.pdf": ""} {
		p := filepath.Join(src, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	start := len(e.cloud.Writes())
	e.golden("mput-dry-run", e.must("mput", src, "DOCUMENT/Document/In", "--dry-run", "--format", "plain"))
	if len(e.cloud.Writes()) != start {
		t.Fatal("dry run wrote", e.cloud.Writes()[start:])
	}
	if _, _, code := e.run("mput", src, "DOCUMENT/Document/In"); code != 2 || len(e.cloud.Writes()) != start {
		t.Fatal("unconfirmed mput", code)
	}
	r := report(t, e.must("mput", src, "DOCUMENT/Document/In", "-j", "4", "--yes", "--json"))
	if r.Counts[port.StatusUploaded] != 3 || r.Counts[port.StatusCreated] != 3 || r.Counts[port.StatusSkipped] != 3 {
		t.Fatal(r.Counts)
	}
	if data, _ := e.cloud.File("DOCUMENT/Document/In/sub/deep/c.epub"); string(data) != "gamma" {
		t.Fatal(e.cloud.Paths())
	}
	if e.cloud.Count("/api/file/2/files/synchronous/start") != 1 || e.cloud.Count("/api/file/2/files/synchronous/end") != 1 {
		t.Fatal("the batch did not run under the sync lock")
	}
	uploads := e.cloud.Count("/api/file/2/files/upload/finish")
	r = report(t, e.must("mput", src, "DOCUMENT/Document/In", "--incremental", "--yes", "--json"))
	if r.Counts[port.StatusUnchanged] != 3 || e.cloud.Count("/api/file/2/files/upload/finish") != uploads {
		t.Fatal(r.Counts)
	}
	if _, errOut, code := e.run("mput", src, "DOCUMENT/Document/In", "--yes"); code != 4 || !strings.Contains(errOut, "--replace") {
		t.Fatal(code, errOut)
	}
	e.cloud.Hold("tablet")
	if err := os.WriteFile(filepath.Join(src, "new.note"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := e.run("mput", src, "DOCUMENT/Document/In", "--incremental", "--yes"); code != 4 || !strings.Contains(errOut, "syncing") {
		t.Fatal(code, errOut)
	}
	if _, ok := e.cloud.File("DOCUMENT/Document/In/new.note"); ok {
		t.Fatal("uploaded without the lock")
	}
}

// Every new command in every format, on a small fixed tree. The commands run
// in the temporary root with relative paths, so that table widths repeat.
func TestE2EGoldens(t *testing.T) {
	for _, format := range []string{"table", "plain", "json"} {
		t.Run(format, func(t *testing.T) {
			e := newE2E(t)
			t.Chdir(e.root)
			e.seedTree("NOTE/Note/G", 3)
			e.cloud.Seed("NOTE/Note/G/dup.note", false, []byte(strings.Repeat("a", 100)))
			e.golden("mget-"+format, e.must("mget", "NOTE/Note/G", "-o", "out", "--format", format))
			e.golden("mget-incremental-"+format, e.must("mget", "NOTE/Note/G", "-o", "out", "--incremental", "--format", format))
			e.golden("mput-"+format, e.must("mput", "out", "DOCUMENT/Document/G", "--yes", "--format", format))
			e.golden("mput-incremental-"+format, e.must("mput", "out", "DOCUMENT/Document/G", "--incremental", "--dry-run", "--format", format))
			e.golden("mkdir-p-"+format, e.must("mkdir", "-p", "NOTE/Note/G/x/y", "--yes", "--format", format))
			e.golden("put-"+format, e.must("put", filepath.Join("out", "file00.note"), "NOTE/Note/G/x", "--yes", "--format", format))
			e.golden("du-G-"+format, e.must("du", "NOTE/Note/G", "--format", format))
			e.golden("dupes-G-"+format, e.must("dupes", "NOTE/Note/G", "--format", format))
		})
	}
}

// The round trip: a tree mget writes goes back with mput / into an empty account.
func TestE2ERoundTrip(t *testing.T) {
	e := newE2E(t)
	e.seedTree("NOTE/Note/R", 8)
	e.cloud.Seed("DOCUMENT/Document/book.pdf", false, []byte("book"))
	out := e.path("all")
	e.must("mget", "/", "-o", out)
	for _, p := range e.cloud.Paths() {
		e.cloud.Remove(p)
	}
	for _, name := range []string{"NOTE/Note", "DOCUMENT/Document"} {
		e.cloud.Seed(name, true, nil)
	}
	r := report(t, e.must("mput", out, "--yes", "--json"))
	if r.Counts[port.StatusUploaded] != 9 {
		t.Fatal(r.Counts, r.Items)
	}
	if data, _ := e.cloud.File("NOTE/Note/R/a/file01.note"); len(data) != 101 {
		t.Fatal(e.cloud.Paths())
	}
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/adapter/localfs"
	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/app/notebooks"
	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestPassDirectionsAndQuiet(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.write("local.pdf", "local")
	f.c.seed("DOCUMENT/Document/Papers/cloud.epub", "cloud")
	p := f.pass(port.MirrorRunOptions{})
	if !has(p, port.MirrorUpload) || !has(p, port.MirrorDownload) || f.read("cloud.epub") != "cloud" {
		t.Fatal(p)
	}
	if p = f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
		t.Fatal(p)
	}
	f.write("local.pdf", "local changed")
	p = f.pass(port.MirrorRunOptions{})
	if !has(p, port.MirrorUpdate) || len(f.c.trash) != 1 || f.c.data[f.remote("local.pdf").ID] != "local changed" {
		t.Fatal(p, f.c.trash)
	}
	f.remoteChange("cloud.epub", "cloud changed")
	p = f.pass(port.MirrorRunOptions{})
	if !has(p, port.MirrorDownload) || f.read("cloud.epub") != "cloud changed" || f.read(".snctl-removed/cloud.epub") != "cloud" {
		t.Fatal(p)
	}
	if p = f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
		t.Fatal(p)
	}
}

func TestNewCloudExcludesReservedReplacementNames(t *testing.T) {
	f := setup(t)
	f.c.seed("DOCUMENT/Document/Papers/x (replacement 22af645d1859cb5ca6da0c484f1f37ea 1).pdf", "new")
	f.c.seed("DOCUMENT/Document/Papers/sub/y (replacement 22af645d1859cb5ca6da0c484f1f37ea 999).epub", "new")
	f.c.seed("DOCUMENT/Document/Papers/ordinary (replacement draft 1).pdf", "ordinary")
	p := f.pass(port.MirrorRunOptions{})
	if len(p.Actions) != 1 || p.Actions[0].Path != "ordinary (replacement draft 1).pdf" {
		t.Fatalf("unexpected downloads: %+v", p.Actions)
	}
}
func TestNewLocalExcludesReservedReplacementNames(t *testing.T) {
	f := setup(t)
	f.write("x (replacement 22af645d1859cb5ca6da0c484f1f37ea 1).pdf", "new")
	f.write("sub/y (replacement 22af645d1859cb5ca6da0c484f1f37ea 999).epub", "new")
	f.write("ordinary (replacement draft 1).pdf", "ordinary")
	p := f.pass(port.MirrorRunOptions{})
	if len(p.Actions) != 1 || p.Actions[0].Path != "ordinary (replacement draft 1).pdf" {
		t.Fatalf("unexpected uploads: %+v", p.Actions)
	}
}

func TestConflictsKeepBoth(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.write("x.pdf", "initial")
	f.pass(port.MirrorRunOptions{})
	f.write("x.pdf", "local version")
	f.remoteChange("x.pdf", "tablet version")
	p := f.pass(port.MirrorRunOptions{})
	if !has(p, port.MirrorConflict) || f.read("x.pdf") != "tablet version" {
		t.Fatal(p)
	}
	var conflict string
	for _, a := range p.Actions {
		if a.Kind == port.MirrorConflict {
			conflict = a.To
		}
	}
	if f.read(conflict) != "local version" || f.c.data[f.remote(conflict).ID] != "local version" {
		t.Fatal(p)
	}
	if p = f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
		t.Fatal(p)
	}
}
func TestInitialDifferentNamesConflict(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.write("x.pdf", "local")
	f.c.seed("DOCUMENT/Document/Papers/x.pdf", "cloud")
	if p := f.pass(port.MirrorRunOptions{}); !has(p, port.MirrorConflict) || f.read("x.pdf") != "cloud" {
		t.Fatal(p)
	}
}
func TestRenamesAndMoves(t *testing.T) {
	t.Parallel()
	for _, side := range []string{"local", "cloud"} {
		t.Run(side, func(t *testing.T) {
			f := setup(t)
			f.write("a.pdf", "alpha")
			f.write("keep.pdf", "keep")
			f.pass(port.MirrorRunOptions{})
			old := f.remote("a.pdf")
			if side == "local" {
				if err := os.Mkdir(filepath.Join(f.dir, "sub"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(filepath.Join(f.dir, "a.pdf"), filepath.Join(f.dir, "sub/b.pdf")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := f.c.Move(t.Context(), old, "DOCUMENT/Document/Papers/sub/b.pdf"); err != nil {
					t.Fatal(err)
				}
			}
			p := f.pass(port.MirrorRunOptions{})
			kind := port.MirrorMoveLocal
			if side == "local" {
				kind = port.MirrorMoveCloud
			}
			if !has(p, kind) || f.remote("sub/b.pdf").ID != old.ID || f.read("sub/b.pdf") != "alpha" {
				t.Fatal(p)
			}
			if p = f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
				t.Fatal(p)
			}
		})
	}
}

func TestRenameAlreadyAtCloudDestination(t *testing.T) {
	t.Parallel()
	for _, interrupted := range []bool{false, true} {
		t.Run(fmt.Sprintf("interrupted=%t", interrupted), func(t *testing.T) {
			f := setup(t)
			f.write("a.pdf", "alpha")
			f.pass(port.MirrorRunOptions{})
			old := f.remote("a.pdf")
			if err := os.Rename(filepath.Join(f.dir, "a.pdf"), filepath.Join(f.dir, "b.pdf")); err != nil {
				t.Fatal(err)
			}
			if interrupted {
				disk := f.e.disk
				f.e.disk = faultDisk{MirrorDisk: disk, root: func(r port.MirrorDirectory) port.MirrorDirectory {
					return faultRoot{MirrorDirectory: r, fail: "write-journal"}
				}}
				if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); err == nil {
					t.Fatal("journal failure ignored")
				}
				f.e.disk = disk
			} else if err := f.c.Move(t.Context(), old, "DOCUMENT/Document/Papers/b.pdf"); err != nil {
				t.Fatal(err)
			}
			if f.remote("b.pdf").ID != old.ID {
				t.Fatal("cloud move did not finish")
			}
			f.c.fail["move"] = errBoom
			p := f.pass(port.MirrorRunOptions{})
			if len(p.Actions) != 0 || p.Pending != 0 {
				t.Fatal("already paired names should be in step", p)
			}
			root, err := f.e.disk.OpenMirror(t.Context(), f.dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			j, err := readJournal(root)
			if err != nil || j.Items[old.ID].Path != "b.pdf" || j.Items[old.ID].CloudPath != "DOCUMENT/Document/Papers/b.pdf" {
				t.Fatal("pairing was not saved in one pass", j, err)
			}
		})
	}
}
func TestDeletionsRetainAndGuard(t *testing.T) {
	t.Parallel()
	for _, side := range []string{"local", "cloud"} {
		t.Run(side, func(t *testing.T) {
			f := setup(t)
			f.write("a.pdf", "alpha")
			f.write("b.pdf", "beta")
			f.pass(port.MirrorRunOptions{})
			if side == "local" {
				if err := os.Remove(filepath.Join(f.dir, "a.pdf")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := f.c.Delete(t.Context(), f.remote("a.pdf"), false); err != nil {
					t.Fatal(err)
				}
			}
			p := f.pass(port.MirrorRunOptions{})
			if side == "local" {
				if !has(p, port.MirrorTrash) || len(f.c.trash) != 1 {
					t.Fatal(p)
				}
			} else {
				if !has(p, port.MirrorRemove) || f.read(".snctl-removed/a.pdf") != "alpha" {
					t.Fatal(p)
				}
			}
		})
	}
	for _, count := range []int{1, 3, 51} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := setup(t)
			for i := range count {
				f.write(fmt.Sprintf("%d.pdf", i), fmt.Sprint(i))
			}
			f.pass(port.MirrorRunOptions{})
			for i := range count {
				if err := os.Remove(filepath.Join(f.dir, fmt.Sprintf("%d.pdf", i))); err != nil {
					t.Fatal(err)
				}
			}
			p := f.pass(port.MirrorRunOptions{})
			if p.Pending != count || !has(p, port.MirrorRefuse) || len(f.c.trash) != 0 {
				t.Fatal(p)
			}
			f.pass(port.MirrorRunOptions{ForceDelete: true})
			if len(f.c.trash) != count {
				t.Fatal(len(f.c.trash))
			}
		})
	}
	if guard(2, 3, false, "x") == "" || guard(51, 200, false, "x") == "" || guard(1, 2, false, "x") != "" {
		t.Fatal("guard")
	}
}
func TestNoDeleteDetaches(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.write("x.pdf", "x")
	f.pass(port.MirrorRunOptions{})
	f.spec.NoDelete = true
	if err := os.Remove(filepath.Join(f.dir, "x.pdf")); err != nil {
		t.Fatal(err)
	}
	if p := f.pass(port.MirrorRunOptions{}); !has(p, port.MirrorDetach) {
		t.Fatal(p)
	}
	if p := f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
		t.Fatal(p)
	}
	if len(f.c.trash) != 0 {
		t.Fatal("deleted")
	}
}

func TestLocalDeletionRestoresChangedCloudCopy(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.write("a.pdf", "before")
	f.pass(port.MirrorRunOptions{})
	old := f.remote("a.pdf")
	if err := os.Remove(filepath.Join(f.dir, "a.pdf")); err != nil {
		t.Fatal(err)
	}
	f.remoteChange("a.pdf", "tablet edit")
	p := f.pass(port.MirrorRunOptions{})
	if len(p.Actions) != 1 || p.Actions[0].Kind != port.MirrorDownload || p.Actions[0].Detail != "deleted here, changed there: restored" || p.Pending != 0 {
		t.Fatal(p)
	}
	if f.read("a.pdf") != "tablet edit" || f.remote("a.pdf").ID != old.ID || len(f.c.trash) != 0 {
		t.Fatal("changed cloud copy was not restored intact", f.c.trash)
	}
	if p = f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
		t.Fatal(p)
	}
}
func TestNamesAndIgnoredSidecars(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.write("Cafe\u0301.PDF", "pdf")
	cloud := f.c.seed("DOCUMENT/Document/Papers/Caf\u00e9.pdf", "pdf")
	f.c.seed("DOCUMENT/Document/Papers/Caf\u00e9.mark", "marks")
	for _, rel := range []string{".hidden/a.pdf", "_annotated/a.pdf", "~$part.pdf", "a.pdf.part", "Thumbs.db", "desktop.ini", "a.mark", "a.note"} {
		f.write(rel, "ignored")
	}
	p := f.pass(port.MirrorRunOptions{})
	if len(p.Actions) != 1 || !has(p, port.MirrorAdopt) {
		t.Fatal(p)
	}
	f.remoteChange("Caf\u00e9.mark", "new marks")
	if p = f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
		t.Fatal(p)
	}
	if err := f.c.Move(t.Context(), cloud, "DOCUMENT/Document/Papers/CAF\u00c9.pdf"); err != nil {
		t.Fatal(err)
	}
	if p = f.pass(port.MirrorRunOptions{}); !has(p, port.MirrorMoveLocal) {
		t.Fatal(p)
	}
}
func TestRefuseDamagedJournalAndMovedFolder(t *testing.T) {
	t.Parallel()
	for _, data := range []string{"{", `{"v":2,"items":{}}`, `{"v":1,"items":{"a":{"path":"../x.pdf","cloud_path":"DOCUMENT/Document/Papers/x.pdf"}}}`, `{"v":1,"items":{"a":{"path":"x.pdf","cloud_path":"DOCUMENT/Document/Papers/x.pdf","output":"outside.pdf"}}}`} {
		f := setup(t)
		f.write(domain.MirrorJournalName, data)
		if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, ErrJournal) {
			t.Fatal(err)
		}
	}
	f := setup(t)
	f.write("a.pdf", "a")
	f.pass(port.MirrorRunOptions{})
	top, err := f.c.Stat(t.Context(), "DOCUMENT/Document/Papers")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.c.Move(t.Context(), top, "DOCUMENT/Document/Moved"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, port.ErrMirrorRefused) || !strings.Contains(err.Error(), "/DOCUMENT/Document/Moved") {
		t.Fatal(err)
	}
	if f.read("a.pdf") != "a" {
		t.Fatal("lost local")
	}
}

func TestFolderPairingUsesFoldedNames(t *testing.T) {
	t.Parallel()
	for _, folder := range []string{"/document/document/papers", "/DOCUMENT/Document/Cafe\u0301"} {
		t.Run(folder, func(t *testing.T) {
			f := setup(t)
			cloudFolder := "DOCUMENT/Document/Papers"
			if strings.Contains(folder, "Cafe") {
				cloudFolder = "DOCUMENT/Document/Caf\u00e9"
				f.c.seed(cloudFolder, "<folder>")
			}
			top, err := f.c.Stat(t.Context(), cloudFolder)
			if err != nil {
				t.Fatal(err)
			}
			f.c.seed(cloudFolder+"/cloud.pdf", "cloud")
			f.spec.Folder = folder
			f.c.fail["mkdir"] = errBoom
			p := f.pass(port.MirrorRunOptions{})
			if !has(p, port.MirrorDownload) || f.read("cloud.pdf") != "cloud" || f.spec.Folder != folder {
				t.Fatal(p, f.spec)
			}
			delete(f.c.fail, "mkdir")
			f.write("local.pdf", "local")
			f.pass(port.MirrorRunOptions{})
			if _, err = f.c.Stat(t.Context(), cloudFolder+"/local.pdf"); err != nil {
				t.Fatal("upload used the configured alias", err)
			}
			if p = f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
				t.Fatal(p)
			}
			if err = f.c.Move(t.Context(), top, cloudFolder+"-moved"); err != nil {
				t.Fatal(err)
			}
			if _, err = f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, port.ErrMirrorRefused) {
				t.Fatal("moved folder accepted", err)
			}
		})
	}
}

func TestAmbiguousFoldedFoldersRefused(t *testing.T) {
	t.Parallel()
	for _, alias := range []string{"DOCUMENT/Document/PAPERS", "document/document/papers"} {
		f := setup(t)
		f.c.seed(alias, "<folder>")
		if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, port.ErrMirrorRefused) {
			t.Fatal("ambiguous folders accepted", alias, err)
		}
	}
}
func TestInterruptedReplacementResumes(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.write("x.pdf", "before")
	f.pass(port.MirrorRunOptions{})
	f.write("x.pdf", "after")
	ctx, cancel := context.WithCancel(t.Context())
	f.c.afterDelete = cancel
	if _, err := f.e.Pass(ctx, f.spec, port.MirrorRunOptions{}); err == nil {
		t.Fatal("cancellation ignored")
	}
	f.c.afterDelete = nil
	p := f.pass(port.MirrorRunOptions{})
	if !has(p, port.MirrorAdopt) && !has(p, port.MirrorUpload) {
		t.Fatal(p)
	}
	if f.c.data[f.remote("x.pdf").ID] != "after" {
		t.Fatal("did not resume")
	}
	if p = f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
		t.Fatal(p)
	}
}

func TestUnlockedDeletionAndFolderRename(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		t.Run(fmt.Sprintf("refuse-locked-moves=%t", refuse), func(t *testing.T) {
			f := setup(t)
			f.c.refuseLockedMoves = refuse
			f.write("old/a.pdf", "a")
			f.write("gone/last.pdf", "last")
			f.pass(port.MirrorRunOptions{})
			if err := os.Rename(filepath.Join(f.dir, "old"), filepath.Join(f.dir, "new")); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(f.dir, "gone/last.pdf")); err != nil {
				t.Fatal(err)
			}
			f.write("new/b.pdf", "b")
			p := f.pass(port.MirrorRunOptions{})
			if !has(p, port.MirrorTrash) || !has(p, port.MirrorMoveCloud) || !has(p, port.MirrorUpload) || p.Pending != 0 {
				t.Fatal(p)
			}
			f.remote("new/a.pdf")
			f.remote("new/b.pdf")
			f.remote("gone")
			if len(f.c.trash) != 1 {
				t.Fatal(f.c.trash)
			}
			if p = f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
				t.Fatal("completed changes repeated", p)
			}
		})
	}
}

func TestRenameResumesBetweenCreationMoveAndUpload(t *testing.T) {
	for _, fail := range []string{"move", "upload"} {
		t.Run(fail, func(t *testing.T) {
			f := setup(t)
			f.c.refuseLockedMoves = true
			f.write("old/a.pdf", "a")
			f.pass(port.MirrorRunOptions{})
			if err := os.Rename(filepath.Join(f.dir, "old"), filepath.Join(f.dir, "new")); err != nil {
				t.Fatal(err)
			}
			f.write("new/b.pdf", "b")
			f.c.fail[fail] = errBoom
			if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); err == nil {
				t.Fatal("failure ignored")
			}
			delete(f.c.fail, fail)
			f.pass(port.MirrorRunOptions{})
			f.remote("new/a.pdf")
			f.remote("new/b.pdf")
			if p := f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
				t.Fatal(p)
			}
		})
	}
}
func TestDrawNotebookAndRemoval(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.spec.PullNew = true
	note := f.c.seed("DOCUMENT/Document/Papers/sub/A.note", "notebook")
	p := f.pass(port.MirrorRunOptions{})
	if !has(p, port.MirrorDraw) || f.read("_annotated/notebooks/sub/A.pdf") != "%PDF notebook" {
		t.Fatal(p)
	}
	if p = f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
		t.Fatal(p)
	}
	if err := f.c.Move(t.Context(), note, "DOCUMENT/Document/Papers/B.note"); err != nil {
		t.Fatal(err)
	}
	f.pass(port.MirrorRunOptions{})
	if f.read("_annotated/notebooks/B.pdf") != "%PDF notebook" || f.read(".snctl-removed/_annotated/notebooks/sub/A.pdf") != "%PDF notebook" {
		t.Fatal("drawing move")
	}
	note = f.remote("B.note")
	if err := f.c.Delete(t.Context(), note, false); err != nil {
		t.Fatal(err)
	}
	f.pass(port.MirrorRunOptions{ForceDelete: true})
	if f.read(".snctl-removed/_annotated/notebooks/B.pdf") != "%PDF notebook" {
		t.Fatal("drawing removal")
	}
}

type drawingRequests struct {
	port.Files
	lookups, downloads int
}

func (c *drawingRequests) Stat(ctx context.Context, name string) (port.Entry, error) {
	c.lookups++
	return c.Files.Stat(ctx, name)
}

func (c *drawingRequests) Download(ctx context.Context, e port.Entry, p port.Progress) (string, error) {
	c.downloads++
	return c.Files.Download(ctx, e, p)
}

func TestDrawingUsesListedEntries(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.spec.PullNew = true
	f.c.seed("DOCUMENT/Document/Papers/a.note", "first notebook")
	f.c.seed("DOCUMENT/Document/Papers/b.note", "second notebook")
	c := &drawingRequests{Files: f.c}
	disk := localfs.Files{}
	f.e.cloud = c
	f.e.notes = notebooks.Service{
		Fetch: files.Service{Cloud: c, Local: disk}, Files: disk,
		Reader:   porttest.NoteReader{Notebook: port.Notebook{Pages: []port.NotePage{{Number: 1}}}},
		Renderer: porttest.Renderer{Pages: 1},
	}
	p := f.pass(port.MirrorRunOptions{})
	if len(p.Actions) != 2 || c.lookups != 0 || c.downloads != 2 {
		t.Fatalf("drawing requests: lookups=%d downloads=%d pass=%+v", c.lookups, c.downloads, p)
	}
	if p = f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 || c.lookups != 0 || c.downloads != 2 {
		t.Fatal("idle pass fetched notebooks", p, c)
	}
}
func TestDryRunAndLock(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.write("a.pdf", "a")
	p := f.pass(port.MirrorRunOptions{DryRun: true})
	if !has(p, port.MirrorUpload) {
		t.Fatal(p)
	}
	if _, err := os.Stat(filepath.Join(f.dir, domain.MirrorJournalName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	root, err := f.e.disk.OpenMirror(t.Context(), f.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err = f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, port.ErrMirrorBusy) {
		t.Fatal(err)
	}
}
func TestJournalRecoveryDownload(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.write("a.pdf", "old")
	f.pass(port.MirrorRunOptions{})
	f.remoteChange("a.pdf", "new")
	root, err := f.e.disk.OpenMirror(t.Context(), f.dir)
	if err != nil {
		t.Fatal(err)
	}
	j, err := readJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	en := j.Items[f.remote("a.pdf").ID]
	en.RestoreLocal = true
	j.Items[f.remote("a.pdf").ID] = en
	if err = j.write(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	root.Close()
	if err = os.Rename(filepath.Join(f.dir, "a.pdf"), filepath.Join(f.dir, "saved")); err != nil {
		t.Fatal(err)
	}
	f.pass(port.MirrorRunOptions{})
	if f.read("a.pdf") != "new" || f.read("saved") != "old" {
		t.Fatal("recovery")
	}
	data, err := os.ReadFile(filepath.Join(f.dir, domain.MirrorJournalName))
	if err != nil {
		t.Fatal(err)
	}
	var saved journal
	if err = json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.LastPass.IsZero() || saved.Items[f.remote("a.pdf").ID].RestoreLocal {
		t.Fatal(saved)
	}
}
func TestRecentFileWaits(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.write("a.pdf", "a")
	if err := os.Chtimes(filepath.Join(f.dir, "a.pdf"), now, now); err != nil {
		t.Fatal(err)
	}
	if p := f.pass(port.MirrorRunOptions{}); !has(p, port.MirrorSkip) || p.Pending != 1 {
		t.Fatal(p)
	}
	if !(&localFile{ModTime: now.Add(2 * time.Hour)}).settled(now) {
		t.Fatal("future timestamp")
	}
}

func TestLongPassCheckpointsNewUploads(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.write("a.pdf", "a")
	f.write("b.pdf", "b")
	tm := porttest.NewTimer(now)
	f.e.timer = tm
	calls := 0
	f.c.afterUpload = func() {
		calls++
		if calls == 1 {
			tm.Jump(checkpointEvery)
		}
		if calls == 2 {
			data, err := os.ReadFile(filepath.Join(f.dir, domain.MirrorJournalName))
			if err != nil {
				t.Fatal(err)
			}
			var j journal
			if err = json.Unmarshal(data, &j); err != nil {
				t.Fatal(err)
			}
			if len(j.Items) != 1 {
				t.Fatalf("first upload not checkpointed: %+v", j)
			}
		}
	}
	f.pass(port.MirrorRunOptions{})
	if calls != 2 {
		t.Fatal(calls)
	}
}

func TestOtherWritableRootsAndServerTypeRefusal(t *testing.T) {
	t.Parallel()
	for _, folder := range []string{"/NOTE/Note/Papers", "/NOTE/MyStyle/Papers", "/EXPORT/Papers"} {
		t.Run(folder, func(t *testing.T) {
			f := setup(t)
			f.spec.Folder = folder
			f.c.seed(strings.Trim(folder, "/"), "<folder>")
			f.write("a.pdf", "a")
			if p := f.pass(port.MirrorRunOptions{}); !has(p, port.MirrorUpload) {
				t.Fatal(p)
			}
		})
	}
	f := setup(t)
	f.spec.Folder = "/NOTE/Note/Papers"
	f.c.seed("NOTE/Note/Papers", "<folder>")
	f.write("a.pdf", "a")
	f.c.fail["upload"] = port.ErrRefused
	if p, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); err == nil || p.Pending != 1 {
		t.Fatal(p, err)
	}
	if f.read("a.pdf") != "a" {
		t.Fatal("refusal touched original")
	}
}

func TestInterruptedReplacementPreservesTabletEdit(t *testing.T) {
	t.Parallel()
	for _, interrupted := range []bool{false, true} {
		t.Run(fmt.Sprint(interrupted), func(t *testing.T) {
			f := setup(t)
			f.write("x.pdf", "before")
			f.pass(port.MirrorRunOptions{})
			old := f.remote("x.pdf")
			f.write("x.pdf", "local edit")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f.c.fail["upload"] = errBoom
			if interrupted {
				f.e.upload = interruptedUpload{BackupUploader: f.e.upload, cancel: cancel}
			}
			writer := f.e.upload
			if _, err := f.e.Pass(ctx, f.spec, port.MirrorRunOptions{}); err == nil {
				t.Fatal("upload failure ignored")
			}
			if interrupted {
				f.e.upload = writer.(interruptedUpload).BackupUploader
			}
			delete(f.c.fail, "upload")
			var saved journal
			if err := json.Unmarshal([]byte(f.read(domain.MirrorJournalName)), &saved); err != nil {
				t.Fatal(err)
			}
			if en := saved.Items[old.ID]; !en.Recover || en.Record != old.Hash {
				t.Fatal("replacement intent missing", saved)
			}
			f.remoteChange("x.pdf", "tablet edit")
			p := f.pass(port.MirrorRunOptions{})
			if !has(p, port.MirrorConflict) || p.Pending != 0 || f.remote("x.pdf").ID != old.ID || f.c.data[old.ID] != "tablet edit" || f.read("x.pdf") != "tablet edit" || len(f.c.trash) != 0 {
				t.Fatal("tablet version was overwritten", p, f.c.trash)
			}
			for _, a := range p.Actions {
				if a.Kind == port.MirrorConflict && (f.read(a.To) != "local edit" || f.c.data[f.remote(a.To).ID] != "local edit") {
					t.Fatal("local version missing", p)
				}
			}
			if err := json.Unmarshal([]byte(f.read(domain.MirrorJournalName)), &saved); err != nil {
				t.Fatal(err)
			}
			for _, en := range saved.Items {
				if en.Recover {
					t.Fatal("recovery intent not cleared", saved)
				}
			}
			if p = f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
				t.Fatal(p)
			}
		})
	}
}

type interruptedUpload struct {
	port.BackupUploader
	cancel context.CancelFunc
}

func (u interruptedUpload) RestoreFiles(ctx context.Context, entries []port.RestoreFile, opts port.UploadOptions) (port.TransferReport, error) {
	u.cancel()
	return u.BackupUploader.RestoreFiles(ctx, entries, opts)
}

func TestRecoveredConflictResumesLocalRestoration(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.write("x.pdf", "before")
	f.pass(port.MirrorRunOptions{})
	old := f.remote("x.pdf")
	f.write("x.pdf", "local edit")
	f.c.fail["upload"] = errBoom
	if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); err == nil {
		t.Fatal("upload failure ignored")
	}
	delete(f.c.fail, "upload")
	f.remoteChange("x.pdf", "tablet edit")
	disk := f.e.disk
	f.e.disk = faultDisk{MirrorDisk: disk, root: func(r port.MirrorDirectory) port.MirrorDirectory {
		return originalPublishFailure{MirrorDirectory: r}
	}}
	if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); err == nil {
		t.Fatal("publication failure ignored")
	}
	f.e.disk = disk
	var saved journal
	if err := json.Unmarshal([]byte(f.read(domain.MirrorJournalName)), &saved); err != nil {
		t.Fatal(err)
	}
	if en := saved.Items[old.ID]; en.Recover || !en.RestoreLocal {
		t.Fatal("download intent can resume as an overwrite", saved)
	}
	p := f.pass(port.MirrorRunOptions{})
	if !has(p, port.MirrorDownload) || f.read("x.pdf") != "tablet edit" || f.c.data[old.ID] != "tablet edit" || len(f.c.trash) != 0 {
		t.Fatal("restoration overwrote tablet version", p, f.c.trash)
	}
	if p = f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
		t.Fatal(p)
	}
}

type originalPublishFailure struct{ port.MirrorDirectory }

func (r originalPublishFailure) Publish(ctx context.Context, name string, fn func(io.Writer) error) error {
	if name == "x.pdf" {
		return errBoom
	}
	return r.MirrorDirectory.Publish(ctx, name, fn)
}

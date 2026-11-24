// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package mirror

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

// Seed an interrupted migration with a previous drawing and an occupied aside.
func pendingDocument(t *testing.T, block bool) (*fixture, *drawingChange) {
	t.Helper()
	f, _, doc, mark := overlayFixture(t)
	f.spec.PullNew = false
	f.pass(port.MirrorRunOptions{})
	f.spec.PullNew = true
	r := drawingRun(t, f)
	en := r.j.Items[doc.ID]
	en.Output, en.Drawn = "_annotated/legacy.pdf", "old"
	r.j.Items[doc.ID] = en
	old := drawingRetirement{Owner: doc.ID, From: en.Output, To: ".snctl-removed/" + en.Output}
	en.Output, en.Drawn = "_annotated/documents/Report.pdf", doc.Hash+"/"+mark.ID+"/"+mark.Hash
	d := &drawingChange{ID: doc.ID, Stage: ".snctl-drawing-test", Entry: en, Retired: []drawingRetirement{old}}
	r.j.Drawings = []*drawingChange{d}
	f.write(old.From, "legacy")
	f.write(old.To, "already retained")
	f.write(d.Stage, "%PDF notebook")
	if block {
		if err := r.root.MkdirAll(en.Output); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.j.write(t.Context(), r.root); err != nil {
		t.Fatal(err)
	}
	if err := r.root.Close(); err != nil {
		t.Fatal(err)
	}
	return f, d
}

func blockedPass(t *testing.T, f *fixture, d *drawingChange) port.MirrorPass {
	t.Helper()
	p, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{ForceDelete: true})
	if err == nil || p.Pending == 0 {
		t.Fatal("expected one publication failure", p, err)
	}
	var failures int
	for _, a := range p.Actions {
		if a.Kind == port.MirrorDraw && a.Document == d.ID && a.Err != nil {
			failures++
		}
	}
	j := savedJournal(t, f)
	old := d.Retired[0]
	if failures != 1 || !reflect.DeepEqual(j.Drawings, []*drawingChange{d}) || j.Items[d.ID].Output != old.From ||
		f.read(old.From) != "legacy" || f.read(old.To) != "already retained" || f.read(d.Stage) != "%PDF notebook" {
		t.Fatal("reserved drawing changed or failure missing", j, p)
	}
	return p
}

type inPlaceUpload struct{ f *fixture }

func (u inPlaceUpload) RestoreFiles(_ context.Context, files []port.RestoreFile, _ port.UploadOptions) (port.TransferReport, error) {
	body, err := os.ReadFile(files[0].Local)
	if err != nil {
		return port.TransferReport{}, err
	}
	name := files[0].Expected.Name
	u.f.remoteChange(name, string(body))
	e := u.f.remote(name)
	return port.TransferReport{Items: []port.TransferItem{{Entry: &e}}}, nil
}

func TestDrawingRecoveryOriginalConverges(t *testing.T) {
	var baseline map[string]string
	for _, block := range []bool{false, true} {
		f, d := pendingDocument(t, block)
		f.e.notes, f.e.upload = noteRender{}, inPlaceUpload{f}
		run := func(kind port.MirrorActionKind) {
			t.Helper()
			var p port.MirrorPass
			if block {
				p = blockedPass(t, f, d)
			} else {
				p = f.pass(port.MirrorRunOptions{})
			}
			if !has(p, kind) {
				t.Fatal("missing original action", kind, p)
			}
		}
		f.write("Report.pdf", "local edit")
		before := f.read(domain.MirrorJournalName)
		if p := f.pass(port.MirrorRunOptions{DryRun: true}); !has(p, port.MirrorUpdate) || f.read(domain.MirrorJournalName) != before {
			t.Fatal("dry-run lost original or changed journal", p)
		}
		run(port.MirrorUpdate)
		if f.c.data[d.ID] != "local edit" {
			t.Fatal("upload did not reach original")
		}
		f.remoteChange("Report.pdf", "cloud edit")
		run(port.MirrorDownload)
		if f.read("Report.pdf") != "cloud edit" {
			t.Fatal("download did not reach original")
		}
		f.write("Report.pdf", "cloud edit") // Identical filesystem times in the two runs.
		if err := f.c.Move(t.Context(), f.remote("Report.pdf"), "DOCUMENT/Document/Papers/New.pdf"); err != nil {
			t.Fatal(err)
		}
		run(port.MirrorMoveLocal)
		if f.read("New.pdf") != "cloud edit" {
			t.Fatal("missing renamed original")
		}
		if block {
			if err := os.Remove(filepath.Join(f.dir, filepath.FromSlash(d.Entry.Output))); err != nil {
				t.Fatal(err)
			}
		}
		f.write("New.pdf", "last local edit")
		p := f.pass(port.MirrorRunOptions{})
		if !has(p, port.MirrorUpdate) || f.c.data[d.ID] != "last local edit" {
			t.Fatal(p)
		}
		assertDrawingStages(t, f, true)
		tree := map[string]string{}
		err := filepath.WalkDir(f.dir, func(name string, de fs.DirEntry, err error) error {
			if err == nil && de.Type().IsRegular() {
				rel, relErr := filepath.Rel(f.dir, name)
				if relErr != nil {
					return relErr
				}
				tree[rel] = f.read(rel)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if block && !reflect.DeepEqual(tree, baseline) {
			t.Fatalf("recovered tree and journal differ:\ngot %v\nwant %v", tree, baseline)
		}
		baseline = tree
	}
}

func TestDrawingRecoveryDeletedOriginal(t *testing.T) {
	for _, side := range []string{"local", "cloud", "both", "replacement"} {
		t.Run(side, func(t *testing.T) {
			f, d := pendingDocument(t, true)
			if side == "local" || side == "both" {
				if err := os.Remove(filepath.Join(f.dir, "Report.pdf")); err != nil {
					t.Fatal(err)
				}
			}
			if side == "cloud" || side == "both" {
				delete(f.c.entries, d.ID)
			}
			if side == "replacement" {
				f.write("Report.pdf", "replacement local original")
			}
			p := blockedPass(t, f, d)
			if _, present := f.c.entries[d.ID]; present || savedJournal(t, f).Items[d.ID].Path != "" {
				t.Fatal("original still tracked", p)
			}
			if side == "cloud" && f.read(".snctl-removed/Report.pdf") != "original" {
				t.Fatal("local original was not retained")
			}
			if err := os.Remove(filepath.Join(f.dir, filepath.FromSlash(d.Entry.Output))); err != nil {
				t.Fatal(err)
			}
			f.pass(port.MirrorRunOptions{})
			if _, present := savedJournal(t, f).Items[d.ID]; present || f.read(".snctl-removed/"+d.Entry.Output) != "%PDF notebook" ||
				f.read(d.Retired[0].To) != "already retained" || f.read(strings.TrimSuffix(d.Retired[0].To, ".pdf")+" (2).pdf") != "legacy" {
				t.Fatal("drawing retirement incomplete")
			}
			assertDrawingStages(t, f, true)
		})
	}
}

func TestDrawingCompletionWithOriginalRecovery(t *testing.T) {
	for _, marker := range []string{"restore", "recover"} {
		t.Run(marker, func(t *testing.T) {
			f, d := pendingDocument(t, true)
			r := drawingRun(t, f)
			en := r.j.Items[d.ID]
			en.RestoreLocal, en.Recover = marker == "restore", marker == "recover"
			r.j.Items[d.ID] = en
			if err := r.j.write(t.Context(), r.root); err != nil {
				t.Fatal(err)
			}
			if err := r.root.Close(); err != nil {
				t.Fatal(err)
			}
			// The interrupted original resumes while publication is still blocked.
			p := blockedPass(t, f, d)
			want := port.MirrorDownload
			if marker == "recover" {
				want = port.MirrorAdopt
			}
			if !has(p, want) {
				t.Fatal(p)
			}
			if err := os.Remove(filepath.Join(f.dir, filepath.FromSlash(d.Entry.Output))); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(f.dir, "Report.pdf"), filepath.Join(f.dir, "Moved.pdf")); err != nil {
				t.Fatal(err)
			}
			f.write("Moved.pdf", "original")
			// Match the renamed original with its mark so the replacement is drawn.
			if err := f.c.Move(t.Context(), f.remote("Report.pdf.mark"), "DOCUMENT/Document/Papers/Moved.pdf.mark"); err != nil {
				t.Fatal(err)
			}
			p = f.pass(port.MirrorRunOptions{})
			en = savedJournal(t, f).Items[d.ID]
			if !has(p, port.MirrorMoveCloud) || en.Path != "Moved.pdf" || en.Output != "_annotated/documents/Moved.pdf" ||
				en.Record != d.Entry.Record || en.Drawn != d.Entry.Drawn || en.Recover || en.RestoreLocal || f.read(en.Output) != "%PDF originalink" {
				t.Fatal("lost drawing or reconciliation markers", en, p)
			}
		})
	}
}

func TestOriginalConflictWhileDrawingIsBlocked(t *testing.T) {
	f, d := pendingDocument(t, true)
	f.write("Report.pdf", "local version")
	f.remoteChange("Report.pdf", "cloud version")
	p := blockedPass(t, f, d)
	for _, a := range p.Actions {
		if a.Kind == port.MirrorConflict && f.read(a.To) == "local version" && f.c.data[f.remote(a.To).ID] == "local version" &&
			f.read("Report.pdf") == "cloud version" {
			return
		}
	}
	t.Fatal("both versions should survive", p)
}

func TestDrawingOnlyOriginalReappears(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		action     port.MirrorActionKind
	}{
		{"different", "user file", port.MirrorConflict}, {"same", "original", port.MirrorAdopt}, {"absent", "", port.MirrorDownload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, d := pendingDocument(t, true)
			original := f.remote("Report.pdf")
			delete(f.c.entries, original.ID)
			blockedPass(t, f, d)
			if savedJournal(t, f).Items[d.ID].Path != "" {
				t.Fatal("original path was not released")
			}
			f.c.entries[original.ID] = original
			if tc.body != "" {
				f.write("Report.pdf", tc.body)
			}
			req := &drawingRequests{Files: f.c}
			f.e.cloud = req
			p := blockedPass(t, f, d)
			if !has(p, tc.action) || f.read("Report.pdf") != "original" || savedJournal(t, f).Items[d.ID].Path != "Report.pdf" {
				t.Fatal(p)
			}
			if tc.action == port.MirrorAdopt && req.downloads != 0 {
				t.Fatal("adoption fetched identical bytes")
			}
			if tc.action == port.MirrorConflict {
				for _, a := range p.Actions {
					if a.Kind == port.MirrorConflict && f.read(a.To) == tc.body && f.c.data[f.remote(a.To).ID] == tc.body {
						return
					}
				}
				t.Fatal("conflict copy missing", p)
			}
		})
	}
}

type interruptDeletionWrite struct {
	port.MirrorDirectory
	writes, allow int
}

func (d *interruptDeletionWrite) Produce(ctx context.Context, name string, write func(io.Writer) error) error {
	if name == domain.MirrorJournalName {
		d.writes++
		if d.writes > d.allow {
			return errBoom
		}
	}
	return d.MirrorDirectory.Produce(ctx, name, write)
}

func TestDeletionRestartWithPendingDrawing(t *testing.T) {
	for _, allow := range []int{0, 1} {
		t.Run(fmt.Sprintf("after-%d-checkpoints", allow), func(t *testing.T) {
			f, d := pendingDocument(t, true)
			if err := os.Remove(filepath.Join(f.dir, "Report.pdf")); err != nil {
				t.Fatal(err)
			}
			timer := porttest.NewTimer(now)
			f.e.timer = timer
			f.c.afterDelete = func() { timer.Jump(checkpointEvery) }
			disk := f.e.disk
			var writes *interruptDeletionWrite
			f.e.disk = faultDisk{MirrorDisk: disk, root: func(root port.MirrorDirectory) port.MirrorDirectory {
				writes = &interruptDeletionWrite{MirrorDirectory: root, allow: allow}
				return writes
			}}
			p, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{ForceDelete: true})
			if !errors.Is(err, errBoom) || writes.writes != 2 || !has(p, port.MirrorTrash) {
				t.Fatal(p, err, writes.writes)
			}
			j := savedJournal(t, f)
			if (j.Items[d.ID].Path == "") != (allow == 1) || len(j.Drawings) != 1 || len(f.c.trash) != 1 {
				t.Fatal(j)
			}
			f.e.disk = disk
			blockedPass(t, f, d)
			if savedJournal(t, f).Items[d.ID].Path != "" || len(f.c.trash) != 1 {
				t.Fatal("deletion did not converge")
			}
			if err := os.Remove(filepath.Join(f.dir, d.Entry.Output)); err != nil {
				t.Fatal(err)
			}
			f.pass(port.MirrorRunOptions{})
			if _, ok := savedJournal(t, f).Items[d.ID]; ok || f.read(".snctl-removed/"+d.Entry.Output) != "%PDF notebook" || f.read(d.Retired[0].To) != "already retained" {
				t.Fatal("drawing retirement failed")
			}
			assertDrawingStages(t, f, true)
		})
	}
}

func TestDeletionPoliciesWithPendingDrawing(t *testing.T) {
	for _, side := range []string{"local", "cloud"} {
		t.Run(side, func(t *testing.T) {
			f, d := pendingDocument(t, true)
			want := port.MirrorRemove
			if side == "local" {
				if err := os.Remove(filepath.Join(f.dir, "Report.pdf")); err != nil {
					t.Fatal(err)
				}
				want = port.MirrorTrash
			} else {
				delete(f.c.entries, d.ID)
			}
			before := f.read(domain.MirrorJournalName)
			p := f.pass(port.MirrorRunOptions{ForceDelete: true, DryRun: true})
			if !has(p, port.MirrorDraw) || !has(p, want) || f.read(domain.MirrorJournalName) != before {
				t.Fatal(p)
			}
			f.spec.NoDelete = true
			p = blockedPass(t, f, d)
			if !has(p, port.MirrorDetach) || !savedJournal(t, f).Items[d.ID].Detached || len(f.c.trash) != 0 {
				t.Fatal(p)
			}
			if side == "cloud" && f.read("Report.pdf") != "original" || side == "local" && f.c.entries[d.ID].ID != d.ID {
				t.Fatal("surviving original lost")
			}
			if err := os.Remove(filepath.Join(f.dir, d.Entry.Output)); err != nil {
				t.Fatal(err)
			}
			f.pass(port.MirrorRunOptions{})
			en := savedJournal(t, f).Items[d.ID]
			if !en.Detached || f.read(en.Output) != "%PDF notebook" {
				t.Fatal(en)
			}
		})
	}
}

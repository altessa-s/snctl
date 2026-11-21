// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package mirror

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
)

type failDownloadOnce struct {
	port.Files
	failed bool
}

func (c *failDownloadOnce) Download(ctx context.Context, e port.Entry, p port.Progress) (string, error) {
	if !c.failed {
		c.failed = true
		return "", errBoom
	}
	return c.Files.Download(ctx, e, p)
}

type failedOriginalMove struct{ port.MirrorDirectory }

func (r failedOriginalMove) Move(from, to string) error {
	if from == "Report.pdf" {
		return errBoom
	}
	return r.MirrorDirectory.Move(from, to)
}

func savedJournal(t *testing.T, f *fixture) journal {
	t.Helper()
	var j journal
	if err := json.Unmarshal([]byte(f.read(domain.MirrorJournalName)), &j); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestDrawingDefersUnreconciledDocument(t *testing.T) {
	for _, scenario := range []string{"download", "download-local-edit", "still-writing", "move"} {
		t.Run(scenario, func(t *testing.T) {
			f, n, doc, mark := overlayFixture(t)
			f.pass(port.MirrorRunOptions{})
			before := savedJournal(t, f).Items[doc.ID]
			disk := f.e.disk
			want := port.MirrorDownload
			if scenario == "move" {
				if err := f.c.Move(t.Context(), doc, "DOCUMENT/Document/Papers/Renamed.pdf"); err != nil {
					t.Fatal(err)
				}
				if err := f.c.Move(t.Context(), mark, "DOCUMENT/Document/Papers/Renamed.pdf.mark"); err != nil {
					t.Fatal(err)
				}
				f.e.disk = faultDisk{MirrorDisk: disk, root: func(r port.MirrorDirectory) port.MirrorDirectory { return failedOriginalMove{MirrorDirectory: r} }}
				want = port.MirrorMoveLocal
			} else {
				f.remoteChange("Report.pdf", "new cloud version")
				if scenario == "still-writing" {
					f.write("Report.pdf", "local edit")
					if err := os.Chtimes(filepath.Join(f.dir, "Report.pdf"), now, now); err != nil {
						t.Fatal(err)
					}
					want = port.MirrorConflict
				} else {
					f.e.cloud = &failDownloadOnce{Files: f.c}
				}
			}
			p, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{})
			if scenario != "still-writing" && !errors.Is(err, errBoom) {
				t.Fatal(p, err)
			}
			if has(p, port.MirrorDraw) || n.calls != 1 {
				t.Error("drew an unreconciled document", p, n.calls)
			}
			after := savedJournal(t, f).Items[doc.ID]
			if before.Record != after.Record || before.CloudPath != after.CloudPath {
				t.Error("drawing changed reconciliation markers", before, after)
			}
			f.e.cloud, f.e.disk = f.c, disk
			if scenario == "download-local-edit" {
				f.write("Report.pdf", "local edit")
				want = port.MirrorConflict
			}
			if scenario == "still-writing" {
				if err := os.Chtimes(filepath.Join(f.dir, "Report.pdf"), now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
					t.Fatal(err)
				}
			}
			p = f.pass(port.MirrorRunOptions{})
			if !has(p, want) || !has(p, port.MirrorDraw) {
				t.Fatal("retry lost reconciliation", p)
			}
			if scenario == "move" {
				if f.read("Renamed.pdf") != "original" {
					t.Fatal(p)
				}
			} else if f.read("Report.pdf") != "new cloud version" {
				t.Fatal(p)
			}
		})
	}
}

func TestBadSidecarsDoNotBlockOriginals(t *testing.T) {
	for _, damage := range []string{"duplicate", "id", "hash", "size"} {
		t.Run(damage, func(t *testing.T) {
			f, n, doc, mark := overlayFixture(t)
			switch damage {
			case "duplicate":
				f.c.seed("DOCUMENT/Document/Papers/REPORT.PDF.MARK", "other ink")
			case "id":
				mark.ID = ""
			case "hash":
				mark.Hash = ""
			case "size":
				mark.Size = nil
			}
			if damage != "duplicate" {
				for id := range f.c.entries {
					if strings.HasSuffix(f.c.entries[id].Path, ".mark") {
						f.c.entries[id] = mark
					}
				}
			}
			f.c.seed("DOCUMENT/Document/Papers/Other.pdf", "unaffected")
			f.c.seed("DOCUMENT/Document/Papers/Other.pdf.mark", "other ink")
			p := f.pass(port.MirrorRunOptions{})
			if !has(p, port.MirrorWarning) || !has(p, port.MirrorDraw) || n.calls != 1 || f.read("Report.pdf") != "original" || f.read("Other.pdf") != "unaffected" {
				t.Fatal(p, n.calls)
			}
			for _, a := range p.Actions {
				if a.Kind == port.MirrorDraw && a.Document == doc.ID {
					t.Fatal("bad sidecar was drawn", a)
				}
			}
			if quiet := f.pass(port.MirrorRunOptions{}); len(quiet.Actions) != 0 {
				t.Fatal("unchanged bad sidecar was noisy", quiet)
			}
			f.remoteChange("Report.pdf", "new original")
			p = f.pass(port.MirrorRunOptions{})
			if !has(p, port.MirrorDownload) || f.read("Report.pdf") != "new original" {
				t.Fatal(p)
			}
		})
	}
}

type migrationNotes struct{ noteRender }

func (migrationNotes) Render(_ context.Context, input, target string, _ port.RenderOptions) (port.RenderReport, error) {
	data, err := os.ReadFile(input)
	if err != nil {
		return port.RenderReport{}, err
	}
	return port.RenderReport{}, os.WriteFile(target, append([]byte("drawn "), data...), 0o600)
}

func TestLegacyDrawingMigrationCollisions(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy-first", true: "new-first"}[reverse], func(t *testing.T) {
			f := setup(t)
			f.spec.PullNew = true
			paths := []string{"notebooks/X.note", "documents/Y.note", "Z.note", "X.note"}
			if reverse {
				paths[0], paths[3] = paths[3], paths[0]
			}
			j := journal{Version: 1, Folder: f.remoteFolderID(), Path: strings.Trim(f.spec.Folder, "/"), Profile: f.spec.Profile, Items: map[string]entry{}}
			for _, p := range paths {
				c := f.c.seed("DOCUMENT/Document/Papers/"+p, "source "+p)
				out := domain.MirrorAnnotatedPath(p, "pdf")
				j.Items[c.ID] = entry{CloudPath: c.Path, Record: c.Hash, Drawn: c.Hash, Output: out}
				f.write(out, "legacy "+p)
			}
			f.c.seed("DOCUMENT/Document/Papers/Y.pdf", "original")
			f.c.seed("DOCUMENT/Document/Papers/Y.pdf.mark", "ink")
			// A renderer that accepts both notebooks and overlays.
			f.e.notes = migrationNotes{}
			b, err := json.Marshal(j)
			if err != nil {
				t.Fatal(err)
			}
			f.write(domain.MirrorJournalName, string(b))
			f.pass(port.MirrorRunOptions{})
			for _, p := range paths {
				old := domain.MirrorAnnotatedPath(p, "pdf")
				if f.read(".snctl-removed/"+old) != "legacy "+p {
					t.Fatal("lost legacy bytes", p)
				}
				if f.read(domain.MirrorAnnotatedPath("notebooks/"+p, "pdf")) != "drawn source "+p {
					t.Fatal(p)
				}
			}
			if f.read("_annotated/notebooks/X.pdf") != "drawn source X.note" || f.read("_annotated/documents/Y.pdf") != "drawn ink" {
				t.Fatal("new drawing was retired by legacy owner")
			}
			j = savedJournal(t, f)
			if j.Version <= 1 {
				t.Fatal("layout version not bumped", j.Version)
			}
			if p := f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
				t.Fatal(p)
			}
		})
	}
}
func (f *fixture) remoteFolderID() string {
	f.t.Helper()
	e, err := f.c.Stat(f.t.Context(), f.spec.Folder)
	if err != nil {
		f.t.Fatal(err)
	}
	return e.ID
}

func TestDrawingCaseOnlyRename(t *testing.T) {
	for _, notebook := range []bool{false, true} {
		t.Run(map[bool]string{false: "document", true: "notebook"}[notebook], func(t *testing.T) {
			f, n, doc, mark := overlayFixture(t)
			namespace, ext := "documents", ".pdf"
			if notebook {
				delete(f.c.entries, doc.ID)
				delete(f.c.entries, mark.ID)
				doc = f.c.seed("DOCUMENT/Document/Papers/Report.note", "note")
				f.e.notes = noteRender{}
				namespace, ext = "notebooks", ".note"
			}
			f.pass(port.MirrorRunOptions{})
			// Exercise the real filesystem alias rather than assuming the host is case-insensitive.
			if _, err := os.Stat(filepath.Join(f.dir, "_annotated", namespace, "REPORT.pdf")); err != nil {
				t.Skip("case-sensitive filesystem")
			}
			if err := f.c.Move(t.Context(), doc, "DOCUMENT/Document/Papers/REPORT"+ext); err != nil {
				t.Fatal(err)
			}
			if !notebook {
				if err := f.c.Move(t.Context(), mark, "DOCUMENT/Document/Papers/REPORT.pdf.mark"); err != nil {
					t.Fatal(err)
				}
			}
			f.pass(port.MirrorRunOptions{})
			if f.read("_annotated/"+namespace+"/REPORT.pdf") == "" {
				t.Fatal("drawing missing", n.calls)
			}
			if p := f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
				t.Fatal(p)
			}
		})
	}
}

func TestAmbiguousSidecarWarningChanges(t *testing.T) {
	f, _, _, mark := overlayFixture(t)
	delete(f.c.entries, mark.ID)
	f.c.seed("DOCUMENT/Document/Papers/Report.mark", "legacy")
	epub := f.c.seed("DOCUMENT/Document/Papers/Report.epub", "epub")
	if p := f.pass(port.MirrorRunOptions{}); !has(p, port.MirrorWarning) {
		t.Fatal(p)
	}
	if p := f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
		t.Fatal("repeated ambiguity was noisy", p)
	}
	f.remoteChange("Report.mark", "changed legacy")
	if p := f.pass(port.MirrorRunOptions{}); !has(p, port.MirrorWarning) {
		t.Fatal("changed ambiguity did not warn", p)
	}
	delete(f.c.entries, epub.ID)
	f.pass(port.MirrorRunOptions{ForceDelete: true})
	f.c.seed("DOCUMENT/Document/Papers/Report.epub", "epub")
	if p := f.pass(port.MirrorRunOptions{}); !has(p, port.MirrorWarning) {
		t.Fatal("returning ambiguity did not warn", p)
	}
}

func TestDrawingOutputOwnerSwap(t *testing.T) {
	f := setup(t)
	f.spec.PullNew = true
	f.e.notes = migrationNotes{}
	a := f.c.seed("DOCUMENT/Document/Papers/A.note", "first")
	b := f.c.seed("DOCUMENT/Document/Papers/B.note", "second")
	f.pass(port.MirrorRunOptions{})
	if err := f.c.Move(t.Context(), a, "DOCUMENT/Document/Papers/B.note"); err != nil {
		t.Fatal(err)
	}
	if err := f.c.Move(t.Context(), b, "DOCUMENT/Document/Papers/A.note"); err != nil {
		t.Fatal(err)
	}
	f.pass(port.MirrorRunOptions{})
	if f.read("_annotated/notebooks/A.pdf") != "drawn second" || f.read("_annotated/notebooks/B.pdf") != "drawn first" {
		t.Fatal("outputs lost their owners")
	}
	if f.read(".snctl-removed/_annotated/notebooks/A.pdf") != "drawn first" || f.read(".snctl-removed/_annotated/notebooks/B.pdf") != "drawn second" {
		t.Fatal("displaced drawings lost")
	}
	if p := f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
		t.Fatal(p)
	}
}

func TestLegacyMigrationRetriesFailures(t *testing.T) {
	for _, fault := range []string{"move", "write-journal"} {
		t.Run(fault, func(t *testing.T) {
			f := setup(t)
			f.spec.PullNew = true
			c := f.c.seed("DOCUMENT/Document/Papers/X.note", "note")
			j := journal{Version: 1, Folder: f.remoteFolderID(), Path: strings.Trim(f.spec.Folder, "/"), Items: map[string]entry{c.ID: {CloudPath: c.Path, Record: c.Hash, Drawn: c.Hash, Output: "_annotated/X.pdf"}}}
			data, err := json.Marshal(j)
			if err != nil {
				t.Fatal(err)
			}
			f.write(domain.MirrorJournalName, string(data))
			f.write("_annotated/X.pdf", "legacy")
			disk := f.e.disk
			f.e.disk = faultDisk{MirrorDisk: disk, root: func(r port.MirrorDirectory) port.MirrorDirectory { return faultRoot{MirrorDirectory: r, fail: fault} }}
			if _, err = f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, errBoom) {
				t.Fatal(err)
			}
			f.e.disk = disk
			f.pass(port.MirrorRunOptions{})
			if f.read(".snctl-removed/_annotated/X.pdf") != "legacy" || f.read("_annotated/notebooks/X.pdf") != "%PDF notebook" {
				t.Fatal("migration retry lost content")
			}
		})
	}
}

func TestDrawingDisplacesDeletedOwner(t *testing.T) {
	f := setup(t)
	f.spec.PullNew = true
	f.e.notes = migrationNotes{}
	live := f.c.seed("DOCUMENT/Document/Papers/A.note", "live")
	dead := f.c.seed("DOCUMENT/Document/Papers/B.note", "deleted")
	f.pass(port.MirrorRunOptions{})
	delete(f.c.entries, dead.ID)
	if err := f.c.Move(t.Context(), live, "DOCUMENT/Document/Papers/B.note"); err != nil {
		t.Fatal(err)
	}
	f.pass(port.MirrorRunOptions{ForceDelete: true})
	if f.read("_annotated/notebooks/B.pdf") != "drawn live" || f.read(".snctl-removed/_annotated/notebooks/B.pdf") != "drawn deleted" {
		t.Fatal("deleted owner retired the fresh drawing")
	}
	if _, ok := savedJournal(t, f).Items[dead.ID]; ok {
		t.Fatal("deleted owner still tracked")
	}
	if p := f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
		t.Fatal(p)
	}
}

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

	"github.com/altessa-s/snctl/internal/adapter/localfs"
	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/app/notebooks"
	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

type recognizedNotes struct {
	noteRender
	text    string
	readErr error
}

func (n recognizedNotes) Read(context.Context, string, []int, port.Progress) (port.Notebook, error) {
	return port.Notebook{Pages: []port.NotePage{{Number: 1, Recognition: n.text, Text: []string{"Typed\r\nbox"}}}}, n.readErr
}

func TestRecognizedDrawingLifecycle(t *testing.T) {
	for _, noDelete := range []bool{false, true} {
		t.Run(map[bool]string{true: "retain", false: "remove"}[noDelete], func(t *testing.T) {
			f := setup(t)
			f.spec.PullNew, f.spec.NoDelete = true, noDelete
			c := f.c.seed("DOCUMENT/Document/Papers/X.note", "first")
			f.e.notes = recognizedNotes{text: "co\u00adpy\u200b words"}
			f.pass(port.MirrorRunOptions{})
			out := "_annotated/notebooks/X.pdf"
			md := drawingTextPath(out)
			if got := f.read(md); got != "## Page 1\n\nTyped\nbox\ncopy words\n" {
				t.Fatal(got)
			}
			if !savedJournal(t, f).Items[c.ID].Text {
				t.Fatal("companion untracked")
			}
			if p := f.pass(port.MirrorRunOptions{}); has(p, port.MirrorDraw) {
				t.Fatal("unchanged notebook redrawn")
			}
			f.e.notes = recognizedNotes{text: "changed"}
			f.remoteChange("X.note", "second")
			f.pass(port.MirrorRunOptions{})
			if !strings.Contains(f.read(md), "changed") {
				t.Fatal("companion not updated")
			}
			c = f.remote("X.note")
			if err := f.c.Move(t.Context(), c, "DOCUMENT/Document/Papers/Renamed.note"); err != nil {
				t.Fatal(err)
			}
			f.pass(port.MirrorRunOptions{})
			if f.read(".snctl-removed/"+md) == "" {
				t.Fatal("renamed companion not retained")
			}
			out = "_annotated/notebooks/Renamed.pdf"
			md = drawingTextPath(out)
			if err := f.c.Delete(t.Context(), c, false); err != nil {
				t.Fatal(err)
			}
			f.pass(port.MirrorRunOptions{ForceDelete: true})
			if noDelete {
				if f.read(md) == "" || f.read(out) == "" {
					t.Fatal("no-delete discarded output")
				}
			} else if f.read(".snctl-removed/"+md) == "" || f.read(".snctl-removed/"+out) == "" {
				t.Fatal("removed companion not retained")
			}
		})
	}
}

func TestRecognitionRemovedAndReadFailure(t *testing.T) {
	f := setup(t)
	f.spec.PullNew = true
	f.c.seed("DOCUMENT/Document/Papers/X.note", "first")
	f.e.notes = recognizedNotes{text: "recognized"}
	f.pass(port.MirrorRunOptions{})
	f.remoteChange("X.note", "second")
	f.e.notes = recognizedNotes{readErr: errBoom}
	if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	if !strings.Contains(f.read("_annotated/notebooks/X.md"), "recognized") {
		t.Fatal("failed read lost text")
	}
	f.e.notes = recognizedNotes{}
	f.pass(port.MirrorRunOptions{})
	if _, err := os.Stat(filepath.Join(f.dir, "_annotated/notebooks/X.md")); !os.IsNotExist(err) {
		t.Fatal("stale recognized text remains", err)
	}
	if !strings.Contains(f.read(".snctl-removed/_annotated/notebooks/X.md"), "recognized") {
		t.Fatal("old text not retained")
	}
}

type textPublicationFailure struct{ port.MirrorDirectory }

func (d textPublicationFailure) Produce(ctx context.Context, name string, write func(io.Writer) error) error {
	if strings.HasSuffix(name, ".md") {
		return errBoom
	}
	return d.MirrorDirectory.Produce(ctx, name, write)
}

func TestRecognitionPublicationRecovery(t *testing.T) {
	f := setup(t)
	f.spec.PullNew = true
	c := f.c.seed("DOCUMENT/Document/Papers/X.note", "first")
	f.e.notes = recognizedNotes{text: "recover me"}
	original := f.e.disk
	f.e.disk = faultDisk{MirrorDisk: original, root: func(d port.MirrorDirectory) port.MirrorDirectory { return textPublicationFailure{d} }}
	if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	j := savedJournal(t, f)
	if len(j.Drawings) != 1 || j.Drawings[0].TextStage == "" {
		t.Fatal("text intent missing", j.Drawings)
	}
	f.e.disk = original
	f.e.notes = noteRender{err: errBoom} // Recovery must use staged bytes, without re-reading or rendering.
	f.pass(port.MirrorRunOptions{})
	if !strings.Contains(f.read("_annotated/notebooks/X.md"), "recover me") {
		t.Fatal("text not recovered")
	}
	j = savedJournal(t, f)
	if len(j.Drawings) != 0 || !j.Items[c.ID].Text {
		t.Fatal("publication not committed")
	}
	if strings.Contains(f.read(domain.MirrorJournalName), "recover me") {
		t.Fatal("journal persisted private text")
	}
}

func TestTextStageMissingAndMarkdownDeleted(t *testing.T) {
	f := setup(t)
	f.spec.PullNew = true
	c := f.c.seed("DOCUMENT/Document/Papers/X.note", "first")
	f.e.notes = recognizedNotes{text: "rebuilt"}
	f.pass(port.MirrorRunOptions{})
	r := drawingRun(t, f)
	d := drawingIntent(r, c)
	d.Entry.Text, d.Entry.TextChecked = true, true
	d.TextStage = d.Stage + "-text"
	// Both staging files were removed after publishing; the user then deleted Markdown.
	if err := r.root.Remove(drawingTextPath(d.Entry.Output)); err != nil {
		t.Fatal(err)
	}
	if err := r.j.write(t.Context(), r.root); err != nil {
		t.Fatal(err)
	}
	if err := r.root.Close(); err != nil {
		t.Fatal(err)
	}
	f.pass(port.MirrorRunOptions{})
	if !strings.Contains(f.read(drawingTextPath(d.Entry.Output)), "rebuilt") || len(savedJournal(t, f).Drawings) != 0 {
		t.Fatal("missing text left the owner pending")
	}
}

func TestUpgradeCurrentDrawingTextOnly(t *testing.T) {
	for _, text := range []string{"recognized", ""} {
		t.Run(text, func(t *testing.T) {
			f := setup(t)
			f.spec.PullNew = true
			c := f.c.seed("DOCUMENT/Document/Papers/X.note", "first")
			f.pass(port.MirrorRunOptions{})
			r := drawingRun(t, f)
			en := r.j.Items[c.ID]
			en.TextChecked = false // A version 2 journal written before recognition support.
			r.j.Items[c.ID] = en
			if err := r.j.write(t.Context(), r.root); err != nil {
				t.Fatal(err)
			}
			if err := r.root.Close(); err != nil {
				t.Fatal(err)
			}
			f.write(en.Output, "keep exact PDF bytes")
			f.e.notes = recognizedNotes{err: errBoom, text: text}
			dry := f.pass(port.MirrorRunOptions{DryRun: true})
			if !has(dry, port.MirrorDraw) || savedJournal(t, f).Items[c.ID].TextChecked {
				t.Fatal("dry upgrade mutated journal")
			}
			p := f.pass(port.MirrorRunOptions{})
			got := savedJournal(t, f).Items[c.ID]
			if !got.TextChecked || got.Text != (text != "") || f.read(en.Output) != "keep exact PDF bytes" {
				t.Fatal(got)
			}
			want := 0
			if text != "" {
				want = 1
			}
			if p.TextAdded != want {
				t.Fatal(p.TextAdded, want)
			}
			if p = f.pass(port.MirrorRunOptions{}); has(p, port.MirrorDraw) {
				t.Fatal("upgrade repeated")
			}
		})
	}
}

func TestTextTargetDirectoryReservesOnlyItsOwner(t *testing.T) {
	f := setup(t)
	f.spec.PullNew = true
	x := f.c.seed("DOCUMENT/Document/Papers/X.note", "first")
	f.c.seed("DOCUMENT/Document/Papers/Y.note", "other")
	f.e.notes = recognizedNotes{text: "text"}
	if err := os.MkdirAll(filepath.Join(f.dir, "_annotated/notebooks/X.md"), 0700); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); err == nil {
			t.Fatal("directory accepted")
		}
		j := savedJournal(t, f)
		if len(j.Drawings) != 1 || j.Drawings[0].ID != x.ID {
			t.Fatal(j.Drawings)
		}
		if !strings.Contains(f.read("_annotated/notebooks/Y.md"), "text") {
			t.Fatal("unrelated owner blocked")
		}
	}
	if err := os.Remove(filepath.Join(f.dir, "_annotated/notebooks/X.md")); err != nil {
		t.Fatal(err)
	}
	f.e.notes = noteRender{err: errBoom}
	f.pass(port.MirrorRunOptions{})
	if !strings.Contains(f.read("_annotated/notebooks/X.md"), "text") {
		t.Fatal("staged text not recovered")
	}
}

func TestTextRetirementsPlannedAndUntrackedTextPreserved(t *testing.T) {
	f := setup(t)
	f.spec.PullNew = true
	f.c.seed("DOCUMENT/Document/Papers/X.note", "first")
	f.write("_annotated/notebooks/X.md", "user text")
	f.e.notes = recognizedNotes{text: "recognized"}
	f.pass(port.MirrorRunOptions{})
	if f.read(".snctl-removed/_annotated/notebooks/X.md") != "user text" {
		t.Fatal("untracked text overwritten")
	}
	f.remoteChange("X.note", "second")
	f.e.notes = recognizedNotes{}
	dry := f.pass(port.MirrorRunOptions{DryRun: true})
	planned := false
	for _, a := range dry.Actions {
		if a.Kind == port.MirrorRemove && a.Path == "_annotated/notebooks/X.md" && a.To != "" {
			planned = true
		}
	}
	if !planned || !strings.Contains(f.read("_annotated/notebooks/X.md"), "recognized") {
		t.Fatal(dry)
	}
	f.pass(port.MirrorRunOptions{})
	if !strings.Contains(f.read(".snctl-removed/_annotated/notebooks/X (2).md"), "recognized") {
		t.Fatal("tracked text not retained")
	}
}

func TestCurrentDrawingWarnsOfRejectedTextCheck(t *testing.T) {
	f := setup(t)
	f.spec.PullNew = true
	c := f.c.seed("DOCUMENT/Document/Papers/X.note", "notebook")
	f.pass(port.MirrorRunOptions{})
	r := drawingRun(t, f)
	en := r.j.Items[c.ID]
	en.TextChecked = false
	r.j.Items[c.ID] = en
	if err := r.j.write(t.Context(), r.root); err != nil {
		t.Fatal(err)
	}
	if err := r.root.Close(); err != nil {
		t.Fatal(err)
	}
	req := &drawingRequests{Files: f.c}
	f.e.cloud = req
	f.e.notes = recognizedNotes{readErr: port.ErrNotebookContent}
	if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, port.ErrNotebookContent) {
		t.Fatal(err)
	}
	p := f.pass(port.MirrorRunOptions{})
	if len(p.Actions) != 1 || p.Actions[0].Kind != port.MirrorWarning || !strings.Contains(p.Actions[0].Detail, "text check") || req.downloads != 1 || f.read(en.Output) != "%PDF notebook" {
		t.Fatal(p, req.downloads)
	}
}

type countingIndex struct {
	writes, merges int
	values         map[string]bool
}

func (*countingIndex) Recognition(string) *bool { return nil }
func (c *countingIndex) RememberRecognition(context.Context, string, bool) error {
	c.writes++
	return nil
}
func (*countingIndex) RecognitionSnapshot() map[string]bool { return nil }
func (c *countingIndex) RememberRecognitions(_ context.Context, values map[string]bool) error {
	c.merges++
	c.values = values
	return nil
}

type observingCloud struct {
	port.Files
	notes notebooks.Service
}

func (c observingCloud) Download(ctx context.Context, e port.Entry, p port.Progress) (string, error) {
	file, err := c.Files.Download(ctx, e, p)
	if err == nil {
		c.notes.ObserveBody(ctx, e, file)
	}
	return file, err
}

func TestFolderTransferAndMirrorPassMergeRecognitionOnce(t *testing.T) {
	for _, op := range []string{"folder download", "mirror pass"} {
		t.Run(op, func(t *testing.T) {
			f := setup(t)
			f.spec.PullNew = true
			index := &countingIndex{}
			disk := localfs.Files{}
			notes := notebooks.Service{Index: index, Files: disk, Reader: porttest.NoteReader{Notebook: port.Notebook{Kind: "note", Pages: []port.NotePage{{Number: 1, Recognition: "text"}}}}, Renderer: porttest.Renderer{Pages: 1}}
			remote := observingCloud{Files: f.c, notes: notes}
			var hashes []string
			for _, name := range []string{"a", "b", "c"} {
				e := f.c.seed("DOCUMENT/Document/Papers/"+name+".note", name)
				hashes = append(hashes, e.Hash)
			}
			if op == "folder download" {
				transfer := files.Service{Cloud: remote, Disk: disk, Recognition: notes.BatchRecognition}
				if _, err := transfer.Mirror(t.Context(), "/", t.TempDir(), port.MirrorOptions{Jobs: 1}); err != nil {
					t.Fatal(err)
				}
			} else {
				f.e.notes, f.e.cloud = notes, remote
				f.pass(port.MirrorRunOptions{})
			}
			if index.writes != 0 || index.merges != 1 || len(index.values) != len(hashes) {
				t.Fatalf("single writes=%d merges=%d values=%v", index.writes, index.merges, index.values)
			}
			for _, hash := range hashes {
				if !index.values[hash] {
					t.Errorf("missing recognized hash %s", hash)
				}
			}
		})
	}
}

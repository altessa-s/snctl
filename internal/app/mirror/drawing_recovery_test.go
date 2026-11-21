// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package mirror

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
)

// Only stages referenced by a durable intent may survive a returned error.
func assertDrawingStages(t *testing.T, f *fixture, empty bool) {
	t.Helper()
	keep := map[string]bool{}
	j := savedJournal(t, f)
	for _, d := range j.Drawings {
		keep[d.Stage] = true
	}
	names, err := os.ReadDir(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name.Type().IsRegular() && strings.HasPrefix(name.Name(), ".snctl-drawing-") && (empty || !keep[name.Name()]) {
			t.Fatal("stray drawing stage", name.Name())
		}
	}
}

func drawingRun(t *testing.T, f *fixture) *run {
	t.Helper()
	root, err := f.e.disk.OpenMirror(t.Context(), f.dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	j, err := readJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	return &run{e: f.e, root: root, j: j, spec: f.spec, pass: f.e.started(f.spec, port.MirrorRunOptions{}), deferredDrawings: map[string]bool{}}
}

func drawingIntent(r *run, c port.Entry) *drawingChange {
	en := r.j.Items[c.ID]
	en.Output, en.Drawn = "_annotated/notebooks/X.pdf", c.Hash
	d := &drawingChange{ID: c.ID, Entry: en, Stage: ".snctl-drawing-pending"}
	r.j.Drawings = append(r.j.Drawings, d)
	return d
}

func TestDrawingStageCleanupScope(t *testing.T) {
	f, c := legacyDrawing(t)
	r := drawingRun(t, f)
	d := drawingIntent(r, c)
	for _, name := range []string{d.Stage, ".snctl-drawing-orphan", "nested/.snctl-drawing-nested", ".snctl-drawing", ".snctl-drawings-other"} {
		f.write(name, "keep")
	}
	if err := os.Mkdir(filepath.Join(f.dir, ".snctl-drawing-directory"), 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(f.dir, ".snctl-drawing-link")); err != nil {
		t.Fatal(err)
	}
	if err := r.j.write(t.Context(), r.root); err != nil {
		t.Fatal(err)
	}
	// Exercise the pass's startup even when subsequent cloud access fails.
	if err := r.root.Close(); err != nil {
		t.Fatal(err)
	}
	f.c.fail["list"] = errBoom
	if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{DryRun: true}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	if f.read(".snctl-drawing-orphan") != "keep" {
		t.Fatal("dry run removed stage")
	}
	if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(f.dir, ".snctl-drawing-orphan")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("orphan not cleaned", err)
	}
	for _, name := range []string{d.Stage, "nested/.snctl-drawing-nested", ".snctl-drawing", ".snctl-drawings-other", ".snctl-drawing-directory", ".snctl-drawing-link"} {
		if _, err := os.Lstat(filepath.Join(f.dir, name)); err != nil {
			t.Fatal(name, err)
		}
	}
}

func TestMissingDrawingStageDiscardsIntent(t *testing.T) {
	for _, nonregular := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "directory"}[nonregular], func(t *testing.T) {
			f, c := legacyDrawing(t)
			r := drawingRun(t, f)
			d := drawingIntent(r, c)
			old := drawingRetirement{Owner: c.ID, From: "_annotated/X.pdf", To: ".snctl-removed/_annotated/X.pdf", Before: true}
			d.Retired = []drawingRetirement{old}
			before := r.j.Items[c.ID]
			if err := r.root.Move(old.From, old.To); err != nil {
				t.Fatal(err)
			}
			if nonregular {
				if err := r.root.MkdirAll(d.Entry.Output); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.j.write(t.Context(), r.root); err != nil {
				t.Fatal(err)
			}
			if err := r.migrateDrawings(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(r.j.Drawings) != 0 || r.j.Items[c.ID] != before || !has(r.pass, port.MirrorWarning) || f.read(old.From) != "previous drawing" {
				t.Fatal("intent did not recover", r.j, r.pass)
			}
			if nonregular {
				if err := r.root.Remove(d.Entry.Output); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.root.Close(); err != nil {
				t.Fatal(err)
			}
			f.pass(port.MirrorRunOptions{})
			if f.read(d.Entry.Output) != "%PDF notebook" {
				t.Fatal("drawing not redrawn")
			}
		})
	}
}

// Fail only X, so another drawing and both original transfer directions remain observable.
type isolatedDrawingFault struct {
	port.MirrorDirectory
	point string
}

func (r isolatedDrawingFault) Produce(ctx context.Context, name string, write func(io.Writer) error) error {
	if r.point == "publish" && name == "_annotated/notebooks/X.pdf" || r.point == "stage" && strings.HasPrefix(name, ".snctl-drawing-") {
		return errBoom
	}
	return r.MirrorDirectory.Produce(ctx, name, write)
}
func (r isolatedDrawingFault) Move(from, to string) error {
	if r.point == "retire" && from == "_annotated/X.pdf" {
		return errBoom
	}
	return r.MirrorDirectory.Move(from, to)
}

type isolatedRenderFault struct{ noteRender }

func (r isolatedRenderFault) Render(ctx context.Context, input, target string, opts port.RenderOptions) (port.RenderReport, error) {
	b, err := os.ReadFile(input)
	if err != nil {
		return port.RenderReport{}, err
	}
	if string(b) == "note" {
		return port.RenderReport{}, errBoom
	}
	return noteRender{}.Render(ctx, input, target, opts)
}

func TestDrawingFailureContinuesPass(t *testing.T) {
	for _, point := range []string{"publish", "stage", "render", "retire", "resume"} {
		t.Run(point, func(t *testing.T) {
			f, c := legacyDrawing(t)
			disk := f.e.disk
			// A notebook fails first in tracked processing, before either original transfer.
			if point == "render" {
				f.e.notes = isolatedRenderFault{}
			} else {
				actual := point
				if point == "resume" {
					actual = "publish"
				}
				f.e.disk = faultDisk{MirrorDisk: disk, root: func(r port.MirrorDirectory) port.MirrorDirectory {
					return isolatedDrawingFault{MirrorDirectory: r, point: actual}
				}}
			}
			if point == "resume" {
				if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, errBoom) {
					t.Fatal(err)
				}
			}
			other := f.c.seed("DOCUMENT/Document/Papers/Y.note", "other note")
			f.c.seed("DOCUMENT/Document/Papers/download.pdf", "downloaded")
			f.write("upload.pdf", "uploaded")
			// The stage fault is one-shot for X; Y must still publish.
			if point == "stage" {
				hit := false
				f.e.disk = faultDisk{MirrorDisk: disk, root: func(r port.MirrorDirectory) port.MirrorDirectory {
					return &oneStageFailure{MirrorDirectory: r, hit: &hit}
				}}
			}
			p, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{})
			if !errors.Is(err, errBoom) || p.Pending == 0 || !has(p, port.MirrorUpload) || !has(p, port.MirrorDownload) {
				t.Fatal("pass stopped after drawing failure", p, err)
			}
			j := savedJournal(t, f)
			if j.Items[other.ID].Drawn != other.Hash || f.read("_annotated/notebooks/Y.pdf") != "%PDF notebook" || f.read("download.pdf") != "downloaded" || f.c.data[f.remote("upload.pdf").ID] != "uploaded" {
				t.Fatal("unrelated work not completed", j)
			}
			if point == "publish" || point == "retire" || point == "resume" {
				if len(j.Drawings) != 1 || j.Drawings[0].ID != c.ID {
					t.Fatal("failed drawing lost durable intent", j)
				}
				f.e.notes = noteRender{err: port.ErrRenderUnavailable}
			} else {
				f.e.notes = noteRender{}
			}
			assertDrawingStages(t, f, false)
			f.e.disk = disk
			f.pass(port.MirrorRunOptions{})
			assertDrawingStages(t, f, true)
			if savedJournal(t, f).Items[c.ID].Output != "_annotated/notebooks/X.pdf" {
				t.Fatal("failed drawing not retried")
			}
		})
	}
}

type oneStageFailure struct {
	port.MirrorDirectory
	hit *bool
}

func (r *oneStageFailure) Produce(ctx context.Context, name string, write func(io.Writer) error) error {
	if strings.HasPrefix(name, ".snctl-drawing-") && !*r.hit {
		*r.hit = true
		return errBoom
	}
	return r.MirrorDirectory.Produce(ctx, name, write)
}

func TestDrawingCommitPreservesCurrentEntry(t *testing.T) {
	f, c := legacyDrawing(t)
	r := drawingRun(t, f)
	d := drawingIntent(r, c)
	f.write(d.Stage, "replacement")
	current := r.j.Items[c.ID]
	current.Path = "original.pdf"
	current.CloudPath = "DOCUMENT/Document/Papers/original.pdf"
	current.Record = strings.Repeat("a", 32)
	current.SHA256 = strings.Repeat("b", 64)
	current.Size = 42
	current.ModTime = now.Add(time.Hour)
	current.DrawingWarning = "new warning"
	current.Detached, current.Recover, current.RestoreLocal = true, true, true
	r.j.Items[c.ID] = current
	if err := r.completeDrawing(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	want := current
	want.Output, want.Drawn = d.Entry.Output, d.Entry.Drawn
	if got := r.j.Items[c.ID]; !reflect.DeepEqual(got, want) {
		t.Fatalf("publication clobbered current item: got %+v want %+v", got, want)
	}
}

func TestDrawingRetirementBothPathsExist(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(map[bool]string{false: "after", true: "before"}[before], func(t *testing.T) {
			f, c := legacyDrawing(t)
			r := drawingRun(t, f)
			d := drawingIntent(r, c)
			old := drawingRetirement{Owner: c.ID, From: "_annotated/X.pdf", To: ".snctl-removed/_annotated/X.pdf", Before: before}
			d.Retired = []drawingRetirement{old}
			f.write(d.Stage, "replacement")
			f.write(old.To, "earlier retained drawing")
			if err := r.completeDrawing(t.Context(), d); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(f.dir, old.From)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("untracked live output remains", err)
			}
			if f.read(old.To) != "earlier retained drawing" || f.read(d.Retired[0].To) != "previous drawing" || d.Retired[0].To == old.To {
				t.Fatal("retirement lost bytes")
			}
			if got := savedJournal(t, f); len(got.Drawings) != 0 || got.Items[c.ID].Output != d.Entry.Output {
				t.Fatal(got)
			}
		})
	}
}

type restoreFailure struct{ port.MirrorDirectory }

func (r restoreFailure) Move(from, to string) error {
	if strings.HasPrefix(from, domain.MirrorRemovedDir+"/") {
		return errBoom
	}
	return r.MirrorDirectory.Move(from, to)
}

func TestFailedDrawingRestoreRemainsPending(t *testing.T) {
	for _, stage := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing-stage", true: "failed-publication"}[stage], func(t *testing.T) {
			f, c := legacyDrawing(t)
			r := drawingRun(t, f)
			d := drawingIntent(r, c)
			old := drawingRetirement{Owner: c.ID, From: "_annotated/X.pdf", To: ".snctl-removed/_annotated/X.pdf", Before: true}
			d.Retired = []drawingRetirement{old}
			if err := r.root.Move(old.From, old.To); err != nil {
				t.Fatal(err)
			}
			if stage {
				f.write(d.Stage, "replacement")
			}
			base := r.root
			r.root = restoreFailure{MirrorDirectory: isolatedDrawingFault{MirrorDirectory: base, point: "publish"}}
			if err := r.j.write(t.Context(), r.root); err != nil {
				t.Fatal(err)
			}
			if err := r.migrateDrawings(t.Context()); err != nil {
				t.Fatal("restore failure was pass-fatal", err)
			}
			if len(r.j.Drawings) != 1 || r.pass.Pending == 0 || !strings.Contains(r.pass.Actions[0].Err.Error(), "restoring drawing") {
				t.Fatal("failed restore not reported as pending", r.pass, r.j)
			}
			if savedJournal(t, f).Items[c.ID].Output != old.From || f.read(old.To) != "previous drawing" {
				t.Fatal("restore failure lost ownership")
			}
			r.root = base
			if err := r.migrateDrawings(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(r.j.Drawings) != 0 {
				t.Fatal("restore never retried")
			}
		})
	}
}

func TestSuccessfulJournalWritePreservesFailure(t *testing.T) {
	f, c := legacyDrawing(t)
	r := drawingRun(t, f)
	r.failure = errBoom
	r.saved = now.Add(-checkpointEvery)
	r.checkpoint(t.Context())
	if !errors.Is(r.failure, errBoom) {
		t.Fatal("checkpoint cleared earlier failure")
	}
	en := r.j.Items[c.ID]
	en.Output = "_annotated/notebooks/X.pdf"
	if err := r.publishDrawing(t.Context(), c.ID, en, strings.NewReader("replacement")); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(r.failure, errBoom) {
		t.Fatal("publication cleared earlier failure")
	}
}

// Multiple simultaneous failures must not replace each other's resumable bytes.
type blockedPublications struct{ port.MirrorDirectory }

func (r blockedPublications) Produce(ctx context.Context, name string, write func(io.Writer) error) error {
	if name == "_annotated/notebooks/X.pdf" || name == "_annotated/notebooks/Y.pdf" {
		return errBoom
	}
	return r.MirrorDirectory.Produce(ctx, name, write)
}
func TestMultipleDrawingIntentsResumeIndependently(t *testing.T) {
	f, _ := legacyDrawing(t)
	f.c.seed("DOCUMENT/Document/Papers/Y.note", "second note")
	good := f.c.seed("DOCUMENT/Document/Papers/Z.note", "third note")
	disk := f.e.disk
	f.e.disk = faultDisk{MirrorDisk: disk, root: func(r port.MirrorDirectory) port.MirrorDirectory { return blockedPublications{r} }}
	for range 2 {
		p, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{})
		if !errors.Is(err, errBoom) || p.Pending < 2 {
			t.Fatal(p, err)
		}
		j := savedJournal(t, f)
		if len(j.Drawings) != 2 || j.Items[good.ID].Drawn != good.Hash {
			t.Fatal("pending intents lost or independent drawing blocked", j)
		}
		assertDrawingStages(t, f, false)
	}
	f.e.disk = disk
	f.e.notes = noteRender{err: port.ErrRenderUnavailable}
	f.pass(port.MirrorRunOptions{})
	if j := savedJournal(t, f); len(j.Drawings) != 0 || len(j.Items) != 3 {
		t.Fatal(j)
	}
	assertDrawingStages(t, f, true)
}

func TestDrawingJournalFailureStopsPass(t *testing.T) {
	f, _ := legacyDrawing(t)
	f.c.seed("DOCUMENT/Document/Papers/later.note", "later note")
	f.write("upload.pdf", "original")
	disk := f.e.disk
	f.e.disk = faultDisk{MirrorDisk: disk, root: func(r port.MirrorDirectory) port.MirrorDirectory {
		return faultRoot{MirrorDirectory: r, fail: "write-journal"}
	}}
	p, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{})
	if !errors.Is(err, errBoom) || has(p, port.MirrorUpload) || len(p.Actions) != 1 {
		t.Fatal("journal failure did not stop pass", p, err)
	}
	f.e.disk = disk
	assertDrawingStages(t, f, true)
}

func TestDrawingIntentJournalCompatibility(t *testing.T) {
	f, c := legacyDrawing(t)
	r := drawingRun(t, f)
	d := drawingIntent(r, c)
	r.j.Drawing = d
	r.j.Drawings = nil
	if err := r.j.write(t.Context(), r.root); err != nil {
		t.Fatal(err)
	}
	got, err := readJournal(r.root)
	if err != nil || got.Drawing != nil || len(got.Drawings) != 1 || got.Drawings[0].ID != c.ID {
		t.Fatal("single intent not accepted", got, err)
	}
	for _, bad := range []string{"nil", "duplicate", "stage", "output", "owner", "from", "to"} {
		t.Run(bad, func(t *testing.T) {
			copyIntent := *d
			copyIntent.Retired = []drawingRetirement{{Owner: c.ID, From: "_annotated/X.pdf", To: ".snctl-removed/_annotated/X.pdf"}}
			r.j.Drawing = nil
			r.j.Drawings = []*drawingChange{&copyIntent}
			switch bad {
			case "nil":
				r.j.Drawings = []*drawingChange{nil}
			case "duplicate":
				r.j.Drawings = append(r.j.Drawings, &copyIntent)
			case "stage":
				copyIntent.Stage = "nested/.snctl-drawing-bad"
			case "output":
				copyIntent.Entry.Output = ""
			case "owner":
				copyIntent.Retired[0].Owner = "unknown"
			case "from":
				copyIntent.Retired[0].From = "original.pdf"
			case "to":
				copyIntent.Retired[0].To = "original.pdf"
			}
			if err := r.j.write(t.Context(), r.root); err != nil {
				t.Fatal(err)
			}
			if _, err := readJournal(r.root); !errors.Is(err, ErrJournal) {
				t.Fatal("invalid intent accepted", bad, err)
			}
		})
	}
}

type cleanupFailure struct {
	port.MirrorDirectory
	point string
}

func (r cleanupFailure) RootEntries() ([]port.LocalEntry, error) {
	if r.point == "list" {
		return nil, errBoom
	}
	return r.MirrorDirectory.RootEntries()
}
func (r cleanupFailure) Remove(name string) error {
	if strings.HasPrefix(name, ".snctl-drawing-") {
		return errBoom
	}
	return r.MirrorDirectory.Remove(name)
}
func TestDrawingCleanupFailureContinuesPass(t *testing.T) {
	for _, point := range []string{"list", "remove"} {
		t.Run(point, func(t *testing.T) {
			f := setup(t)
			f.write(".snctl-drawing-orphan", "abandoned PDF")
			f.write("original.pdf", "upload")
			disk := f.e.disk
			f.e.disk = faultDisk{MirrorDisk: disk, root: func(r port.MirrorDirectory) port.MirrorDirectory {
				return cleanupFailure{MirrorDirectory: r, point: point}
			}}
			p, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{})
			if !errors.Is(err, errBoom) || p.Pending == 0 || !has(p, port.MirrorWarning) || !has(p, port.MirrorUpload) {
				t.Fatal("cleanup failure stopped originals or was unreported", p, err)
			}
			f.e.disk = disk
			f.pass(port.MirrorRunOptions{})
			assertDrawingStages(t, f, true)
		})
	}
}

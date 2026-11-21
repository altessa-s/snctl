// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package mirror

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
)

func legacyDrawing(t *testing.T) (*fixture, port.Entry) {
	t.Helper()
	f := setup(t)
	f.spec.PullNew = true
	c := f.c.seed("DOCUMENT/Document/Papers/X.note", "note")
	j := journal{Version: 1, Folder: f.remoteFolderID(), Path: strings.Trim(f.spec.Folder, "/"),
		Items: map[string]entry{c.ID: {CloudPath: c.Path, Record: c.Hash, Drawn: c.Hash, Output: "_annotated/X.pdf"}}}
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	f.write(domain.MirrorJournalName, string(data))
	f.write("_annotated/X.pdf", "previous drawing")
	return f, c
}

func TestFailedDrawingKeepsLiveOutput(t *testing.T) {
	for _, scenario := range []string{"migration", "redraw", "rename"} {
		for _, fault := range []string{"renderless", "render", "produce"} {
			t.Run(scenario+"/"+fault, func(t *testing.T) {
				f, c := legacyDrawing(t)
				old := "_annotated/X.pdf"
				if scenario != "migration" {
					f.pass(port.MirrorRunOptions{})
					old = "_annotated/notebooks/X.pdf"
					f.write(old, "previous drawing")
					if scenario == "redraw" {
						f.remoteChange("X.note", "changed note")
					} else if err := f.c.Move(t.Context(), c, "DOCUMENT/Document/Papers/Renamed.note"); err != nil {
						t.Fatal(err)
					}
				}
				before := savedJournal(t, f).Items[c.ID]
				want := errBoom
				if fault == "renderless" {
					want = port.ErrRenderUnavailable
				}
				if fault == "produce" {
					f.e.disk = faultDisk{MirrorDisk: f.e.disk, root: func(r port.MirrorDirectory) port.MirrorDirectory {
						return drawingProduceFailure{MirrorDirectory: r}
					}}
				} else {
					f.e.notes = noteRender{err: want}
				}
				for range 2 {
					if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, want) {
						t.Fatal(err)
					}
					if f.read(old) != "previous drawing" {
						t.Fatal("previous drawing is no longer live")
					}
					after := savedJournal(t, f).Items[c.ID]
					if after.Output != before.Output || after.Drawn != before.Drawn {
						t.Fatal("failed draw lost retry state", before, after)
					}
				}
			})
		}
	}
}

type drawingProduceFailure struct{ port.MirrorDirectory }

func (r drawingProduceFailure) Produce(ctx context.Context, name string, write func(io.Writer) error) error {
	if name != domain.MirrorJournalName {
		return errBoom
	}
	return r.MirrorDirectory.Produce(ctx, name, write)
}

func TestLegacyDrawingDryRunReportsRetirement(t *testing.T) {
	f, c := legacyDrawing(t)
	before := f.read(domain.MirrorJournalName)
	p := f.pass(port.MirrorRunOptions{DryRun: true})
	var removed, drawn bool
	for _, a := range p.Actions {
		if a.Document == c.ID && a.Kind == port.MirrorRemove && a.Path == "_annotated/X.pdf" &&
			a.To == ".snctl-removed/_annotated/X.pdf" {
			removed = true
		}
		if a.Document == c.ID && a.Kind == port.MirrorDraw && a.To == "_annotated/notebooks/X.pdf" {
			drawn = true
		}
	}
	if !removed || !drawn {
		t.Fatal("dry-run omitted migration work", p)
	}
	if f.read(domain.MirrorJournalName) != before || f.read("_annotated/X.pdf") != "previous drawing" {
		t.Fatal("dry-run changed migration state")
	}
	for _, name := range []string{".snctl-removed", "_annotated/notebooks"} {
		if _, err := os.Stat(filepath.Join(f.dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("dry-run created output", name, err)
		}
	}
}

// publicationFault interrupts staging, intent writes and publication boundaries.
// Failures return normally; crashes leave only the last written journal behind.
type publicationFault struct {
	port.MirrorDirectory
	point  string
	crash  bool
	hit    bool
	writes int
}

func (r *publicationFault) trip(point string) error {
	if r.point != point || r.hit {
		return nil
	}
	r.hit = true
	if r.crash {
		panic(errBoom)
	}
	return errBoom
}

func (r *publicationFault) Produce(ctx context.Context, name string, write func(io.Writer) error) error {
	if name == domain.MirrorJournalName {
		r.writes++
		if r.writes == 1 {
			if err := r.trip("intent-journal"); err != nil {
				return err
			}
		}
		if r.writes == 2 {
			if err := r.trip("journal"); err != nil {
				return err
			}
		}
	}
	if strings.HasPrefix(name, "_annotated/") {
		if err := r.trip("publish"); err != nil {
			return err
		}
	}
	if err := r.MirrorDirectory.Produce(ctx, name, write); err != nil {
		return err
	}
	if strings.HasPrefix(name, ".snctl-drawing-") {
		return r.trip("stage-create")
	}
	return nil
}

func (r *publicationFault) Open(name string) (io.ReadCloser, error) {
	if strings.HasPrefix(name, ".snctl-drawing-") {
		if err := r.trip("open"); err != nil {
			return nil, err
		}
	}
	return r.MirrorDirectory.Open(name)
}

func (r *publicationFault) Info(name string) (port.FileInfo, error) {
	point := "info-retained"
	if strings.HasPrefix(name, ".snctl-drawing-") {
		point = "info-stage"
	}
	if err := r.trip(point); err != nil {
		return port.FileInfo{}, err
	}
	return r.MirrorDirectory.Info(name)
}

func (r *publicationFault) Remove(name string) error {
	if strings.HasPrefix(name, ".snctl-drawing-") {
		if err := r.trip("remove-stage"); err != nil {
			return err
		}
	}
	return r.MirrorDirectory.Remove(name)
}

func (r *publicationFault) Move(from, to string) error {
	if err := r.trip("move"); err != nil {
		return err
	}
	if err := r.MirrorDirectory.Move(from, to); err != nil {
		return err
	}
	return r.trip("moved")
}

func TestDrawingPublicationRecovery(t *testing.T) {
	for _, alias := range []bool{false, true} {
		for _, crash := range []bool{false, true} {
			for _, point := range []string{"stage-create", "intent-journal", "publish", "open", "info-stage", "remove-stage", "move", "moved", "journal"} {
				name := map[bool]string{false: "distinct/", true: "alias/"}[alias] + point
				t.Run(name+map[bool]string{false: "/error", true: "/crash"}[crash], func(t *testing.T) {
					f, c := legacyDrawing(t)
					old, out := "_annotated/X.pdf", "_annotated/notebooks/X.pdf"
					if alias {
						f.pass(port.MirrorRunOptions{})
						old, out = out, "_annotated/notebooks/x.pdf"
						f.write(old, "previous drawing")
						if err := f.c.Move(t.Context(), c, "DOCUMENT/Document/Papers/x.note"); err != nil {
							t.Fatal(err)
						}
					}
					disk := f.e.disk
					var fault *publicationFault
					f.e.disk = faultDisk{MirrorDisk: disk, root: func(r port.MirrorDirectory) port.MirrorDirectory {
						fault = &publicationFault{MirrorDirectory: r, point: point, crash: crash}
						return fault
					}}
					func() {
						defer func() {
							if got := recover(); got != nil && (!crash || got != errBoom) {
								panic(got)
							}
						}()
						if _, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, errBoom) {
							t.Fatal(err)
						}
					}()
					if !fault.hit {
						t.Fatal("did not reach publication fault", point)
					}
					if !crash && (point == "publish" || point == "open" || point == "info-stage" || point == "move") {
						if f.read(old) != "previous drawing" {
							t.Fatal("failed replacement removed the live drawing")
						}
					}
					if !crash {
						assertDrawingStages(t, f, false)
					}
					f.e.disk = disk
					f.e.notes = noteRender{err: port.ErrRenderUnavailable}
					if point == "stage-create" || point == "intent-journal" {
						f.e.notes = noteRender{}
					}
					f.pass(port.MirrorRunOptions{})
					assertDrawingStages(t, f, true)
					if f.read(out) != "%PDF notebook" || f.read(".snctl-removed/"+old) != "previous drawing" {
						t.Fatal("recovery lost output or retained bytes")
					}
					if j := savedJournal(t, f); j.Drawing != nil || j.Items[c.ID].Output != out {
						t.Fatal("publication intent not completed", j)
					}
					if p := f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
						t.Fatal("recovered drawing is still pending", p)
					}
				})
			}
		}
	}
}

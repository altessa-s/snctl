// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package mirror

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/altessa-s/snctl/internal/port"
)

type overlayNotes struct {
	port.Notebooks
	calls int
	err   error
}

func (n *overlayNotes) Render(_ context.Context, input, target string, o port.RenderOptions) (port.RenderReport, error) {
	n.calls++
	if n.err != nil {
		return port.RenderReport{}, n.err
	}
	mark, err := os.ReadFile(input)
	if err != nil {
		return port.RenderReport{}, err
	}
	source, err := os.ReadFile(o.Source)
	if err != nil {
		return port.RenderReport{}, err
	}
	return port.RenderReport{Warnings: []string{"dropped annotations"}}, os.WriteFile(target, append(append([]byte("%PDF "), source...), mark...), 0o600)
}

func overlayFixture(t *testing.T) (*fixture, *overlayNotes, port.Entry, port.Entry) {
	t.Helper()
	f := setup(t)
	f.spec.PullNew = true
	n := &overlayNotes{}
	f.e.notes = n
	doc := f.c.seed("DOCUMENT/Document/Papers/Report.pdf", "original")
	mark := f.c.seed("DOCUMENT/Document/Papers/Report.pdf.mark", "ink")
	return f, n, doc, mark
}

func TestDocumentDrawingLifecycle(t *testing.T) {
	f, n, doc, mark := overlayFixture(t)
	p := f.pass(port.MirrorRunOptions{DryRun: true})
	if !has(p, port.MirrorDraw) || n.calls != 0 {
		t.Fatal(p, n.calls)
	}
	p = f.pass(port.MirrorRunOptions{})
	if !has(p, port.MirrorDraw) || n.calls != 1 || f.read("_annotated/documents/Report.pdf") != "%PDF originalink" {
		t.Fatal(p, n.calls)
	}
	for _, a := range p.Actions {
		if a.Kind == port.MirrorDraw && len(a.Warnings) != 1 {
			t.Fatal(a)
		}
	}
	if p = f.pass(port.MirrorRunOptions{}); len(p.Actions) != 0 {
		t.Fatal(p)
	}
	f.remoteChange("Report.pdf.mark", "new ink")
	if p = f.pass(port.MirrorRunOptions{}); !has(p, port.MirrorDraw) || n.calls != 2 {
		t.Fatal(p, n.calls)
	}
	f.remoteChange("Report.pdf", "new original")
	if p = f.pass(port.MirrorRunOptions{}); !has(p, port.MirrorDraw) || n.calls != 3 {
		t.Fatal(p, n.calls)
	}
	if err := f.c.Move(t.Context(), doc, "DOCUMENT/Document/Papers/Renamed.pdf"); err != nil {
		t.Fatal(err)
	}
	if err := f.c.Move(t.Context(), mark, "DOCUMENT/Document/Papers/Renamed.pdf.mark"); err != nil {
		t.Fatal(err)
	}
	p = f.pass(port.MirrorRunOptions{})
	if !has(p, port.MirrorDraw) || f.read(".snctl-removed/_annotated/documents/Report.pdf") != "%PDF new originalnew ink" {
		t.Fatal(p)
	}
	delete(f.c.entries, mark.ID)
	p = f.pass(port.MirrorRunOptions{})
	if !has(p, port.MirrorRemove) || f.read(".snctl-removed/_annotated/documents/Renamed.pdf") != "%PDF new originalnew ink" {
		t.Fatal(p)
	}
	f.c.seed("DOCUMENT/Document/Papers/Renamed.pdf.mark", "ink again")
	f.pass(port.MirrorRunOptions{})
	delete(f.c.entries, doc.ID)
	f.pass(port.MirrorRunOptions{ForceDelete: true})
	if f.read(".snctl-removed/_annotated/documents/Renamed (2).pdf") != "%PDF new originalink again" {
		t.Fatal("removal did not retain drawing")
	}
}

func TestDocumentOverlayPendingAndUnsupported(t *testing.T) {
	f, n, _, _ := overlayFixture(t)
	n.err = port.ErrRenderUnavailable
	p, err := f.e.Pass(t.Context(), f.spec, port.MirrorRunOptions{})
	if err == nil || p.Pending != 1 {
		t.Fatal(p, err)
	}
	for _, a := range p.Actions {
		if a.Kind == port.MirrorDraw && !errors.Is(a.Err, port.ErrRenderUnavailable) {
			t.Fatal(a)
		}
	}
	n.err = nil
	f.pass(port.MirrorRunOptions{})
	f.c.seed("DOCUMENT/Document/Papers/Book.epub", "epub")
	f.c.seed("DOCUMENT/Document/Papers/Book.mark", "epub ink")
	p = f.pass(port.MirrorRunOptions{})
	if !has(p, port.MirrorWarning) || p.Pending != 0 || n.calls != 2 {
		t.Fatal(p, n.calls)
	}
}

func TestLegacySidecarsAndCollisions(t *testing.T) {
	f, n, _, mark := overlayFixture(t)
	delete(f.c.entries, mark.ID)
	f.c.seed("DOCUMENT/Document/Papers/Report.mark", "legacy ink")
	f.pass(port.MirrorRunOptions{})
	if n.calls != 1 {
		t.Fatal(n.calls)
	}
	f.c.seed("DOCUMENT/Document/Papers/Report.epub", "epub")
	p := f.pass(port.MirrorRunOptions{})
	if !has(p, port.MirrorWarning) || n.calls != 1 {
		t.Fatal(p, n.calls)
	}
}

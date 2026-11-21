// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
)

func (r *run) notebook(ctx context.Context, id string, en entry, c port.Entry, remote bool) {
	if r.drawingPending(id, "") {
		return
	}
	if !remote {
		// A displaced drawing has already been retained by its new owner.
		if en.Output == "" {
			if !r.opts.DryRun {
				delete(r.j.Items, id)
			}
			return
		}
		if r.deletion(id, en, true) {
			r.remove(ctx, id, en, en.Output)
		}
		return
	}
	if !r.spec.PullNew {
		return
	}
	rel := r.relative(c)
	out := domain.MirrorAnnotatedPath(path.Join("notebooks", rel), "pdf")
	if en.Drawn == c.Hash && en.Output == out {
		if en.TextChecked {
			return
		}
		r.addDrawingText(ctx, c, en)
		return
	}
	r.drawing(ctx, c, port.Entry{}, en, out, c.Hash)
}

// Upgrade current drawings using cache-backed notebook reads, without rendering PDFs.
func (r *run) addDrawingText(ctx context.Context, c port.Entry, en entry) {
	a := port.MirrorAction{Kind: port.MirrorDraw, Path: r.relative(c), To: drawingTextPath(en.Output), Document: c.ID,
		Detail: "check recognized text for current drawing"}
	if !r.opts.DryRun {
		n, err := r.e.notes.Read(ctx, "/"+strings.TrimPrefix(c.Path, "/"), nil, nil)
		a.Err = err
		if err == nil {
			en.TextChecked, en.Text = true, n.HasRecognition()
			if en.Text {
				a.Err = r.publishDrawing(ctx, c.ID, en, nil, n.Clean().Text(true))
				if a.Err == nil {
					r.pass.TextAdded++
				}
			} else {
				r.j.Items[c.ID] = en
			}
		}
	}
	r.record(a)
	r.checkpoint(ctx)
}

func (r *run) drawing(ctx context.Context, c, mark port.Entry, en entry, out, version string) {
	if r.drawingPending(c.ID, out) {
		r.record(port.MirrorAction{Kind: port.MirrorSkip, Path: out, Document: c.ID, Detail: "drawing publication awaits recovery"})
		return
	}
	a := port.MirrorAction{Kind: port.MirrorDraw, Path: r.relative(c), To: out, Document: c.ID}
	if r.opts.DryRun {
		a.Err = r.planDrawing(c.ID, out)
	} else {
		a.Warnings, a.Err = r.draw(ctx, c, mark, en, out, version)
	}
	r.record(a)
	r.checkpoint(ctx)
}
func (r *run) draw(ctx context.Context, c, mark port.Entry, en entry, out, version string) ([]string, error) {
	original, err := r.e.cloud.Download(ctx, c, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.e.files.Remove(original) }()
	// A renderer writes to its private staging file; only the confined root publishes the drawing.
	temp, err := r.e.files.Stage(strings.NewReader(""))
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.e.files.Remove(temp) }()
	input := original
	opts := port.RenderOptions{Format: "pdf"}
	if mark.ID != "" {
		input, err = r.e.cloud.Download(ctx, mark, nil)
		if err != nil {
			return nil, err
		}
		defer func() { _ = r.e.files.Remove(input) }()
		opts.Source = original
	}
	report, err := r.e.notes.Render(ctx, input, temp, opts)
	if err != nil {
		return nil, err
	}
	text := ""
	if mark.ID == "" {
		n, readErr := r.e.notes.Read(ctx, input, nil, nil)
		if readErr != nil {
			return nil, readErr
		}
		en.TextChecked = true
		en.Text = n.HasRecognition()
		if en.Text {
			text = n.Clean().Text(true)
		}
	}
	if err = r.root.MkdirAll(path.Dir(out)); err != nil {
		return nil, err
	}
	in, err := r.e.files.Open(temp)
	if err != nil {
		return nil, err
	}
	// Original documents own their reconcile markers. Only notebook drawings
	// use these fields as their cloud identity and version.
	if en.Path == "" {
		en.CloudPath, en.Record = c.Path, c.Hash
	}
	en.Output, en.Drawn = out, version
	err = r.publishDrawing(ctx, c.ID, en, in, text)
	err = errors.Join(err, in.Close())
	if err != nil {
		return nil, err
	}
	return report.Warnings, nil
}

// documentDrawings runs after reconciliation so downloads, renames and uploads
// cannot overwrite the drawing state. Both input identities form its version.
func (r *run) documentDrawings(ctx context.Context) {
	if !r.spec.PullNew {
		return
	}
	for _, id := range slices.Sorted(maps.Keys(r.docs)) {
		if ctx.Err() != nil || r.failure != nil {
			return
		}
		c := r.docs[id]
		if !domain.MirrorUploadable(c.Path) {
			continue
		}
		en := r.j.Items[id]
		if en.Detached || r.drawingPending(id, "") || r.deferredDrawings[id] || en.Path == "" && !r.opts.DryRun {
			continue
		}
		mark, warning := r.sidecar(c)
		rel := r.relative(c)
		r.drawingWarning(id, &en, warning)
		if warning != "" {
			continue
		}
		if mark.ID == "" || !strings.EqualFold(path.Ext(c.Path), ".pdf") {
			if mark.ID != "" && r.spec.PullNew {
				r.record(port.MirrorAction{Kind: port.MirrorWarning, Path: rel, Document: id, Detail: "EPUB annotation overlay is unsupported; skipped"})
			}
			if en.Output != "" {
				r.retireDrawing(ctx, id, en)
			}
			continue
		}
		if !r.spec.PullNew {
			continue
		}
		out := path.Join(domain.MirrorAnnotatedDir, "documents", rel)
		version := c.Hash + "/" + mark.ID + "/" + mark.Hash
		if en.Drawn == version && en.Output == out {
			continue
		}
		r.drawing(ctx, c, mark, en, out, version)
	}
}

func (r *run) sidecar(c port.Entry) (port.Entry, string) {
	rel := r.relative(c)
	if problem := r.markProblems[foldName(rel+".mark")]; problem != "" {
		return port.Entry{}, problem
	}
	if mark, ok := r.marks[foldName(rel+".mark")]; ok {
		return mark, ""
	}
	stem := strings.TrimSuffix(rel, path.Ext(rel))
	if problem := r.markProblems[foldName(stem+".mark")]; problem != "" {
		return port.Entry{}, problem
	}
	mark, ok := r.marks[foldName(stem+".mark")]
	if !ok {
		return port.Entry{}, ""
	}
	// A legacy stem-only sidecar cannot distinguish PDF from EPUB of one stem.
	matches := []string{}
	for _, other := range r.docs {
		if domain.MirrorUploadable(other.Path) && foldName(strings.TrimSuffix(r.relative(other), path.Ext(other.Path))) == foldName(stem) {
			matches = append(matches, other.ID+"/"+other.Path)
		}
	}
	if len(matches) > 1 {
		slices.Sort(matches)
		warning := "ambiguous stem-only .mark sidecar; use the original's full name plus .mark"
		return port.Entry{}, warning + "\n" + sidecarIdentity(mark) + "\n" + strings.Join(matches, "\n")
	}
	return mark, ""
}

func sidecarIdentity(c port.Entry) string {
	size := "missing"
	if c.Size != nil {
		size = fmt.Sprint(*c.Size)
	}
	return c.Path + "/" + c.ID + "/" + c.Hash + "/" + size
}

func (r *run) drawingWarning(id string, en *entry, warning string) {
	if warning == en.DrawingWarning {
		return
	}
	if warning != "" {
		detail, _, _ := strings.Cut(warning, "\n")
		r.record(port.MirrorAction{Kind: port.MirrorWarning, Path: r.relative(r.docs[id]), Document: id, Detail: detail})
	}
	if !r.opts.DryRun {
		en.DrawingWarning = warning
		r.j.Items[id] = *en
	}
}

// Legacy paths remain tracked until their replacements have been published.
// The output path, rather than the journal version, keeps failed draws pending.
func (r *run) migrateDrawings(ctx context.Context) error {
	if r.opts.DryRun {
		return nil
	}
	for _, d := range slices.Clone(r.j.Drawings) {
		if err := r.completeDrawing(ctx, d); err != nil {
			r.record(port.MirrorAction{Kind: port.MirrorDraw, Path: d.Entry.Output, Document: d.ID, Err: err})
			if r.failure != nil {
				return r.failure
			}
		}
	}
	r.j.Version = journalVersion
	return nil
}

func (r *run) retainDrawingInputs() {
	keeper, ok := r.e.cloud.(port.DrawingInputRetainer)
	if !ok {
		return
	}
	var inputs []port.Entry
	if r.spec.PullNew {
		for _, c := range r.docs {
			if strings.EqualFold(path.Ext(c.Path), ".note") {
				inputs = append(inputs, c)
				continue
			}
			mark, warning := r.sidecar(c)
			if strings.EqualFold(path.Ext(c.Path), ".pdf") && mark.ID != "" && warning == "" {
				inputs = append(inputs, c, mark)
			}
		}
	}
	keeper.RetainDrawingInputs(inputs)
}

func (r *run) retireDrawing(ctx context.Context, id string, en entry) {
	a := port.MirrorAction{Kind: port.MirrorRemove, Path: en.Output, Document: id}
	if !r.opts.DryRun {
		a.To, a.Err = r.aside(en.Output)
		if a.Err == nil && en.Text {
			_, a.Err = r.aside(drawingTextPath(en.Output))
		}
		if a.Err == nil {
			en.Output, en.Drawn = "", ""
			en.Text, en.TextChecked = false, false
			r.j.Items[id] = en
		}
	}
	r.record(a)
	r.checkpoint(ctx)
}

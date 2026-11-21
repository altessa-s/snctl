// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package mirror

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
)

// drawingChange persists a fully produced replacement before any live path is
// retired. A pass can finish publication after interruption without rendering.
type drawingChange struct {
	ID        string              `json:"id"`
	Stage     string              `json:"stage"`
	TextStage string              `json:"text_stage,omitempty"`
	Entry     entry               `json:"entry"`
	Retired   []drawingRetirement `json:"retired"`
}

type drawingRetirement struct {
	Owner  string `json:"owner"`
	From   string `json:"from"`
	To     string `json:"to"`
	Before bool   `json:"before"`
}

func (j *journal) validDrawing() bool {
	seen := map[string]bool{}
	all := slices.Clone(j.Drawings)
	if j.Drawing != nil {
		all = append(all, j.Drawing)
	}
	for _, d := range all {
		if d == nil || seen[d.ID] || !j.validDrawingChange(d) {
			return false
		}
		seen[d.ID] = true
	}
	return true
}

func (j *journal) validDrawingChange(d *drawingChange) bool {
	if d.ID == "" || !safeRel(d.Stage, false) || !strings.HasPrefix(d.Stage, ".snctl-drawing-") || strings.Contains(d.Stage, "/") {
		return false
	}
	if d.TextStage != "" && (!d.Entry.Text || d.TextStage != d.Stage+"-text") {
		return false
	}
	if d.Entry.Text && d.TextStage == "" {
		return false
	}
	check := *j
	check.Items = map[string]entry{d.ID: d.Entry}
	if d.Entry.Output == "" || !check.validEntries() {
		return false
	}
	for _, old := range d.Retired {
		if _, ok := j.Items[old.Owner]; !ok {
			return false
		}
		if !safeRel(old.From, false) || !strings.HasPrefix(old.From, domain.MirrorAnnotatedDir+"/") ||
			!safeRel(old.To, false) || !strings.HasPrefix(old.To, domain.MirrorRemovedDir+"/") {
			return false
		}
	}
	return true
}

func (r *run) drawingRetirements(id, out string) ([]drawingRetirement, error) {
	var retired []drawingRetirement
	for _, owner := range slices.Sorted(maps.Keys(r.j.Items)) {
		en := r.j.Items[owner]
		displaced := owner == id && en.Output != out || owner != id && foldName(en.Output) == foldName(out)
		if en.Output == "" || !displaced {
			continue
		}
		before := owner != id
		if !before && foldName(en.Output) == foldName(out) {
			var err error
			before, err = r.root.SameFile(en.Output, out)
			if err != nil {
				return nil, err
			}
		}
		to, err := r.drawingAside(en.Output)
		if err != nil {
			return nil, err
		}
		retired = append(retired, drawingRetirement{Owner: owner, From: en.Output, To: to, Before: before})
		if en.Text {
			from := drawingTextPath(en.Output)
			to, err := r.drawingAside(from)
			if err != nil {
				return nil, err
			}
			retired = append(retired, drawingRetirement{Owner: owner, From: from, To: to, Before: before})
		}
	}
	return retired, nil
}

func (r *run) drawingAside(from string) (string, error) {
	for n := 1; n <= maxAside; n++ {
		to := path.Join(domain.MirrorRemovedDir, from)
		if n > 1 {
			ext := path.Ext(to)
			to = strings.TrimSuffix(to, ext) + fmt.Sprintf(" (%d)", n) + ext
		}
		info, err := r.root.Info(to)
		if err != nil || !info.Exists {
			return to, err
		}
	}
	return "", errors.New("removed directory has too many copies")
}

// textRetirement also protects an untracked regular companion at the new target.
func (r *run) textRetirement(id string, en entry) ([]drawingRetirement, error) {
	old := r.j.Items[id]
	from := drawingTextPath(en.Output)
	before := en.Text && (!old.Text || old.Output != en.Output)
	retire := !en.Text && old.Text && old.Output == en.Output
	if before {
		info, err := r.root.Info(from)
		if err != nil {
			return nil, err
		}
		retire = info.Exists && info.Regular
	}
	if !retire {
		return nil, nil
	}
	to, err := r.drawingAside(from)
	return []drawingRetirement{{Owner: id, From: from, To: to, Before: before}}, err
}

func (r *run) planDrawing(id, out string) error {
	retired, err := r.drawingRetirements(id, out)
	if err == nil {
		text, textErr := r.textRetirement(id, entry{Output: out})
		err = textErr
		if text != nil {
			retired = append(retired, text...)
		}
	}
	for _, old := range retired {
		detail := ""
		if old.From == drawingTextPath(out) && r.j.Items[id].Output == out {
			detail = "if replacement has no recognition"
		}
		r.record(port.MirrorAction{Kind: port.MirrorRemove, Path: old.From, To: old.To, Document: old.Owner, Detail: detail})
	}
	return err
}

// cleanupDrawingStages only removes unreferenced regular files at the pinned root.
func (r *run) cleanupDrawingStages() error {
	entries, err := r.root.RootEntries()
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, d := range r.j.Drawings {
		keep[d.Stage] = true
		if d.TextStage != "" {
			keep[d.TextStage] = true
		}
	}
	var result error
	for _, en := range entries {
		if strings.HasPrefix(en.Path, ".snctl-drawing-") && !keep[en.Path] {
			result = errors.Join(result, r.root.Remove(en.Path))
		}
	}
	return result
}

// Drawing I/O failures are local to a drawing. Journal failures alone stop the pass.
func (r *run) writeDrawingJournal(ctx context.Context) error {
	err := r.j.write(ctx, r.root)
	if err != nil {
		r.failure = err
	}
	return err
}

func (r *run) publishDrawing(ctx context.Context, id string, en entry, in io.Reader, text ...string) error {
	retired, err := r.drawingRetirements(id, en.Output)
	if err != nil {
		return err
	}
	stage := ".snctl-drawing-" + rand.Text()
	if in != nil {
		if err = r.root.Produce(ctx, stage, func(w io.Writer) error { _, copyErr := io.Copy(w, in); return copyErr }); err != nil {
			return errors.Join(err, r.cleanupDrawingStages())
		}
	}
	d := &drawingChange{ID: id, Stage: stage, Entry: en, Retired: retired}
	if en.Text {
		if len(text) == 0 {
			return errors.Join(errors.New("recognized drawing needs text"), r.cleanupDrawingStages())
		}
		d.TextStage = stage + "-text"
		if err = r.root.Produce(ctx, d.TextStage, func(w io.Writer) error { _, writeErr := io.WriteString(w, text[0]); return writeErr }); err != nil {
			return errors.Join(err, r.cleanupDrawingStages())
		}
	}
	textRetired, err := r.textRetirement(id, en)
	if err != nil {
		return errors.Join(err, r.cleanupDrawingStages())
	}
	if textRetired != nil {
		d.Retired = append(d.Retired, textRetired...)
	}

	r.j.Drawings = append(r.j.Drawings, d)
	if err = r.writeDrawingJournal(ctx); err != nil {
		// No live path has changed, and no durable intent references the stage.
		r.j.Drawings = slices.Delete(r.j.Drawings, len(r.j.Drawings)-1, len(r.j.Drawings))
		return errors.Join(err, r.cleanupDrawingStages())
	}
	return r.completeDrawing(ctx, d)
}

func (r *run) completeDrawing(ctx context.Context, d *drawingChange) error {
	stage, err := r.root.Info(d.Stage)
	if err != nil {
		return err
	}
	if !stage.Exists {
		output, infoErr := r.root.Info(d.Entry.Output)
		if infoErr != nil {
			return infoErr
		}
		if !output.Exists || !output.Regular {
			return r.abandonDrawing(ctx, d)
		}
	}
	if err = r.publishDrawingPDF(ctx, d, stage.Exists); err != nil {
		return err
	}

	if err = r.publishDrawingText(ctx, d); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return r.abandonDrawing(ctx, d)
		}
		return errors.Join(err, r.restoreDrawingPaths(d))
	}
	if stage.Exists {
		if err = r.root.Remove(d.Stage); err != nil {
			return err
		}
	}

	if err = r.moveDrawingPaths(ctx, d, false); err != nil {
		return err
	}
	for _, old := range d.Retired {
		en := r.j.Items[old.Owner]
		en.Output, en.Drawn = "", ""
		en.Text, en.TextChecked = false, false
		r.j.Items[old.Owner] = en
	}
	en := r.j.Items[d.ID]
	en.Output, en.Drawn = d.Entry.Output, d.Entry.Drawn
	en.Text, en.TextChecked = d.Entry.Text, d.Entry.TextChecked
	if d.Entry.Path == "" && en.Path == "" {
		en.CloudPath, en.Record = d.Entry.CloudPath, d.Entry.Record
	}
	r.j.Items[d.ID] = en
	if d.TextStage != "" {
		if err = r.root.Remove(d.TextStage); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return r.dropDrawing(ctx, d)
}

// publishDrawingPDF also retires colliding Markdown before a text-only upgrade.
func (r *run) publishDrawingPDF(ctx context.Context, d *drawingChange, staged bool) error {
	var err error
	if err = r.root.MkdirAll(path.Dir(d.Entry.Output)); err != nil {
		return err
	}
	needsPublish := staged
	if d.TextStage != "" {
		text, infoErr := r.root.Info(d.TextStage)
		if infoErr != nil {
			return infoErr
		}
		needsPublish = needsPublish || text.Exists
	}
	if needsPublish {
		if err = r.moveDrawingPaths(ctx, d, true); err != nil {
			return errors.Join(err, r.restoreDrawingPaths(d))
		}
	}
	if staged {
		in, openErr := r.root.Open(d.Stage)
		if openErr != nil {
			return errors.Join(openErr, r.restoreDrawingPaths(d))
		}
		err = r.root.Produce(ctx, d.Entry.Output, func(w io.Writer) error { _, copyErr := io.Copy(w, in); return copyErr })
		err = errors.Join(err, in.Close())
		if err != nil {
			return errors.Join(err, r.restoreDrawingPaths(d))
		}
	}
	return nil
}

// dropDrawing retains the intent in memory too when its removal cannot be persisted.
func (r *run) dropDrawing(ctx context.Context, d *drawingChange) error {
	previous := slices.Clone(r.j.Drawings)
	r.j.Drawings = slices.DeleteFunc(r.j.Drawings, func(other *drawingChange) bool { return other == d })
	if err := r.writeDrawingJournal(ctx); err != nil {
		r.j.Drawings = previous
		return err
	}
	return nil
}

func (r *run) abandonDrawing(ctx context.Context, d *drawingChange) error {
	if err := r.restoreDrawingPaths(d); err != nil {
		return err
	}
	en := r.j.Items[d.ID]
	if en.Output == d.Entry.Output && en.Drawn == d.Entry.Drawn {
		en.Drawn, en.TextChecked = "", false
	}
	r.j.Items[d.ID] = en
	if err := r.dropDrawing(ctx, d); err != nil {
		return err
	}
	r.record(port.MirrorAction{Kind: port.MirrorWarning, Document: d.ID, Path: d.Entry.Output,
		Detail: "drawing stage or regular output is missing; discarded intent for redraw"})
	return nil
}

// Only colliding names must be retired before publication. Destination changes
// are persisted before moving, so recovery never guesses which bytes it owns.
func (r *run) moveDrawingPaths(ctx context.Context, d *drawingChange, before bool) error {
	for i := range d.Retired {
		old := &d.Retired[i]
		if old.Before != before {
			continue
		}
		retained, err := r.root.Info(old.To)
		if err != nil {
			return err
		}
		live, err := r.root.Info(old.From)
		if err != nil {
			return err
		}
		if retained.Exists && live.Exists {
			to, asideErr := r.drawingAside(old.From)
			if asideErr != nil {
				return asideErr
			}
			old.To = to
			if err = r.writeDrawingJournal(ctx); err != nil {
				return err
			}
		}
		if live.Exists {
			if err = r.root.Move(old.From, old.To); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *run) restoreDrawingPaths(d *drawingChange) error {
	var result error
	for _, old := range d.Retired {
		if !old.Before {
			continue
		}
		retained, err := r.root.Info(old.To)
		if err == nil && retained.Exists {
			err = r.root.Move(old.To, old.From)
		}
		if err != nil {
			result = errors.Join(result, fmt.Errorf("restoring drawing %s: %w", old.From, err))
		}
	}
	return result
}

// Pending intents reserve their owners and names while unrelated work continues.
func (r *run) drawingPending(id, out string) bool {
	for _, d := range r.j.Drawings {
		if d.ID == id || out != "" && foldName(d.Entry.Output) == foldName(out) {
			return true
		}
		for _, old := range d.Retired {
			if old.Owner == id || out != "" && foldName(old.From) == foldName(out) {
				return true
			}
		}
	}
	return false
}

func drawingTextPath(out string) string { return strings.TrimSuffix(out, path.Ext(out)) + ".md" }

func (r *run) publishDrawingText(ctx context.Context, d *drawingChange) error {
	if !d.Entry.Text {
		return nil
	}
	target := drawingTextPath(d.Entry.Output)
	stage, err := r.root.Info(d.TextStage)
	if err != nil {
		return err
	}
	if !stage.Exists {
		info, infoErr := r.root.Info(target)
		if infoErr != nil {
			return infoErr
		}
		if !info.Exists || !info.Regular {
			return fmt.Errorf("recognized drawing text stage and regular output are missing: %w", fs.ErrNotExist)
		}
		return nil
	}
	in, err := r.root.Open(d.TextStage)
	if err != nil {
		return err
	}
	err = r.root.Produce(ctx, target, func(w io.Writer) error { _, copyErr := io.Copy(w, in); return copyErr })
	return errors.Join(err, in.Close())
}

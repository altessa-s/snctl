//go:build cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package notes translates native notebook values into portable reading views.
package notes

import (
	"fmt"

	"github.com/altessa-s/snctl/internal/port"

	snnote "github.com/altessa-s/snnote-go"
)

// Reader owns no native state: Parse copies every value before returning.
type Reader struct{}

// Available reports that the native reader is compiled in.
func (Reader) Available() error { return nil }

// Parse translates inspection, text and highlight data without exposing native types.
func (Reader) Parse(data []byte) (port.Notebook, error) {
	n, err := snnote.Parse(data)
	if err != nil {
		return port.Notebook{}, fmt.Errorf("%w: %w", port.ErrNotebook, err)
	}
	out := port.Notebook{Kind: n.Kind.String(), Version: n.FormatVersion, Device: n.EquipmentRaw,
		Links: len(n.Links), Keywords: len(n.Keywords), SkippedPages: n.SkippedPages,
		Pages: []port.NotePage{}, Titles: []port.NoteTitle{}, Highlights: []port.NoteHighlight{},
		Header: metadata(n.Header), Footer: metadata(n.Footer)}
	for _, p := range n.Pages {
		v := port.NotePage{Number: int(p.Number), Width: p.Size.Width, Height: p.Size.Height,
			Orientation: p.Orientation.String(), Template: value(p.TemplateName), Strokes: len(p.Strokes),
			SkippedStrokes: p.SkippedStrokes, Layers: []string{}, Text: []string{}}
		for _, l := range p.Layers {
			v.Layers = append(v.Layers, l.Name.Key())
		}
		for _, t := range p.TextBoxes {
			v.Text = append(v.Text, t.Text)
		}
		if p.Recognition != nil {
			v.Recognition = p.Recognition.Text
		}
		out.Pages = append(out.Pages, v)
	}
	for _, t := range n.Titles {
		// Titles are normally bitmap regions. A missing textual label must not be invented as handwriting.
		label, _ := t.Metadata.Get("TITLE")
		out.Titles = append(out.Titles, port.NoteTitle{Page: int(t.PageNumber), Level: int(t.Level), Text: label})
	}
	for _, h := range n.Highlights {
		out.Highlights = append(out.Highlights, port.NoteHighlight{Page: int(h.PageNumber), Color: h.ColorType,
			Comment: h.Comment, Rects: rects(h.Rects), DocumentRects: rects(h.DocumentRects)})
	}
	return out, nil
}
func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func metadata(m snnote.Metadata) []port.MetadataEntry {
	out := make([]port.MetadataEntry, len(m.Entries))
	for i, e := range m.Entries {
		out[i] = port.MetadataEntry{Key: e.Key, Value: e.Value}
	}
	return out
}
func rects(rs []*snnote.Rect) []*port.Rect {
	out := make([]*port.Rect, len(rs))
	for i, r := range rs {
		if r != nil {
			out[i] = &port.Rect{X: r.X, Y: r.Y, Width: r.Width, Height: r.Height}
		}
	}
	return out
}

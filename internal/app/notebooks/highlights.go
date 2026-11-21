// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package notebooks

import (
	"context"
	"fmt"
	"slices"

	"github.com/altessa-s/snctl/internal/port"
)

func (s Service) highlightText(ctx context.Context, query string, n *port.Notebook, progress port.Progress) error {
	if n.Kind != kindMark || len(n.Highlights) == 0 {
		return nil
	}
	// Extraction enriches a copy; a reader may retain its parsed model.
	n.Highlights = slices.Clone(n.Highlights)
	for i := range n.Highlights {
		n.Highlights[i].Text = nil
	}
	if err := s.Renderer.Available(); err != nil {
		n.Warnings = append(n.Warnings, "highlight text unavailable: "+err.Error())
		return nil
	}
	pdf, warnings, err := s.sourcePDF(ctx, query, n.Name, port.RenderOptions{DownloadProgress: progress}, false)
	n.Warnings = append(n.Warnings, warnings...)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		n.Warnings = append(n.Warnings, "highlight text unavailable: "+err.Error())
		return nil
	}
	if len(pdf) == 0 {
		n.Warnings = append(n.Warnings, "highlight text unavailable: no unambiguous source PDF (EPUB text extraction is unsupported)")
		return nil
	}
	// snrender-go v1.0.0 reparses the PDF per PageText call. It exposes neither
	// a reusable document nor separate results for multiple rectangle groups;
	// combining highlights on a page would lose their individual passages.
	// Keep calls sequential and fetch the shared PDF only once.
	for i := range n.Highlights {
		h := &n.Highlights[i]
		text, err := s.Renderer.PageText(ctx, pdf, h.Page, h.DocumentRects)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			n.Warnings = append(n.Warnings, fmt.Sprintf("highlight text unavailable on page %d: %s", h.Page, err))
			continue
		}
		h.Text = &text
	}
	return nil
}

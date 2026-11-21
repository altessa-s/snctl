// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/altessa-s/snctl/internal/domain"
)

// RecognitionIndex stores no text, only recognition presence by content MD5.
// A nil lookup is unknown; a missing or damaged index must never imply false.
type RecognitionIndex interface {
	Recognition(string) *bool
	RememberRecognition(context.Context, string, bool) error
}

// RecognitionBatchIndex supports a single snapshot and merge-write per scan.
// Implementations must merge with the latest disk state under the process lock.
type RecognitionBatchIndex interface {
	RecognitionIndex
	RecognitionSnapshot() map[string]bool
	RememberRecognitions(context.Context, map[string]bool) error
}

// WithRecognitionIndex gives body observers the same command-local batch as readers.
func WithRecognitionIndex(ctx context.Context, index RecognitionIndex) context.Context {
	return context.WithValue(ctx, recognitionIndexKey{}, index)
}

type recognitionIndexKey struct{}

// ContextRecognitionIndex selects a scan's batch, falling back to the normal index.
func ContextRecognitionIndex(ctx context.Context, fallback RecognitionIndex) RecognitionIndex {
	if index, ok := ctx.Value(recognitionIndexKey{}).(RecognitionIndex); ok {
		return index
	}
	return fallback
}

// HasRecognition requires nonblank cleaned recognition on at least one page.
// Typed text and header recognition flags alone do not qualify.
func (n Notebook) HasRecognition() bool {
	for _, p := range n.Pages {
		if strings.TrimSpace(domain.CleanText(p.Recognition)) != "" {
			return true
		}
	}
	return false
}

// Clean returns a copy with cleaned output text, preserving parser-owned data.
func (n Notebook) Clean() Notebook {
	n.Pages = slices.Clone(n.Pages)
	for i := range n.Pages {
		p := &n.Pages[i]
		if p.Text != nil {
			p.Text = append([]string{}, p.Text...)
		}
		for j := range p.Text {
			p.Text[j] = domain.CleanText(p.Text[j])
		}
		p.Recognition = domain.CleanText(p.Recognition)
	}
	n.Titles = slices.Clone(n.Titles)
	for i := range n.Titles {
		n.Titles[i].Text = domain.CleanText(n.Titles[i].Text)
	}
	return n
}

// Text returns typed boxes and recognition with optional Markdown page/title headings.
func (n Notebook) Text(markdown bool) string {
	var b strings.Builder
	if n.Mark != "" {
		fmt.Fprintf(&b, "%s (annotations: %s)\n\n", n.Name, n.Mark)
	}
	for _, p := range n.Pages {
		if markdown {
			fmt.Fprintf(&b, "## Page %d\n\n", p.Number)
		} else if len(n.Pages) > 1 {
			fmt.Fprintf(&b, "Page %d\n", p.Number)
		}
		if markdown {
			for _, t := range n.Titles {
				if t.Page != p.Number {
					continue
				}
				label := t.Text
				if label == "" {
					label = "Title"
				}
				const maxHeading = 6
				fmt.Fprintf(&b, "%s %s\n\n", strings.Repeat("#", min(max(t.Level, 1), maxHeading)), label)
			}
		}
		for _, text := range p.Text {
			fmt.Fprintln(&b, text)
		}
		if p.Recognition != "" {
			fmt.Fprintln(&b, p.Recognition)
		}
	}
	return b.String()
}

// TextMatch reports stored pages containing query words in a matching notebook.
type TextMatch struct {
	Entry Entry `json:"entry"`
	Pages []int `json:"pages"`
}

// TextOptions controls local export. Encode defines the requested content format.
type TextOptions struct {
	Extension string
	Pages     []int
	Raw       bool
	Progress  Progress
	Encode    func(Notebook) ([]byte, error)
}

// NotebookText extends reading with recognition discovery, search and atomic exports.
type NotebookText interface {
	Recognition(context.Context, []Entry, bool, Progress) ([]Entry, error)
	FindText(context.Context, string, string, Progress, bool) ([]TextMatch, error)
	ExportText(context.Context, string, string, TextOptions) ([]string, error)
}

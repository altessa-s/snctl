// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package port

import (
	"github.com/altessa-s/snctl/internal/domain"

	"golang.org/x/text/unicode/norm"
)

// SidecarCounts distinguishes path collisions from usable annotation files.
// Documents and Marks include folders, preserving conflict checks before writes;
// Files counts only non-folder matches at the sidecar path.
type SidecarCounts struct {
	Documents, Marks, Files int
}

// SidecarOf finds NFC-equal docPath + ".mark" matches in a sibling listing.
// It returns the last non-folder match and the counts needed to detect ambiguity.
// Paths are case-sensitive, and the input is never changed.
func SidecarOf(siblings []Entry, docPath string) (mark Entry, counts SidecarCounts) {
	docPath = norm.NFC.String(docPath)
	for _, sibling := range siblings {
		switch norm.NFC.String(sibling.Path) {
		case docPath:
			counts.Documents++
		case docPath + ".mark":
			counts.Marks++
			if !sibling.Folder {
				counts.Files++
				mark = sibling
			}
		}
	}
	return mark, counts
}

// FoldMarks decorates documents from a complete listing of siblings. Raw keeps
// sidecars as separate entries too. Paths compare in NFC: case and folder matter.
// The input is never changed, and ambiguous duplicate paths remain visible.
func FoldMarks(entries []Entry, raw bool) []Entry {
	byPath := make(map[string][]Entry, len(entries))
	for _, e := range entries {
		if e.Path != "" {
			key := norm.NFC.String(e.Path)
			byPath[key] = append(byPath[key], e)
		}
	}
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		key := norm.NFC.String(e.Path)
		owner := norm.NFC.String(domain.MarkOwner(e.Path))
		if !raw && !e.Folder && len(byPath[owner]) == 1 && !byPath[owner][0].Folder && len(byPath[key]) == 1 {
			continue
		}
		if !e.Folder && domain.DocumentName(e.Path) && len(byPath[key]) == 1 && len(byPath[key+".mark"]) == 1 && !byPath[key+".mark"][0].Folder {
			mark := byPath[key+".mark"][0]
			e.Mark = &mark
		}
		out = append(out, e)
	}
	return out
}

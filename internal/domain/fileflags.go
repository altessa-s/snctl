// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package domain

import (
	"strings"
)

// FileFlag defines a listing flag's stable name, letter and legend text.
type FileFlag struct {
	Name, Letter, Meaning string
}

// FileFlags returns the complete flag vocabulary in display and JSON order.
func FileFlags() []FileFlag {
	return []FileFlag{{"recognition", "R", "recognized handwriting"}, {"highlights", "H", "highlights"}}
}

// DocumentName reports whether a filename can own a full-name .mark sidecar.
func DocumentName(name string) bool {
	return MirrorUploadable(name)
}

// MarkOwner returns the document name extended by a .mark, preserving spelling.
// Stem-only legacy sidecars remain independent entries.
func MarkOwner(name string) string {
	if !strings.HasSuffix(name, ".mark") {
		return ""
	}
	owner := strings.TrimSuffix(name, ".mark")
	if DocumentName(owner) {
		return owner
	}
	return ""
}

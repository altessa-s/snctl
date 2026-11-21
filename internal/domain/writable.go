// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import "strings"

// Writable reports whether snctl creates an entry at the tablet path name.
// NOTE and DOCUMENT are virtual categories: an entry written directly below
// one of them, or below a subfolder other than NOTE/Note, NOTE/MyStyle and
// DOCUMENT/Document, can appear twice on the tablet, so writes there are
// refused. Below other top-level folders the cloud's own rules apply, and
// nothing is created at the top level itself.
func Writable(name string) bool {
	parts := strings.Split(strings.Trim(name, "/"), "/")
	const below = 3
	switch parts[0] {
	case "NOTE":
		return len(parts) >= below && (parts[1] == "Note" || parts[1] == "MyStyle")
	case "DOCUMENT":
		return len(parts) >= below && parts[1] == "Document"
	case "":
		return false
	}
	return len(parts) > 1
}

// UploadSkip is the reason mput leaves a local file out, or "" when it goes
// up. Neither the cloud protocol nor sncloud-go has a type rule; mput carries
// the files the tablet opens from its folders: notebooks (.note), annotation
// sidecars (.mark), PDFs and EPUBs. Hidden files, which include snctl's own
// records, and empty files are never sent. put sends any file a person names,
// because naming it is the decision.
func UploadSkip(name string, size int64) string {
	base := name
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		base = name[i+1:]
	}
	switch {
	case strings.HasPrefix(base, "."):
		return "hidden file"
	case size == 0:
		return "empty file"
	}
	if dot := strings.LastIndex(base, "."); dot > 0 {
		switch strings.ToLower(base[dot:]) {
		case ".note", ".mark", ".pdf", ".epub":
			return ""
		}
	}
	return "not a .note, .mark, .pdf or .epub file"
}

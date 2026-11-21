// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import "context"

// DigestKind selects a digest collection.
type DigestKind string

// The digest collections have separate identifiers on the service.
const (
	DigestGroups DigestKind = "groups"
	DigestItems  DigestKind = "digests"
	DigestTags   DigestKind = "tags"
)

// DigestRecord is the editable public view of a digest, group or tag.
type DigestRecord struct {
	ID         string   `json:"id"`
	Identifier string   `json:"identifier"`
	Name       string   `json:"name"`
	Content    *string  `json:"content"`
	Comment    *string  `json:"comment"`
	Group      *string  `json:"group"`
	Tags       []string `json:"tags"`
}

// DigestEdit changes only explicitly supplied fields.
type DigestEdit struct {
	Name                    string
	Content, Comment, Group *string
	Tags                    *[]string
}

// Digests manages digest collections without exposing protocol types.
type Digests interface {
	DigestList(context.Context, DigestKind) ([]DigestRecord, error)
	DigestAdd(context.Context, DigestKind, DigestEdit) (string, error)
	DigestEdit(context.Context, DigestKind, string, DigestEdit) error
	DigestRemove(context.Context, DigestKind, string) error
	HandwritingGet(context.Context, string, Progress) (string, error)
	HandwritingPut(context.Context, string, string, Progress) error
}

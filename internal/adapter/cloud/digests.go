// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"context"
	"strconv"

	"github.com/altessa-s/snctl/internal/port"

	sncloud "github.com/altessa-s/sncloud-go"
)

// DigestList lists one digest collection.
func (c *Client) DigestList(ctx context.Context, kind port.DigestKind) ([]port.DigestRecord, error) {
	out := []port.DigestRecord{}
	switch kind {
	case port.DigestGroups:
		rows, err := c.digests.Groups(ctx)
		for _, r := range rows {
			out = append(out, port.DigestRecord{ID: r.ID.String(), Identifier: r.UniqueIdentifier, Name: r.Name, Comment: r.Description})
		}
		return out, c.failure("digestlist", "", err)
	case port.DigestTags:
		rows, err := c.digests.Tags(ctx)
		for _, r := range rows {
			out = append(out, port.DigestRecord{ID: r.ID.String(), Name: r.Name})
		}
		return out, c.failure("digestlist", "", err)
	case port.DigestItems:
		rows, err := c.digests.List(ctx, "")
		for _, r := range rows {
			out = append(out, port.DigestRecord{ID: r.ID.String(), Content: r.Content, Comment: r.Comment, Group: r.GroupIdentifier, Tags: r.Tags})
		}
		return out, c.failure("digestlist", "", err)
	default:
		return nil, port.ErrNotFound
	}
}

func digestDraft(e port.DigestEdit) sncloud.DigestDraft {
	return sncloud.DigestDraft{Content: e.Content, Comment: e.Comment, GroupIdentifier: e.Group, Tags: e.Tags}
}

// DigestAdd creates a digest, group or tag.
func (c *Client) DigestAdd(ctx context.Context, kind port.DigestKind, e port.DigestEdit) (string, error) {
	var id sncloud.DigestID
	var err error
	switch kind {
	case port.DigestGroups:
		id, err = c.digests.CreateGroup(ctx, e.Name, e.Comment, "")
	case port.DigestTags:
		id, err = c.digests.CreateTag(ctx, e.Name)
	case port.DigestItems:
		id, err = c.digests.Create(ctx, digestDraft(e))
	default:
		return "", port.ErrNotFound
	}
	return id.String(), c.failure("digestadd", "", err)
}

func digestID(value string) (sncloud.DigestID, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, port.ErrNotFound
	}
	return sncloud.DigestID(id), nil
}

// DigestEdit changes explicitly provided digest fields.
func (c *Client) DigestEdit(ctx context.Context, kind port.DigestKind, value string, e port.DigestEdit) error {
	id, err := digestID(value)
	if err != nil {
		return err
	}
	switch kind {
	case port.DigestGroups:
		return c.failure("digestedit", "", c.digests.UpdateGroup(ctx, id, e.Name, e.Comment))
	case port.DigestTags:
		return c.failure("digestedit", "", c.digests.RenameTag(ctx, id, e.Name))
	case port.DigestItems:
		return c.failure("digestedit", "", c.digests.Update(ctx, id, digestDraft(e)))
	default:
		return port.ErrNotFound
	}
}

// DigestRemove removes only the named identifier.
func (c *Client) DigestRemove(ctx context.Context, kind port.DigestKind, value string) error {
	id, err := digestID(value)
	if err != nil {
		return err
	}
	switch kind {
	case port.DigestGroups:
		return c.failure("digestremove", "", c.digests.DeleteGroup(ctx, id))
	case port.DigestTags:
		return c.failure("digestremove", "", c.digests.DeleteTag(ctx, id))
	case port.DigestItems:
		return c.failure("digestremove", "", c.digests.Delete(ctx, id))
	default:
		return port.ErrNotFound
	}
}

// HandwritingGet downloads the named digest's handwritten comment.
func (c *Client) HandwritingGet(ctx context.Context, value string, p port.Progress) (string, error) {
	id, err := digestID(value)
	if err != nil {
		return "", err
	}
	rows, err := c.digests.ByID(ctx, []sncloud.DigestID{id})
	if err != nil {
		return "", c.failure("handwritingget", "", err)
	}
	if len(rows) != 1 {
		return "", port.ErrNotFound
	}
	file, err := c.digests.DownloadHandwriting(ctx, rows[0], sncloud.Progress(p))
	if err == nil && file == "" {
		return "", port.ErrNotFound
	}
	return file, c.failure("handwritingget", "", err)
}

// HandwritingPut uploads and attaches a handwritten comment to a digest.
func (c *Client) HandwritingPut(ctx context.Context, value, file string, p port.Progress) error {
	id, err := digestID(value)
	if err != nil {
		return err
	}
	rows, err := c.digests.ByID(ctx, []sncloud.DigestID{id})
	if err != nil {
		return c.failure("handwritingput", "", err)
	}
	if len(rows) != 1 {
		return port.ErrNotFound
	}
	ref, err := c.digests.UploadHandwriting(ctx, file, "", sncloud.Progress(p))
	if err != nil {
		return c.failure("handwritingput", "", err)
	}
	return c.failure("handwritingput", "", c.digests.Update(ctx, id, sncloud.DigestDraft{Handwriting: &ref}))
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package cloud

import (
	"context"
	"fmt"
	"path"

	"github.com/altessa-s/snctl/internal/port"
)

// RestoreTarget maps the original parent ID to its current tablet path. Bin
// entries have no paths; name-based guesses could restore into another folder.
func (c *Client) RestoreTarget(ctx context.Context, recycled port.Entry) (port.Entry, error) {
	if c.offline {
		return port.Entry{}, fmt.Errorf("offline: cloud writes require a live listing")
	}
	id, err := fileID(recycled.ID)
	if err != nil {
		return port.Entry{}, err
	}
	location, err := c.web.Location(ctx, id)
	if err != nil {
		return port.Entry{}, c.failure("restore location", recycled.Name, err)
	}
	if len(location.IDs) == 0 || recycled.Name == "" || path.Base(recycled.Name) != recycled.Name || recycled.Name == "." || recycled.Name == ".." {
		return port.Entry{}, fmt.Errorf("%w: cannot establish original parent/name for recycled ID %s", port.ErrConflict, recycled.ID)
	}
	// The service ends the location of a recycled entry with the entry itself;
	// its parent is the element before it.
	if location.IDs[len(location.IDs)-1] == id {
		if len(location.Names) != len(location.IDs) || location.Names[len(location.Names)-1] != recycled.Name {
			return port.Entry{}, fmt.Errorf("%w: location of recycled ID %s does not end with its name", port.ErrConflict, recycled.ID)
		}
		location.IDs = location.IDs[:len(location.IDs)-1]
	}
	if len(location.IDs) == 0 {
		return port.Entry{}, fmt.Errorf("%w: cannot establish original parent for recycled ID %s", port.ErrConflict, recycled.ID)
	}
	entries, err := c.tree(ctx)
	if err != nil {
		return port.Entry{}, err
	}
	parentID := location.IDs[len(location.IDs)-1]
	var parents []port.Entry
	for _, e := range entries {
		if e.ID == parentID && e.IsFolder {
			parents = append(parents, deviceEntry(e))
		}
	}
	if len(parents) != 1 {
		return port.Entry{}, fmt.Errorf("%w: original parent ID %s unavailable for recycled ID %s", port.ErrConflict, parentID, recycled.ID)
	}
	recycled.ParentID = parentID.String()
	recycled.Path = path.Join(parents[0].Path, recycled.Name)
	return recycled, nil
}

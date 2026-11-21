// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"context"
	"iter"

	"github.com/altessa-s/snctl/internal/port"

	sncloud "github.com/altessa-s/sncloud-go"
)

// WebList checks the independent web listing view.
func (c *Client) WebList(ctx context.Context) error {
	_, err := c.web.List(ctx, sncloud.RootFileID, sncloud.SortName, true)
	return c.failure("weblist", "", err)
}

// Watch expands batches into individual announcements and drops device identity.
func (c *Client) Watch(ctx context.Context) iter.Seq2[port.Change, error] {
	return func(yield func(port.Change, error) bool) {
		for change, err := range c.feed.Watch(ctx) {
			if err != nil {
				yield(port.Change{}, c.failure("watch", "", err))
				return
			}
			for _, event := range changes(change) {
				if !yield(event, nil) {
					return
				}
			}
		}
	}
}

func changes(change sncloud.Change) []port.Change {
	events := []port.Change{}
	kind := string(change.Type)
	if change.Type == sncloud.ChangeConnected {
		return []port.Change{{Type: kind}}
	}
	for _, r := range change.Files {
		e := port.Change{Type: kind, Kind: string(r.Kind)}
		if r.ID != nil {
			e.ID = r.ID.String()
		}
		if r.Path != nil {
			e.Path = *r.Path
		}
		events = append(events, e)
	}
	for _, r := range change.Tasks {
		e := port.Change{Type: kind, Kind: string(r.Kind)}
		if r.TaskID != nil {
			e.ID = *r.TaskID
		}
		events = append(events, e)
	}
	for _, r := range change.Digests {
		e := port.Change{Type: kind, Kind: string(r.Kind)}
		if r.ID != nil {
			e.ID = r.ID.String()
		}
		events = append(events, e)
	}
	return events
}

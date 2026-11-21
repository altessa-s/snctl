// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package files

import (
	"context"
	"errors"
	"path"
	"slices"
	"strings"

	"github.com/altessa-s/snctl/internal/port"
)

type writeBatchKey struct{}

// writeBatch switches from the planning view to a live view at the first
// re-check. Subsequent plans and writes share that view, including our changes.
// Destructive writes additionally check live affected parents for each operation.
// It belongs to one sequential mirror pass, never to concurrent commands.
type writeBatch struct {
	port.Files
	source  port.Files
	fresh   port.FileFreshSnapshot
	checked bool
	written []port.Entry
}

// WithWriteBatch bounds fresh listings across a mirror's sequential writes.
// Verification runs once after the callback, outside its sync sessions.
func (s Service) WithWriteBatch(ctx context.Context, body func(context.Context) error) error {
	fresh, ok := s.Cloud.(port.FileFreshSnapshot)
	if !ok {
		return body(ctx)
	}
	cloud, err := s.view(ctx)
	if err != nil {
		return err
	}
	b := &writeBatch{Files: cloud, source: s.Cloud, fresh: fresh}
	err = body(context.WithValue(ctx, writeBatchKey{}, b))
	return errors.Join(err, verifyWrites(ctx, b.source, b.written, nil, nil))
}

func recheckView(ctx context.Context, cloud port.Files) (port.Files, bool, error) {
	if b, ok := cloud.(*writeBatch); ok {
		if !b.checked {
			view, err := b.fresh.FreshSnapshot(ctx)
			if err != nil {
				return nil, false, err
			}
			b.Files = view
			b.checked = true
		}
		return b.Files, false, nil
	}
	if snapshots, ok := cloud.(port.FileFreshSnapshot); ok {
		view, err := snapshots.FreshSnapshot(ctx)
		return view, false, err
	}
	return cloud, true, nil
}

func (b *writeBatch) Delete(ctx context.Context, e port.Entry, permanent bool) error {
	if err := b.Files.Delete(ctx, e, permanent); err != nil {
		return err
	}
	b.written = slices.DeleteFunc(b.written, func(w port.Entry) bool {
		return w.ID == e.ID || e.Folder && strings.HasPrefix(w.Path, e.Path+"/")
	})
	return nil
}

func (b *writeBatch) Move(ctx context.Context, e port.Entry, destination string) error {
	if err := b.Files.Move(ctx, e, destination); err != nil {
		return err
	}
	for i := range b.written {
		w := &b.written[i]
		if w.ID == e.ID || e.Folder && strings.HasPrefix(w.Path, e.Path+"/") {
			w.Path = destination + strings.TrimPrefix(w.Path, e.Path)
			w.Name = path.Base(w.Path)
		}
	}
	return nil
}

func (b *writeBatch) Refresh(ctx context.Context, name string) (port.Entry, error) {
	if live, ok := b.Files.(port.FileRefresh); ok {
		return live.Refresh(ctx, name)
	}
	return b.Stat(ctx, name)
}

func (b *writeBatch) RetainUpload(ctx context.Context, e port.Entry, file string) {
	retainUpload(ctx, b.Files, e, file)
}

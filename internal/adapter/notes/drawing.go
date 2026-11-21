//go:build cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package notes

import (
	"fmt"

	"github.com/altessa-s/snctl/internal/port"

	snspd "github.com/altessa-s/snspd-go"
)

// ParseDrawing copies canvas geometry and layer counts from an Atelier snapshot.
func (Reader) ParseDrawing(data []byte) (port.Notebook, error) {
	d, err := snspd.Open(data, nil)
	if err != nil {
		return port.Notebook{}, fmt.Errorf("%w: %w", port.ErrNotebook, err)
	}
	info := &port.DrawingInfo{FormatVersion: d.Canvas.FormatVersion, Layers: make([]port.DrawingLayer, len(d.Layers))}
	if d.Canvas.HasWidth {
		info.Width = &d.Canvas.Width
	}
	if d.Canvas.HasHeight {
		info.Height = &d.Canvas.Height
	}
	for i, l := range d.Layers {
		info.Layers[i] = port.DrawingLayer{ID: l.ID, Name: l.Name, Tiles: len(l.Tiles)}
		info.Tiles += len(l.Tiles)
	}
	return port.Notebook{Kind: "drawing", Drawing: info}, nil
}

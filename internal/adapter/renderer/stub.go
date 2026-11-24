//go:build norender || !((darwin || linux || windows) && cgo)

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package renderer owns native rendering and its handle lifetime.
package renderer

import (
	"context"
	"io"

	"github.com/altessa-s/snctl/internal/port"
)

// LibraryVersion identifies the linked library for mirror rejection records.
const LibraryVersion = "unavailable"

// Renderer keeps rendering commands present in portable builds.
type Renderer struct{}

// Available explains why drawing is unavailable.
func (Renderer) Available() error { return port.ErrRenderUnavailable }

// Render refuses input without producing partial output.
func (Renderer) Render(context.Context, []byte, []byte, port.RenderOptions, io.Writer) (int, error) {
	return 0, port.ErrRenderUnavailable
}

// Limits is empty because this build cannot draw.
func (Renderer) Limits() port.RenderLimits { return port.RenderLimits{} }

// RenderPages refuses directory exports when rendering is unavailable.
func (Renderer) RenderPages(context.Context, []byte, port.RenderOptions, func(int, func(io.Writer) error) error) error {
	return port.ErrRenderUnavailable
}

// PageText explains why source PDF extraction is unavailable.
func (Renderer) PageText(context.Context, []byte, int, []*port.Rect) (string, error) {
	return "", port.ErrRenderUnavailable
}

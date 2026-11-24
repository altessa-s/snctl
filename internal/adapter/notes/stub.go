//go:build !cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package notes owns native notebook parsing and copies portable views.
package notes

import "github.com/altessa-s/snctl/internal/port"

// LibraryVersion identifies the linked library for mirror rejection records.
const LibraryVersion = "unavailable"

// Reader preserves command availability when the native parser is absent.
type Reader struct{}

// Available explains how to enable reading.
func (Reader) Available() error { return port.ErrNotebookUnavailable }

// Parse refuses input without attempting to read it.
func (Reader) Parse([]byte) (port.Notebook, error) {
	return port.Notebook{}, port.ErrNotebookUnavailable
}

// ParseDrawing explains why drawing inspection is unavailable.
func (Reader) ParseDrawing([]byte) (port.Notebook, error) {
	return port.Notebook{}, port.ErrNotebookUnavailable
}

//go:build norender || !((darwin || linux || windows) && cgo)

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package renderer_test

import (
	"errors"
	"io"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/renderer"
	"github.com/altessa-s/snctl/internal/port"
)

func TestUnavailable(t *testing.T) {
	r := renderer.Renderer{}
	if !errors.Is(r.Available(), port.ErrRenderUnavailable) {
		t.Fatal(r.Available())
	}
	_, e := r.Render(t.Context(), nil, nil, port.RenderOptions{}, io.Discard)
	if !errors.Is(e, port.ErrRenderUnavailable) {
		t.Fatal(e)
	}
	_, e = r.PageText(t.Context(), nil, 1, nil)
	if !errors.Is(e, port.ErrRenderUnavailable) {
		t.Fatal(e)
	}

}

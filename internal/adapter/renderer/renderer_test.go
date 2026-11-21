//go:build (darwin || linux || windows) && cgo && !norender

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package renderer_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/renderer"
	"github.com/altessa-s/snctl/internal/port"
)

type badWriter struct{}

func (badWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestFormats(t *testing.T) {
	for _, name := range []string{"blank.note", "highlights.mark"} {
		t.Run(name, func(t *testing.T) { testFormats(t, name) })
	}
}
func testFormats(t *testing.T, name string) {
	data, err := os.ReadFile("../../../testdata/notebooks/" + name)
	if err != nil {
		t.Fatal(err)
	}
	r := renderer.Renderer{}
	if r.Available() != nil {
		t.Fatal(r.Available())
	}
	for _, format := range []string{"pdf", "png", "svg", "jpg"} {
		opts := port.RenderOptions{Format: format, Scale: .1, Progress: func(int64, int64) {}}
		if format != "pdf" {
			opts.Page = 1
		}
		var out bytes.Buffer
		count, err := r.Render(t.Context(), data, nil, opts, &out)
		if err != nil || count < 1 || out.Len() < 20 {
			t.Fatalf("%s %d %v", format, out.Len(), err)
		}
		if _, err = r.Render(t.Context(), data, nil, opts, badWriter{}); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatal(err)
		}
	}
	for _, opts := range []port.RenderOptions{{Format: "bad", Page: 1}, {Format: "png"}, {Format: "png", Page: 99}, {Format: "png", Page: 1, Scale: 1000000}, {Format: "jpg", Page: 1, Scale: 1000000}} {
		if _, err = r.Render(t.Context(), data, nil, opts, io.Discard); err == nil {
			t.Fatalf("%+v", opts)
		}
	}
	if _, err = r.Render(t.Context(), nil, nil, port.RenderOptions{}, io.Discard); err == nil {
		t.Fatal("empty accepted")
	}
	if _, err = r.Render(t.Context(), data, []byte("source"), port.RenderOptions{}, io.Discard); err == nil {
		t.Fatal("source ignored")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = r.Render(ctx, data, nil, port.RenderOptions{}, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestDirectoryLifetime(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/notebooks/highlights.mark")
	if err != nil {
		t.Fatal(err)
	}
	r := renderer.Renderer{}
	l := r.Limits()
	if l.MinimumScale <= 0 || l.MaximumScale < l.MinimumScale || l.MaximumPixels <= 0 {
		t.Fatal(l)
	}
	count := 0
	err = r.RenderPages(t.Context(), data, port.RenderOptions{Format: "png", Scale: .1, Progress: func(done, total int64) {
		if done > total {
			t.Error(done, total)
		}
	}}, func(i int, draw func(io.Writer) error) error {
		if i != count {
			t.Error(i, count)
		}
		count++
		var out bytes.Buffer
		if err := draw(&out); err != nil {
			return err
		}
		if !bytes.HasPrefix(out.Bytes(), []byte("\x89PNG")) {
			t.Fatal("not PNG")
		}
		return nil
	})
	if err != nil || count == 0 {
		t.Fatal(count, err)
	}
	if err = r.RenderPages(t.Context(), nil, port.RenderOptions{}, func(int, func(io.Writer) error) error { return nil }); err == nil {
		t.Fatal("invalid bytes accepted")
	}
	if err = r.RenderPages(t.Context(), data, port.RenderOptions{Format: "png"}, func(int, func(io.Writer) error) error { return io.ErrClosedPipe }); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = r.RenderPages(ctx, data, port.RenderOptions{}, func(int, func(io.Writer) error) error { t.Error("cancelled output"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

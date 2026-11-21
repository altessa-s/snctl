// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package notebooks

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/altessa-s/snctl/internal/port"
)

func (s Service) openDocument(ctx context.Context, query string, o port.RenderOptions) (port.RenderReport, error) {
	name, data, err := s.load(ctx, query, o.DownloadProgress)
	if err != nil {
		return port.RenderReport{}, err
	}
	dir, err := s.Files.PreviewDir()
	if err != nil {
		return port.RenderReport{}, err
	}
	notice := "has no annotations; opening the original document"
	if o.NoMarks {
		notice = "annotations skipped (--no-marks); opening the original document"
	}
	report := port.RenderReport{Path: filepath.Join(dir, safeName(name)), Warnings: []string{fmt.Sprintf("%s: %s", query, notice)}}
	err = s.Files.Produce(ctx, report.Path, func(w io.Writer) error {
		_, writeErr := w.Write(data)
		return writeErr
	})
	if err != nil {
		_ = s.Files.Remove(dir)
		return report, err
	}
	return report, s.openPreview(ctx, dir, report.Path)
}

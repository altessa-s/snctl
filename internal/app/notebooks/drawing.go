// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package notebooks

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/altessa-s/snctl/internal/port"
)

func (s Service) renderDrawingData(ctx context.Context, name string, data []byte, dest string, o port.RenderOptions) (port.RenderReport, error) {
	if o.Page != 0 || o.Source != "" || o.NoText || o.Quality != 0 || o.Format == formatJPEG ||
		o.DrawingPage != "" && o.DrawingPage != "painted" && o.DrawingPage != "canvas" {
		return port.RenderReport{}, fmt.Errorf(
			"%w: .spd requires PNG, PDF or SVG; use --drawing-page painted|canvas, not notebook page/source/text or JPEG options",
			port.ErrOptions)
	}
	if err := validateScale(s.Renderer.Limits(), port.Notebook{}, o); err != nil {
		return port.RenderReport{}, err
	}
	name = safeName(strings.TrimSuffix(name, filepath.Ext(name))) + "-rendered." + o.Format
	report := port.RenderReport{}
	var err error
	if dest == "-" {
		if o.Writer == nil {
			return report, port.ErrOptions
		}
		report.Path = dest
		report.Pages, err = s.Renderer.Render(ctx, data, nil, o, o.Writer)
		return report, err
	}
	report.Path, err = s.Files.Destination(dest, name)
	if err != nil {
		return report, err
	}
	report.Pages, err = s.draw(ctx, data, nil, report.Path, o)
	return report, err
}

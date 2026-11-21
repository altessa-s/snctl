// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package files

import (
	"context"
	"fmt"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
)

// RestoreFiles plans all verified backup bodies before confirming or taking the
// sync lock, then uses the same upload and post-lock verification as put.
func (s Service) RestoreFiles(ctx context.Context, files []port.RestoreFile, o port.UploadOptions) (port.TransferReport, error) {
	report := port.TransferReport{Items: []port.TransferItem{}, DryRun: o.DryRun}
	if o.Jobs < 0 {
		return report, fmt.Errorf("%w: --jobs must not be negative", port.ErrArgument)
	}
	for _, f := range files {
		if !domain.Writable(clean(f.Path)) {
			return report, fmt.Errorf("%w: %s: %s", port.ErrArgument, f.Path, notWritable)
		}
	}
	cloud, err := s.view(ctx)
	if err != nil {
		return report, err
	}
	o.Incremental = true
	p := planner{s: s, ctx: ctx, cloud: cloud, known: map[string]bool{}, opts: o, create: true}
	for _, f := range files {
		info, err := s.Disk.Info(f.Local)
		if err != nil {
			return report, err
		}
		if !info.Regular {
			return report, fmt.Errorf("%s: not a regular file", f.Local)
		}
		if err = p.file(f.Local, f.Display, clean(f.Path), info.Size); err != nil {
			return report, err
		}
		if f.Expected != nil {
			p.uploads[len(p.uploads)-1].previous = *f.Expected
			p.uploads[len(p.uploads)-1].expected = f.Expected
		}
	}
	return s.carryOut(ctx, cloud, p, report, "restores", true)
}

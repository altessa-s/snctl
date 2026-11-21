// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package files

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"

	"github.com/altessa-s/snctl/internal/port"
)

// Put uploads the files a person names. Each file is its own change: one
// that fails is reported and the others still go up, one at a time, in the
// order given. The destination is checked against one listing before
// anything is sent; a file of the same name is replaced only with Replace.
func (s Service) Put(ctx context.Context, req port.PutRequest, o port.UploadOptions) (port.TransferReport, error) {
	report := port.TransferReport{Items: []port.TransferItem{}, DryRun: o.DryRun}
	stdin, err := s.checkPut(req)
	if err != nil {
		return report, err
	}
	cloud, err := s.view(ctx)
	if err != nil {
		return report, err
	}
	dest := clean(req.Destination)
	folder, err := putFolder(ctx, cloud, dest, req.Folder || len(req.Sources) > 1 || req.Name != "", stdin)
	if err != nil {
		return report, err
	}
	var staged []string
	defer func() {
		for _, file := range staged {
			_ = s.Disk.Remove(file)
		}
	}()
	o.Jobs = 1
	p := planner{s: s, ctx: ctx, cloud: cloud, known: map[string]bool{}, opts: o}
	for _, src := range req.Sources {
		local, name := src.Path, filepath.Base(src.Path)
		if req.Name != "" {
			name = req.Name
		}
		var n int64
		if src.Path == "-" {
			if local, err = s.Disk.Stage(src.Stdin); err != nil {
				return report, err
			}
			staged = append(staged, local)
		}
		info, infoErr := s.Disk.Info(local)
		switch {
		case infoErr != nil:
			p.other = append(p.other, failed(port.TransferItem{Local: src.Path}, infoErr))
			continue
		case !info.Regular:
			p.other = append(p.other, port.TransferItem{Status: port.StatusFailed, Local: src.Path, Reason: "not a regular file"})
			continue
		}
		n = info.Size
		target := dest
		if folder {
			target = path.Join(dest, name)
		}
		if err = p.file(local, src.Path, target, n); err != nil {
			return report, err
		}
	}
	return s.carryOut(ctx, cloud, p, report, "uploads", true)
}

// checkPut refuses a request before any request is made and reports whether
// it reads standard input.
func (s Service) checkPut(req port.PutRequest) (bool, error) {
	stdin := 0
	for _, src := range req.Sources {
		if src.Path == "-" {
			stdin++
		}
	}
	switch {
	case len(req.Sources) == 0:
		return false, fmt.Errorf("%w: put needs a file", port.ErrArgument)
	case req.Name != "" && len(req.Sources) > 1:
		return false, fmt.Errorf("%w: --name takes one file", port.ErrArgument)
	case stdin > 1:
		return false, fmt.Errorf("%w: standard input can be put once", port.ErrArgument)
	case s.Disk == nil:
		return false, errNoDisk
	}
	return stdin == 1, nil
}

// putFolder decides whether dest is the folder files go into or the full
// path of the one file: an existing folder takes a named file, never bytes
// from standard input, whose name it could not know.
func putFolder(ctx context.Context, cloud port.Files, dest string, folder, stdin bool) (bool, error) {
	e, err := cloud.Stat(ctx, dest)
	switch {
	case err == nil && e.Folder && !folder && !stdin:
		return true, nil
	case err == nil && e.Folder && !folder:
		return false, fmt.Errorf("%w: %s is a folder; name the file with --name", port.ErrArgument, dest)
	case folder && err != nil:
		return false, err
	case folder && !e.Folder:
		return false, fmt.Errorf("%w: %s is not a folder", port.ErrArgument, dest)
	case err != nil && !errors.Is(err, port.ErrNotFound):
		return false, err
	}
	return folder, nil
}

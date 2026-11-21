// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package notebooks

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/altessa-s/snctl/internal/port"
)

// maximumSourcePDFBytes matches snrender-go v1.0.0's default PDF text input limit.
const maximumSourcePDFBytes int64 = 512 << 20

func sourceSizeError(limit int64) error {
	return fmt.Errorf("source PDF exceeds size budget of %d bytes", limit)
}

func (s Service) sourcePDF(ctx context.Context, query, name string, o port.RenderOptions, sourceFlag bool) ([]byte, []string, error) {
	source := o.Source
	var warnings []string
	explicit := source != ""
	if !explicit {
		var err error
		var ambiguous bool
		source, ambiguous, err = s.siblingPDF(ctx, query, name)
		if ambiguous {
			warnings = []string{"ambiguous stem-only .mark sidecar; use the original's full name plus .mark"}
			if sourceFlag {
				warnings[0] += " or --source"
			}
		}
		if err != nil || source == "" {
			return nil, warnings, err
		}
	}

	if strings.EqualFold(path.Ext(source), ".epub") {
		return nil, warnings, fmt.Errorf("%w: --source requires a PDF", port.ErrOptions)
	}
	limit := maximumSourcePDFBytes
	if cache, ok := s.Index.(port.FileCache); ok {
		status, err := cache.Status()
		if err != nil {
			return nil, warnings, err
		}
		if status.Budget > 0 {
			limit = min(limit, status.Budget)
		}
	}
	_, data, err := s.loadLimited(ctx, source, o.DownloadProgress, limit)
	if !explicit && (errors.Is(err, fs.ErrNotExist) || errors.Is(err, port.ErrNotFound)) {
		return nil, warnings, nil
	}
	if err == nil && len(data) == 0 {
		err = fmt.Errorf("%w: empty source PDF", port.ErrOptions)
	}
	return data, warnings, err
}

func (s Service) siblingPDF(ctx context.Context, query, name string) (string, bool, error) {
	if !strings.EqualFold(path.Ext(name), ".mark") {
		return "", false, nil
	}
	local, err := s.Files.LocalInput(query)
	if err != nil {
		return "", false, err
	}
	if !local {
		entry, err := s.Fetch.Resolve(ctx, query)
		if err != nil {
			return "", false, err
		}
		query = entry.Path
	}
	source := strings.TrimSuffix(query, path.Ext(query))
	if strings.EqualFold(path.Ext(source), ".epub") {
		return "", false, nil
	}
	legacy := !strings.EqualFold(path.Ext(source), ".pdf")
	if legacy {
		source += ".pdf"
	}
	if local {
		exists, err := s.Files.LocalInput(source)
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		if !exists {
			return "", false, nil
		}
	}
	if legacy {
		ambiguous, err := s.legacySourceAmbiguous(ctx, strings.TrimSuffix(source, ".pdf"), local)
		if err != nil || ambiguous {
			return "", ambiguous, err
		}
	}
	if !local {
		// Resolve returns a relative cloud path; preserve cloud selection when
		// loading its sibling even if a local file has the same name.
		source = "/" + strings.TrimPrefix(source, "/")
	}
	return source, false, nil
}

func (s Service) legacySourceAmbiguous(ctx context.Context, stem string, local bool) (bool, error) {
	if local {
		exists, err := s.Files.LocalInput(stem + ".epub")
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return exists, err
	}
	if s.Fetch == nil {
		return false, nil
	}
	if _, err := s.Fetch.Resolve(ctx, stem+".pdf"); err != nil {
		if errors.Is(err, port.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	_, err := s.Fetch.Resolve(ctx, stem+".epub")
	if errors.Is(err, port.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

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

func (s Service) annotationPath(ctx context.Context, query string) (string, error) {
	mark, err := s.optionalAnnotation(ctx, query)
	if err != nil || mark != "" {
		return mark, err
	}
	return "", fmt.Errorf("%s has no annotations: %w", query, port.ErrNotFound)
}

func (s Service) optionalAnnotation(ctx context.Context, query string) (string, error) {
	local, err := s.Files.LocalInput(query)
	if err != nil {
		return "", err
	}
	if local {
		found, err := s.Files.LocalInput(query + ".mark")
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if found {
			return query + ".mark", nil
		}
	} else {
		mark, err := s.cloudAnnotation(ctx, query)
		if err != nil {
			return "", err
		}
		if mark != "" {
			return mark, nil
		}
	}
	return "", nil
}

func (s Service) cloudAnnotation(ctx context.Context, query string) (string, error) {
	if s.Fetch == nil {
		return "", fs.ErrNotExist
	}
	doc, err := s.Fetch.Resolve(ctx, query)
	if err != nil {
		return "", err
	}
	if doc.Folder {
		return "", nil
	}
	if s.Cloud == nil {
		mark, markErr := s.Fetch.Resolve(ctx, absoluteCloudPath(doc.Path)+".mark")
		if errors.Is(markErr, port.ErrNotFound) || errors.Is(markErr, fs.ErrNotExist) {
			return "", nil
		}
		if markErr != nil || mark.Folder {
			return "", markErr
		}
		return absoluteCloudPath(mark.Path), nil
	}
	entries, err := s.Cloud.List(ctx, path.Dir(doc.Path), false)
	if err != nil {
		return "", err
	}
	mark, counts := port.SidecarOf(entries, doc.Path)
	if counts.Documents > 1 || counts.Marks > 1 {
		return "", fmt.Errorf("%s: ambiguous annotation sidecar: %w", query, port.ErrConflict)
	}
	if counts.Files == 0 {
		return "", nil
	}
	return absoluteCloudPath(mark.Path), nil
}

func absoluteCloudPath(p string) string { return "/" + strings.TrimPrefix(p, "/") }

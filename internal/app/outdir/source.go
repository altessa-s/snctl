// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package outdir

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"

	"github.com/altessa-s/snctl/internal/port"
)

type record struct {
	Source  port.OutputSource `json:"source"`
	Entries map[string]Entry  `json:"entries"`
}

// Source identifies a folder without retaining a token or a device secret.
func Source(ctx context.Context, cloud port.Files, top port.Entry) (port.OutputSource, error) {
	var source port.OutputSource
	if provider, ok := cloud.(port.OutputSourceReader); ok {
		var err error
		source, err = provider.OutputSource(ctx)
		if err != nil {
			return source, err
		}
	}
	source.FolderID, source.FolderPath = top.ID, top.Path
	return source, nil
}

// Load refuses to trust a foreign or legacy record. Force starts a new record;
// it never authorizes deleting files described by a different source.
func Load(files port.NotebookFiles, dir, name string, source port.OutputSource, force bool) (map[string]Entry, error) {
	r, err := files.Open(filepath.Join(dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	var saved record
	if err = json.UnmarshalRead(io.LimitReader(r, maxRecord), &saved); err != nil {
		if force {
			return map[string]Entry{}, nil
		}
		return nil, fmt.Errorf("%w: output record is unreadable", port.ErrRefused)
	}
	if saved.Entries == nil || saved.Source != source {
		if force {
			return map[string]Entry{}, nil
		}
		return nil, fmt.Errorf("%w: output record belongs to a different or unknown cloud source; "+
			"use --force to start a new record without deleting the earlier source's files", port.ErrRefused)
	}
	for key := range saved.Entries {
		if !Local(key) {
			delete(saved.Entries, key)
		}
	}
	return saved.Entries, nil
}

// Store commits identity together with the completed entries.
func Store(ctx context.Context, files port.NotebookFiles, dir, name string, source port.OutputSource, entries map[string]Entry) error {
	return files.Produce(context.WithoutCancel(ctx), filepath.Join(dir, name), func(w io.Writer) error {
		return json.MarshalWrite(w, record{Source: source, Entries: entries}, json.Deterministic(true))
	})
}

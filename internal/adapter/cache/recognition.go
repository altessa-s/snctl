// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cache

import (
	"context"
	"errors"
	"maps"

	"github.com/altessa-s/snctl/internal/port"
)

const recognitionFile = "recognition"

type recognitionIndex struct {
	Version int             `json:"version"`
	Bodies  map[string]bool `json:"bodies"`
}

var _ port.RecognitionIndex = (*Store)(nil)

// Recognition reads one command's memoised snapshot without touching cache recency.
func (s *Store) Recognition(hash string) *bool {
	if !validHash(hash) {
		return nil
	}
	s.recognitionMu.Lock()
	defer s.recognitionMu.Unlock()
	s.loadRecognition()
	value, ok := s.recognitionValues[hash]
	if !ok {
		return nil
	}
	return &value
}

// RecognitionSnapshot returns an independent copy of the command's snapshot.
func (s *Store) RecognitionSnapshot() map[string]bool {
	s.recognitionMu.Lock()
	defer s.recognitionMu.Unlock()
	s.loadRecognition()
	return maps.Clone(s.recognitionValues)
}

func (s *Store) loadRecognition() {
	if s.recognitionLoaded {
		return
	}
	var index recognitionIndex
	if s.read(recognitionFile, &index) == nil && index.Version == 1 {
		s.recognitionValues = index.Bodies
	}
	s.recognitionLoaded = true
}

// RememberRecognition merges under the cache's process lock and publishes atomically.
func (s *Store) RememberRecognition(ctx context.Context, hash string, value bool) error {
	return s.RememberRecognitions(ctx, map[string]bool{hash: value})
}

// RememberRecognitions reloads under the process lock so concurrent writers merge.
// A damaged or unreadable cache index is disposable and is replaced on write.
func (s *Store) RememberRecognitions(ctx context.Context, values map[string]bool) error {
	for hash := range values {
		if !validHash(hash) {
			return errors.New("invalid recognition MD5")
		}
	}
	if len(values) == 0 {
		return nil
	}
	return s.locked(ctx, func() error {
		index := recognitionIndex{}
		if s.read(recognitionFile, &index) != nil || index.Version != 1 {
			index = recognitionIndex{Version: 1}
		}
		if index.Bodies == nil {
			index.Bodies = map[string]bool{}
		}
		maps.Copy(index.Bodies, values)
		if err := s.save(recognitionFile, index); err != nil {
			return err
		}
		s.recognitionMu.Lock()
		s.recognitionLoaded, s.recognitionValues = true, index.Bodies
		s.recognitionMu.Unlock()
		return nil
	})
}

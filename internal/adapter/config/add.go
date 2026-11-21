// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config

import (
	"bytes"
	"fmt"

	"github.com/altessa-s/snctl/internal/domain"
)

// AddProfile appends a new profile while preserving comments and refusing
// concurrent hand edits, as SetSecretsBackend does.
func (w Writer) AddProfile(name, endpoint, backend string) error {
	if !profileName.MatchString(name) {
		return ErrProfile
	}
	endpoint, err := domain.Endpoint(endpoint)
	if err != nil {
		return err
	}
	if err = checkBackend("backend", backend); err != nil {
		return err
	}
	read := w.Read
	if read == nil {
		read = ReadFile
	}
	unlock, err := lockFile(w.Path)
	if err != nil {
		return err
	}
	defer unlock()
	data, exists, err := read(w.Path)
	if err != nil {
		return err
	}
	f, err := ParseFile(data)
	if err != nil {
		return err
	}
	if _, ok := f.Profiles[name]; ok {
		return fmt.Errorf("profile %q already exists", name)
	}
	lines := fmt.Sprintf("[profiles.%s]\nendpoint = %q\n", name, endpoint)
	if backend != "" {
		lines += "\n" + w.SecretsLines(name, backend)
	}
	next := append(bytes.Clone(data), []byte(separator(data)+lines)...)
	if _, err := ParseFile(next); err != nil {
		return err
	}
	return replaceUnlessChanged(w.Path, data, exists, next)
}

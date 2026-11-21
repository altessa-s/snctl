// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package profile manages local profiles through a store and its vaults.
package profile

import (
	"context"
	"errors"
	"fmt"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
)

// Service manages the profiles of one machine.
type Service struct {
	store   port.ProfileStore
	current string
}

// New builds the service over store, marking current in its answers.
func New(store port.ProfileStore, current string) *Service {
	if store == nil || current == "" {
		panic("profile: store and current profile required")
	}
	return &Service{store: store, current: current}
}

// List returns the saved profiles in store order.
func (s *Service) List(ctx context.Context) ([]port.ProfileSettings, error) {
	names, err := s.store.Names()
	if err != nil {
		return nil, err
	}
	out := make([]port.ProfileSettings, 0, len(names))
	for _, n := range names {
		p, err := s.Info(ctx, n)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// Info resolves name, or the selected profile when name is empty.
func (s *Service) Info(ctx context.Context, name string) (port.ProfileSettings, error) {
	if err := ctx.Err(); err != nil {
		return port.ProfileSettings{}, err
	}
	if name == "" {
		name = s.current
	}
	p, err := s.store.Settings(name)
	p.Current = name == s.current
	return p, err
}

// Add saves a local profile. It neither logs in nor probes the Keychain.
func (s *Service) Add(ctx context.Context, name, endpoint, backend string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	endpoint, err := domain.Endpoint(endpoint)
	if err != nil {
		return err
	}
	if backend != "" && backend != string(port.SecretsFile) && backend != string(port.SecretsKeychain) {
		return errors.New("backend must be file or keychain")
	}
	p, err := s.store.Settings(name)
	if err != nil {
		return err
	}
	if p.Exists {
		return port.ErrProfileExists
	}
	return s.store.Add(name, endpoint, backend)
}

// Remove clears both vaults before removing settings. An inaccessible
// Keychain leaves the profile intact so its secrets are never orphaned.
func (s *Service) Remove(ctx context.Context, name string) error {
	p, err := s.Info(ctx, name)
	if err != nil {
		return err
	}
	if !p.Exists {
		return port.ErrNoProfile
	}
	vaults, err := s.store.Vaults(name)
	if err != nil {
		return err
	}
	for _, v := range vaults {
		for _, n := range v.Names() {
			if err := ctx.Err(); err != nil {
				return err
			}
			err := v.Delete(ctx, n)
			if errors.Is(err, port.ErrKeychainUnsupported) && p.Backend != port.SecretsKeychain {
				break
			}
			if err != nil {
				return fmt.Errorf("remove profile secrets: %w", err)
			}
		}
	}
	return s.store.Remove(name)
}

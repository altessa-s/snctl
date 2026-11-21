// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package porttest provides isolated doubles of the local ports.
package porttest

import (
	"context"
	"sync"

	"github.com/altessa-s/snctl/internal/port"
)

// Store is a concurrency-safe secret store for synthetic test values.
type Store struct {
	mu     sync.Mutex
	values map[port.SecretName]string
}

// Secret implements port.SecretStore.
func (s *Store) Secret(_ context.Context, n port.SecretName) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[n]
	return v, ok, nil
}

// Save implements port.SecretStore.
func (s *Store) Save(_ context.Context, n port.SecretName, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = map[port.SecretName]string{}
	}
	s.values[n] = v
	return nil
}

// Forget implements port.SecretStore.
func (s *Store) Forget(_ context.Context, n port.SecretName) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, n)
	return nil
}

// Items is a fake Keychain, keyed by service and account. Err injects failures.
type Items struct {
	mu     sync.Mutex
	values map[string]string
	Err    error
}

// Get implements the Keychain item seam.
func (i *Items) Get(service, account string) (string, bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	v, ok := i.values[service+"\x00"+account]
	return v, ok, i.Err
}

// Put implements the Keychain item seam.
func (i *Items) Put(service, account, _, value string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.Err != nil {
		return i.Err
	}
	if i.values == nil {
		i.values = map[string]string{}
	}
	i.values[service+"\x00"+account] = value
	return nil
}

// Delete implements the Keychain item seam.
func (i *Items) Delete(service, account string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.Err != nil {
		return i.Err
	}
	delete(i.values, service+"\x00"+account)
	return nil
}

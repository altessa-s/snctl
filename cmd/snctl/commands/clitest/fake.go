// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package clitest

import (
	"context"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/internal/port"
)

// Fake supplies local command results and records mutations.
type Fake struct {
	Err            error
	Profiles       []port.ProfileSettings
	Status         port.SecretsStatus
	Migration      port.SecretsMigration
	Added, Removed string
	Migrated       port.SecretsBackend
}

// Services presents the fake through the command line's ports.
func (f *Fake) Services() cliutil.Services {
	return cliutil.Services{Profiles: profileFake{f}, Secrets: secretFake{f}}
}

type profileFake struct{ f *Fake }

func (p profileFake) List(context.Context) ([]port.ProfileSettings, error) {
	return p.f.Profiles, p.f.Err
}
func (p profileFake) Info(context.Context, string) (port.ProfileSettings, error) {
	if len(p.f.Profiles) == 0 {
		return port.ProfileSettings{}, p.f.Err
	}
	return p.f.Profiles[0], p.f.Err
}
func (p profileFake) Add(_ context.Context, n, _, _ string) error { p.f.Added = n; return p.f.Err }
func (p profileFake) Remove(_ context.Context, n string) error    { p.f.Removed = n; return p.f.Err }

type secretFake struct{ f *Fake }

func (s secretFake) Status(context.Context) (port.SecretsStatus, error) { return s.f.Status, s.f.Err }
func (s secretFake) Migrate(_ context.Context, to port.SecretsBackend, _ port.MigrateOptions) (port.SecretsMigration, error) {
	s.f.Migrated = to
	return s.f.Migration, s.f.Err
}

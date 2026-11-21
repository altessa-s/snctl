// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package profile_test

import (
	"context"
	"errors"
	"testing"

	"github.com/altessa-s/snctl/internal/app/profile"
	"github.com/altessa-s/snctl/internal/port"
)

type store struct {
	fail    string
	exists  bool
	backend port.SecretsBackend
	v       *vault
}

func (s store) Names() ([]string, error) {
	if s.fail == "names" {
		return nil, errors.New("names")
	}
	return []string{"work"}, nil
}
func (s store) Settings(name string) (port.ProfileSettings, error) {
	if s.fail == "settings" {
		return port.ProfileSettings{}, errors.New("settings")
	}
	return port.ProfileSettings{Name: name, Exists: s.exists, Backend: s.backend}, nil
}
func (s store) Add(string, string, string) error { return nil }
func (s store) Vaults(string) ([]port.SecretVault, error) {
	if s.fail == "vaults" {
		return nil, errors.New("vaults")
	}
	return []port.SecretVault{s.v}, nil
}
func (s store) Remove(string) error { return nil }

type vault struct{ err error }

func (*vault) Backend() port.SecretsBackend                      { return port.SecretsKeychain }
func (*vault) Location() string                                  { return "synthetic" }
func (*vault) Names() []string                                   { return []string{"accessToken"} }
func (*vault) Get(context.Context, string) (string, bool, error) { return "", false, nil }
func (*vault) Put(context.Context, string, string) error         { return nil }
func (v *vault) Delete(context.Context, string) error            { return v.err }

func TestProfile(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	st := store{exists: true, v: &vault{}}
	s := profile.New(st, "work")
	if ps, err := s.List(ctx); err != nil || len(ps) != 1 || !ps[0].Current {
		t.Fatalf("%+v %v", ps, err)
	}
	if p, err := s.Info(ctx, ""); err != nil || p.Name != "work" {
		t.Fatalf("%+v %v", p, err)
	}
	if err := s.Remove(ctx, "work"); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(ctx, "work", "official", ""); !errors.Is(err, port.ErrProfileExists) {
		t.Fatal(err)
	}
	for _, fail := range []string{"names", "settings", "vaults"} {
		broken := profile.New(store{fail: fail, exists: true, v: &vault{}}, "work")
		if fail != "vaults" {
			if _, err := broken.List(ctx); err == nil {
				t.Fatal(fail)
			}
		}
		if fail != "names" {
			if err := broken.Remove(ctx, "work"); err == nil {
				t.Fatal(fail)
			}
		}
		if fail == "settings" {
			if err := broken.Add(ctx, "work", "official", ""); err == nil {
				t.Fatal(fail)
			}
		}
	}
	for _, backend := range []port.SecretsBackend{port.SecretsFile, port.SecretsKeychain} {
		for _, err := range []error{port.ErrKeychainUnsupported, errors.New("locked")} {
			broken := profile.New(store{exists: true, backend: backend, v: &vault{err: err}}, "work")
			got := broken.Remove(ctx, "work")
			if backend == port.SecretsFile && errors.Is(err, port.ErrKeychainUnsupported) {
				if got != nil {
					t.Fatal(got)
				}
			} else if got == nil {
				t.Fatal("failure ignored")
			}
		}
	}
	empty := profile.New(store{v: &vault{}}, "work")
	if err := empty.Remove(ctx, "work"); !errors.Is(err, port.ErrNoProfile) {
		t.Fatal(err)
	}
	if err := empty.Add(ctx, "work", "http://private.example", "file"); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ endpoint, backend string }{{"ftp://x", ""}, {"official", "invalid"}} {
		if err := empty.Add(ctx, "work", tt.endpoint, tt.backend); err == nil {
			t.Fatal("invalid settings")
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Info(canceled, ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := s.Add(canceled, "work", "official", ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, f := range []func(){func() { profile.New(nil, "work") }, func() { profile.New(st, "") }} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("missing panic")
				}
			}()
			f()
		}()
	}
}

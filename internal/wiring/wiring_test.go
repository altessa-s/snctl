// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wiring_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/config"
	"github.com/altessa-s/snctl/internal/adapter/keychain"
	"github.com/altessa-s/snctl/internal/adapter/secretfile"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
	"github.com/altessa-s/snctl/internal/wiring"
)

func TestLocalLifecycleAndMigration(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	opts := wiring.Options{Dirs: config.BaseDirs{Config: t.TempDir(), Cache: t.TempDir()}, Keychain: &porttest.Items{}}
	svc, err := wiring.Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := svc.Profiles.List(ctx); err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
	if err := svc.Profiles.Add(ctx, "work", "http://private.example/", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.Profiles.Add(ctx, "home", "official", "file"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Profiles.Add(ctx, "home", "official", ""); !errors.Is(err, port.ErrProfileExists) {
		t.Fatal(err)
	}
	opts.Profile = "work"
	svc, err = wiring.Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	info, err := svc.Profiles.Info(ctx, "")
	if err != nil || !info.Exists || !info.Current || info.Endpoint != "http://private.example" {
		t.Fatalf("%+v %v", info, err)
	}
	if ps, err := svc.Profiles.List(ctx); err != nil || len(ps) != 2 || ps[0].Name != "home" {
		t.Fatalf("%+v %v", ps, err)
	}
	store := secretfile.New(svc.Paths.TokenFile)
	for _, n := range []port.SecretName{port.SecretAccessToken, port.SecretAccount, port.SecretClientID} {
		if err := store.Save(ctx, n, "synthetic-"+string(n)); err != nil {
			t.Fatal(err)
		}
	}
	if m, err := svc.Secrets.Migrate(ctx, port.SecretsKeychain, port.MigrateOptions{SetDefault: true}); err != nil || len(m.Moved) != 3 {
		t.Fatalf("%+v %v", m, err)
	}
	svc, err = wiring.Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := svc.Secrets.Status(ctx); err != nil || st.Backend != port.SecretsKeychain || len(st.Present) != 3 {
		t.Fatalf("%+v %v", st, err)
	}
	// Preserve files that snctl did not create in a profile directory.
	if err := os.WriteFile(filepath.Join(info.ConfigDir, "keep.txt"), []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := svc.Profiles.Remove(ctx, "work"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(info.ConfigDir, "keep.txt")); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := opts.Keychain.Get(keychain.DefaultService, "work/accessToken"); err != nil || ok {
		t.Fatalf("%v %v", ok, err)
	}
	if err := svc.Profiles.Remove(ctx, "home"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Profiles.Remove(ctx, "missing"); !errors.Is(err, port.ErrNoProfile) {
		t.Fatal(err)
	}
}

func TestBuildFailuresAndOverrides(t *testing.T) {
	t.Parallel()
	opts := wiring.Options{Dirs: config.BaseDirs{Config: t.TempDir(), Cache: t.TempDir()}, Keychain: &porttest.Items{}}
	opts.Profile = "../bad"
	if _, err := wiring.Build(t.Context(), opts); err == nil {
		t.Fatal("unsafe profile")
	}
	opts.Profile = "work"
	opts.Read = func(string) ([]byte, bool, error) { return nil, false, errors.New("unreadable") }
	if _, err := wiring.Build(t.Context(), opts); err == nil {
		t.Fatal("read failure")
	}
	opts.Read = nil
	opts.Env = map[string]string{config.EnvSecrets: "keychain", config.EnvKeychainService: "synthetic.test.service"}
	s, err := wiring.Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Profiles.Add(t.Context(), "work", "official", ""); err != nil {
		t.Fatal(err)
	}
	if st, err := s.Secrets.Status(t.Context()); err != nil || st.Source != "env" {
		t.Fatalf("%+v %v", st, err)
	}
	if err := s.Profiles.Add(t.Context(), "../bad", "official", ""); err == nil {
		t.Fatal("unsafe profile")
	}
}

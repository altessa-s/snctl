// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package account_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/cmd/snctl/commands/account"
	"github.com/altessa-s/snctl/cmd/snctl/commands/clitest"
	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/internal/port"
)

func fake() *clitest.Fake {
	return &clitest.Fake{
		Profiles:  []port.ProfileSettings{{Name: "work", Endpoint: "official", Backend: port.SecretsFile, BackendSource: "default", ConfigFile: "/config/config.toml", ConfigDir: "/config/work", CacheDir: "/cache/work", TokenPath: "/config/work/token.json", Exists: true, Current: true}},
		Status:    port.SecretsStatus{Profile: "work", Backend: port.SecretsFile, Location: "/config/work/token.json", Source: "default", ConfigFile: "/config/config.toml", Present: []string{"accessToken", "account", "clientID"}},
		Migration: port.SecretsMigration{From: port.SecretsFile, To: port.SecretsKeychain, Location: "Keychain", Moved: []string{"accessToken", "account", "clientID"}, ConfigFile: "/config/config.toml", ConfigLines: "[profiles.work.secrets]\nbackend = \"keychain\"\n"},
	}
}

func TestGolden(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		args []string
	}{
		{"profile-ls", []string{"profile", "ls"}}, {"profile-info", []string{"profile", "info"}},
		{"profile-ls-json", []string{"profile", "ls", "--json"}}, {"profile-info-table", []string{"profile", "info", "--format", "table"}},
		{"secrets-status", []string{"secrets", "status"}}, {"secrets-status-json", []string{"secrets", "status", "--json"}},
		{"profile-add", []string{"profile", "add", "work", "--yes"}}, {"profile-del", []string{"profile", "del", "work", "--yes"}},
		{"secrets-migrate", []string{"secrets", "migrate", "--to", "keychain", "--yes"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := clitest.Exec(t, fake().Services(), clitest.Options{}, account.Commands, tt.args...)
			if r.Error != nil {
				t.Fatal(r.Error)
			}
			clitest.Golden(t, tt.name, r.Out)
		})
	}
}

func TestRefusals(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"profile", "add", "work"}, {"profile", "del", "work"}, {"secrets", "migrate", "--to", "keychain"}} {
		f := fake()
		r := clitest.Exec(t, f.Services(), clitest.Options{}, account.Commands, args...)
		if r.Code != 2 || f.Added != "" || f.Removed != "" || f.Migrated != "" {
			t.Fatalf("%v: %+v", args, r)
		}
		r = clitest.Exec(t, f.Services(), clitest.Options{Terminal: true, Stdin: "no\n"}, account.Commands, args...)
		if !errors.Is(r.Error, cliutil.ErrDeclined) {
			t.Fatal(r.Error)
		}
		r = clitest.Exec(t, f.Services(), clitest.Options{Terminal: true, Stdin: "yes\n"}, account.Commands, args...)
		if r.Error != nil {
			t.Fatal(r.Error)
		}
	}
	for _, args := range [][]string{{"profile", "add"}, {"profile", "del"}, {"profile", "info", "a", "b"}, {"secrets", "migrate", "--to", "invalid", "--yes"}, {"secrets", "migrate"}} {
		if r := clitest.Exec(t, fake().Services(), clitest.Options{}, account.Commands, args...); r.Code != 2 {
			t.Fatalf("%v: %+v", args, r)
		}
	}
	for _, args := range [][]string{{"profile", "ls"}, {"profile", "info"}, {"profile", "add", "work", "--yes"}, {"profile", "del", "work", "--yes"}, {"secrets", "status"}, {"secrets", "migrate", "--to", "file", "--yes"}} {
		f := fake()
		f.Err = errors.New("synthetic failure")
		if r := clitest.Exec(t, f.Services(), clitest.Options{}, account.Commands, args...); r.Code != 1 {
			t.Fatalf("%v: %+v", args, r)
		}
		if r := clitest.Exec(t, cliutil.Services{}, clitest.Options{}, account.Commands, args...); !errors.Is(r.Error, cliutil.ErrNoService) {
			t.Fatalf("%v: %+v", args, r)
		}
	}
	f := fake()
	f.Status.Present = nil
	f.Profiles = nil
	for _, args := range [][]string{{"profile", "ls", "--json"}, {"secrets", "status", "--json"}} {
		if r := clitest.Exec(t, f.Services(), clitest.Options{}, account.Commands, args...); r.Error != nil || !strings.Contains(r.Out, "[]") {
			t.Fatalf("%+v", r)
		}
	}
	f.Migration.Override = "SNCTL_SECRETS=file"
	r := clitest.Exec(t, f.Services(), clitest.Options{}, account.Commands, "secrets", "migrate", "--to", "keychain", "--yes")
	if !strings.Contains(r.Err, "unset SNCTL_SECRETS") {
		t.Fatal(r.Err)
	}
	f.Migration.DefaultSet = true
	r = clitest.Exec(t, f.Services(), clitest.Options{}, account.Commands, "secrets", "migrate", "--to", "keychain", "--yes")
	if r.Err != "" {
		t.Fatal(r.Err)
	}
}

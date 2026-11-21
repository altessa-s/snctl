// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vault_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/secretfile"
	"github.com/altessa-s/snctl/internal/adapter/vault"
	"github.com/altessa-s/snctl/internal/port"
)

func TestVaultOverAStore(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "token.json")
	v := vault.New(secretfile.New(path), port.SecretsFile, path)
	if v.Backend() != port.SecretsFile || v.Location() != path {
		t.Fatalf("%s %s", v.Backend(), v.Location())
	}
	if !slices.Equal(v.Names(), []string{"accessToken", "clientID", "account"}) {
		t.Fatalf("%v", v.Names())
	}
	ctx := t.Context()
	if err := v.Put(ctx, "accessToken", "t"); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := v.Get(ctx, "accessToken"); got != "t" || !ok || err != nil {
		t.Fatalf("%q %v %v", got, ok, err)
	}
	if err := v.Delete(ctx, "accessToken"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := v.Get(ctx, "accessToken"); ok {
		t.Fatal("deleted")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a nil store did not panic")
		}
	}()
	vault.New(nil, port.SecretsFile, "")
}

// The profile use case knows a registered profile by string(port.SecretAccessToken): it
// must be the name the transport keeps the device token under.
func TestDeviceTokenIsTheTransports(t *testing.T) {
	t.Parallel()
	v := vault.New(secretfile.New(filepath.Join(t.TempDir(), "token.json")), port.SecretsFile, "")
	if v.Names()[0] != string(port.SecretAccessToken) {
		t.Fatalf("%v", v.Names())
	}
}

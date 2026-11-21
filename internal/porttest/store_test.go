// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest_test

import (
	"errors"
	"testing"

	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestStores(t *testing.T) {
	t.Parallel()
	var s porttest.Store
	ctx := t.Context()
	if _, ok, err := s.Secret(ctx, port.SecretClientID); ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := s.Save(ctx, port.SecretClientID, "synthetic"); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := s.Secret(ctx, port.SecretClientID); v != "synthetic" || !ok || err != nil {
		t.Fatal(v, ok, err)
	}
	if err := s.Forget(ctx, port.SecretClientID); err != nil {
		t.Fatal(err)
	}
	var k porttest.Items
	if err := k.Put("s", "a", "l", "synthetic"); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := k.Get("s", "a"); v != "synthetic" || !ok || err != nil {
		t.Fatal(v, ok, err)
	}
	if err := k.Delete("s", "a"); err != nil {
		t.Fatal(err)
	}
	k.Err = errors.New("locked")
	if k.Put("s", "a", "l", "synthetic") == nil || k.Delete("s", "a") == nil {
		t.Fatal("failure ignored")
	}
}

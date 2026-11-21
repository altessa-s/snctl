// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestSessionFlow(t *testing.T) {
	t.Parallel()
	for _, verification := range []bool{false, true} {
		var store porttest.Store
		sent := false
		verified := false
		session := porttest.Session{
			LogInFunc: func(ctx context.Context, account, password string) (port.LoginResult, error) {
				if password != "synthetic-password" {
					t.Fatal("password not forwarded")
				}
				return port.LoginResult{VerificationRequired: verification}, store.Save(ctx, port.SecretAccount, account)
			},
			SendVerificationCodeFunc: func(context.Context) error { sent = true; return nil },
			VerifyFunc: func(_ context.Context, code string) error {
				if !sent || code != "123456" {
					t.Fatal("unexpected verification")
				}
				verified = true
				return nil
			},
			AccountFunc: func(ctx context.Context) (string, bool, error) { return store.Secret(ctx, port.SecretAccount) },
			LogOutFunc:  func(ctx context.Context) error { return store.Forget(ctx, port.SecretAccount) },
		}
		ctx := t.Context()
		result, err := session.LogIn(ctx, "reader@example.invalid", "synthetic-password")
		if err != nil {
			t.Fatal(err)
		}
		if result.VerificationRequired {
			if err := session.SendVerificationCode(ctx); err != nil {
				t.Fatal(err)
			}
			if err := session.Verify(ctx, "123456"); err != nil {
				t.Fatal(err)
			}
		}
		if verified != verification {
			t.Fatal("wrong login flow")
		}
		if account, found, err := session.Account(ctx); account != "reader@example.invalid" || !found || err != nil {
			t.Fatal("stored account not returned")
		}
		if err := session.LogOut(ctx); err != nil {
			t.Fatal(err)
		}
		if _, found, err := session.Account(ctx); found || err != nil {
			t.Fatal("account remains after logout")
		}
	}
}

func TestSessionErrors(t *testing.T) {
	t.Parallel()
	failure := errors.New("synthetic rejection")
	session := porttest.Session{
		LogInFunc:                func(context.Context, string, string) (port.LoginResult, error) { return port.LoginResult{}, failure },
		SendVerificationCodeFunc: func(context.Context) error { return failure },
		VerifyFunc:               func(context.Context, string) error { return failure },
		LogOutFunc:               func(context.Context) error { return failure },
		AccountFunc:              func(context.Context) (string, bool, error) { return "", false, failure },
	}
	ctx := t.Context()
	_, loginErr := session.LogIn(ctx, "reader@example.invalid", "synthetic")
	_, _, accountErr := session.Account(ctx)
	for _, err := range []error{loginErr, accountErr, session.SendVerificationCode(ctx), session.Verify(ctx, "123456"), session.LogOut(ctx)} {
		if !errors.Is(err, failure) {
			t.Fatalf("error = %v, want rejection", err)
		}
	}
}

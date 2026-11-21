// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest

import (
	"context"

	"github.com/altessa-s/snctl/internal/port"
)

// Session delegates to test callbacks so callers can script the emailed-code
// flow and failures without a network or retaining passwords in a fake.
// Set each callback that the test expects to call.
type Session struct {
	LogInFunc                func(context.Context, string, string) (port.LoginResult, error)
	SendVerificationCodeFunc func(context.Context) error
	VerifyFunc               func(context.Context, string) error
	LogOutFunc               func(context.Context) error
	AccountFunc              func(context.Context) (string, bool, error)
}

var _ port.Session = (*Session)(nil)

// LogIn implements port.Session.
func (s *Session) LogIn(ctx context.Context, account, password string, _ ...int) (port.LoginResult, error) {
	return s.LogInFunc(ctx, account, password)
}

// SendVerificationCode implements port.Session.
func (s *Session) SendVerificationCode(ctx context.Context) error {
	return s.SendVerificationCodeFunc(ctx)
}

// Verify implements port.Session.
func (s *Session) Verify(ctx context.Context, code string) error { return s.VerifyFunc(ctx, code) }

// LogOut implements port.Session.
func (s *Session) LogOut(ctx context.Context) error { return s.LogOutFunc(ctx) }

// Account implements port.Session.
func (s *Session) Account(ctx context.Context) (string, bool, error) { return s.AccountFunc(ctx) }

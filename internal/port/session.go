// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"time"
)

// LoginResult says whether login completed or needs an emailed code.
type LoginResult struct {
	VerificationRequired bool
}

// Session is the profile's cloud session. Implementations must never persist
// or log passwords. The library adapter translates this contract. An optional
// country code selects a telephone login.
type Session interface {
	LogIn(ctx context.Context, account, password string, country ...int) (LoginResult, error)
	SendVerificationCode(context.Context) error
	Verify(ctx context.Context, code string) error
	LogOut(context.Context) error
	// Account reads the account held in the secret store, without a network call.
	Account(context.Context) (account string, found bool, err error)
}

// AccountInfo is the credential-free account summary.
type AccountInfo struct {
	Account string     `json:"account"`
	User    string     `json:"user"`
	Cloud   string     `json:"cloud"`
	Used    int64      `json:"used"`
	Total   int64      `json:"total"`
	Expiry  *time.Time `json:"expiry"`
}

// CloudSession adds remote account inspection and lifecycle to the session.
type CloudSession interface {
	Session
	Info(context.Context) (AccountInfo, error)
	Close() error
}

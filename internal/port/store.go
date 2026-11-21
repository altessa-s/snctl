// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package port declares the contracts between commands, use cases and adapters.
package port

import "context"

// SecretName is one of the opaque values sncloud's session keeps.
type SecretName string

// The session's secret names. Passwords are never stored.
const (
	SecretAccessToken SecretName = "accessToken"
	SecretAccount     SecretName = "account"
	SecretClientID    SecretName = "clientID"
)

// SecretStore is the cloud client's persistence boundary.
type SecretStore interface {
	Secret(context.Context, SecretName) (string, bool, error)
	Save(context.Context, SecretName, string) error
	Forget(context.Context, SecretName) error
}

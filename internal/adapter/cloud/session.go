// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package cloud adapts sncloud-go to the CLI's credential-free ports.
package cloud

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/altessa-s/snctl/internal/port"

	sncloud "github.com/altessa-s/sncloud-go"
)

// Client owns the library client for one profile.
type Client struct {
	observeBody func(context.Context, port.Entry, string) bool
	// drawingInputs belongs to one pass and contains only selected drawing inputs.
	drawingInputs map[[2]string]bool
	cache         port.FileCache
	sessionCache  port.FileCache
	offline       bool
	ttl           time.Duration
	now           func() time.Time
	warning       func(string)
	session       sessionAPI
	device        deviceAPI
	web           webAPI
	tasks         tasksAPI
	digests       digestsAPI
	feed          feedAPI
	endpoint      string
	logger        *slog.Logger
	// view is the listing of one invocation, or nil to list on every lookup.
	view      *listing
	lockState port.SyncLockState
}

// New constructs a client without contacting the service or reading secrets.
// Adapter diagnostics go to log, or nowhere when nil. Library wire logging
// stays disabled because request bodies are outside the CLI diagnostic contract.
func New(vault port.SecretVault, endpoint, socket string, log io.Writer) (*Client, error) {
	endpoints := sncloud.Official()
	if endpoint != "official" {
		endpoints = sncloud.PrivateCloud(endpoint, false)
	}
	if socket != "" {
		endpoints.SocketHost = socket
	}
	if log == nil {
		log = io.Discard
	}
	logger := slog.New(slog.NewTextHandler(log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	c, err := sncloud.New(secretStore{vault}, sncloud.WithEndpoints(endpoints),
		sncloud.WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		return nil, errors.New("invalid cloud configuration")
	}
	return &Client{session: c, device: c.Device(), web: c.Files(), tasks: c.Tasks(), digests: c.Digests(), feed: c.Feed(),
		endpoint: endpoint, logger: logger}, nil
}

// Close releases idle connections without logging out.
func (c *Client) Close() error { return c.failure("close", "", c.session.Close()) }

// LogIn never retains the supplied password.
func (c *Client) LogIn(ctx context.Context, account, password string, country ...int) (port.LoginResult, error) {
	if c.sessionCache != nil {
		old, _ := c.session.Account()
		l, listingErr := c.sessionCache.Listing()
		if old != account || listingErr != nil || l.Endpoint != c.cacheEndpoint() || l.Account != account {
			if err := c.sessionCache.Clear(ctx); err != nil {
				return port.LoginResult{}, err
			}
		}
	}
	var code *int
	if len(country) > 0 {
		code = &country[0]
	}
	result, err := c.session.LogIn(ctx, account, password, code)
	return port.LoginResult{VerificationRequired: result == sncloud.VerificationRequired}, c.failure("login", "", err)
}

// SendVerificationCode sends the pending emailed challenge.
func (c *Client) SendVerificationCode(ctx context.Context) error {
	return c.failure("sendverificationcode", "", c.session.SendVerificationCode(ctx))
}

// Verify completes the pending challenge.
func (c *Client) Verify(ctx context.Context, code string) error {
	return c.failure("verify", "", c.session.Verify(ctx, code))
}

// LogOut ends the remote session and removes local credentials.
func (c *Client) LogOut(ctx context.Context) error {
	if c.sessionCache != nil {
		if err := c.sessionCache.Clear(ctx); err != nil {
			return err
		}
	}
	return c.failure("logout", "", c.session.LogOut(ctx))
}

// Account reads the saved account name, without a network request.
func (c *Client) Account(context.Context) (string, bool, error) {
	account, err := c.session.Account()
	return account, account != "", c.failure("account", "", err)
}

// Info returns only public account fields, never a token or device identifier.
func (c *Client) Info(ctx context.Context) (port.AccountInfo, error) {
	if c.offline {
		if c.warning != nil {
			c.warning("account used space and capacity are unavailable offline; reported as zero")
		}
		return port.AccountInfo{}, nil
	}
	account, err := c.session.Account()
	if err != nil {
		return port.AccountInfo{}, c.failure("info", "", err)
	}
	expiry, err := c.session.Expiry()
	if err != nil {
		return port.AccountInfo{}, c.failure("info", "", err)
	}
	user, err := c.web.User(ctx)
	if err != nil {
		return port.AccountInfo{}, c.failure("info", "", err)
	}
	capacity, err := c.web.Capacity(ctx)
	return port.AccountInfo{Account: account, User: user.Name, Cloud: c.endpoint,
		Used: capacity.Used, Total: capacity.Total, Expiry: expiry}, c.failure("info", "", err)
}

// The library store has no context parameter. Local vault operations use a
// background context so cancellation cannot strand a partially saved session.
type secretStore struct{ vault port.SecretVault }

func (s secretStore) Secret(key sncloud.Secret) (string, error) {
	value, _, err := s.vault.Get(context.Background(), string(key))
	return value, err
}
func (s secretStore) Keep(value string, key sncloud.Secret) error {
	return s.vault.Put(context.Background(), string(key), value)
}
func (s secretStore) Discard(key sncloud.Secret) error {
	return s.vault.Delete(context.Background(), string(key))
}

// Never propagate service messages or arbitrary store errors: either could
// echo credentials. Preserve only classifications the CLI can safely display.
func safe(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, sncloud.ErrNotLoggedIn), errors.Is(err, sncloud.ErrSessionExpired):
		return port.ErrSession
	case errors.Is(err, sncloud.ErrAlreadyExists):
		return port.ErrConflict
	case errors.Is(err, sncloud.ErrNotFound):
		return port.ErrNotFound
	case errors.Is(err, sncloud.ErrInvalidCredentials):
		return errors.New("invalid account or password")
	case errors.Is(err, sncloud.ErrInvalidVerificationCode):
		return errors.New("invalid verification code")
	default:
		return errors.New("cloud operation failed")
	}
}

// OutputSource exposes only the endpoint and saved account identifier.
func (c *Client) OutputSource(context.Context) (port.OutputSource, error) {
	account, err := c.session.Account()
	return port.OutputSource{Endpoint: c.endpoint, Account: account}, c.failure("outputsource", "", err)
}

// SetSyncLockState installs profile-local persistence without reading secrets.
func (c *Client) SetSyncLockState(state port.SyncLockState) { c.lockState = state }

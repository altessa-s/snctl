// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"errors"
)

// ErrNoProfile means no directory or configuration table names this profile.
var ErrNoProfile = errors.New("no such profile")

// ErrProfileExists prevents overwriting an existing profile's endpoint.
var ErrProfileExists = errors.New("the profile already exists")

// ProfileSettings are the resolved local settings of a profile.
type ProfileSettings struct {
	CacheBytes        int64          `json:"cache_bytes"`
	CacheBudget       int64          `json:"cache_budget"`
	CacheBudgetSource string         `json:"cache_budget_source"`
	Name              string         `json:"name"`
	Endpoint          string         `json:"endpoint"`
	Backend           SecretsBackend `json:"backend"`
	BackendSource     string         `json:"backend_source"`
	ConfigFile        string         `json:"config_file"`
	ConfigDir         string         `json:"config_dir"`
	CacheDir          string         `json:"cache_dir"`
	TokenPath         string         `json:"token_path"`
	Exists            bool           `json:"exists"`
	Current           bool           `json:"current"`
}

// ProfileStore persists profile settings and opens their secret vaults.
type ProfileStore interface {
	Names() ([]string, error)
	Settings(string) (ProfileSettings, error)
	Add(name, endpoint, backend string) error
	Vaults(string) ([]SecretVault, error)
	Remove(string) error
}

// Profiles manages local profiles without contacting a cloud.
type Profiles interface {
	List(context.Context) ([]ProfileSettings, error)
	Info(context.Context, string) (ProfileSettings, error)
	Add(ctx context.Context, name, endpoint, backend string) error
	Remove(context.Context, string) error
}

// Paths are the effective locations, whether or not they exist yet.
type Paths struct {
	ConfigFile   string `json:"config_file"`
	ConfigRoot   string `json:"config_root"`
	ProfileDir   string `json:"profile_dir"`
	TokenFile    string `json:"token_file"`
	CacheDir     string `json:"cache_dir"`
	ProfileCache string `json:"profile_cache"`
}

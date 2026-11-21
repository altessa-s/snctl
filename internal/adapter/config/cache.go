// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config

import (
	"fmt"
	"time"
)

// EnvCacheTTL overrides how long reads may trust the last listing.
const EnvCacheTTL = "SNCTL_CACHE_TTL"

func duration(key, value string) (time.Duration, error) {
	d, err := time.ParseDuration(value)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%w: %s = %q: expected a nonnegative duration", ErrConfig, key, value)
	}
	return d, nil
}

func checkCache(key string, c fileCache) error {
	if _, err := parseBudget(key+".budget", c.Budget); err != nil {
		return err
	}
	if c.TTL != nil {
		_, err := duration(key+".ttl", *c.TTL)
		return err
	}
	return nil
}

func cacheSettings(env map[string]string, f File, profile string) (time.Duration, bool, error) {
	ttl, offline := "0", false
	for _, c := range []fileCache{f.Cache, f.Profiles[profile].Cache} {
		if c.TTL != nil {
			ttl = *c.TTL
		}
		if c.Offline != nil {
			offline = *c.Offline
		}
	}
	key := "cache.ttl"
	if v := env[EnvCacheTTL]; v != "" {
		ttl, key = v, EnvCacheTTL
	}
	d, err := duration(key, ttl)
	return d, offline, err
}

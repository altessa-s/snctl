// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config

import (
	"strings"
	"testing"
	"time"
)

func TestCacheSettings(t *testing.T) {
	dirs := BaseDirs{Config: "/config", Cache: "/cache"}
	for _, tt := range []struct {
		toml, profile, envBudget, envTTL, source string
		budget                                   int64
		ttl                                      time.Duration
		offline                                  bool
	}{
		{profile: "work", source: SourceDefault, budget: DefaultCacheBudget},
		{toml: "[cache]\nbudget='2MiB'\nttl='5m'\noffline=true", profile: "work", source: SourceConfig, budget: 2 << 20, ttl: 5 * time.Minute, offline: true},
		{toml: "[cache]\nbudget='2MiB'\nttl='5m'\noffline=true\n[profiles.work.cache]\nbudget='0'\nttl='0'\noffline=false", profile: "work", source: SourceProfile},
		{toml: "[cache]\nbudget='2MiB'", profile: "work", envBudget: "3MiB", envTTL: "1m", source: SourceEnv, budget: 3 << 20, ttl: time.Minute},
	} {
		env := map[string]string{EnvCacheBudget: tt.envBudget, EnvCacheTTL: tt.envTTL}
		c, err := LoadFrom(env, tt.profile, dirs, func(string) ([]byte, bool, error) { return []byte(tt.toml), true, nil })
		if err != nil || c.Budget.Bytes != tt.budget || c.Budget.Source != tt.source || c.TTL != tt.ttl || c.Offline != tt.offline {
			t.Fatalf("%+v: %+v %v", tt, c, err)
		}
		if c.Budget.Unlimited() != (tt.budget == 0) {
			t.Fatal("unlimited")
		}
	}
	for _, v := range []string{"1", "0.5", "1KiB", "-1", "garbage", "999999999999999999999999999TiB"} {
		if _, err := Load(map[string]string{EnvCacheBudget: v}, "", dirs); err == nil || !strings.Contains(err.Error(), EnvCacheBudget) {
			t.Fatalf("%q: %v", v, err)
		}
	}
	for _, text := range []string{"[cache]\nttl='-1h'", "[cache]\nbudget='1'", "[profiles.work.cache]\nttl='bad'", "[profiles.work.cache]\nbudget='bad'", "[cache]\nother=1", "[cache]\nbudget=0"} {
		if _, err := ParseFile([]byte(text)); err == nil {
			t.Fatalf("accepted %s", text)
		}
	}
	if _, err := Load(map[string]string{EnvCacheTTL: "bad"}, "", dirs); err == nil || !strings.Contains(err.Error(), EnvCacheTTL) {
		t.Fatal(err)
	}
	for _, text := range []string{"1.5 GB", "1MiB", "0", "1024 KiB", "1TB", "1.2.3MiB", "MiB"} {
		_, _ = ParseSize(text)
	}
}

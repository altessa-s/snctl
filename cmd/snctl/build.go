// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/altessa-s/snctl/cmd/snctl/commands"
	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/internal/adapter/config"
	"github.com/altessa-s/snctl/internal/wiring"
)

// seams replace machine state in tests, including the login Keychain.
type seams struct {
	keychain wiring.KeychainItems
	env      map[string]string
	dirs     *config.BaseDirs
	streams  func(*commands.Streams)
}

func build(ctx context.Context, req commands.Request, errOut io.Writer, s seams) (commands.Services, func() error, error) {
	env := s.env
	if env == nil {
		env = config.Environ()
	}
	if len(req.ServiceEnv) > 0 {
		env = maps.Clone(env)
		for _, pair := range req.ServiceEnv {
			key, value, ok := strings.Cut(pair, "=")
			if !ok || !slices.Contains([]string{"SNCTL_CONFIG_DIR", "SNCTL_CACHE_DIR", "SNCTL_CONFIG_FILE", "SNCTL_PROFILE",
				"SNCTL_SECRETS", "SNCTL_KEYCHAIN_SERVICE", "SNCTL_CACHE_TTL", "SNCTL_CACHE_BUDGET"}, key) {
				return commands.Services{}, nil, cliutil.Usagef("invalid service environment variable")
			}
			env[key] = value
		}
	}
	var dirs config.BaseDirs
	if s.dirs != nil {
		dirs = *s.dirs
	} else if env[config.EnvConfigDir] == "" || env[config.EnvCacheDir] == "" {
		var err error
		dirs, err = config.SystemDirs()
		if err != nil {
			return commands.Services{}, nil, err
		}
	}
	var log io.Writer
	if req.Verbose {
		log = errOut
	}
	svc, err := wiring.Build(ctx, wiring.Options{Offline: req.Offline, NoCache: req.NoCache, ReadOnly: req.ReadOnly, Completion: req.Completion,
		Warning: func(message string) { _, _ = fmt.Fprintln(errOut, "snctl: warning: "+message) },
		Env:     env, Profile: req.Profile, Dirs: dirs, Keychain: s.keychain, Log: log})
	if err != nil {
		return commands.Services{}, nil, err
	}
	if req.Verbose {
		_, _ = io.WriteString(errOut, "snctl: local configuration loaded\n")
	}
	return commands.Services{Mirrors: svc.Mirrors, MirrorService: svc.MirrorService,
		Backups: svc.Backups, Offline: svc.Offline, Cache: svc.Cache, Syncer: svc.Syncer, Completion: svc.Completion,
		Profiles: svc.Profiles, Secrets: svc.Secrets, Paths: svc.Paths, Session: svc.Session,
		Files: svc.Files, Local: svc.Local, Tasks: svc.Tasks, Digests: svc.Digests, Feed: svc.Feed,
		Library: svc.Fetch, Fetch: svc.Fetch, Notebooks: svc.Notebooks, Check: svc.Check}, svc.Session.Close, nil
}

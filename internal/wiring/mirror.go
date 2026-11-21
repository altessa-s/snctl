// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wiring

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/altessa-s/go-atlas-core/time/clock"

	"github.com/altessa-s/snctl/internal/adapter/autostart"
	"github.com/altessa-s/snctl/internal/adapter/cloud"
	"github.com/altessa-s/snctl/internal/adapter/config"
	"github.com/altessa-s/snctl/internal/adapter/localfs"
	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/app/mirror"
	"github.com/altessa-s/snctl/internal/app/notebooks"
	"github.com/altessa-s/snctl/internal/port"
)

// mirrorConfig implements port.MirrorConfig over the [[mirror]] tables of
// config.toml.
type mirrorConfig struct {
	writer config.Writer
}

var _ port.MirrorConfig = mirrorConfig{}

// Mirrors implements port.MirrorConfig.
func (m mirrorConfig) Mirrors() ([]port.MirrorSpec, error) {
	tables, err := config.Mirrors(m.writer.Path, m.writer.Read)
	if err != nil {
		return nil, err
	}
	out := make([]port.MirrorSpec, 0, len(tables))
	for _, t := range tables {
		out = append(out, port.MirrorSpec(t))
	}
	return out, nil
}

// AddMirror implements port.MirrorConfig.
func (m mirrorConfig) AddMirror(spec port.MirrorSpec) error {
	return m.writer.AddMirror(config.Mirror(spec))
}

// RemoveMirror implements port.MirrorConfig.
func (m mirrorConfig) RemoveMirror(name string) (bool, error) {
	return m.writer.RemoveMirror(name)
}

// engines implements port.MirrorEngines: the run's own engine for its
// profile, and for any other the engine of a run of snctl -P <profile>,
// on a transport of its own.
type engines struct {
	opts    Options
	profile string
	own     port.MirrorEngine
}

var _ port.MirrorEngines = engines{}

// Open implements port.MirrorEngines.
func (e engines) Open(profile string) (port.MirrorEngine, func() error, error) {
	if profile == "" || profile == e.profile {
		return e.own, func() error { return nil }, nil
	}
	opts := e.opts
	opts.Profile, opts.ReadOnly = profile, false
	svc, err := Build(context.Background(), opts)
	if err != nil {
		return nil, nil, err
	}
	return svc.MirrorEngine, svc.Session.Close, nil
}

// serviceEnv are the variables of this run the mirror service keeps: where
// the configuration, the tokens and the cache are, and which hosts it talks
// to.
var serviceEnv = []string{ //nolint:gochecknoglobals // a fixed list
	config.EnvConfigDir, config.EnvCacheDir, config.EnvConfigFile, config.EnvProfile, config.EnvSecrets,
	config.EnvKeychainService, config.EnvCacheTTL, config.EnvCacheBudget,
}

// LogDir is the directory of the mirror service's log, below the
// configuration directory of every profile.
func LogDir(cfg config.Config) string { return filepath.Join(cfg.ConfigRoot, "logs") }

// mirrorService is the autostart service of the mirrors on this system,
// running this very snctl.
func mirrorService(cfg config.Config, opts Options, timer port.Timer) port.MirrorService {
	env := map[string]string{}
	for _, k := range serviceEnv {
		if v := opts.Env[k]; v != "" {
			env[k] = v
		}
	}
	env[config.EnvConfigFile] = cfg.ConfigFile
	env[config.EnvConfigDir] = cfg.ConfigRoot
	env[config.EnvCacheDir] = cfg.CacheDir
	env[config.EnvProfile] = cfg.Profile
	program, err := os.Executable()
	if err == nil {
		program = autostart.StableExecutable(program, func(p string) bool {
			_, serr := os.Stat(p)
			return serr == nil
		})
	}
	o := autostart.Options{GOOS: runtime.GOOS, LogDir: LogDir(cfg), ConfigHome: opts.Env["XDG_CONFIG_HOME"], Runner: opts.ServiceRunner}
	o.Home, _ = os.UserHomeDir() //nolint:errcheck // a missing home makes the definition's path fail to write, which says so
	if u, uerr := user.Current(); uerr == nil {
		o.UID, o.User = u.Uid, u.Username
	} else {
		o.UID = strconv.Itoa(os.Getuid())
	}
	if opts.ServiceHome != "" {
		o.Home = opts.ServiceHome
	}
	return mirror.NewAutostart(autostart.New(o), program, env, timer)
}

type mirrorTimer struct{}

func (mirrorTimer) Now() time.Time { return clock.Real().Now() }
func (mirrorTimer) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type mirrorConnections struct {
	client *cloud.Client
	note   notebooks.Service
}

func (m mirrorConnections) OpenMirrorConnection(ctx context.Context) (port.MirrorConnection, error) {
	remote, err := m.client.FreshSnapshot(ctx)
	if err != nil {
		return port.MirrorConnection{}, err
	}
	transfer := files.Service{Account: m.client, Cloud: remote, Local: localfs.Files{}, Disk: localfs.Files{}, Lock: m.client}
	note := m.note
	note.Fetch, note.Cloud = transfer, remote
	return port.MirrorConnection{Cloud: remote, Upload: transfer, Library: transfer, Notes: note}, nil
}

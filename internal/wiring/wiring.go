// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package wiring composes profile services and cloud adapters without network requests.
package wiring

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"slices"

	"github.com/altessa-s/go-atlas-core/time/clock"

	"github.com/altessa-s/snctl/internal/adapter/cache"
	"github.com/altessa-s/snctl/internal/adapter/cloud"
	"github.com/altessa-s/snctl/internal/adapter/config"
	"github.com/altessa-s/snctl/internal/adapter/fswatch"
	"github.com/altessa-s/snctl/internal/adapter/keychain"
	"github.com/altessa-s/snctl/internal/adapter/localfs"
	"github.com/altessa-s/snctl/internal/adapter/notes"
	"github.com/altessa-s/snctl/internal/adapter/opener"
	"github.com/altessa-s/snctl/internal/adapter/renderer"
	"github.com/altessa-s/snctl/internal/adapter/secretfile"
	"github.com/altessa-s/snctl/internal/adapter/vault"
	"github.com/altessa-s/snctl/internal/app/archives"
	"github.com/altessa-s/snctl/internal/app/check"
	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/app/mirror"
	"github.com/altessa-s/snctl/internal/app/notebooks"
	"github.com/altessa-s/snctl/internal/app/prefetch"
	"github.com/altessa-s/snctl/internal/app/profile"
	"github.com/altessa-s/snctl/internal/app/secrets"
	"github.com/altessa-s/snctl/internal/port"
)

// KeychainItems is the seam that keeps tests away from the login Keychain.
type KeychainItems = keychain.Items

// Options supplies every machine-specific input, including file reads.
type Options struct {
	ServiceRunner                 port.ServiceRunner
	ServiceHome                   string
	Context                       context.Context
	Offline                       *bool
	NoCache, ReadOnly, Completion bool
	Warning                       func(string)
	Env                           map[string]string
	Profile                       string
	Dirs                          config.BaseDirs
	Read                          config.ReadFunc
	Keychain                      KeychainItems
	// Log receives the cloud library's diagnostics; nil discards them.
	Log io.Writer
}

// Services are the local use cases of a configured run.
type Services struct {
	Mirrors       port.Mirrors
	MirrorEngine  port.MirrorEngine
	MirrorService port.MirrorService
	Backups       port.Backups
	Offline       bool
	Cache         port.CacheAdmin
	Syncer        port.Syncer
	Completion    port.Files
	cloudServices
	Profiles  port.Profiles
	Secrets   port.Secrets
	Paths     port.Paths
	Local     port.LocalFiles
	Fetch     files.Service
	Notebooks notebooks.Service
	Check     check.Service
}

// The cloud capabilities share one client and one transport lifetime.
type cloudServices struct {
	Session port.CloudSession
	Files   port.Files
	Tasks   port.Tasks
	Digests port.Digests
	Feed    port.Feed
}

// Build resolves settings without reading any secret values.
func Build(ctx context.Context, opts Options) (Services, error) {
	opts.Context = ctx
	if opts.Read == nil {
		opts.Read = config.ReadFile
	}
	cfg, err := config.LoadFrom(opts.Env, opts.Profile, opts.Dirs, opts.Read)
	if err != nil {
		return Services{}, err
	}
	if opts.Offline != nil {
		cfg.Offline = *opts.Offline
	}
	store := profileStore{opts: opts, cfg: cfg}
	vaults, err := store.Vaults(cfg.Profile)
	if err != nil {
		return Services{}, err
	}
	client, err := cloud.New(vaults[0], cfg.Endpoint, opts.Env["SNCTL_SOCKET_URL"], opts.Log)
	if err != nil {
		return Services{}, err
	}
	client.SetSyncLockState(localfs.SyncLockState{Path: filepath.Join(cfg.ConfigDir, "sync-lock")})
	var diskCache port.FileCache
	var cacheFailure error
	if !opts.NoCache {
		disk := cacheStore(cfg)
		if !opts.Completion {
			if cacheErr := disk.Reconcile(ctx); cacheErr != nil {
				if cfg.Offline {
					return Services{}, cacheErr
				}
				cacheFailure = cacheErr
				staleCtx, cancel := context.WithCancel(ctx)
				cancel()
				_ = disk.Invalidate(staleCtx)
				if opts.Log != nil {
					_, _ = fmt.Fprintf(opts.Log, "cache unavailable: %v\n", cacheErr)
				}
			}
		}
		diskCache = disk
	}
	ttl := cfg.TTL
	if !opts.ReadOnly {
		ttl = 0
	}
	clientCache := diskCache
	if cacheFailure != nil {
		clientCache = nil
	}
	client.SetCache(clientCache, cfg.Offline || opts.Completion, ttl, clock.Real().Now, opts.Warning)
	client.SetSessionCache(diskCache)
	completion, err := cloud.New(vaults[0], cfg.Endpoint, "", nil)
	if err != nil {
		return Services{}, err
	}
	completion.SetCache(diskCache, true, 0, clock.Real().Now, nil)
	fetch := files.Service{Account: client, Cloud: client, Local: localfs.Files{}, Disk: localfs.Files{}, Lock: client}
	var recognitionIndex port.RecognitionIndex
	if clientCache != nil {
		recognitionIndex, _ = clientCache.(port.RecognitionIndex)
	}
	noteService := notebooks.Service{Index: recognitionIndex, Fetch: fetch, Cloud: client, Files: localfs.Files{}, Reader: notes.Reader{},
		Renderer: renderer.Renderer{}, Opener: opener.New()}
	client.SetBodyObserver(noteService.ObserveBody)
	engine := mirror.NewEngine(mirror.Deps{Cloud: client, Upload: fetch, Library: fetch, Files: localfs.Files{}, Disk: localfs.Files{}, Notes: noteService,
		Feed: client, Dirs: fswatch.New(), Timer: mirrorTimer{}, Lock: client, Profile: cfg.Profile,
		Connections: mirrorConnections{client: client, note: noteService}})
	return Services{
		Mirrors: mirror.NewService(mirrorConfig{writer: config.Writer{Path: cfg.ConfigFile, Read: opts.Read, Env: opts.Env}},
			engines{opts: opts, profile: cfg.Profile, own: engine}, localfs.Files{}, localfs.Files{}, mirrorTimer{}),
		MirrorEngine: engine, MirrorService: mirrorService(cfg, opts, mirrorTimer{}),
		Backups: archives.Service{Cloud: client, Local: localfs.Files{}, Upload: fetch, Profile: cfg.Profile, Endpoint: cfg.Endpoint, Now: clock.Real().Now},
		Offline: cfg.Offline, Cache: cacheAdmin{store: store},
		Syncer:     prefetch.Service{Inspect: noteService.ObserveCached, InitError: cacheFailure, Cloud: client, Cache: diskCache, Local: localfs.Files{}},
		Completion: completion, Fetch: fetch, Notebooks: noteService,
		Session: client, Files: client, Tasks: client, Digests: client, Feed: client,
		Check: check.Service{Session: client, Files: client, Tasks: client, Digests: client, Feed: client,
			Probe: client, Local: localfs.Files{}}, Local: localfs.Files{},
		Profiles: profile.New(store, cfg.Profile),
		Secrets: secrets.New(cfg.Profile, cfg.SecretsSource, cfg.ConfigFile, vaults[0], vaults,
			config.Writer{Path: cfg.ConfigFile, Read: opts.Read, Env: opts.Env}),
		Paths: port.Paths{ConfigFile: cfg.ConfigFile,
			ConfigRoot:   cfg.ConfigRoot,
			ProfileDir:   cfg.ConfigDir,
			TokenFile:    cfg.TokenPath,
			CacheDir:     cfg.CacheDir,
			ProfileCache: filepath.Join(cfg.CacheDir, cfg.Profile)},
	}, nil
}

type profileStore struct {
	opts Options
	cfg  config.Config
}

func (p profileStore) config(name string) (config.Config, error) {
	return config.LoadFrom(p.opts.Env, name, p.opts.Dirs, p.opts.Read)
}

func (p profileStore) Names() ([]string, error) {
	dirs, err := config.ProfileDirs(p.cfg.ConfigRoot)
	if err != nil {
		return nil, err
	}
	names, err := config.TableProfiles(p.cfg.ConfigFile, p.opts.Read)
	if err != nil {
		return nil, err
	}
	if !p.opts.NoCache {
		cacheNames, cacheErr := config.CacheProfiles(p.cfg.CacheDir)
		if cacheErr != nil {
			return nil, cacheErr
		}
		for _, name := range cacheNames {
			cfg, err := p.config(name)
			if err != nil {
				return nil, err
			}
			status, err := cacheStore(cfg).Status()
			if err != nil {
				return nil, err
			}
			if status.Blobs > 0 || !status.Confirmed.IsZero() {
				names = append(names, name)
			}
		}
	}
	names = append(names, dirs...)
	slices.Sort(names)
	return slices.Compact(names), nil
}

func (p profileStore) Settings(name string) (port.ProfileSettings, error) {
	cfg, err := p.config(name)
	if err != nil {
		return port.ProfileSettings{}, err
	}
	exists, err := config.DirExists(cfg.ConfigDir)
	if err != nil {
		return port.ProfileSettings{}, err
	}
	var status port.CacheStatus
	if !p.opts.NoCache {
		disk := cacheStore(cfg)
		if cacheErr := disk.Reconcile(p.cacheContext()); cacheErr != nil {
			return port.ProfileSettings{}, cacheErr
		}
		status, err = disk.Status()
		if err != nil {
			return port.ProfileSettings{}, err
		}
	}
	return port.ProfileSettings{CacheBytes: status.Bytes, CacheBudget: cfg.Budget.Bytes, CacheBudgetSource: cfg.Budget.Source, Name: cfg.Profile,
		Endpoint:      cfg.Endpoint,
		Backend:       port.SecretsBackend(cfg.Secrets),
		BackendSource: cfg.SecretsSource,
		ConfigFile:    cfg.ConfigFile,
		ConfigDir:     cfg.ConfigDir,
		CacheDir:      filepath.Join(cfg.CacheDir, cfg.Profile),
		TokenPath:     cfg.TokenPath,
		Exists:        exists || cfg.ProfileTable || status.Blobs > 0 || !status.Confirmed.IsZero()}, nil
}

func (p profileStore) Add(name, endpoint, backend string) error {
	return (config.Writer{Path: p.cfg.ConfigFile, Read: p.opts.Read, Env: p.opts.Env}).AddProfile(name, endpoint, backend)
}

func (p profileStore) Vaults(name string) ([]port.SecretVault, error) {
	cfg, err := p.config(name)
	if err != nil {
		return nil, err
	}
	service := cfg.KeychainService
	if service == "" {
		service = keychain.DefaultService
	}
	key := keychain.New(service, cfg.Profile)
	if p.opts.Keychain != nil {
		key = keychain.NewWith(p.opts.Keychain, service, cfg.Profile)
	}
	file := secretfile.New(cfg.TokenPath)
	f, k := vault.New(file, port.SecretsFile, file.Path()), vault.New(key, port.SecretsKeychain, key.Location())
	if cfg.Secrets == config.BackendKeychain {
		return []port.SecretVault{k, f}, nil
	}
	return []port.SecretVault{f, k}, nil
}

func (p profileStore) Remove(name string) error {
	cfg, err := p.config(name)
	if err != nil {
		return err
	}
	if !p.opts.NoCache {
		if clearErr := cacheStore(cfg).Clear(p.cacheContext()); clearErr != nil {
			return clearErr
		}
	}
	if _, _, err = config.RemoveProfileDir(cfg); err != nil {
		return err
	}
	_, err = (config.Writer{Path: cfg.ConfigFile, Read: p.opts.Read, Env: p.opts.Env}).RemoveProfile(name)
	return err
}

func cacheStore(cfg config.Config) *cache.Store {
	return &cache.Store{Dir: filepath.Join(cfg.CacheDir, cfg.Profile), Profile: cfg.Profile,
		Budget: cfg.Budget.Bytes, Source: cfg.Budget.Source, Now: clock.Real().Now}
}

type cacheAdmin struct{ store profileStore }

func (a cacheAdmin) caches(ctx context.Context, all bool) ([]*cache.Store, error) {
	if a.store.opts.NoCache {
		return nil, fmt.Errorf("cache access is disabled by --no-cache")
	}
	names := []string{a.store.cfg.Profile}
	if all {
		others, err := a.store.Names()
		if err != nil {
			return nil, err
		}
		names = append(names, others...)
		slices.Sort(names)
		names = slices.Compact(names)
	}
	out := make([]*cache.Store, 0, len(names))
	for _, name := range names {
		cfg, err := a.store.config(name)
		if err != nil {
			return nil, err
		}
		disk := cacheStore(cfg)
		if err := disk.Reconcile(ctx); err != nil {
			return nil, err
		}
		out = append(out, disk)
	}
	return out, nil
}

func (a cacheAdmin) Status(ctx context.Context, all bool) ([]port.CacheStatus, error) {
	stores, err := a.caches(ctx, all)
	if err != nil {
		return nil, err
	}
	out := make([]port.CacheStatus, 0, len(stores))
	for _, disk := range stores {
		status, err := disk.Status()
		if err != nil {
			return nil, err
		}
		out = append(out, status)
	}
	return out, nil
}

func (a cacheAdmin) Clear(ctx context.Context, all bool) ([]string, error) {
	stores, err := a.caches(ctx, all)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(stores))
	for _, disk := range stores {
		if err := disk.Clear(ctx); err != nil {
			return names, err
		}
		names = append(names, disk.Profile)
	}
	return names, nil
}

func (p profileStore) cacheContext() context.Context {
	if p.opts.Context != nil {
		return p.opts.Context
	}
	return context.Background()
}

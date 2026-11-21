// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/internal/port"
)

// ProfileEnv names the profile when --profile is not given.
const ProfileEnv = "SNCTL_PROFILE"

// The persistent flags every command reads.
const (
	FlagProfile = "profile"
	FlagJSON    = "json"
	FlagQuiet   = "quiet"
	FlagVerbose = "verbose"
	FlagYes     = "yes"
	FlagFormat  = "format"
	FlagColor   = "color"
)

// Services are the use cases the command line drives. The command line
// imports nothing below the input ports.
type Services struct {
	Mirrors       port.Mirrors
	MirrorService port.MirrorService
	Backups       port.Backups
	Offline       bool
	Cache         port.CacheAdmin
	Syncer        port.Syncer
	Completion    port.Files
	Profiles      port.Profiles
	Secrets       port.Secrets
	Paths         port.Paths
	Session       port.CloudSession
	Files         port.Files
	Local         port.LocalFiles
	Tasks         port.Tasks
	Digests       port.Digests
	Feed          port.Feed
	Library       port.Library
	Fetch         port.Fetch
	Notebooks     port.Notebooks
	Check         port.Check
}

// Streams supplies the standard streams, terminal properties and environment.
type Streams struct {
	Now         func() time.Time
	In          io.Reader
	Out, Err    io.Writer
	IsTerminal  bool
	OutTerminal bool
	OutColor    bool
	Width       int
	Getenv      func(string) string
}

// Request selects the profile and verbosity once flags are parsed.
type Request struct {
	ServiceEnv []string
	Offline    *bool
	NoCache    bool
	ReadOnly   bool
	Completion bool
	Profile    string
	Verbose    bool
}

// Factory builds the services once the flags are parsed. The returned
// close releases what they hold.
type Factory func(ctx context.Context, req Request) (Services, func() error, error)

// Env is what every command shares: the streams, the persistent flags and
// the services, built on first use.
type Env struct {
	Streams    Streams
	factory    Factory
	completing bool

	built    bool
	services Services
	closer   func() error
	err      error
}

// NewEnv returns an environment that builds its services with factory.
func NewEnv(streams Streams, factory Factory) *Env {
	return &Env{Streams: streams, factory: factory}
}

// Fixed is a factory that always returns svc, for tests and embedding.
func Fixed(svc Services) Factory {
	return func(context.Context, Request) (Services, func() error, error) {
		return svc, func() error { return nil }, nil
	}
}

// Services builds the local services once with the selected profile.
func (e *Env) Services(cmd *cobra.Command) (Services, error) {
	if e.built {
		return e.services, e.err
	}
	e.built = true
	req := Request{Profile: Profile(cmd)}
	req.ServiceEnv, _ = cmd.Flags().GetStringArray("service-env")
	if len(req.ServiceEnv) > 0 {
		if Profile(cmd) == "" {
			req.Profile = ""
		}
		service, _ := cmd.Flags().GetBool("service")
		if !service {
			e.err = Usagef("--service-env requires --service")
			return e.services, e.err
		}
	}
	if req.Profile == "" {
		req.Profile = e.Streams.getenv(ProfileEnv)
	}
	if cmd.Flags().Changed("offline") {
		v, _ := cmd.Flags().GetBool("offline")
		req.Offline = &v
	}
	req.NoCache, _ = cmd.Flags().GetBool("no-cache")
	if req.NoCache && (cmd.CommandPath() == "snctl login" || cmd.CommandPath() == "snctl logout") {
		e.err = Usagef("remove --no-cache: session changes must clear the profile cache")
		return e.services, e.err
	}
	req.ReadOnly = offlineAllowed(cmd.CommandPath())
	req.Completion = e.completing
	req.Verbose, _ = cmd.Flags().GetBool(FlagVerbose)
	e.services, e.closer, e.err = e.factory(cmd.Context(), req)
	if e.err == nil && e.services.Offline && !req.ReadOnly && !req.Completion {
		e.err = Usagef("%s is unavailable offline", cmd.CommandPath())
	}
	if e.err == nil && req.Offline != nil && *req.Offline && !req.ReadOnly && !req.Completion {
		e.err = Usagef("%s is unavailable offline", cmd.CommandPath())
	}
	return e.services, e.err
}

// Close releases the services if they were built.
func (e *Env) Close() error {
	var errs []error
	for _, c := range []*func() error{&e.closer} {
		if *c == nil {
			continue
		}
		closer := *c
		*c = nil
		errs = append(errs, closer())
	}
	return errors.Join(errs...)
}

// Profile is --profile when explicitly supplied. Env resolves SNCTL_PROFILE.
func Profile(cmd *cobra.Command) string {
	if f := cmd.Flags().Lookup(FlagProfile); f != nil && f.Changed {
		return f.Value.String()
	}
	return ""
}

// DefaultProfile is the profile used when none is named.
const DefaultProfile = "default"

// ProfileName is [Profile], or DefaultProfile when none is named.
func ProfileName(cmd *cobra.Command) string {
	if p := Profile(cmd); p != "" {
		return p
	}
	return DefaultProfile
}

// Yes reports --yes.
func Yes(cmd *cobra.Command) bool {
	on, _ := cmd.Flags().GetBool(FlagYes) //nolint:errcheck // the flag is persistent on the root
	return on
}

// ErrNoService means a command needs a service the factory did not provide.
var ErrNoService = errors.New("snctl: this command is not available in this build")

// Need returns the service pick selects, building the services on first use;
// a service the factory left nil is [ErrNoService].
func Need[T any](cmd *cobra.Command, env *Env, pick func(Services) T) (T, error) {
	var zero T
	svc, err := env.Services(cmd)
	if err != nil {
		return zero, err
	}
	s := pick(svc)
	if any(s) == nil {
		return zero, ErrNoService
	}
	return s, nil
}

// AddPersistentFlags declares the flags every command reads on root, and
// checks the output flags of env before any command runs, so a bad --format
// is refused before a change is made rather than after.
func AddPersistentFlags(root *cobra.Command, env *Env) {
	flags := root.PersistentFlags()
	flags.StringP(FlagProfile, "P", "", "account profile (or "+ProfileEnv+")")
	flags.Bool("offline", false, "read cached listings and bodies without any request")
	flags.Bool("no-cache", false, "neither read nor write the cache on disk")
	flags.Bool(FlagQuiet, false, "hide progress lines")
	flags.Bool(FlagJSON, false, "print answers as JSON (the same as --format json)")
	flags.String(FlagFormat, "", "answer format: table, plain or json (or "+FormatEnv+"; default: table on a terminal, plain otherwise)")
	flags.String(FlagColor, ColorAuto, "color a table: auto, always or never")
	flags.BoolP(FlagVerbose, "v", false, "log what happens on standard error")
	flags.BoolP(FlagYes, "y", false, "confirm a command that changes settings or cloud data")
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		_, err := env.Output(cmd)
		return err
	}
}

// offlineAllowed is deliberately closed: new commands require an explicit decision.
func offlineAllowed(command string) bool {
	switch command {
	case "snctl mirror add", "snctl mirror ls", "snctl mirror rm", "snctl mirror service install",
		"snctl mirror service uninstall", "snctl mirror service status", "snctl mirror service logs",
		"snctl backup", "snctl ls", "snctl tree", "snctl stat", "snctl find", "snctl du", "snctl dupes", "snctl get", "snctl mget",
		"snctl render", "snctl open", "snctl text", "snctl highlights", "snctl note",
		"snctl profile ls", "snctl profile info", "snctl profile del", "snctl secrets status",
		"snctl debug paths", "snctl debug cache status", "snctl debug cache clear":
		return true
	default:
		return false
	}
}

// EffectiveProfile resolves the selected profile exactly as service construction does.
func (e *Env) EffectiveProfile(cmd *cobra.Command) string {
	name := Profile(cmd)
	if name == "" {
		name = e.Streams.getenv(ProfileEnv)
	}
	if name == "" {
		name = DefaultProfile
	}
	return name
}

// SelectedProfile is the explicitly supplied flag or environment profile, without a default.
func (e *Env) SelectedProfile(cmd *cobra.Command) string {
	if name := Profile(cmd); name != "" {
		return name
	}
	return e.Streams.getenv(ProfileEnv)
}

// ProgressStreams applies the command's output policy to progress only.
// Confirmation prompts, warnings and verbose commentary keep their streams.
func (e *Env) ProgressStreams(cmd *cobra.Command) Streams {
	s := e.Streams
	quiet, _ := cmd.Flags().GetBool(FlagQuiet) //nolint:errcheck // persistent flag
	localFormat := cmd.Flags().Lookup(FlagFormat)
	localJSON := localFormat != nil && localFormat != globalFlag(cmd, FlagFormat) && localFormat.Value.String() == "json"
	s.IsTerminal = s.IsTerminal && !e.JSON(cmd) && !localJSON && !quiet
	return s
}

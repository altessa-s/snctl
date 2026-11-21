// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wiring_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/config"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
	"github.com/altessa-s/snctl/internal/wiring"
)

type serviceRunner struct{ calls []string }

func (r *serviceRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return []byte("active\nstate = running\nRunning"), nil
}
func TestMirrorCompositionAndServiceRunner(t *testing.T) {
	t.Parallel()
	runner := &serviceRunner{}
	opts := wiring.Options{Dirs: config.BaseDirs{Config: t.TempDir(), Cache: t.TempDir()}, Keychain: &porttest.Items{},
		ServiceRunner: runner, ServiceHome: t.TempDir(), Env: map[string]string{"SNCTL_SECRETS": "file", "SNCTL_CACHE_TTL": "0", "SNCTL_CACHE_BUDGET": "0"}}
	svc, err := wiring.Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Session.Close()
	spec := port.MirrorSpec{Name: "papers", Dir: t.TempDir(), Folder: "/DOCUMENT/Document/Papers"}
	if _, err = svc.Mirrors.Add(t.Context(), spec); err != nil {
		t.Fatal(err)
	}
	states, err := svc.Mirrors.List(t.Context())
	if err != nil || len(states) != 1 {
		t.Fatal(states, err)
	}
	if err = svc.Mirrors.Run(t.Context(), port.MirrorSelection{All: true}, port.MirrorRunOptions{}, nil); err == nil {
		t.Fatal("unsigned profile reached cloud")
	}
	spec.Name = "work"
	spec.Dir = t.TempDir()
	spec.Profile = "work"
	spec.Folder = "/DOCUMENT/Document/Work"
	if _, err = svc.Mirrors.Add(t.Context(), spec); err != nil {
		t.Fatal(err)
	}
	if err = svc.Mirrors.Run(t.Context(), port.MirrorSelection{Names: []string{"work"}}, port.MirrorRunOptions{}, nil); err == nil {
		t.Fatal("unsigned profile reached cloud")
	}
	if _, err = svc.Mirrors.Remove(t.Context(), "papers"); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.MirrorService.Status(t.Context()); err != nil {
		t.Fatal(err)
	}
	info, err := svc.MirrorService.Install(t.Context())
	if err != nil || !info.Installed {
		t.Fatal(info, err)
	}
	definitionRoot := opts.ServiceHome
	if runtime.GOOS == "windows" {
		// Task Scheduler imports its XML from the service's log directory;
		// it has no per-user definition directory below ServiceHome.
		definitionRoot = filepath.Join(opts.Dirs.Config, "snctl", "logs")
		if info.File != filepath.Join(definitionRoot, "snctl-mirror.xml") {
			t.Fatal("unexpected task definition", info)
		}
	}
	if rel, relErr := filepath.Rel(definitionRoot, info.File); relErr != nil || !filepath.IsLocal(rel) {
		t.Fatal("definition outside fixture", info)
	}
	data, err := os.ReadFile(info.File)
	if err != nil {
		t.Fatal(err)
	}
	// All service-manager calls are recorded by the fake; only the temporary definition is real.
	if len(runner.calls) == 0 {
		t.Fatal("runner bypassed")
	}
	if filepath.Ext(info.File) != ".xml" && !strings.Contains(string(data), "SNCTL_CONFIG_FILE") {
		t.Fatal(string(data))
	}
	log, err := svc.MirrorService.OpenLog()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = log.Write([]byte("action\n")); err != nil {
		t.Fatal(err)
	}
	log.Close()
	var lines []string
	if err = svc.MirrorService.Log(t.Context(), 10, false, func(s string) { lines = append(lines, s) }); err != nil || len(lines) != 1 {
		t.Fatal(lines, err)
	}
	if _, err = svc.MirrorService.Uninstall(t.Context()); err != nil {
		t.Fatal(err)
	}
}

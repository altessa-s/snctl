// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// The layers of Clean Architecture, checked on the imports of every package.
// depguard in .golangci.yml keeps the same lines for production code; this
// test is the one that fails the build when a new package has no rule.

package snctl_test

import (
	"go/build"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/altessa-s/snctl/"

// The future cloud client stays outside the domain, ports and commands.
const transport = "github.com/altessa-s/sncloud-go"
const configFormat = "github.com/pelletier/go-toml/v2"

type rule struct {
	// allow are the packages of this module the layer may import, relative
	// to the module root.
	allow []string
	// forbid are outside packages it may not import.
	forbid []string
}

func useCase(allow ...string) rule {
	return rule{
		allow:  append([]string{"internal/domain", "internal/port"}, allow...),
		forbid: []string{"os", "net/http", "github.com/spf13/cobra", transport, configFormat},
	}
}

// adapter is an adapter that may import own, the one outside framework it
// stands on, and none of the others ("" for none).
func adapter(own string) rule {
	r := rule{allow: []string{"internal/domain", "internal/port"}, forbid: []string{"github.com/spf13/cobra"}}
	for _, framework := range []string{transport, configFormat, "github.com/altessa-s/snnote-go", "github.com/altessa-s/snrender-go"} {
		if framework != own {
			r.forbid = append(r.forbid, framework)
		}
	}
	return r
}

// command is a command package of the command line: the input ports and the
// model, and its own helpers.
func command() rule {
	return rule{
		allow:  []string{"internal/domain", "internal/port", "cmd/snctl/commands/cliutil", "cmd/snctl/commands/output"},
		forbid: []string{transport, configFormat, "github.com/altessa-s/snnote-go", "github.com/altessa-s/snrender-go"},
	}
}

var layers = map[string]rule{
	"internal/clouddouble":             {forbid: []string{transport, "github.com/spf13/cobra"}},
	"internal/domain":                  {forbid: []string{"os", "syscall", "log/slog", "net", "net/http", "io/fs", transport}},
	"internal/port":                    {allow: []string{"internal/domain"}, forbid: []string{"os", "net/http", transport}},
	"internal/app/archives":            useCase(),
	"internal/app/mirror":              useCase(),
	"internal/adapter/fswatch":         adapter(""),
	"internal/adapter/autostart":       adapter(""),
	"cmd/snctl/commands/mirror":        command(),
	"internal/app/prefetch":            useCase(),
	"internal/adapter/cache":           {allow: []string{"internal/domain", "internal/port", "internal/adapter/flock", "internal/adapter/fsdir"}, forbid: []string{transport, configFormat, "github.com/spf13/cobra"}},
	"internal/app/outdir":              useCase(),
	"internal/app/files":               useCase("internal/app/outdir"),
	"internal/app/notebooks":           useCase("internal/app/outdir"),
	"internal/adapter/notes":           adapter("github.com/altessa-s/snnote-go"),
	"internal/adapter/renderer":        adapter("github.com/altessa-s/snrender-go"),
	"internal/adapter/opener":          adapter(""),
	"cmd/snctl/commands/notebooks":     command(),
	"internal/app/check":               useCase(),
	"internal/app/profile":             useCase(),
	"internal/app/secrets":             useCase(),
	"internal/adapter/config":          {allow: []string{"internal/domain", "internal/adapter/flock", "internal/adapter/fsdir"}},
	"internal/adapter/cloud":           adapter(transport),
	"internal/adapter/localfs":         {allow: []string{"internal/domain", "internal/port", "internal/adapter/flock"}, forbid: adapter("").forbid},
	"internal/adapter/flock":           adapter(""),
	"internal/adapter/fsdir":           adapter(""),
	"internal/adapter/keychain":        {allow: []string{"internal/port", "internal/adapter/keychain/system"}},
	"internal/adapter/keychain/system": adapter(""),
	"internal/adapter/secretfile":      {allow: []string{"internal/domain", "internal/port", "internal/adapter/flock"}, forbid: adapter("").forbid},
	"internal/adapter/vault":           adapter(""),
	"internal/wiring":                  {allow: []string{"internal/app/mirror", "internal/adapter/fswatch", "internal/adapter/autostart", "internal/app/archives", "internal/app/prefetch", "internal/adapter/cache", "internal/app/files", "internal/app/notebooks", "internal/adapter/notes", "internal/adapter/renderer", "internal/adapter/opener", "internal/app/check", "internal/app/profile", "internal/app/secrets", "internal/adapter/cloud", "internal/adapter/localfs", "internal/adapter/config", "internal/adapter/flock", "internal/adapter/keychain", "internal/adapter/keychain/system", "internal/adapter/secretfile", "internal/adapter/vault", "internal/domain", "internal/port"}},
	"internal/porttest":                {allow: []string{"internal/port", "internal/domain"}},
	"cmd/snctl":                        {allow: []string{"cmd/snctl/commands", "cmd/snctl/commands/cliutil", "internal/adapter/config", "internal/wiring"}},
	"cmd/snctl/commands":               {allow: []string{"cmd/snctl/commands/mirror", "cmd/snctl/commands/notebooks", "cmd/snctl/commands/account", "cmd/snctl/commands/cloud", "cmd/snctl/commands/debug", "cmd/snctl/commands/version", "cmd/snctl/commands/cliutil"}},
	"cmd/snctl/commands/account":       command(),
	"cmd/snctl/commands/cloud":         command(),
	"cmd/snctl/commands/cliutil":       command(),
	"cmd/snctl/commands/clitest":       command(),
	"cmd/snctl/commands/output":        {forbid: []string{"os", "github.com/spf13/cobra", transport}},
	"cmd/snctl/commands/version":       command(),
	"cmd/snctl/commands/debug":         command(),
}

func forbidden(forbid []string, imp string) bool {
	return slices.ContainsFunc(forbid, func(f string) bool {
		return imp == f || (strings.Contains(f, ".") && strings.HasPrefix(imp, f+"/"))
	})
}

func TestLayers(t *testing.T) {
	t.Parallel()
	for name, rule := range layers {
		pkg, err := build.ImportDir(filepath.FromSlash(name), 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		checkNativeTests(t, name)

		for _, imp := range pkg.Imports {
			for lib, owner := range map[string]string{"github.com/altessa-s/snnote-go": "internal/adapter/notes", "github.com/altessa-s/snrender-go": "internal/adapter/renderer", "github.com/altessa-s/snspd-go": "internal/adapter/notes"} {
				if name != owner && forbidden([]string{lib}, imp) {
					t.Errorf("%s imports native library %s", name, imp)
				}
			}
			if name != "internal/adapter/fswatch" && forbidden([]string{"github.com/fsnotify/fsnotify"}, imp) {
				t.Errorf("%s imports directory watcher library %s", name, imp)
			}
			if name != "internal/adapter/cloud" && forbidden([]string{transport}, imp) {
				t.Errorf("%s imports cloud library %s", name, imp)
			}
			if own, ok := strings.CutPrefix(imp, module); ok {
				if !slices.Contains(rule.allow, own) {
					t.Errorf("%s imports %s", name, own)
				}
				continue
			}
			if forbidden(rule.forbid, imp) {
				t.Errorf("%s imports %s", name, imp)
			}
		}
	}
}

// Test support never reaches production: no production package imports the
// port doubles, the emulated cloud or the command doubles.
func TestTestSupportStaysInTests(t *testing.T) {
	t.Parallel()
	support := []string{"internal/porttest", "internal/clouddouble", "cmd/snctl/commands/clitest"}
	for name := range layers {
		if slices.Contains(support, name) {
			continue
		}
		pkg, err := build.ImportDir(filepath.FromSlash(name), 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range pkg.Imports {
			if own, ok := strings.CutPrefix(imp, module); ok && slices.Contains(support, own) {
				t.Errorf("%s imports test support %s", name, own)
			}
		}
	}
}

func TestEveryPackageHasALayer(t *testing.T) {
	t.Parallel()
	var dirs []string
	for _, pattern := range []string{"internal/*", "internal/app/*", "internal/adapter/*", "internal/adapter/*/*", "cmd/snctl", "cmd/snctl/commands", "cmd/snctl/commands/*"} {
		found, err := filepath.Glob(filepath.FromSlash(pattern))
		if err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, found...)
	}
	for _, dir := range dirs {
		name := filepath.ToSlash(dir)
		if name == "internal/app" || name == "internal/adapter" || strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "testdata") {
			continue
		}
		if _, ok := layers[name]; !ok {
			t.Errorf("%s has no rule in layers", name)
		}
	}
}

func checkNativeTests(t *testing.T, name string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(name, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if name == "cmd/snctl" && strings.HasSuffix(file, "integration_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range parsed.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			for lib, owner := range map[string]string{"github.com/altessa-s/snnote-go": "internal/adapter/notes", "github.com/altessa-s/snrender-go": "internal/adapter/renderer", "github.com/altessa-s/snspd-go": "internal/adapter/notes"} {
				if name != owner && forbidden([]string{lib}, path) {
					t.Errorf("%s imports native library %s", file, path)
				}
			}
		}
	}
}

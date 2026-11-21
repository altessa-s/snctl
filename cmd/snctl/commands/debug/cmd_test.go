// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package debug_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/clitest"
	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/debug"
	"github.com/altessa-s/snctl/internal/port"
)

func TestPaths(t *testing.T) {
	t.Parallel()
	build := func(e *cliutil.Env) []*cobra.Command { return []*cobra.Command{debug.New(e)} }
	r := clitest.Exec(t, cliutil.Services{Paths: port.Paths{ConfigFile: "/config/config.toml"}}, clitest.Options{}, build, "debug", "paths", "--json")
	if r.Error != nil || !strings.Contains(r.Out, "/config/config.toml") {
		t.Fatalf("%+v", r)
	}
	r = clitest.Exec(t, cliutil.Services{}, clitest.Options{Factory: func(context.Context, cliutil.Request) (cliutil.Services, func() error, error) {
		return cliutil.Services{}, nil, errors.New("broken")
	}}, build, "debug", "paths")
	if r.Code != 1 {
		t.Fatalf("%+v", r)
	}
}

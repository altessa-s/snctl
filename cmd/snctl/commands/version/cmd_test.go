// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package version_test

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/clitest"
	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/version"
)

func TestVersion(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"version"}, {"version", "--full", "--format", "json"}, {"version", "--full", "--format", "text"}} {
		r := clitest.Exec(t, cliutil.Services{}, clitest.Options{}, func(e *cliutil.Env) []*cobra.Command { return []*cobra.Command{version.New(e)} }, args...)
		if r.Error != nil || strings.TrimSpace(r.Out) == "" {
			t.Fatalf("%+v", r)
		}
	}
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package clitest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/clitest"
	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
)

func TestExecAndFake(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{}
	build := func(e *cliutil.Env) []*cobra.Command {
		return []*cobra.Command{{Use: "inspect", RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := e.Services(cmd)
			if err != nil {
				return err
			}
			_, err = svc.Profiles.List(cmd.Context())
			return err
		}}}
	}
	r := clitest.Exec(t, f.Services(), clitest.Options{Context: t.Context()}, build, "inspect")
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	failure := errors.New("close")
	r = clitest.Exec(t, f.Services(), clitest.Options{Factory: func(context.Context, cliutil.Request) (cliutil.Services, func() error, error) {
		return f.Services(), func() error { return failure }, nil
	}}, build, "inspect")
	if !errors.Is(r.Error, failure) {
		t.Fatal(r.Error)
	}
}

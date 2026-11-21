// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/internal/porttest"
)

type badWriter struct{}

func (badWriter) Write([]byte) (int, error) { return 0, errors.New("synthetic output failure") }

type badTerminal struct{ io.Reader }

func (badTerminal) Fd() uintptr { return ^uintptr(0) }

func TestPasswordAndCodeInput(t *testing.T) {
	t.Parallel()
	env := cliutil.NewEnv(cliutil.Streams{In: strings.NewReader(""), Out: io.Discard, Err: io.Discard}, nil)
	for _, input := range []string{"synthetic-password\n", "synthetic-password\r\n", "synthetic-password"} {
		value, err := readPassword(env, bufio.NewReader(strings.NewReader(input)), true)
		if err != nil || value != "synthetic-password" {
			t.Fatal("password line parsing failed")
		}
	}
	env.Streams.IsTerminal = true
	env.Streams.In = badTerminal{strings.NewReader("")}
	if value, err := readPassword(env, nil, false); err == nil || value != "" {
		t.Fatal("bad terminal accepted")
	}
	env.Streams.Err = badWriter{}
	if value, err := readPassword(env, nil, false); err == nil || value != "" {
		t.Fatal("prompt failure ignored")
	}
	var out bytes.Buffer
	env.Streams.Err = &out
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	f := &porttest.Cloud{}
	if err := verifyLogin(cmd, env, f, bufio.NewReader(strings.NewReader("123456\n")), ""); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Emailed code: " {
		t.Fatal("unexpected prompt")
	}
	if err := verifyLogin(cmd, env, f, bufio.NewReader(strings.NewReader("")), ""); err == nil {
		t.Fatal("code EOF accepted")
	}
	env.Streams.Err = badWriter{}
	if err := verifyLogin(cmd, env, f, bufio.NewReader(strings.NewReader("")), ""); err == nil {
		t.Fatal("code prompt failure ignored")
	}
	env.Streams.Err = &out
	p, done := cliutil.Progress(env.Streams, "transfer")
	p(1, 2)
	done()
	if !strings.Contains(out.String(), "50%") {
		t.Fatal("missing terminal progress")
	}
	if optionalText(new("comment")) != "comment" {
		t.Fatal("missing text")
	}
}

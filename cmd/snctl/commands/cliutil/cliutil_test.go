// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/internal/port"
)

func TestExitCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err  error
		want int
	}{
		{nil, CodeOK},
		{errors.New("x"), CodeError},
		{Usagef("bad %s", "args"), CodeUsage},
		{&ExitError{Code: 7}, 7},
		{ErrInterrupted, CodeInterrupted},
		{errors.New(`unknown command "x" for "snctl"`), CodeUsage},
		{errors.New("unknown flag: --x"), CodeUsage},
		{errors.New("unknown shorthand flag: 'q' in -q"), CodeUsage},
		{errors.New("flag needs an argument: -o"), CodeUsage},
		{errors.New(`invalid argument "n" for "--jobs" flag`), CodeUsage},
		{errors.New(`required flag(s) "output" not set`), CodeUsage},
		{errors.New(`the required flag(s) note`), CodeError},
	}
	for _, tc := range tests {
		if got := ExitCode(tc.err); got != tc.want {
			t.Errorf("ExitCode(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
	if Usage(nil) != nil {
		t.Fatal("Usage(nil) is not nil")
	}
	wrapped := &ExitError{Code: 2, Err: ErrDeclined}
	if !errors.Is(wrapped, ErrDeclined) || wrapped.Error() != ErrDeclined.Error() {
		t.Fatal("ExitError does not wrap")
	}
	if (&ExitError{Code: 5}).Error() != "exit status 5" {
		t.Fatal((&ExitError{Code: 5}).Error())
	}
}

func TestInterrupted(t *testing.T) {
	t.Parallel()
	if Interrupted(context.Background(), nil) != nil {
		t.Fatal("nil error changed")
	}
	plain := errors.New("x")
	if !errors.Is(Interrupted(context.Background(), plain), plain) {
		t.Fatal("unrelated error changed")
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(ErrInterrupted)
	if !errors.Is(Interrupted(ctx, context.Canceled), ErrInterrupted) {
		t.Fatal("interruption not named")
	}
}

func TestInterrupt(t *testing.T) {
	t.Parallel()
	sigs := make(chan os.Signal)
	var errOut syncBuffer
	var exited atomic.Int32
	exitCalled := make(chan struct{})
	ctx, stop := Interrupt(context.Background(), sigs, &errOut, func(code int) {
		exited.Store(int32(code)) //nolint:gosec // an exit code fits
		close(exitCalled)
	})
	sigs <- os.Interrupt
	<-ctx.Done()
	if !errors.Is(context.Cause(ctx), ErrInterrupted) || errOut.String() != InterruptNotice+"\n" {
		t.Fatalf("first signal: cause %v notice %q", context.Cause(ctx), errOut.String())
	}
	if exited.Load() != 0 {
		t.Fatal("exited on the first signal")
	}
	sigs <- os.Interrupt
	<-exitCalled
	if exited.Load() != CodeInterrupted {
		t.Fatalf("exit %d", exited.Load())
	}
	stop()
}

func TestInterruptStopWithoutSignal(t *testing.T) {
	t.Parallel()
	ctx, stop := Interrupt(context.Background(), make(chan os.Signal), &bytes.Buffer{}, func(int) { t.Error("exited") })
	stop()
	if ctx.Err() == nil || errors.Is(context.Cause(ctx), ErrInterrupted) {
		t.Fatalf("stop: %v", context.Cause(ctx))
	}
}

// syncBuffer is a buffer the interrupt goroutine and the test share.
type syncBuffer struct {
	mu  chan struct{}
	buf bytes.Buffer
}

func (b *syncBuffer) lock() {
	if b.mu == nil {
		b.mu = make(chan struct{}, 1)
	}
	b.mu <- struct{}{}
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.lock()
	defer func() { <-b.mu }()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.lock()
	defer func() { <-b.mu }()
	return b.buf.String()
}

func TestMaskName(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"":                "",
		"Diary":           "<name:5>",
		"Plan.pdf":        "<name:4>.pdf",
		"тетрадь.epub":    "<name:7>.epub",
		"a.verylongext1":  "<name:14>",
		"v1.2 notes":      "<name:10>",
		".hidden":         "<name:7>",
		"report.tar-gz":   "<name:13>",
		"x.PDF":           "<name:1>.PDF",
		"name.":           "<name:5>",
		"two.dots.notes1": "<name:8>.notes1",
	} {
		if got := MaskName(in); got != want {
			t.Errorf("MaskName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMaskJSON(t *testing.T) {
	t.Parallel()
	in := `{"visibleName":"Secret.pdf","source":"https://x","tags":[{"name":"work","timestamp":1730000000000123}],` +
		`"nested":{"visibleName":"deep"},"parent":"p","size":12345678901234567890,"empty":""}`
	got := string(MaskJSON([]byte(in)))
	for _, want := range []string{
		`"visibleName": "<name:6>.pdf"`, `"source": "<name:9>"`, `"name": "<name:4>"`, `"timestamp": 1730000000000123`,
		`"visibleName": "<name:4>"`, `"parent": "p"`, `"size": 12345678901234567890`, `"empty": ""`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
	if string(MaskJSON([]byte("Blank\n"))) != "Blank\n" {
		t.Fatal("non-JSON changed")
	}
}

func FuzzMaskJSON(f *testing.F) {
	f.Add([]byte(`{"visibleName":"Secret.pdf","tags":[{"name":"a"}]}`))
	f.Add([]byte(`[1,"x",{"source":["s"]}]`))
	f.Add([]byte("not json"))
	f.Fuzz(func(t *testing.T, data []byte) {
		out := MaskJSON(data)
		if bytes.Equal(out, data) {
			return
		}
		// What masking changed is JSON again, and stays JSON masked once more.
		if !json.Valid(out) || !json.Valid(MaskJSON(out)) {
			t.Fatalf("masked output is not JSON:\n%s", out)
		}
	})
}

func TestConfirm(t *testing.T) {
	t.Parallel()
	newCmd := func(yes bool) *cobra.Command {
		cmd := &cobra.Command{Use: "x"}
		AddPersistentFlags(cmd, NewEnv(Streams{}, Fixed(Services{})))
		args := []string{}
		if yes {
			args = append(args, "--yes")
		}
		if err := cmd.ParseFlags(args); err != nil {
			t.Fatal(err)
		}
		return cmd
	}
	env := NewEnv(Streams{In: strings.NewReader(""), Err: &bytes.Buffer{}}, nil)
	if Confirm(newCmd(true), env, "?") != nil {
		t.Fatal("--yes refused")
	}
	if ExitCode(Confirm(newCmd(false), env, "?")) != CodeUsage {
		t.Fatal("off a terminal without --yes")
	}
	tty := NewEnv(Streams{In: strings.NewReader(strings.Repeat("y", 300) + "\n"), Err: &bytes.Buffer{}, IsTerminal: true}, nil)
	if !errors.Is(Confirm(newCmd(false), tty, "?"), ErrDeclined) {
		t.Fatal("an overlong answer passed")
	}
}

func TestEnv(t *testing.T) {
	builds := 0
	closed := 0
	env := NewEnv(Streams{}, func(_ context.Context, req Request) (Services, func() error, error) {
		builds++
		if req.Profile != "work" || !req.Verbose {
			t.Errorf("%+v", req)
		}
		return Services{}, func() error { closed++; return nil }, nil
	})
	cmd := &cobra.Command{Use: "x"}
	AddPersistentFlags(cmd, NewEnv(Streams{}, Fixed(Services{})))
	t.Setenv(ProfileEnv, "work")
	if err := cmd.ParseFlags([]string{"--verbose"}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := env.Services(cmd); err != nil {
			t.Fatal(err)
		}
	}
	if builds != 1 {
		t.Fatalf("built %d times", builds)
	}
	if env.Close() != nil || env.Close() != nil || closed != 1 {
		t.Fatalf("closed %d times", closed)
	}
	if err := cmd.ParseFlags([]string{"--profile", "home"}); err != nil {
		t.Fatal(err)
	}
	if Profile(cmd) != "home" || ProfileName(cmd) != "home" {
		t.Fatal("--profile does not win over the environment")
	}
	t.Setenv(ProfileEnv, "")
	if bare := (&cobra.Command{}); ProfileName(bare) != DefaultProfile {
		t.Fatal("no profile is not the default one")
	}
	if NewEnv(Streams{}, nil).Close() != nil {
		t.Fatal("closing unbuilt services failed")
	}
}

func TestSecretMask(t *testing.T) {
	t.Parallel()
	in := []byte(`{"accessToken":"synthetic-one","account":{"email":"synthetic@example.test"},"nested":[{"clientID":"synthetic-two","password":"synthetic-three","Authorization":"synthetic-four"}]}`)
	got := string(MaskJSON(in))
	if strings.Contains(got, "synthetic") || strings.Count(got, "<redacted>") != 5 {
		t.Fatal(got)
	}
}

func TestTerminate(t *testing.T) {
	sigs := make(chan os.Signal, 2)
	var errOut bytes.Buffer
	ctx, stop := Interrupt(t.Context(), sigs, &errOut, func(int) { t.Error("forced exit") })
	defer stop()
	sigs <- syscall.SIGTERM
	<-ctx.Done()
	if ExitCode(Interrupted(ctx, context.Canceled)) != 143 {
		t.Fatal(context.Cause(ctx))
	}
	// The notice is written before the cancellation that ctx.Done observed.
	if got := errOut.String(); got != TerminateNotice+"\n" || strings.Contains(got, "Ctrl-C") {
		t.Fatal(got)
	}
}

func TestCountProgress(t *testing.T) {
	var out bytes.Buffer
	for _, terminal := range []bool{true, false} {
		p, done := CountProgress(Streams{Err: &out, IsTerminal: terminal}, "documents")
		if p != nil {
			p(0, 0)
			p(1, 2)
			p(2, 2)
		}
		done()
	}
	if !strings.Contains(out.String(), "2/2") || strings.Contains(out.String(), "bytes") {
		t.Fatal(out.String())
	}
}

func TestCancellationWinsJoinedErrors(t *testing.T) {
	for _, cancellation := range []struct {
		err  error
		code int
	}{{context.Canceled, 130}, {ErrInterrupted, 130}, {ErrTerminated, 143}} {
		for _, other := range []error{port.ErrWriteVerification, port.ErrRefused, port.ErrBusy, Usagef("bad flag")} {
			for _, joined := range []error{errors.Join(other, cancellation.err), errors.Join(cancellation.err, other), &ExitError{Code: 6, Err: errors.Join(other, cancellation.err)}} {
				if got := ExitCode(joined); got != cancellation.code {
					t.Fatalf("ExitCode(%v)=%d, want %d", joined, got, cancellation.code)
				}
			}
		}
	}
}

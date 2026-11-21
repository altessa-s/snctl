// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package cloud provides the cloud commands over input ports.
package cloud

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/port"

	"golang.org/x/term"
)

const pairArgs = 2

// Commands returns the cloud commands.
func Commands(env *cliutil.Env) []*cobra.Command {
	return append([]*cobra.Command{login(env), logout(env), account(env), tasks(env), digests(env), watch(env), backup(env), check(env)}, fileCommands(env)...)
}

func session(cmd *cobra.Command, env *cliutil.Env) (port.CloudSession, error) {
	return cliutil.Need(cmd, env, func(s cliutil.Services) port.CloudSession { return s.Session })
}

func success(cmd *cobra.Command, env *cliutil.Env, status string) error {
	return env.Emit(cmd, output.Result{JSON: struct {
		Status string `json:"status"`
	}{status}, Doc: output.Doc{output.Body{Text: status}}})
}

func login(env *cliutil.Env) *cobra.Command {
	var account, code string
	var stdin bool
	var country int
	cmd := &cobra.Command{Use: "login", Short: "Log in to this profile's cloud", GroupID: cliutil.GroupAccount,
		Long: `Log in to the official or private cloud selected by the profile and save its
session secrets. The password is read without echo at a terminal;
--password-stdin reads one line elsewhere. An emailed challenge is sent when
required; --code supplies its answer outside a terminal. The saved account
is reused unless --account is given. The answer on standard output is the
login status; prompts go to standard error. No confirmation is asked.`,
		Example: "snctl -P work login --account reader@example.com\nsnctl -P private login --account reader@example.com --password-stdin < password.txt",
		Args:    cliutil.Args(cobra.NoArgs)}
	cmd.Flags().StringVar(&account, "account", "", "email or telephone account (default: saved account)")
	cmd.Flags().StringVar(&code, "code", "", "emailed verification code")
	cmd.Flags().IntVar(&country, "country-code", 0, "numeric telephone country code")
	cmd.Flags().BoolVar(&stdin, "password-stdin", false, "read the password from one line of standard input")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		svc, err := session(cmd, env)
		if err != nil {
			return err
		}
		name := account
		if name == "" {
			name, _, err = svc.Account(cmd.Context())
			if err != nil {
				return err
			}
		}
		if name == "" {
			return cliutil.Usagef("login requires --account on the first login")
		}
		if cmd.Flags().Changed("country-code") && country <= 0 {
			return cliutil.Usagef("country code must be positive")
		}
		reader := bufio.NewReader(env.Streams.In)
		password, err := readPassword(env, reader, stdin)
		if err != nil {
			return err
		}
		var countries []int
		if country > 0 {
			countries = []int{country}
		}
		result, err := svc.LogIn(cmd.Context(), name, password, countries...)
		if err != nil {
			return err
		}
		if result.VerificationRequired {
			if err = verifyLogin(cmd, env, svc, reader, code); err != nil {
				return err
			}
		}
		return success(cmd, env, "logged in")
	}
	return cmd
}

func readLine(reader *bufio.Reader) (string, error) {
	const limit = 64 * 1024
	var b strings.Builder
	for b.Len() < limit {
		c, err := reader.ReadByte()
		if errors.Is(err, io.EOF) && b.Len() > 0 {
			return strings.TrimSuffix(b.String(), "\r"), nil
		}
		if err != nil {
			return "", errors.New("cannot read input")
		}
		if c == '\n' {
			return strings.TrimSuffix(b.String(), "\r"), nil
		}
		b.WriteByte(c)
	}
	return "", errors.New("input line too long")
}

func readPassword(env *cliutil.Env, reader *bufio.Reader, stdin bool) (string, error) {
	if stdin {
		return readLine(reader)
	}
	fd, ok := env.Streams.In.(interface{ Fd() uintptr })
	if !env.Streams.IsTerminal || !ok {
		return "", cliutil.Usagef("use --password-stdin outside a terminal")
	}
	if _, err := fmt.Fprint(env.Streams.Err, "Password: "); err != nil {
		return "", err
	}
	value, err := term.ReadPassword(int(fd.Fd())) //nolint:gosec // file descriptors fit int
	if _, writeErr := fmt.Fprintln(env.Streams.Err); writeErr != nil {
		return "", writeErr
	}
	if err != nil {
		return "", errors.New("cannot read password from terminal")
	}
	defer clear(value)
	return string(value), nil
}

func logout(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{Use: "logout", Short: "End the profile's cloud session", GroupID: cliutil.GroupAccount,
		Long: `End the selected profile's cloud session and clear its saved session and
cached account data. This acts directly, without a confirmation prompt. The
answer on standard output is the logout status; it never includes a token.`,
		Example: "snctl -P work logout",
		Args:    cliutil.Args(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := session(cmd, env)
			if err != nil {
				return err
			}
			if err = svc.LogOut(cmd.Context()); err != nil {
				return err
			}
			return success(cmd, env, "logged out")
		}}
}

func account(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{Use: "account", Short: "Show account, capacity and session expiry", GroupID: cliutil.GroupAccount,
		Long: `Show the selected profile's account, user name, cloud, used and total
capacity, and session expiry on standard output. No token or password is
printed. An opaque private-cloud token has no expiry claim. Nothing is
changed in the cloud.`,
		Example: "snctl -P work account\nsnctl -P work account --json",
		Args:    cliutil.Args(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := session(cmd, env)
			if err != nil {
				return err
			}
			info, err := svc.Info(cmd.Context())
			if err != nil {
				return err
			}
			return env.Emit(cmd, output.Result{JSON: info, Doc: output.Doc{output.Record{Fields: []output.Field{
				{Key: "account", Value: output.Text(info.Account)}, {Key: "user", Value: output.Text(info.User)},
				{Key: "cloud", Value: output.Text(info.Cloud)}, {Key: "used", Value: output.Text(output.HumanSize(info.Used))},
				{Key: "total", Value: output.Text(output.HumanSize(info.Total))}, {Key: "expiry", Value: output.Text(fmt.Sprint(info.Expiry))},
			}}}})
		}}
}

func verifyLogin(cmd *cobra.Command, env *cliutil.Env, svc port.Session, reader *bufio.Reader, code string) error {
	if err := svc.SendVerificationCode(cmd.Context()); err != nil {
		return err
	}
	if code == "" {
		if !env.Streams.IsTerminal {
			return cliutil.Usagef("verification requires --code outside a terminal")
		}
		if _, err := fmt.Fprint(env.Streams.Err, "Emailed code: "); err != nil {
			return err
		}
		value, err := readLine(reader)
		if err != nil {
			return err
		}
		code = value
	}
	return svc.Verify(cmd.Context(), code)
}

func mutate(cmd *cobra.Command, env *cliutil.Env, question, status string, run func(context.Context) error) error {
	if err := cliutil.Confirm(cmd, env, question); err != nil {
		return err
	}
	if err := run(cmd.Context()); err != nil {
		return err
	}
	return success(cmd, env, status)
}

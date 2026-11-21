// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/vault"
	"github.com/altessa-s/snctl/internal/clouddouble"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"

	sncloud "github.com/altessa-s/sncloud-go"
)

func TestFailureDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		err                error
		kind, code, status string
		definite           bool
	}{
		{&sncloud.RejectedError{Code: "E0300", Message: "synthetic-token https://storage.invalid/?signature=secret"}, "rejected", "E0300", "", false},
		{&sncloud.RejectedError{Code: "token", Message: "password"}, "rejected", "", "", false},
		{&sncloud.RejectedError{Code: "E0x00"}, "rejected", "", "", false},
		{&sncloud.NotFoundError{Code: "E0336"}, "notFound", "E0336", "", true},
		{&sncloud.AccountLockedError{Code: "E0709"}, "accountLocked", "E0709", "", true},
		{&sncloud.HTTPError{Status: 403, Path: "https://storage.invalid/?signature=secret"}, "http", "", "403", false},
		{&sncloud.HTTPError{Status: 500}, "http", "", "500", false},
		{sncloud.ErrAlreadyExists, "alreadyExists", "", "", true},
		{&sncloud.RejectedError{Code: "500", Message: "The system is temporarily unable to process your request."}, "rejected", "500", "", false},
		{&sncloud.TransportError{Reason: sncloud.ReasonConnectionLost}, "transport", "", "", false},
		{errors.New("password"), "", "", "", false},
	} {
		var out bytes.Buffer
		c := &Client{logger: slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))}
		if c.failure("upload", "NOTE/Note/a.pdf", tc.err) == nil {
			t.Fatal("lost error")
		}
		text := out.String()
		for _, secret := range []string{"synthetic-token", "https://storage.invalid/", "password", "signature=secret"} {
			if strings.Contains(text, secret) {
				t.Fatal("secret leaked")
			}
		}
		if tc.code != "" && !strings.Contains(text, "errorCode="+tc.code) {
			t.Fatal(text)
		}
		if tc.code == "" && strings.Contains(text, "errorCode=") {
			t.Fatal(text)
		}
		if tc.status != "" && !strings.Contains(text, "status="+tc.status) {
			t.Fatal(text)
		}
		if tc.kind != "" && !strings.Contains(text, "kind="+tc.kind) {
			t.Fatal(text)
		}
		if e, ok := errors.AsType[*sncloud.RejectedError](tc.err); ok && e.Code == "500" && !strings.Contains(text, "errorMsg=\"The system is temporarily unable") {
			t.Fatal(text)
		}
		if sncloud.KindOf(tc.err) == sncloud.KindTransport && !strings.Contains(text, "class=connectionLost") {
			t.Fatal(text)
		}
		if definiteUploadRefusal(tc.err) != tc.definite {
			t.Fatal(tc)
		}
	}
}

func TestBoundedMessage(t *testing.T) {
	if got := boundedMessage(strings.Repeat("界", 201)); got != strings.Repeat("界", 200) {
		t.Fatal(got)
	}
	for _, text := range []string{"Cookie: sid=private", "Authorization: Bearer private", "request body: private", "password=private"} {
		if got := boundedMessage(text); got != "[redacted]" {
			t.Fatal(got)
		}
	}
	if validCode("E") || validCode(strings.Repeat("E", 33)) {
		t.Fatal("unbounded code")
	}
}

// These cases pin every envelope mapping exposed by sncloud-go v1.0.0.
func TestDefiniteUploadRefusal(t *testing.T) {
	for _, tc := range []struct {
		code string
		err  error
		want bool
	}{
		{"E0712", sncloud.ErrSessionExpired, true},
		{"E0085", sncloud.ErrSessionExpired, true},
		{"E0019", sncloud.ErrInvalidCredentials, true},
		{"E0708", sncloud.ErrInvalidCredentials, true},
		{"E0711", sncloud.ErrInvalidCredentials, true},
		{"E0709", sncloud.ErrAccountLocked, true},
		{"E0710", sncloud.ErrAccountLocked, true},
		{"E0061", sncloud.ErrAccountLocked, true},
		{"E1760", sncloud.ErrVerificationRequired, true},
		{"E1756", sncloud.ErrInvalidVerificationCode, true},
		{"E0101", sncloud.ErrInvalidVerificationCode, true},
		{"E0322", sncloud.ErrAlreadyExists, true},
		{"E0335", sncloud.ErrAlreadyExists, true},
		{"E0337", sncloud.ErrAlreadyExists, true},
		{"E0338", sncloud.ErrAlreadyExists, true},
		{"E0081", sncloud.ErrNotFound, true},
		{"E0308", sncloud.ErrNotFound, true},
		{"E0311", sncloud.ErrNotFound, true},
		{"E0318", sncloud.ErrNotFound, true},
		{"E0320", sncloud.ErrNotFound, true},
		{"E0321", sncloud.ErrNotFound, true},
		{"E0328", sncloud.ErrNotFound, true},
		{"E0329", sncloud.ErrNotFound, true},
		{"E0336", sncloud.ErrNotFound, true},
		{"E0339", sncloud.ErrNotFound, true},
		{"E0340", sncloud.ErrNotFound, true},
		{"E0078", sncloud.ErrSyncLocked, true},
		{"E0079", sncloud.ErrSyncLocked, true},
		{"E0301", sncloud.ErrSyncLocked, true},
		{"E0007 folder collision", sncloud.ErrAlreadyExists, true},
		{"no login", sncloud.ErrNotLoggedIn, true},
		{"no challenge", sncloud.ErrNoVerificationPending, true},
		{"500", &sncloud.RejectedError{Code: "500"}, false},
		{"503", &sncloud.RejectedError{Code: "503"}, false},
		{"E9999", &sncloud.RejectedError{Code: "E9999"}, false},
		{"HTTP 500", &sncloud.HTTPError{Status: 500}, false},
		{"HTTP 403 unmapped", &sncloud.HTTPError{Status: 403}, false},
		{"HTTP 429", &sncloud.RateLimitedError{}, false},
		{"transport", &sncloud.TransportError{Reason: sncloud.ReasonConnectionLost}, false},
		{"malformed response", &sncloud.MalformedResponseError{}, false},
	} {
		t.Run(tc.code, func(t *testing.T) {
			if !strings.Contains(tc.code, " ") && (strings.HasPrefix(tc.code, "E") || tc.code == "500" || tc.code == "503") {
				server := clouddouble.New()
				defer server.Close()
				store := &porttest.Store{}
				if err := store.Save(t.Context(), port.SecretAccessToken, clouddouble.Token); err != nil {
					t.Fatal(err)
				}
				c, err := New(vault.New(store, port.SecretsFile, "synthetic"), server.URL(), "", nil)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				server.Inject(clouddouble.Fault{Path: "/api/file/2/files/list_folder", Count: 1, Code: tc.code})
				_, actual := c.device.List(t.Context(), "/", true)
				if sncloud.KindOf(actual) != sncloud.KindOf(tc.err) {
					t.Fatalf("kind=%s, want %s", sncloud.KindOf(actual), sncloud.KindOf(tc.err))
				}
				tc.err = actual
			}
			if got := definiteUploadRefusal(tc.err); got != tc.want {
				t.Fatalf("definite=%v, want %v", got, tc.want)
			}
		})
	}
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"errors"
	"fmt"
	"strings"

	"github.com/altessa-s/snctl/internal/domain"

	sncloud "github.com/altessa-s/sncloud-go"
)

// failure preserves bounded server diagnostics without logging transport URLs or raw errors.
func (c *Client) failure(operation, name string, err error) error {
	if err != nil && c.logger != nil {
		fields := []any{"operation", operation, "path", name, "kind", string(sncloud.KindOf(err))}
		var code string
		if e, ok := errors.AsType[*sncloud.RejectedError](err); ok {
			code = e.Code
			fields = append(fields, "errorMsg", boundedMessage(e.Message))
		}
		if e, ok := errors.AsType[*sncloud.NotFoundError](err); ok {
			code = e.Code
		}
		if e, ok := errors.AsType[*sncloud.AccountLockedError](err); ok {
			code = e.Code
		}
		if validCode(code) {
			fields = append(fields, "errorCode", code)
		}
		if e, ok := errors.AsType[*sncloud.HTTPError](err); ok {
			fields = append(fields, "status", e.Status)
		}
		if e, ok := errors.AsType[*sncloud.TransportError](err); ok {
			fields = append(fields, "class", string(e.Reason))
		} else if sncloud.KindOf(err) == "" {
			fields = append(fields, "class", fmt.Sprintf("%T", err))
		}
		c.logger.Debug("cloud failure", fields...)
	}
	return safe(err)
}

// boundedMessage keeps the server's explanation within a fixed character budget.
func boundedMessage(message string) string {
	const limit = 200
	runes := []rune(domain.MaskDiagnostic(message))
	return string(runes[:min(len(runes), limit)])
}

// validCode accepts short protocol identifiers, including numeric server errors.
func validCode(code string) bool {
	if code == "" || len(code) > 32 {
		return false
	}
	code = strings.TrimPrefix(code, "E")
	if code == "" {
		return false
	}
	return strings.IndexFunc(code, func(r rune) bool {
		return r < '0' || r > '9'
	}) < 0
}

// definiteUploadRefusal accepts only meanings established by the library.
// Unmapped envelopes and HTTP statuses remain uncertain, even when numeric.
func definiteUploadRefusal(err error) bool {
	for _, refusal := range []error{sncloud.ErrAlreadyExists, sncloud.ErrNotFound, sncloud.ErrNotLoggedIn,
		sncloud.ErrSessionExpired, sncloud.ErrInvalidCredentials, sncloud.ErrAccountLocked, sncloud.ErrSyncLocked,
		sncloud.ErrVerificationRequired, sncloud.ErrInvalidVerificationCode, sncloud.ErrNoVerificationPending} {
		if errors.Is(err, refusal) {
			return true
		}
	}
	return false
}

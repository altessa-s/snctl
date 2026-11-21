// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import "testing"

func TestMaskDiagnostic(t *testing.T) {
	for _, secret := range []string{"token=secret", "Password: secret", "Cookie: sid=secret", "Authorization: Bearer secret", "signature=secret", "https://host/?secret", "request body: secret", "request_body=secret"} {
		if got := MaskDiagnostic("fixed explanation\n" + secret); got != "fixed explanation\n[redacted]" {
			t.Fatal(got)
		}
	}
}

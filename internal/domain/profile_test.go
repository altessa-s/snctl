// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain_test

import (
	"errors"
	"testing"

	"github.com/altessa-s/snctl/internal/domain"
)

func TestEndpoint(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ in, want string }{{"", "official"}, {"official", "official"}, {"http://private.example/", "http://private.example"}, {"https://private.example/base/", "https://private.example/base"}} {
		got, err := domain.Endpoint(tt.in)
		if err != nil || got != tt.want {
			t.Fatalf("%q: %q, %v", tt.in, got, err)
		}
	}
	for _, in := range []string{"ftp://private.example", "//private.example", "https://", "http://user:password@private.example", "https://x/?token=synthetic", "https://x/#fragment", "https://x/?", "http://%zz", "file:///tmp/x"} {
		if _, err := domain.Endpoint(in); !errors.Is(err, domain.ErrEndpoint) {
			t.Errorf("accepted %q: %v", in, err)
		}
	}
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"testing"
	"time"
)

func TestElapsedIsRoundedForReading(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{0, "-"},
		{127083 * time.Nanosecond, "<1ms"},
		{282226500 * time.Nanosecond, "282ms"},
		{999 * time.Millisecond, "999ms"},
		{2093506625 * time.Nanosecond, "2.1s"},
		{59940 * time.Millisecond, "59.9s"},
		{82804579833 * time.Nanosecond, "1m23s"},
		{10 * time.Minute, "10m00s"},
	} {
		if got := elapsed(tc.d); got != tc.want {
			t.Errorf("elapsed(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

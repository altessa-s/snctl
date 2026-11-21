// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"iter"
)

// Change is a credential-free change-feed announcement.
type Change struct {
	Type string `json:"type"`
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Path string `json:"path"`
}

// Feed yields one announcement at a time until canceled.
type Feed interface {
	Watch(context.Context) iter.Seq2[Change, error]
}

// Probe supplies read-only checks of the library's web view.
type Probe interface{ WebList(context.Context) error }

// CheckStep is one measured self-test result.
type CheckStep struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Duration int64  `json:"duration_ns"`
	Detail   string `json:"detail"`
}

// Check runs a live self-test, writing only when explicitly requested.
type Check interface {
	Run(context.Context, bool, func(CheckStep) error) error
}

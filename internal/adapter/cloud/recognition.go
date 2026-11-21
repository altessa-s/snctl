// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"context"

	"github.com/altessa-s/snctl/internal/port"
)

// SetBodyObserver installs best-effort metadata discovery on verified reads.
// A true result also retains the inspected notebook through the body cache.
// The callback must not alter the body or its ownership.
func (c *Client) SetBodyObserver(observe func(context.Context, port.Entry, string) bool) {
	c.observeBody = observe
}

func (c *Client) observe(ctx context.Context, e port.Entry, file string) bool {
	return c.observeBody != nil && c.observeBody(ctx, e, file)
}

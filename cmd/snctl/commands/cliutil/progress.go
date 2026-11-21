// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/altessa-s/snctl/internal/port"
)

// percent is a whole.
const percent = 100

// Progress is a percent line on the error stream for a person at a
// terminal, rewritten in place; it is nil, and prints nothing, anywhere
// else. done finishes the line.
func Progress(streams Streams, label string) (progress port.Progress, done func()) {
	if !streams.IsTerminal {
		return nil, func() {}
	}
	var mu sync.Mutex
	w := streams.Err
	last := -1
	printed := false
	progress = func(n, total int64) {
		mu.Lock()
		defer mu.Unlock()
		pct := -1
		if total > 0 {
			pct = int(min(n*percent/total, percent))
		}
		if pct == last && pct >= 0 {
			return
		}
		last = pct
		printed = true
		writeProgress(w, label, n, pct)
	}
	return progress, func() {
		mu.Lock()
		defer mu.Unlock()
		if printed {
			_, _ = fmt.Fprintln(w) //nolint:errcheck // progress
		}
	}
}

func writeProgress(w io.Writer, label string, n int64, pct int) {
	if pct < 0 {
		_, _ = fmt.Fprintf(w, "\r%s %s", label, fmt.Sprintf("%d bytes", n)) //nolint:errcheck // progress
		return
	}
	filled := max(pct, 0) * barWidth / percent
	bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
	_, _ = fmt.Fprintf(w, "\r%s [%s] %3d%%", label, bar, pct) //nolint:errcheck // progress
}

// CountProgress keeps page and document counts separate from transfer byte rates.
func CountProgress(streams Streams, label string) (port.Progress, func()) {
	if !streams.IsTerminal {
		return nil, func() {}
	}
	var mu sync.Mutex
	line := newStatusLine(streams)
	update := func(n, total int64) {
		mu.Lock()
		defer mu.Unlock()
		pct := int64(0)
		if total > 0 {
			pct = min(n*percent/total, percent)
		}
		filled := int(pct) * barWidth / percent
		bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
		line.draw(fmt.Sprintf("%s [%s] %d/%d", label, bar, n, total))
	}
	return update, func() { mu.Lock(); defer mu.Unlock(); line.finish() }
}

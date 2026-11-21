// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import "io"

// LocalFiles handles local transfer staging and atomic destination replacement.
type LocalFiles interface {
	Open(string) (io.ReadCloser, error)
	Stage(io.Reader) (string, error)
	Remove(string) error
	Save(source, destination, name string, stdout io.Writer) error
}

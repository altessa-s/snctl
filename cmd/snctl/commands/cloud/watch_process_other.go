//go:build !unix && !windows

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import "os/exec"

func runCommand(cmd *exec.Cmd) error { return cmd.Run() }

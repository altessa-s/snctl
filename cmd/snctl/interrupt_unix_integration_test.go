// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build integration && !windows

package main

import (
	"os"
	"os/exec"
)

func prepareInterrupt(*exec.Cmd) {}

func interruptProcess(p *os.Process) error { return p.Signal(os.Interrupt) }

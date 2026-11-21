// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package cliutil shares command streams, output, confirmations, progress,
// help groups and exit handling. Answers go to standard output; commentary
// goes to standard error, so a pipe gets the answer without the progress.
package cliutil

// Help group identifiers shared by cloud and local notebook commands.
const (
	GroupAccount = "account"
	GroupRead    = "read"
	GroupWrite   = "write"
	GroupMixed   = "mixed"
	GroupLocal   = "local"
)

// Help titles describe the safety policy, independent of service boundaries.
const (
	TitleRead  = "Reading — these change nothing:"
	TitleWrite = "Writing — these change the cloud and ask first:"
)

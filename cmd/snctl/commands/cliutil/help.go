// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import "github.com/spf13/cobra"

// GroupReadWrite separates a command's readers from the children that ask
// before changing cloud data. Mixed and local children can override their group.
func GroupReadWrite(cmd *cobra.Command, readers ...string) {
	cmd.AddGroup(
		&cobra.Group{ID: GroupRead, Title: TitleRead},
		&cobra.Group{ID: GroupWrite, Title: TitleWrite},
	)
	for _, child := range cmd.Commands() {
		child.GroupID = GroupWrite
		for _, name := range readers {
			if child.Name() == name {
				child.GroupID = GroupRead
				break
			}
		}
	}
}

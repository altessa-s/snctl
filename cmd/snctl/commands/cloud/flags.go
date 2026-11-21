// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package cloud

import (
	"context"
	"path"
	"strings"

	"github.com/spf13/cobra"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
)

const columnFlags = "flags"

type fileView struct {
	port.Entry
	Flags []string `json:"flags"`
}

func activeFlags(e port.Entry) []domain.FileFlag {
	flags := []domain.FileFlag{}
	for _, flag := range domain.FileFlags() {
		var active bool
		switch flag.Name {
		case columnRecognition:
			active = !e.Folder && strings.HasSuffix(strings.ToLower(e.Name), ".note") && e.Recognition != nil && *e.Recognition
		case "highlights":
			active = e.Mark != nil
		}
		if active {
			flags = append(flags, flag)
		}
	}
	return flags
}

func viewFile(e port.Entry) fileView {
	v := fileView{Entry: e, Flags: []string{}}
	for _, flag := range activeFlags(e) {
		v.Flags = append(v.Flags, flag.Name)
	}
	return v
}

func viewFiles(entries []port.Entry) []fileView {
	views := make([]fileView, 0, len(entries))
	for _, e := range entries {
		views = append(views, viewFile(e))
	}
	return views
}

func flagsCell(e port.Entry) output.Cell {
	var b strings.Builder
	for _, flag := range activeFlags(e) {
		b.WriteString(flag.Letter)
	}
	return output.Text(b.String())
}

func emitFiles(cmd *cobra.Command, env *cliutil.Env, result output.Result, nonempty bool) error {
	o, err := env.Output(cmd)
	if err != nil {
		return err
	}
	if nonempty && o.Format == output.FormatTable {
		parts := make([]string, 0, len(domain.FileFlags()))
		for _, flag := range domain.FileFlags() {
			parts = append(parts, flag.Letter+" "+flag.Meaning)
		}
		cliutil.Notef(cmd.ErrOrStderr(), "Flags: %s", strings.Join(parts, ", "))
	}
	return env.Emit(cmd, result)
}

func statWithMark(ctx context.Context, cloud port.Files, query string) (port.Entry, string, error) {
	if snapshot, ok := cloud.(port.FileSnapshot); ok {
		var err error
		cloud, err = snapshot.Snapshot(ctx)
		if err != nil {
			return port.Entry{}, "", err
		}
	}
	e, err := cloud.Stat(ctx, query)
	if err != nil || e.Folder || !domain.DocumentName(e.Name) {
		return e, "", err
	}
	// Use the sibling listing to distinguish absence from offline cache misses.
	entries, err := cloud.List(ctx, path.Dir(e.Path), false)
	if err != nil {
		return e, "", err
	}
	mark, counts := port.SidecarOf(entries, e.Path)
	if counts.Files > 1 || counts.Files == 1 && e.Mark != nil {
		e.Mark = nil
		return e, e.Path + ": ambiguous annotation sidecar; mark metadata omitted", nil
	}
	if counts.Files == 1 {
		e.Mark = &mark
	}
	return e, "", nil
}

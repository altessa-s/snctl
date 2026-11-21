// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package cloud

import (
	"bytes"
	"slices"
	"testing"

	"github.com/altessa-s/snctl/cmd/snctl/commands/output"
	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func TestOrderedFlags(t *testing.T) {
	for _, tc := range []struct {
		e       port.Entry
		letters string
		names   []string
	}{
		{port.Entry{Name: "a.note", Recognition: new(true)}, "R", []string{"recognition"}},
		{port.Entry{Name: "a.note", Recognition: new(false)}, "", []string{}},
		{port.Entry{Name: "a.note"}, "", []string{}},
		{port.Entry{Name: "a.pdf", Mark: &port.Entry{}}, "H", []string{"highlights"}},
		{port.Entry{Name: "a.note", Recognition: new(true), Mark: &port.Entry{}}, "RH", []string{"recognition", "highlights"}},
		{port.Entry{Name: "a.pdf", Recognition: new(true)}, "", []string{}},
		{port.Entry{Name: "a.note", Folder: true, Recognition: new(true)}, "", []string{}},
		{port.Entry{Name: "a.NOTE", Recognition: new(true)}, "R", []string{"recognition"}},
		{port.Entry{Name: "a.note", Folder: true, Mark: &port.Entry{}}, "H", []string{"highlights"}},
	} {
		if got := flagsCell(tc.e).Text; got != tc.letters {
			t.Fatal(got, tc)
		}
		if got := viewFile(tc.e).Flags; got == nil || !slices.Equal(got, tc.names) {
			t.Fatal(got, tc)
		}
	}
}

func TestEveryFileFlagHasPredicate(t *testing.T) {
	// This entry satisfies every current predicate. A new vocabulary entry must
	// gain a predicate and a positive example here rather than silently disappear.
	active := activeFlags(port.Entry{Name: "all.note", Recognition: new(true), Mark: &port.Entry{}})
	for _, flag := range domain.FileFlags() {
		if !slices.Contains(active, flag) {
			t.Errorf("flag %q has no satisfied predicate", flag.Name)
		}
	}
}

func TestStatSidecarFoldersAndExistingMetadata(t *testing.T) {
	mark := port.Entry{Path: "one/a.pdf.mark"}
	for _, tc := range []struct {
		name     string
		sidecars []port.Entry
		existing bool
		wantMark bool
		warn     bool
	}{
		{name: "folder only", sidecars: []port.Entry{{Path: mark.Path, Folder: true}}},
		{name: "file and folder", sidecars: []port.Entry{mark, {Path: mark.Path, Folder: true}}, wantMark: true},
		{name: "existing metadata without sidecar", existing: true, wantMark: true},
		{name: "existing metadata and sidecar", sidecars: []port.Entry{mark}, existing: true, warn: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := port.Entry{Name: "a.pdf", Path: "one/a.pdf"}
			if tc.existing {
				doc.Mark = &mark
			}
			cloud := &porttest.Cloud{Entries: append([]port.Entry{doc}, tc.sidecars...)}
			got, warning, err := statWithMark(t.Context(), cloud, doc.Path)
			if err != nil || (got.Mark != nil) != tc.wantMark || (warning != "") != tc.warn {
				t.Fatalf("statWithMark = (%+v, %q, %v)", got, warning, err)
			}
		})
	}
}

func TestPlainHasNoFlagsField(t *testing.T) {
	entries := []port.Entry{
		{ID: "1", Name: "recognized.note", Recognition: new(true), Mark: &port.Entry{}},
		{ID: "2", Name: "marked.pdf", Mark: &port.Entry{}},
		{ID: "3", Name: "bare.pdf"},
	}
	for _, tc := range []struct {
		name        string
		ids         bool
		recognition bool
		want        string
	}{
		{"default", false, false, "-\t-\t-\trecognized.note\n-\t-\t-\tmarked.pdf\n-\t-\t-\tbare.pdf\n"},
		{"ids", true, false, "-\t-\t-\trecognized.note\t1\n-\t-\t-\tmarked.pdf\t2\n-\t-\t-\tbare.pdf\t3\n"},
		{"recognition", false, true, "-\t-\t-\trecognized.note\tyes\n-\t-\t-\tmarked.pdf\t-\n-\t-\t-\tbare.pdf\t-\n"},
		{"ids and recognition", true, true, "-\t-\t-\trecognized.note\t1\tyes\n-\t-\t-\tmarked.pdf\t2\t-\n-\t-\t-\tbare.pdf\t3\t-\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := (output.PlainRenderer{}).Render(&out, fileResult(entries, "", tc.ids, tc.recognition), output.Style{}); err != nil {
				t.Fatal(err)
			}
			if out.String() != tc.want {
				t.Fatalf("plain listing = %q, want %q (no flags field)", out.String(), tc.want)
			}
		})
	}
	// Tree names keep their indentation, with only explicitly requested IDs
	// and the existing recognition field following the name.
	entries = []port.Entry{
		{ID: "1", Name: "folder", Path: "folder", Folder: true},
		{ID: "2", Name: "recognized.note", Path: "folder/recognized.note", Recognition: new(true)},
		{ID: "3", Name: "marked.pdf", Path: "folder/marked.pdf", Mark: &port.Entry{}},
	}
	for _, tc := range []struct {
		name        string
		ids         bool
		recognition bool
		want        string
	}{
		{"tree", false, false, "folder\n  recognized.note\n  marked.pdf\n"},
		{"tree ids", true, false, "folder\t1\n  recognized.note\t2\n  marked.pdf\t3\n"},
		{"tree recognition", false, true, "folder\t-\n  recognized.note\tyes\n  marked.pdf\t-\n"},
		{"tree ids and recognition", true, true, "folder\t1\t-\n  recognized.note\t2\tyes\n  marked.pdf\t3\t-\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, plain := treeNodes(entries, "", tc.ids, tc.recognition)
			var out bytes.Buffer
			if err := (output.PlainRenderer{}).Render(&out, output.Result{PlainDoc: output.Doc{output.Tree{Nodes: plain}}}, output.Style{}); err != nil {
				t.Fatal(err)
			}
			if out.String() != tc.want {
				t.Fatalf("plain tree = %q, want %q (no flags field)", out.String(), tc.want)
			}
		})
	}
}

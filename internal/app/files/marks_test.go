// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package files_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

type removalChoices struct {
	*porttest.Drive
	permanent []bool
}

func (c *removalChoices) Delete(ctx context.Context, entry port.Entry, permanent bool) error {
	c.permanent = append(c.permanent, permanent)
	return c.Drive.Delete(ctx, entry, permanent)
}

func TestBothRemovalChoicesAndUnannotatedConflict(t *testing.T) {
	const source = "DOCUMENT/Document/a.pdf"
	for _, permanent := range []bool{false, true} {
		d := &removalChoices{Drive: porttest.NewDrive(&porttest.NotebookFiles{}, map[string]string{source: "pdf", source + ".mark": "mark"})}
		// The wrapper owns Delete; avoid the embedded Drive's snapshot returning
		// the Drive and bypassing our observation seam.
		s := files.Service{Cloud: struct{ port.Files }{d}}
		if _, err := s.RemoveWithMarks(t.Context(), source, permanent, port.ChangeOptions{}); err != nil {
			t.Fatal(err)
		}
		if len(d.permanent) != 2 || d.permanent[0] != permanent || d.permanent[1] != permanent {
			t.Fatal(d.permanent)
		}
	}
	d := porttest.NewDrive(&porttest.NotebookFiles{}, map[string]string{source: "pdf", "DOCUMENT/Document/b.pdf.mark": "orphan"})
	if _, err := (files.Service{Cloud: d}).MoveWithMarks(t.Context(), source, "DOCUMENT/Document/b.pdf", false, port.ChangeOptions{}); !errors.Is(err, port.ErrConflict) || d.Count("move") != 0 {
		t.Fatal(err, d.Calls)
	}
}

func TestDocumentChanges(t *testing.T) {
	for _, operation := range []string{"move", "rename", "recycle", "delete"} {
		for _, ext := range []string{".pdf", ".epub"} {
			for _, mode := range []string{"success", "dry", "decline", "document-failure", "mark-failure", "orphan-target", "document-target"} {
				t.Run(operation+ext+mode, func(t *testing.T) {
					source, target := "DOCUMENT/Document/a"+ext, "DOCUMENT/Document/b"+ext
					d := porttest.NewDrive(&porttest.NotebookFiles{}, map[string]string{source: "pdf", source + ".mark": "mark"})
					moving := operation == "move" || operation == "rename"
					if mode == "orphan-target" || mode == "document-target" {
						if !moving {
							t.Skip("move conflict")
						}
						name := target + ".mark"
						if mode == "document-target" {
							name = target
						}
						d.Seed(name, "occupied")
					}
					verb := "delete"
					if moving {
						verb = "move"
					}
					failure := errors.New("injected failure")
					if mode == "document-failure" {
						d.Fail[verb+" "+source] = failure
					}
					if mode == "mark-failure" {
						d.Fail[verb+" "+source+".mark"] = failure
					}
					confirmed := false
					o := port.ChangeOptions{DryRun: mode == "dry", Confirm: func(plan []port.FileChange) error {
						confirmed = true
						if len(plan) != 2 || plan[0].Entry.Path != source || plan[1].Entry.Path != source+".mark" {
							t.Fatal(plan)
						}
						if d.Count(verb) != 0 {
							t.Fatal("wrote before confirmation")
						}
						if mode == "decline" {
							return failure
						}
						return nil
					}}
					s := files.Service{Cloud: d}
					var plan []port.FileChange
					var err error
					if moving {
						plan, err = s.MoveWithMarks(t.Context(), source, target, operation == "rename", o)
					} else {
						plan, err = s.RemoveWithMarks(t.Context(), source, operation == "delete", o)
					}
					wantErr := mode != "success" && mode != "dry"
					if (err != nil) != wantErr {
						t.Fatalf("%v: %v", plan, err)
					}
					if mode == "mark-failure" {
						if !errors.Is(err, failure) || len(plan) != 1 || !strings.Contains(err.Error(), "but "+source+".mark was not") {
							t.Fatal(plan, err)
						}
						if _, ok := d.Files[source]; ok {
							t.Fatal("document did not change")
						}
						if _, ok := d.Files[source+".mark"]; !ok {
							t.Fatal("mark disappeared")
						}
					}
					if mode == "dry" && (confirmed || len(plan) != 2 || d.Count(verb) != 0) {
						t.Fatal(plan, d.Calls)
					}
					if mode == "success" && (d.Count(verb) != 2 || !confirmed) {
						t.Fatal(d.Calls)
					}
					if d.Count("snapshot") != 1 || d.Count("download") != 0 {
						t.Fatal(d.Calls)
					}
				})
			}
		}
	}
}

func TestChangePlanningFailuresAndRawMarks(t *testing.T) {
	source := "DOCUMENT/Document/a.pdf"
	for _, fail := range []string{"snapshot", "stat " + source, "list", "stat " + source + ".mark", "stat DOCUMENT/Document/b.pdf.mark"} {
		d := porttest.NewDrive(&porttest.NotebookFiles{}, map[string]string{source: "pdf", source + ".mark": "mark"})
		d.Fail[fail] = errors.New("failure")
		s := files.Service{Cloud: d}
		if _, err := s.MoveWithMarks(t.Context(), source, "DOCUMENT/Document/b.pdf", false, port.ChangeOptions{}); err == nil {
			t.Fatal(fail)
		}
		if fail != "stat DOCUMENT/Document/b.pdf.mark" && fail != "stat "+source+".mark" {
			if _, err := s.RemoveWithMarks(t.Context(), source, false, port.ChangeOptions{}); err == nil {
				t.Fatal(fail)
			}
		}
		if d.Count("move")+d.Count("delete") != 0 {
			t.Fatal(d.Calls)
		}
	}
	d := porttest.NewDrive(&porttest.NotebookFiles{}, map[string]string{source: "pdf", source + ".mark": "mark", "DOCUMENT/Document/sub/seed": "seed"})
	s := files.Service{Cloud: d}
	if _, err := s.MoveWithMarks(t.Context(), source, "DOCUMENT/Document/sub/a.pdf", true, port.ChangeOptions{}); !errors.Is(err, port.ErrArgument) {
		t.Fatal(err)
	}
	plan, err := s.MoveWithMarks(t.Context(), source+".mark", "DOCUMENT/Document/sub", false, port.ChangeOptions{})
	if err != nil || len(plan) != 1 || plan[0].Destination != "DOCUMENT/Document/sub/a.pdf.mark" {
		t.Fatal(plan, err)
	}
	plan, err = s.RemoveWithMarks(t.Context(), source, false, port.ChangeOptions{})
	if err != nil || len(plan) != 1 {
		t.Fatal(plan, err)
	}
	d.Seed(source+".mark", "orphan")
	plan, err = s.RemoveWithMarks(t.Context(), source+".mark", true, port.ChangeOptions{})
	if err != nil || len(plan) != 1 {
		t.Fatal(plan, err)
	}
	// A folder with a sidecar-looking name is never carried or hidden.
	d.Seed(source, "pdf")
	d.Folders[source+".mark"] = true
	plan, err = s.RemoveWithMarks(t.Context(), source, false, port.ChangeOptions{DryRun: true})
	if err != nil || len(plan) != 1 {
		t.Fatal(plan, err)
	}
}

func TestDocumentChangesRejectAmbiguousSiblings(t *testing.T) {
	doc := port.Entry{ID: "doc", Name: "X.pdf", Path: "folder/X.pdf"}
	mark := port.Entry{ID: "mark", Name: "X.pdf.mark", Path: doc.Path + ".mark"}
	for _, duplicate := range []port.Entry{doc, mark,
		{Name: doc.Name, Path: doc.Path, Folder: true},
		{Name: mark.Name, Path: mark.Path, Folder: true}} {
		for _, operation := range []string{"mv", "rm", "rm-permanent"} {
			t.Run(duplicate.Name+"/"+operation, func(t *testing.T) {
				cloud := &porttest.Cloud{Entries: []port.Entry{{Path: "folder", Folder: true}, doc, mark, duplicate}}
				s := files.Service{Cloud: cloud}
				o := port.ChangeOptions{Confirm: func([]port.FileChange) error { t.Fatal("confirmed ambiguous plan"); return nil }}
				var err error
				if operation == "mv" {
					_, err = s.MoveWithMarks(t.Context(), doc.Path, "folder/new.pdf", false, o)
				} else {
					_, err = s.RemoveWithMarks(t.Context(), doc.Path, operation == "rm-permanent", o)
				}
				if !errors.Is(err, port.ErrConflict) {
					t.Fatal(err)
				}
				for _, call := range cloud.Calls {
					if call == "move" || call == "delete" {
						t.Fatal("wrote ambiguous plan", cloud.Calls)
					}
				}
			})
		}
	}
}

func TestMoveAcrossDocumentExtensions(t *testing.T) {
	for _, rename := range []bool{false, true} {
		t.Run(map[bool]string{false: "move", true: "rename"}[rename], func(t *testing.T) {
			d := porttest.NewDrive(&porttest.NotebookFiles{}, map[string]string{"folder/X.txt": "text", "folder/X.pdf.mark": "orphan"})
			s := files.Service{Cloud: d}
			if _, err := s.MoveWithMarks(t.Context(), "folder/X.txt", "folder/X.pdf", rename, port.ChangeOptions{}); !errors.Is(err, port.ErrConflict) || d.Count("move") != 0 {
				t.Fatal(err, d.Calls)
			}
			d.Seed("folder/doc.pdf", "pdf")
			d.Seed("folder/doc.pdf.mark", "annotations")
			plan, err := s.MoveWithMarks(t.Context(), "folder/doc.pdf", "folder/doc.txt", rename, port.ChangeOptions{})
			if err != nil || len(plan) != 2 || plan[1].Destination != "folder/doc.txt.mark" || string(d.Files["folder/doc.txt.mark"]) != "annotations" {
				t.Fatal(plan, err, d.Calls)
			}
		})
	}
}

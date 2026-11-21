// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package files_test

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

type racingDrive struct {
	*porttest.Drive
	fresh     []string
	after     func(string)
	failFresh int
}

func (d *racingDrive) Snapshot(context.Context) (port.Files, error) { return d, nil }
func (d *racingDrive) FreshSnapshot(ctx context.Context) (port.Files, error) {
	_, err := d.ListFresh(ctx, "/", true)
	return d, err
}
func (d *racingDrive) ListFresh(ctx context.Context, parent string, recursive bool) ([]port.Entry, error) {
	d.fresh = append(d.fresh, parent)
	if len(d.fresh) == d.failFresh {
		return nil, errors.New("synthetic listing failure")
	}
	return d.List(ctx, parent, recursive)
}
func (d *racingDrive) Upload(ctx context.Context, local, name string, p port.Progress) (port.Entry, error) {
	e, err := d.Drive.Upload(ctx, local, name, p)
	if err == nil && d.after != nil {
		d.after(name)
	}
	return e, err
}
func (d *racingDrive) Mkdir(ctx context.Context, name string) (port.Entry, error) {
	e, err := d.Drive.Mkdir(ctx, name)
	if err == nil && d.after != nil {
		d.after(name)
	}
	return e, err
}
func (d *racingDrive) Move(ctx context.Context, e port.Entry, name string) error {
	err := d.Drive.Move(ctx, e, name)
	if err == nil && d.after != nil {
		d.after(name)
	}
	return err
}

const raceParent = "DOCUMENT/Document"
const raceSource = raceParent + "/source.txt"
const raceTarget = raceParent + "/й.txt"
const raceTwin = raceParent + "/и\u0306.txt"

func raceService() (files.Service, *racingDrive) {
	disk := &porttest.NotebookFiles{Data: map[string][]byte{"local.txt": []byte("new"), filepath.Join("in", "й.pdf"): []byte("new")}}
	drive := &racingDrive{Drive: porttest.NewDrive(disk, map[string]string{raceSource: "old"})}
	return files.Service{Cloud: drive, Disk: disk, Lock: drive}, drive
}

// Each public write path must compare after Confirm (and inside the upload
// lock). A target created while the prompt is open must cause zero writes.
func TestRecheckEveryWritePath(t *testing.T) {
	for _, op := range []string{"put", "replace", "mput", "restore", "mkdir", "mkdir-p", "mv", "rename", "rm"} {
		t.Run(op, func(t *testing.T) {
			s, d := raceService()
			target, twin := raceTarget, raceTwin
			if op == "rm" {
				d.Seed(target, "old")
			}
			if op == "replace" {
				d.Seed(target, "old")
			}
			if op == "mput" {
				target, twin = raceParent+"/й.pdf", raceParent+"/и\u0306.pdf"
			}
			mutate := func() { d.Seed(twin, "other client") }
			changes := port.ChangeOptions{Confirm: func([]port.FileChange) error { mutate(); return nil }}
			uploads := port.UploadOptions{Replace: op == "replace", Confirm: func(port.TransferReport) error { mutate(); return nil }}
			var err error
			switch op {
			case "put", "replace":
				_, err = s.Put(t.Context(), port.PutRequest{Sources: []port.PutSource{{Path: "local.txt"}}, Destination: target}, uploads)
			case "mput":
				_, err = s.Upload(t.Context(), "in", raceParent, uploads)
			case "restore":
				_, err = s.RestoreFiles(t.Context(), []port.RestoreFile{{Path: target, Local: "local.txt"}}, uploads)
			case "mkdir", "mkdir-p":
				_, err = s.Mkdir(t.Context(), target, op == "mkdir-p", changes)
			case "mv", "rename":
				_, err = s.MoveWithMarks(t.Context(), raceSource, target, op == "rename", changes)
			case "rm":
				_, err = s.RemoveWithMarks(t.Context(), target, false, changes)
			}
			if !errors.Is(err, port.ErrConflict) || mutationWrites(d.Drive) != 0 || !strings.Contains(err.Error(), twin) {
				t.Fatalf("%s: %v; calls %v", op, err, d.Calls)
			}
			if len(d.fresh) != 1 {
				t.Fatalf("fresh listings = %v, want one parent", d.fresh)
			}
		})
	}
}

func TestRecheckSourceAndSidecar(t *testing.T) {
	for _, op := range []string{"mv", "rename", "rm"} {
		for _, change := range []string{"source ID", "source moved", "sidecar changed", "sidecar added", "destination sidecar"} {
			t.Run(op+"/"+change, func(t *testing.T) {
				s, d := raceService()
				source, target := raceParent+"/paper.pdf", raceParent+"/new.pdf"
				d.Seed(source, "old")
				if change == "sidecar changed" {
					d.Seed(source+".mark", "old mark")
				}
				o := port.ChangeOptions{Confirm: func([]port.FileChange) error {
					switch change {
					case "source ID":
						// Moving away and seeding again allocates a different identifier.
						e, _ := d.Stat(t.Context(), source)
						if err := d.Drive.Move(t.Context(), e, source+".old"); err != nil {
							return err
						}
						d.Seed(source, "old")
					case "source moved":
						d.Drop(source)
					case "sidecar changed", "sidecar added":
						d.Seed(source+".mark", "new mark")
					case "destination sidecar":
						if op == "rm" {
							d.Seed(source+".mark", "new mark")
						} else {
							d.Seed(target+".mark", "new mark")
						}
					}
					return nil
				}}
				var err error
				if op == "rm" {
					_, err = s.RemoveWithMarks(t.Context(), source, false, o)
				} else {
					_, err = s.MoveWithMarks(t.Context(), source, target, op == "rename", o)
				}
				ownMoves := 0
				if change == "source ID" {
					ownMoves = 1
				}
				if !errors.Is(err, port.ErrConflict) || mutationWrites(d.Drive) != ownMoves {
					t.Fatalf("%v; %v", err, d.Calls)
				}
			})
		}
	}
}

func TestVerifyEveryCreateAndMove(t *testing.T) {
	for _, op := range []string{"put", "replace", "mkdir", "mv", "rename"} {
		t.Run(op, func(t *testing.T) {
			s, d := raceService()
			if op == "replace" {
				d.Seed(raceTarget, "old")
			}
			d.after = func(string) { d.Seed(raceTwin, "other client") }
			var err error
			switch op {
			case "put", "replace":
				_, err = s.Put(t.Context(), port.PutRequest{Sources: []port.PutSource{{Path: "local.txt"}}, Destination: raceTarget}, port.UploadOptions{Replace: op == "replace"})
			case "mkdir":
				_, err = s.Mkdir(t.Context(), raceTarget, false)
			case "mv", "rename":
				_, err = s.MoveWithMarks(t.Context(), raceSource, raceTarget, op == "rename", port.ChangeOptions{})
			}
			if !errors.Is(err, port.ErrWriteVerification) || !errors.Is(err, port.ErrConflict) {
				t.Fatalf("error = %v", err)
			}
			e, statErr := d.Stat(t.Context(), raceTarget)
			twin, _ := d.Stat(t.Context(), raceTwin)
			if statErr != nil || !strings.Contains(err.Error(), fmt.Sprintf("%q (ID %s)", e.Path, e.ID)) || !strings.Contains(err.Error(), fmt.Sprintf("%q (ID %s)", twin.Path, twin.ID)) || !strings.Contains(err.Error(), "no rollback") {
				t.Fatalf("%+v; %v; %v", e, err, d.Calls)
			}
			want := 2
			if op == "mv" || op == "rename" {
				want++ // One targeted parent check immediately before the move.
			}
			if d.Count("delete") != 0 || len(d.fresh) != want {
				t.Fatalf("calls %v; fresh %v", d.Calls, d.fresh)
			}
		})
	}
}

func TestVerificationFailureAndDryRunRequests(t *testing.T) {
	for _, op := range []string{"put", "mkdir", "mv", "rename", "rm"} {
		for _, mode := range []string{"success", "dry", "pre-list failure", "post-list failure"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				s, d := raceService()
				if mode == "pre-list failure" {
					d.failFresh = 1
				}
				if mode == "post-list failure" && op != "rm" {
					d.failFresh = 2
					if op == "mv" || op == "rename" {
						d.failFresh++
					}
				}
				dry := mode == "dry"
				var err error
				switch op {
				case "put":
					_, err = s.Put(t.Context(), port.PutRequest{Sources: []port.PutSource{{Path: "local.txt"}}, Destination: raceTarget}, port.UploadOptions{DryRun: dry})
				case "mkdir":
					_, err = s.Mkdir(t.Context(), raceTarget, false, port.ChangeOptions{DryRun: dry})
				case "mv", "rename":
					_, err = s.MoveWithMarks(t.Context(), raceSource, raceTarget, op == "rename", port.ChangeOptions{DryRun: dry})
				case "rm":
					_, err = s.RemoveWithMarks(t.Context(), raceSource, false, port.ChangeOptions{DryRun: dry})
				}
				want := 2
				if op == "rm" || mode == "pre-list failure" {
					want = 1
				}
				if mode != "pre-list failure" && (op == "mv" || op == "rename" || op == "rm") {
					want++
				}
				if dry {
					want = 0
				}
				if len(d.fresh) != want {
					t.Fatalf("fresh %v; want %d", d.fresh, want)
				}
				switch {
				case mode == "pre-list failure":
					if !errors.Is(err, port.ErrConflict) || mutationWrites(d.Drive) != 0 {
						t.Fatalf("%v; %v", err, d.Calls)
					}
				case mode == "post-list failure" && op != "rm":
					if !errors.Is(err, port.ErrWriteVerification) || !strings.Contains(err.Error(), "write happened") || !strings.Contains(err.Error(), "warning") || mutationWrites(d.Drive) != 1 {
						t.Fatalf("%v; %v", err, d.Calls)
					}
				default:
					if err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestNonDocumentExactSelection(t *testing.T) {
	for _, op := range []string{"rm", "mv", "rename"} {
		for _, exact := range []bool{false, true} {
			for _, folder := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/exact=%t/folder=%t", op, exact, folder), func(t *testing.T) {
					s, d := raceService()
					d.Seed(raceTarget, "one")
					d.Seed(raceTwin, "two")
					// Folder removal uses the same selection and re-check as files.
					if folder {
						d.Drop(raceTarget)
						d.Drop(raceTwin)
						d.Folders[raceTarget] = true
						d.Folders[raceTwin] = true
					}
					o := port.ChangeOptions{Exact: exact, DryRun: true, Confirm: func([]port.FileChange) error { t.Fatal("dry-run confirmed"); return nil }}
					var plan []port.FileChange
					var err error
					if op == "rm" {
						plan, err = s.RemoveWithMarks(t.Context(), raceTarget, false, o)
					} else {
						plan, err = s.MoveWithMarks(t.Context(), raceTarget, raceParent+"/new.txt", op == "rename", o)
					}
					if exact {
						if err != nil || len(plan) != 1 || plan[0].Entry.Path != raceTarget {
							t.Fatalf("%v; %v", plan, err)
						}
					} else if !errors.Is(err, port.ErrConflict) || !strings.Contains(err.Error(), fmt.Sprintf("%q", raceTwin)) || !strings.Contains(err.Error(), "ID ") || !strings.Contains(err.Error(), "--exact") {
						t.Fatal(err)
					}
					if mutationWrites(d.Drive) != 0 || len(d.fresh) != 0 {
						t.Fatal(d.Calls, d.fresh)
					}
				})
			}
		}
	}
	// Exercise real writes, proving the equivalent sibling survives.
	for _, op := range []string{"rm", "mv", "rename"} {
		s, d := raceService()
		d.Seed(raceTarget, "one")
		d.Seed(raceTwin, "two")
		var err error
		if op == "rm" {
			_, err = s.RemoveWithMarks(t.Context(), raceTarget, false, port.ChangeOptions{Exact: true})
		} else {
			_, err = s.MoveWithMarks(t.Context(), raceTarget, raceParent+"/new.txt", op == "rename", port.ChangeOptions{Exact: true})
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err = d.Stat(t.Context(), raceTwin); err != nil {
			t.Fatal("twin changed", err)
		}
	}
}

func TestExactWithoutExactMatch(t *testing.T) {
	s, d := raceService()
	d.Seed(raceTwin, "two")
	// This fake models the adapter's unique NFC fallback for Stat.
	cloud := &nfcFallback{racingDrive: d}
	s.Cloud = cloud
	for _, op := range []string{"rm", "mv", "rename"} {
		var err error
		if op == "rm" {
			_, err = s.RemoveWithMarks(t.Context(), raceTarget, false, port.ChangeOptions{Exact: true})
		} else {
			_, err = s.MoveWithMarks(t.Context(), raceTarget, raceParent+"/new.txt", op == "rename", port.ChangeOptions{Exact: true})
		}
		if !errors.Is(err, port.ErrNotFound) || mutationWrites(d.Drive) != 0 {
			t.Fatal(op, err, d.Calls)
		}
	}
}

type nfcFallback struct{ *racingDrive }

func (d *nfcFallback) Snapshot(context.Context) (port.Files, error) { return d, nil }
func (d *nfcFallback) Stat(ctx context.Context, name string) (port.Entry, error) {
	if path.Base(name) == "й.txt" {
		return d.Drive.Stat(ctx, raceTwin)
	}
	return d.Drive.Stat(ctx, name)
}

func mutationWrites(d *porttest.Drive) int {
	return d.Count("mkdir") + d.Count("upload") + d.Count("move") + d.Count("delete")
}

func TestVerifyAllDocumentMembers(t *testing.T) {
	s, d := raceService()
	source, target := raceParent+"/source.pdf", raceParent+"/й.pdf"
	d.Seed(source, "doc")
	d.Seed(source+".mark", "mark")
	d.after = func(name string) { d.Seed(strings.ReplaceAll(name, "й", "и\u0306"), "raced") }
	plan, err := s.MoveWithMarks(t.Context(), source, target, false, port.ChangeOptions{})
	if len(plan) != 2 || !errors.Is(err, port.ErrWriteVerification) || strings.Count(err.Error(), "NFC-equivalent entries") != 2 {
		t.Fatal(plan, err)
	}
	for _, name := range []string{target, target + ".mark", strings.ReplaceAll(target, "й", "и\u0306"), strings.ReplaceAll(target, "й", "и\u0306") + ".mark"} {
		e, statErr := d.Stat(t.Context(), name)
		if statErr != nil || !strings.Contains(err.Error(), fmt.Sprintf("%q (ID %s)", name, e.ID)) {
			t.Fatal(name, e, statErr, err)
		}
	}
	if len(d.fresh) != 4 || d.Count("move") != 2 || d.Count("delete") != 0 {
		t.Fatal(d.fresh, d.Calls)
	}
}

func TestVerifyRecursiveUploadAndRestore(t *testing.T) {
	for _, op := range []string{"mput", "restore"} {
		t.Run(op, func(t *testing.T) {
			s, d := raceService()
			d.Disk.Data[filepath.Join("in", "й", "й.pdf")] = []byte("nested")
			d.after = func(name string) {
				if strings.Contains(path.Base(name), "й") {
					d.Seed(path.Join(path.Dir(name), strings.ReplaceAll(path.Base(name), "й", "и\u0306")), "raced")
				}
			}
			var r port.TransferReport
			var err error
			if op == "mput" {
				r, err = s.Upload(t.Context(), "in", raceParent, port.UploadOptions{Jobs: 1})
			} else {
				r, err = s.RestoreFiles(t.Context(), []port.RestoreFile{
					{Local: filepath.Join("in", "й.pdf"), Path: raceParent + "/й.pdf"},
					{Local: filepath.Join("in", "й", "й.pdf"), Path: raceParent + "/й/й.pdf"},
				}, port.UploadOptions{Jobs: 1})
			}
			if !errors.Is(err, port.ErrWriteVerification) || strings.Count(err.Error(), "NFC-equivalent entries") != 3 {
				t.Fatal(r, err, d.Calls)
			}
			if d.Count("upload") != 2 || d.Count("mkdir") != 1 || d.Count("delete") != 0 {
				t.Fatal(d.Calls)
			}
			if len(d.fresh) != 2 || d.fresh[0] != "/" || d.fresh[1] != "/" {
				t.Fatal(d.fresh)
			}
		})
	}
}

func TestRecheckUnrelatedSiblingAndParentIdentity(t *testing.T) {
	for _, parentChanged := range []bool{false, true} {
		s, d := raceService()
		o := port.ChangeOptions{Confirm: func([]port.FileChange) error {
			if parentChanged {
				delete(d.Folders, raceParent)
			} else {
				d.Seed(raceParent+"/unrelated.txt", "other")
			}
			return nil
		}}
		_, err := s.Mkdir(t.Context(), raceTarget, false, o)
		if parentChanged {
			if !errors.Is(err, port.ErrConflict) || mutationWrites(d.Drive) != 0 {
				t.Fatal(err, d.Calls)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

func TestExactStillRefusesDocumentAndSidecarTwins(t *testing.T) {
	for _, twinKind := range []string{"document", "sidecar"} {
		for _, op := range []string{"rm", "mv", "rename"} {
			s, d := raceService()
			source := raceParent + "/й.pdf"
			d.Seed(source, "doc")
			d.Seed(source+".mark", "mark")
			twin := raceParent + "/и\u0306.pdf"
			if twinKind == "sidecar" {
				twin += ".mark"
			}
			d.Seed(twin, "twin")
			o := port.ChangeOptions{Exact: true, Confirm: func([]port.FileChange) error { t.Fatal("confirmed ambiguous pairing"); return nil }}
			var err error
			if op == "rm" {
				_, err = s.RemoveWithMarks(t.Context(), source, false, o)
			} else {
				_, err = s.MoveWithMarks(t.Context(), source, raceParent+"/new.pdf", op == "rename", o)
			}
			if !errors.Is(err, port.ErrConflict) || !strings.Contains(err.Error(), fmt.Sprintf("%q", twin)) || mutationWrites(d.Drive) != 0 {
				t.Fatal(op, twinKind, err, d.Calls)
			}
		}
	}
}

func TestUploadRefusesTwinsInItsOwnPlan(t *testing.T) {
	s, d := raceService()
	d.Disk.Data[filepath.Join("in", "и\u0306.pdf")] = []byte("twin")
	_, err := s.Upload(t.Context(), "in", raceParent, port.UploadOptions{Confirm: func(port.TransferReport) error { t.Fatal("confirmed ambiguous upload plan"); return nil }})
	if !errors.Is(err, port.ErrConflict) || mutationWrites(d.Drive) != 0 {
		t.Fatal(err, d.Calls)
	}
}

func TestWriteBatchTracksMovesAndDeletes(t *testing.T) {
	s, d := raceService()
	err := s.WithWriteBatch(t.Context(), func(ctx context.Context) error {
		if _, err := s.Mkdir(ctx, raceParent+"/new/child", true); err != nil {
			return err
		}
		_, err := s.Put(ctx, port.PutRequest{Sources: []port.PutSource{{Path: "local.txt"}}, Destination: raceParent + "/new/file.txt"}, port.UploadOptions{})
		if err != nil {
			return err
		}
		if err = s.Move(ctx, raceParent+"/new/file.txt", raceParent+"/new/moved.txt"); err != nil {
			return err
		}
		file, err := d.Stat(ctx, raceParent+"/new/moved.txt")
		if err != nil {
			return err
		}
		return s.Remove(ctx, file, false)
	})
	if err != nil || len(d.fresh) != 4 {
		t.Fatal(err, d.fresh, d.Calls)
	}
	if _, err := d.Stat(t.Context(), raceParent+"/new/moved.txt"); !errors.Is(err, port.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestWriteBatchRecheckFailure(t *testing.T) {
	s, d := raceService()
	d.failFresh = 1
	err := s.WithWriteBatch(t.Context(), func(ctx context.Context) error { _, err := s.Mkdir(ctx, raceTarget, false); return err })
	if !errors.Is(err, port.ErrConflict) || mutationWrites(d.Drive) != 0 || len(d.fresh) != 1 {
		t.Fatal(err, d.fresh, d.Calls)
	}
}

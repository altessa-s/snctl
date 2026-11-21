// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package cloud_test

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/cmd/snctl/commands"
	"github.com/altessa-s/snctl/cmd/snctl/commands/clitest"
	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/internal/app/files"
	"github.com/altessa-s/snctl/internal/app/notebooks"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func markedServices() (commands.Services, *porttest.Drive) {
	disk := &porttest.NotebookFiles{}
	d := porttest.NewDrive(disk, nil)
	for _, file := range []struct{ name, data string }{
		{"a.pdf", "pdf"}, {"a.pdf.mark", "mark"}, {"b.epub", "epub"}, {"b.epub.mark", "mark"}, {"orphan.pdf.mark", "orphan"}, {"bare.pdf", "bare"},
	} {
		d.Seed("DOCUMENT/Document/"+file.name, file.data)
	}

	f := files.Service{Cloud: d, Local: disk, Disk: disk, Account: d}
	n := notebooks.Service{Cloud: d, Fetch: f, Files: disk,
		Reader:   porttest.NoteReader{Notebook: port.Notebook{Kind: "mark", Pages: []port.NotePage{{Number: 1, Text: []string{"words"}}}}},
		Renderer: porttest.Renderer{Unavailable: port.ErrRenderUnavailable}}
	return commands.Services{Files: d, Completion: d, Library: f, Fetch: f, Notebooks: n}, d
}

func runMarked(t *testing.T, svc commands.Services, terminal bool, args ...string) (string, string, error) {
	t.Helper()
	var out, stderr bytes.Buffer
	err := commands.Run(t.Context(), args, commands.Streams{In: strings.NewReader("no\n"), Out: &out, Err: &stderr,
		IsTerminal: terminal, Getenv: func(string) string { return "" }}, cliutil.Fixed(svc))
	return out.String(), stderr.String(), err
}

func TestMarkListings(t *testing.T) {
	for _, args := range [][]string{{"ls", "DOCUMENT/Document"}, {"tree", "DOCUMENT/Document"}, {"find", "", "DOCUMENT/Document"}} {
		for _, format := range []string{"table", "plain", "json"} {
			for _, raw := range []bool{false, true} {
				t.Run(strings.Join(args, " ")+format+map[bool]string{false: "fold", true: "raw"}[raw], func(t *testing.T) {
					svc, d := markedServices()
					cmd := append(append([]string{}, args...), "--format", format)
					if raw {
						cmd = append(cmd, "--marks")
					}
					out, stderr, err := runMarked(t, svc, false, cmd...)
					if err != nil {
						t.Fatal(err)
					}
					if (strings.Contains(stderr, "Flags: R recognized handwriting, H highlights")) != (format == "table") {
						t.Fatal(stderr)
					}
					if d.Count("download") != 0 {
						t.Fatal(d.Calls)
					}
					if format == "json" {
						var rows []struct {
							port.Entry
							Flags []string `json:"flags"`
						}
						if err := json.Unmarshal([]byte(out), &rows); err != nil {
							t.Fatal(err)
						}
						want := 4
						if raw {
							want = 6
						}
						if len(rows) != want {
							t.Fatal(rows)
						}
						for _, e := range rows {
							if e.Name == "a.pdf" && (e.Mark == nil || e.Mark.Path != e.Path+".mark" || len(e.Flags) != 1 || e.Flags[0] != "highlights") {
								t.Fatal(e)
							}
						}
					} else {
						if strings.Contains(out, "a.pdf.mark") != raw || !strings.Contains(out, "orphan.pdf.mark") {
							t.Fatal(out)
						}
						if format == "table" && args[0] != "tree" && (!strings.Contains(out, "FLAGS") || strings.Contains(out, "RECOGNITION")) {
							t.Fatal(out)
						}
					}
					if (args[0] == "ls" || args[0] == "tree") && !raw {
						clitest.Golden(t, "marks-"+args[0]+"-"+format, out)
						if format == "table" {
							clitest.Golden(t, "marks-legend", stderr)
						}
					}
				})
			}
		}
	}
	for _, args := range [][]string{{"find", "missing"}, {"ls", "DOCUMENT/Document/empty"}} {
		svc, d := markedServices()
		d.Folders["DOCUMENT/Document/empty"] = true
		_, stderr, err := runMarked(t, svc, false, append(args, "--format", "table")...)
		if err != nil || strings.Contains(stderr, "Flags:") {
			t.Fatal(stderr, err)
		}
	}
}

func TestMarkStatAndTextSearch(t *testing.T) {
	for _, format := range []string{"json", "plain", "table"} {
		svc, d := markedServices()
		out, stderr, err := runMarked(t, svc, false, "stat", "DOCUMENT/Document/a.pdf", "--format", format)
		if err != nil || strings.Contains(out, "a.pdf.mark") != (format != "plain") || strings.Contains(stderr, "Flags:") || d.Count("download") != 0 || d.Count("snapshot") != 1 {
			t.Fatal(out, stderr, err, d.Calls)
		}
		for _, raw := range []bool{false, true} {
			args := []string{"find", "--text", "words", "DOCUMENT/Document", "--format", format}
			if raw {
				args = append(args, "--marks")
			}
			out, _, err = runMarked(t, svc, false, args...)
			if !raw {
				clitest.Golden(t, "marks-find-text-"+format, out)
			}
			if err != nil || !strings.Contains(out, "a.pdf") {
				t.Fatal(out, err)
			}
			if format != "json" && strings.Contains(out, "a.pdf.mark") != raw {
				t.Fatal(out)
			}
		}
	}
}

func TestMarkCommandConfirmationAndFailure(t *testing.T) {
	for _, op := range [][]string{{"mv", "DOCUMENT/Document/a.pdf", "DOCUMENT/Document/new.pdf"},
		{"rename", "DOCUMENT/Document/a.pdf", "new.pdf"}, {"rm", "DOCUMENT/Document/a.pdf"}, {"rm", "DOCUMENT/Document/a.pdf", "--permanent"}} {
		for _, mode := range []string{"dry", "decline", "pipe", "yes", "partial"} {
			svc, d := markedServices()
			args := append([]string{}, op...)
			if mode == "dry" {
				args = append(args, "--dry-run", "--json")
			}
			if mode == "yes" || mode == "partial" {
				args = append(args, "--yes")
			}
			if mode == "partial" {
				verb := "move"
				if op[0] == "rm" {
					verb = "delete"
				}
				d.Fail[verb+" DOCUMENT/Document/a.pdf.mark"] = errors.New("failure")
			}
			out, stderr, err := runMarked(t, svc, mode == "decline", args...)
			if mode == "yes" || mode == "dry" {
				if err != nil {
					t.Fatal(args, err)
				}
			} else if err == nil {
				t.Fatal("missing failure", args)
			}
			if mode == "dry" {
				if !strings.Contains(out, "a.pdf.mark") || !strings.Contains(out, `"dry_run": true`) {
					t.Fatal(out)
				}
			} else {
				if strings.Count(stderr, "including the document's .mark annotations") != 1 {
					t.Fatal(stderr)
				}
				wantPaths := 0
				if mode == "decline" {
					wantPaths = 1 // Paths appear only in the confirmation question.
				}
				if strings.Count(stderr, "DOCUMENT/Document/a.pdf.mark") != wantPaths {
					t.Fatal("duplicated path notes", stderr)
				}
			}
			if mode == "decline" {
				question := stderr[strings.LastIndex(stderr, "\n")+1:]
				if !strings.Contains(question, "2 entries:") || !strings.Contains(question, "DOCUMENT/Document/a.pdf,") && !strings.Contains(question, "DOCUMENT/Document/a.pdf ->") || !strings.Contains(question, "DOCUMENT/Document/a.pdf.mark") {
					t.Fatal("incomplete confirmation question", stderr)
				}
			}
			if mode == "partial" && (!strings.Contains(err.Error(), "but DOCUMENT/Document/a.pdf.mark was not") || commands.ExitCode(err) == 0) {
				t.Fatal(err)
			}
			if mode == "pipe" && commands.ExitCode(err) != 2 {
				t.Fatal(err)
			}
			if mode == "dry" || mode == "decline" || mode == "pipe" {
				if d.Count("move")+d.Count("delete") != 0 {
					t.Fatal(d.Calls)
				}
			}
		}
	}
}

func TestDuplicateMarks(t *testing.T) {
	for _, raw := range []bool{false, true} {
		svc, _ := markedServices()
		args := []string{"dupes", "DOCUMENT/Document", "--json"}
		if raw {
			args = append(args, "--marks")
		}
		out, _, err := runMarked(t, svc, false, args...)
		if err != nil || strings.Contains(out, "a.pdf.mark") != raw {
			t.Fatal(out, err)
		}
	}
}

func TestMarkStatFailures(t *testing.T) {
	for _, fail := range []string{"snapshot", "stat DOCUMENT/Document/a.pdf", "list"} {
		svc, d := markedServices()
		d.Fail[fail] = errors.New("failure")
		if _, _, err := runMarked(t, svc, false, "stat", "DOCUMENT/Document/a.pdf", "--json"); err == nil {
			t.Fatal(fail)
		}
	}
	svc, _ := markedServices()
	for _, name := range []string{"bare.pdf", "a.pdf.mark"} {
		out, _, err := runMarked(t, svc, false, "stat", "DOCUMENT/Document/"+name, "--json")
		if err != nil || strings.Contains(out, `"mark":`) || !strings.Contains(out, `"flags": []`) {
			t.Fatal(out, err)
		}
	}
	doc := port.Entry{Name: "a.pdf", Path: "Note/a.pdf"}
	mark := port.Entry{Name: "a.pdf.mark", Path: "Note/a.pdf.mark"}
	cloud := &porttest.Cloud{Entries: []port.Entry{doc, mark, mark}}
	for _, format := range []string{"json", "plain", "table"} {
		out, stderr, err := runMarked(t, commands.Services{Files: cloud}, false, "stat", doc.Path, "--format", format)
		if err != nil || !strings.Contains(out, doc.Path) || strings.Contains(out, `"mark":`) || strings.Contains(out, "mark_path") ||
			!strings.Contains(stderr, "ambiguous annotation sidecar") {
			t.Fatal(out, stderr, err)
		}
	}
}

func TestFindMarkNames(t *testing.T) {
	for _, needle := range []string{".mark", "pdf.mark", "A.PDF.MARK"} {
		for _, format := range []string{"json", "plain", "table"} {
			svc, _ := markedServices()
			out, _, err := runMarked(t, svc, false, "find", needle, "--format", format)
			if err != nil || !strings.Contains(out, "a.pdf") {
				t.Fatal(out, err)
			}
			if format == "json" {
				if !strings.Contains(out, `"highlights"`) {
					t.Fatal(out)
				}
			} else if strings.Contains(out, "a.pdf.mark") || format == "table" && !strings.Contains(out, "H") {
				t.Fatal(out)
			}
		}
	}
}

func TestHighlightsMissingAnnotationsExit(t *testing.T) {
	svc, _ := markedServices()
	_, stderr, err := runMarked(t, svc, false, "highlights", "/DOCUMENT/Document/bare.pdf")
	if !errors.Is(err, port.ErrNotFound) || commands.ExitCode(err) != commands.ExitCode(port.ErrNotFound) || !strings.Contains(err.Error(), "has no annotations") {
		t.Fatal(stderr, err, commands.ExitCode(err))
	}
}

func TestHighlightsAmbiguousAnnotationsExit(t *testing.T) {
	svc, _ := markedServices()
	mark := port.Entry{Path: "DOCUMENT/Document/a.pdf.mark"}
	svc.Notebooks = notebooks.Service{Fetch: svc.Fetch, Files: &porttest.NotebookFiles{},
		Cloud: &porttest.Cloud{Entries: []port.Entry{mark, mark}}, Reader: porttest.NoteReader{}}
	_, stderr, err := runMarked(t, svc, false, "highlights", "/DOCUMENT/Document/a.pdf")
	if !errors.Is(err, port.ErrConflict) || commands.ExitCode(err) != commands.ExitCode(port.ErrConflict) || !strings.Contains(err.Error(), "ambiguous annotation sidecar") {
		t.Fatal(stderr, err, commands.ExitCode(err))
	}
}

func TestMarksHelpWrapping(t *testing.T) {
	for _, command := range []string{"ls", "tree", "find", "mv", "rm", "highlights", "dupes"} {
		out, _, err := runMarked(t, commands.Services{}, false, command, "--help")
		if err != nil {
			t.Fatal(err)
		}
		description, _, _ := strings.Cut(out, "\nUsage:")
		for line := range strings.SplitSeq(description, "\n") {
			if len(line) > 78 {
				t.Fatalf("%s help has a long line (%d): %s", command, len(line), line)
			}
		}
		if command == "ls" {
			clitest.Golden(t, "marks-ls-help", out)
		}
	}
}

func TestFindUnicodeNames(t *testing.T) {
	for _, needle := range []string{"Й", "И\u0306", "Й.PDF.MARK", "И\u0306.PDF.MARK"} {
		svc, d := markedServices()
		doc := "DOCUMENT/Document/й.pdf"
		mark := "DOCUMENT/Document/и\u0306.pdf.mark"
		d.Seed(doc, "pdf")
		d.Seed(mark, "ink")
		out, _, err := runMarked(t, svc, false, "find", needle, "DOCUMENT/Document", "--json")
		var rows []port.Entry
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Path != doc || rows[0].Mark == nil || rows[0].Mark.Path != mark {
			t.Fatal(out)
		}
	}
}

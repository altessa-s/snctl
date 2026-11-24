// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"context"
	"crypto/md5" // #nosec G501 -- Supernote content identifiers.
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"
)

const journalVersion = 2
const maxJournal = 64 << 20

// ErrJournal refuses a damaged journal instead of guessing prior state.
var ErrJournal = errors.New("the mirror's journal cannot be read; move it aside to start over")

type journal struct {
	Endpoint  string           `json:"endpoint"`
	Account   string           `json:"account"`
	Version   int              `json:"v"`
	Folder    string           `json:"folder"`
	Profile   string           `json:"profile"`
	Path      string           `json:"path"`
	LastPass  time.Time        `json:"last_pass"`
	LastError string           `json:"last_error"`
	Pending   int              `json:"pending"`
	Items     map[string]entry `json:"items"`
	Drawing   *drawingChange   `json:"drawing,omitempty"`
	Drawings  []*drawingChange `json:"drawings,omitempty"`
}

type entry struct {
	Path           string    `json:"path"`
	SHA256         string    `json:"sha256"`
	Size           int64     `json:"size"`
	ModTime        time.Time `json:"mtime"`
	CloudPath      string    `json:"cloud_path"`
	Record         string    `json:"record"`
	Output         string    `json:"output"`
	Text           bool      `json:"text,omitempty"`
	TextChecked    bool      `json:"text_checked,omitempty"`
	Drawn          string    `json:"drawn"`
	DrawingWarning string    `json:"drawing_warning,omitempty"`
	Detached       bool      `json:"detached"`
	// Recover records replacement intent until the uploaded original is verified.
	Recover      bool `json:"recover"`
	RestoreLocal bool `json:"restore_local"`
	// RejectedDrawing keys a failed parse by operation, linked libraries and input revision.
	RejectedDrawing string `json:"rejected_drawing,omitempty"`
}

func readJournal(root port.OutputDirectory) (*journal, error) {
	in, err := root.Open(domain.MirrorJournalName)
	if errors.Is(err, fs.ErrNotExist) {
		return &journal{Version: journalVersion, Items: map[string]entry{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJournal, err)
	}
	defer func() { _ = in.Close() }()
	data, err := io.ReadAll(io.LimitReader(in, maxJournal+1))
	if err != nil {
		return nil, err
	}
	j := &journal{}
	if len(data) > maxJournal || json.Unmarshal(data, j) != nil || (j.Version != 1 && j.Version != journalVersion) || j.Items == nil {
		return nil, ErrJournal
	}
	if !safeRel(j.Path, false) || j.Folder == "" || j.Pending < 0 {
		return nil, ErrJournal
	}
	if !j.validEntries() || !j.validDrawing() {
		return nil, ErrJournal
	}
	// Accept the original single-intent journal while writing the resumable list.
	if j.Drawing != nil {
		j.Drawings = append(j.Drawings, j.Drawing)
		j.Drawing = nil
	}
	return j, nil
}

func safeRel(name string, empty bool) bool {
	return name == "" && empty || name != "" && name != "." && name != ".." && !strings.HasPrefix(name, "../") &&
		!strings.HasPrefix(name, "/") && !strings.ContainsAny(name, "\\\x00:") && path.Clean(name) == name
}

func (j *journal) write(ctx context.Context, root port.OutputDirectory) error {
	return root.Produce(context.WithoutCancel(ctx), domain.MirrorJournalName, func(w io.Writer) error {
		return json.MarshalWrite(w, j, json.Deterministic(true))
	})
}

func (e entry) drawingOnlyDocument() bool {
	return e.Path == "" && domain.MirrorUploadable(e.CloudPath)
}

func (e entry) ownsLocalOutput() bool { return e.Path != "" || e.Output != "" }

// countsForDeletion includes live owners of local originals or notebook outputs.
func (e entry) countsForDeletion() bool {
	return !e.Detached && !e.drawingOnlyDocument() && e.ownsLocalOutput()
}

func (j *journal) tracked() (all, local int) {
	for _, e := range j.Items {
		if e.countsForDeletion() {
			all++
			if e.Path != "" {
				local++
			}
		}
	}
	return all, local
}

func validDigest(s string, n int) bool {
	data, err := hex.DecodeString(s)
	return err == nil && len(data) == n
}

func (j *journal) validEntries() bool {
	seen := map[string]bool{}
	for id, e := range j.Items {
		if !validDigest(e.Record, md5.Size) || e.Path != "" && !e.Recover && !e.RestoreLocal && !validDigest(e.SHA256, sha256.Size) {
			return false
		}
		if id == "" || !safeRel(e.Path, true) || !safeRel(e.Output, true) || !safeRel(e.CloudPath, false) || e.Size < 0 {
			return false
		}
		if j.Path != "" && !strings.HasPrefix(e.CloudPath, j.Path+"/") {
			return false
		}
		if !e.validText() {
			return false
		}
		if e.Output != "" && !strings.HasPrefix(e.Output, domain.MirrorAnnotatedDir+"/") {
			return false
		}
		if e.Path != "" {
			key := foldName(e.Path)
			if seen[key] {
				return false
			}
			seen[key] = true
		}
		if e.Output != "" {
			key := foldName(e.Output)
			if seen[key] {
				return false
			}
			seen[key] = true
		}
		if e.Path != "" && (domain.MirrorIgnored(e.Path) || !domain.MirrorUploadable(e.Path)) {
			return false
		}
	}
	return true
}

func (e entry) validText() bool {
	return !e.Text || e.Output != "" && strings.HasPrefix(e.Output, domain.MirrorAnnotatedDir+"/notebooks/") && path.Ext(e.Output) == ".pdf"
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package outdir is what the commands that write a cloud folder into a local
// directory share: the record of the files they wrote, the local names of
// cloud entries, and the guard against deleting what looks like everything.
package outdir

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/altessa-s/snctl/internal/domain"
	"github.com/altessa-s/snctl/internal/port"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// The records snctl keeps at the top of a directory it writes.
const (
	RenderRecord = ".snctl-render.json"
	MgetRecord   = ".snctl-mget.json"
)

// maxRecord bounds the record read: far above any library.
const maxRecord = 64 << 20

// Entry is what a record keeps of one file written: the cloud entry it came
// from as it was then, the options it was written with, and its size, which
// a later run compares with the file on disk.
type Entry struct {
	ID       string    `json:"id"`
	Hash     string    `json:"hash"`
	Options  string    `json:"options"`
	Modified time.Time `json:"modified"`
	Size     int64     `json:"size,omitzero"`
}

// Read is the record named name in dir by slash-separated relative path:
// empty when there is none or it cannot be decoded, so every file is written
// again rather than trusted.
func Read(files port.NotebookFiles, dir, name string) map[string]Entry {
	out := map[string]Entry{}
	r, err := files.Open(filepath.Join(dir, name))
	if err != nil {
		return out
	}
	defer func() { _ = r.Close() }()
	if err = json.UnmarshalRead(io.LimitReader(r, maxRecord), &out); err != nil {
		return map[string]Entry{}
	}
	for key := range out {
		if !Local(key) {
			delete(out, key)
		}
	}
	return out
}

// Write replaces the record whole, atomically. It is written even when ctx is
// done: it records only files already complete, so an interrupted run leaves
// a record that matches the directory.
func Write(ctx context.Context, files port.NotebookFiles, dir, name string, entries map[string]Entry) error {
	return files.Produce(context.WithoutCancel(ctx), filepath.Join(dir, name), func(w io.Writer) error {
		return json.MarshalWrite(w, entries, json.Deterministic(true))
	})
}

// Local reports whether a record key stays inside the directory: relative
// and without "..". A record edited by hand cannot steer a deletion outside it.
func Local(key string) bool {
	return key != "" && filepath.IsLocal(filepath.FromSlash(key))
}

// SameTime compares a file's modification time with the cloud's to the
// second, the coarsest a common file system keeps.
func SameTime(file, cloud time.Time) bool {
	return file.Truncate(time.Second).Equal(cloud.Truncate(time.Second))
}

// Paths gives each entry below top a local path relative to the output
// directory: every component a [domain.SafeName], the last one rewritten by
// name. Paths that collide without regard to case, or with a folder's, get
// the entry identifier before the extension; the result does not depend on
// the order of entries, only on their paths and identifiers.
func Paths(entries []port.Entry, top string, folders []string, name func(string) string) []string {
	rels := make([]string, len(entries))
	count := map[string]int{}
	for _, f := range folders {
		count[collisionKey(Folder(f, top))]++
	}
	for i, e := range entries {
		parts := strings.Split(relative(e.Path, top), "/")
		for j, p := range parts {
			parts[j] = norm.NFC.String(domain.SafeName(p))
		}
		parts[len(parts)-1] = norm.NFC.String(domain.SafeName(name(e.Name)))
		rels[i] = strings.Join(parts, "/")
		count[collisionKey(rels[i])]++
	}
	order := sortedByIdentity(entries)
	reserved := map[string]bool{}
	for _, i := range order {
		rel := rels[i]
		if count[collisionKey(rel)] > 1 {
			ext := path.Ext(rel)
			rel = path.Join(path.Dir(rel), domain.SafeName(fmt.Sprintf("%s (%s)%s", strings.TrimSuffix(path.Base(rel), ext), entries[i].ID, ext)))
		}
		rels[i] = rel
		reserved[collisionKey(rel)] = true
	}
	used := map[string]bool{}
	for _, i := range order {
		base := rels[i]
		rel := base
		for n := 2; used[collisionKey(rel)]; n++ {
			ext := path.Ext(base)
			rel = strings.TrimSuffix(base, ext) + "-" + strconv.Itoa(n) + ext
			for reserved[collisionKey(rel)] {
				n++
				rel = strings.TrimSuffix(base, ext) + "-" + strconv.Itoa(n) + ext
			}
		}
		used[collisionKey(rel)] = true
		rels[i] = rel
	}
	return rels
}

// Folder is the local path of a cloud folder below top.
func Folder(name, top string) string {
	parts := strings.Split(relative(name, top), "/")
	for j, p := range parts {
		parts[j] = norm.NFC.String(domain.SafeName(p))
	}
	return strings.Join(parts, "/")
}

func relative(name, top string) string {
	top = strings.Trim(top, "/")
	name = strings.Trim(name, "/")
	if top == "" {
		return name
	}
	return strings.TrimPrefix(name, top+"/")
}

// sortedByIdentity orders entries by path, then identifier, so that the
// numbered fallback for a repeated suffix is stable.
func sortedByIdentity(entries []port.Entry) []int {
	order := make([]int, len(entries))
	for i := range order {
		order[i] = i
	}
	key := func(i int) string { return entries[i].Path + "\x00" + entries[i].ID }
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && key(order[j]) < key(order[j-1]); j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	return order
}

// The mass-deletion guard, ported from rmctl's mirror: deletions are refused
// when the cloud side is empty although files were recorded (a wrong account
// or an unsynced cloud looks like this), when they are more than MaxDeletes,
// or when they are more than half of the files recorded, two at least.
const (
	// MaxDeletes is the most deletions one run makes without --force.
	MaxDeletes = 50
	// minShare is the fewest deletions the half-of-everything rule refuses.
	minShare = 2
)

// Guard is why deletions are refused, or "". empty says the cloud folder
// holds no file at all.
func Guard(deletions, recorded int, empty bool) string {
	switch {
	case deletions == 0:
		return ""
	case empty:
		return "the cloud folder is empty but the record lists " + strconv.Itoa(recorded) + " files: it may be the wrong folder or account"
	case deletions > MaxDeletes:
		return strconv.Itoa(deletions) + " deletions are more than " + strconv.Itoa(MaxDeletes) + " at once"
	case deletions >= minShare && deletions*2 > recorded:
		return strconv.Itoa(deletions) + " deletions are more than half of the " + strconv.Itoa(recorded) + " files recorded"
	}
	return ""
}

func collisionKey(name string) string { return cases.Fold().String(norm.NFC.String(name)) }

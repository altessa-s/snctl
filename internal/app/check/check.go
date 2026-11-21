// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package check runs bounded live checks and tracks only entries it created.
package check

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/altessa-s/go-atlas-core/time/clock"

	"github.com/altessa-s/snctl/internal/port"
)

const (
	smallSize      = 1024
	largeSize      = 9*1024*1024 + 123
	feedTimeout    = 30 * time.Second
	cleanupTimeout = 2 * time.Minute
)

// Service holds independent ports for the self-test.
type Service struct {
	Session port.CloudSession
	Files   port.Files
	Tasks   port.Tasks
	Digests port.Digests
	Feed    port.Feed
	Probe   port.Probe
	Local   port.LocalFiles
}

type report struct {
	emit      func(port.CheckStep) error
	failed    bool
	outputErr error
}

func (r *report) step(name string, run func() error) bool {
	start := clock.Real().Now()
	err := run()
	status, detail := "pass", ""
	if err != nil {
		status, detail = "fail", err.Error()
		r.failed = true
	}
	if writeErr := r.emit(port.CheckStep{Name: name, Status: status, Duration: int64(clock.Real().Now().Sub(start)), Detail: detail}); writeErr != nil {
		r.outputErr = writeErr
	}
	return err == nil
}
func (r *report) skip(name, detail string) {
	if err := r.emit(port.CheckStep{Name: name, Status: "skipped", Detail: detail}); err != nil {
		r.outputErr = err
	}
}

// Run continues independent read checks after failures. Cleanup uses a fresh,
// bounded context even when the original operation was interrupted.
func (s Service) Run(ctx context.Context, write bool, emit func(port.CheckStep) error) error {
	r := &report{emit: emit}
	r.step("session, user and capacity", func() error { _, err := s.Session.Info(ctx); return err })
	var tree []port.Entry
	r.step("device tree", func() error { var err error; tree, err = s.Files.List(ctx, "/", true); return err })
	r.step("web listing", func() error { return s.Probe.WebList(ctx) })
	s.readDownload(ctx, r, tree)
	r.step("to-do lists", func() error { _, err := s.Tasks.Lists(ctx); return err })
	r.step("tasks", func() error { _, err := s.Tasks.Tasks(ctx, ""); return err })
	for _, kind := range []port.DigestKind{port.DigestGroups, port.DigestItems, port.DigestTags} {
		r.step("digest "+string(kind), func() error { _, err := s.Digests.DigestList(ctx, kind); return err })
	}
	r.step("feed first event", func() error {
		bounded, cancel := context.WithTimeout(ctx, feedTimeout)
		defer cancel()
		for _, err := range s.Feed.Watch(bounded) {
			return err
		}
		return errors.New("feed ended before its first event")
	})
	if write {
		s.write(ctx, r)
	}
	if r.failed {
		return errors.Join(errors.New("self-test failed"), r.outputErr)
	}
	return r.outputErr
}

func (s Service) readDownload(ctx context.Context, r *report, tree []port.Entry) {
	for _, e := range tree {
		if e.Folder {
			continue
		}
		r.step("download and hash check", func() error {
			file, err := s.Files.Download(ctx, e, nil)
			if err != nil {
				return err
			}
			return s.Local.Remove(file)
		})
		return
	}
	r.skip("download and hash check", "no file exists")
}

func (s Service) write(ctx context.Context, r *report) {
	var nonce [16]byte
	_, _ = rand.Read(nonce[:])
	folder := "NOTE/Note/snctl-check-" + hex.EncodeToString(nonce[:])
	var root port.Entry
	var made []port.Entry
	folders := make([]port.Entry, 0, 1)
	defer func() {
		clean, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		s.verifyCleanup(clean, r, folder, append(made, append(folders, root)...))
	}()
	if !r.step("create "+folder, func() error {
		var err error
		root, err = s.Files.Mkdir(ctx, folder)
		return err
	}) {
		return
	}
	defer s.cleanup(ctx, r, root, &made, &folders)
	for _, sample := range []struct {
		name string
		size int
	}{{"small.txt", smallSize}, {"large.txt", largeSize}} {
		data := strings.Repeat("x", sample.size)
		file, err := s.Local.Stage(strings.NewReader(data))
		if err != nil {
			r.step("stage "+sample.name, func() error { return err })
			continue
		}
		e, _ := s.roundTrip(ctx, r, file, folder+"/"+sample.name, data)
		if e.ID != "" {
			made = append(made, e)
		}
		r.step("remove local "+sample.name, func() error { return s.Local.Remove(file) })
	}
	if len(made) == 0 {
		return
	}
	var child port.Entry
	if !r.step("create move destination", func() error {
		var err error
		child, err = s.Files.Mkdir(ctx, folder+"/moved")
		return err
	}) {
		return
	}
	folders = append(folders, child)
	destination := folder + "/moved/small.txt"
	if r.step("move file", func() error { return s.Files.Move(ctx, made[0], destination) }) {
		made[0].Path = destination
	}
	destination = folder + "/moved/renamed.txt"
	if r.step("rename file", func() error { return s.Files.Move(ctx, made[0], destination) }) {
		made[0].Path = destination
	}
}

func (s Service) roundTrip(ctx context.Context, r *report, file, destination, data string) (port.Entry, bool) {
	var e port.Entry
	if !r.step("upload "+path.Base(destination), func() error {
		var err error
		e, err = s.Files.Upload(ctx, file, destination, nil)
		return err
	}) {
		return e, false
	}
	ok := r.step("download and compare "+path.Base(destination), func() (err error) {
		downloaded, err := s.Files.Download(ctx, e, nil)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, s.Local.Remove(downloaded)) }()
		in, err := s.Local.Open(downloaded)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		hash := sha256.New()
		if _, err = io.Copy(hash, in); err != nil {
			return err
		}
		want := sha256.Sum256([]byte(data))
		if hex.EncodeToString(hash.Sum(nil)) != hex.EncodeToString(want[:]) {
			return errors.New("download differs from upload")
		}
		return nil
	})
	return e, ok
}

func (s Service) cleanup(ctx context.Context, r *report, root port.Entry, made, folders *[]port.Entry) {
	clean, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	for _, e := range *made {
		r.step("remove file "+path.Base(e.Path), func() error { return s.Files.Delete(clean, e, true) })
	}
	for _, folder := range append(*folders, root) {
		r.step("remove folder "+path.Base(folder.Path), func() error {
			entries, err := s.Files.List(clean, folder.Path, false)
			if err != nil {
				return err
			}
			if len(entries) > 0 {
				return fmt.Errorf("created folder is not empty; retained %s", folder.Path)
			}
			return s.Files.Delete(clean, folder, true)
		})
	}
}

func (s Service) verifyCleanup(clean context.Context, r *report, folder string, made []port.Entry) {
	r.step("verify nothing is left behind", func() error {
		entries, err := s.Files.List(clean, "/", true)
		if err != nil {
			return err
		}
		for _, e := range entries {
			for _, created := range made {
				if created.ID != "" && e.ID == created.ID {
					return errors.New("self-test identifier remains in the device tree")
				}
			}
			if strings.Contains(e.Path, path.Base(folder)) {
				return errors.New("self-test entries remain in the device tree")
			}
		}
		return nil
	})
}

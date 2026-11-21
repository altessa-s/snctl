// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/altessa-s/snctl/cmd/snctl/commands/cliutil"
	"github.com/altessa-s/snctl/internal/port"

	"golang.org/x/text/unicode/norm"
)

// The defaults of --exec.
const (
	// DefaultDebounce is how long events gather before one run.
	DefaultDebounce = 2 * time.Second
	// DefaultExecTimeout bounds one run.
	DefaultExecTimeout = 5 * time.Minute
	// DefaultRepeatWindow bounds suppression of repeated announcements.
	DefaultRepeatWindow = 5 * time.Minute
	// execGrace is how long Ctrl-C waits for a running command.
	execGrace = 5 * time.Second
	// maxQueue bounds the runs waiting; past it, a batch joins the last
	// one waiting rather than being lost.
	maxQueue = 16
	// maxBatch bounds the events of one batch, pending or waiting: past
	// it the oldest go and are counted, so a run knows it saw a truncated
	// batch and can rescan.
	maxBatch = 1000
	// waitDelay is how long a finished command's output may still flow.
	waitDelay = time.Second
)

// The values of --only.
const (
	onlyFile   = "file"
	onlyFolder = "folder"
)

// execOptions shape --exec.
type execOptions struct {
	// argv is the program and its arguments, as they are run: no shell
	// unless --shell put one there.
	argv         []string
	debounce     time.Duration
	timeout      time.Duration
	grace        time.Duration
	repeatWindow time.Duration
	// only is "", onlyFile or onlyFolder; under a folder path the
	// item must lie below, "" for anywhere.
	only    string
	under   string
	profile string
}

// runner runs one command: argv with env added to snctl's own environment,
// stdin on its standard input, and both its outputs on out. It answers
// the command's exit code.
type runner interface {
	Run(ctx context.Context, argv, env []string, stdin []byte, out io.Writer) (int, error)
}

// execRunner runs commands as processes. ctx ending kills the process tree on Unix and Windows.
// environ is the environment the command inherits; nil is snctl's own.
type execRunner struct {
	environ func() []string
}

func (r execRunner) Run(ctx context.Context, argv, env []string, stdin []byte, out io.Writer) (int, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) // #nosec G204 -- the user's own command, run as argv without a shell
	inherited := os.Environ
	if r.environ != nil {
		inherited = r.environ
	}
	cmd.Env = childEnv(inherited(), env, runtime.GOOS == "windows")
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = waitDelay
	err := runCommand(cmd)
	if cmd.ProcessState == nil {
		// The process never started: a program that is not there or may
		// not run. There is no exit status; the error says why.
		return -1, err
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && ctx.Err() == nil {
		return exitErr.ExitCode(), nil
	}
	return cmd.ProcessState.ExitCode(), err
}

// childEnv is base with the variables of set put in: a variable base
// already holds is replaced, not repeated, so each key appears once. fold
// compares keys without case, as Windows does.
func childEnv(base, set []string, fold bool) []string {
	norm := func(k string) string {
		if fold {
			return strings.ToUpper(k)
		}
		return k
	}
	replaced := make(map[string]bool, len(set))
	for _, kv := range set {
		replaced[norm(envKey(kv))] = true
	}
	out := make([]string, 0, len(base)+len(set))
	for _, kv := range base {
		if !replaced[norm(envKey(kv))] {
			out = append(out, kv)
		}
	}
	return append(out, set...)
}

// envKey is the name of a KEY=value entry. Windows keeps per-drive
// entries such as "=C:=C:\\x", whose name starts with the "=".
func envKey(kv string) string {
	if i := strings.IndexByte(kv[min(1, len(kv)):], '='); i >= 0 {
		return kv[:i+min(1, len(kv))]
	}
	return kv
}

// execEvent is one event as a run receives it.
type execEvent struct {
	Time  string `json:"time"`
	Event string `json:"event"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	Kind  string `json:"kind"`
}

// execInput is what a run reads on its standard input.
// Dropped counts the events of the batch left out, the oldest first, to
// keep it within maxBatch.
type execInput struct {
	Profile string      `json:"profile"`
	Dropped int         `json:"dropped"`
	Events  []execEvent `json:"events"`
}

// dispatcher gathers events into batches and runs the command for each,
// one run at a time.
type dispatcher struct {
	opts   execOptions
	run    runner
	tree   func(ctx context.Context) (map[string]port.Entry, error)
	errOut io.Writer
	now    func() time.Time
	// Only the serial worker accesses acted, which records successfully resolved announcements.
	acted map[port.Change]time.Time

	mu      sync.Mutex
	pending batch
	timer   *time.Timer
	queue   []batch
	closed  bool
	wake    chan struct{}
	done    chan struct{}
}

type stampedEvent struct {
	at time.Time
	e  port.Change
}

// batch is the events of one run, at most maxBatch of them, the newest
// kept, and how many older ones were dropped to stay within it.
type batch struct {
	events  []stampedEvent
	dropped int
}

// add puts events at the end and drops the oldest beyond maxBatch. The
// slice never holds more than maxBatch events once add returns.
func (b *batch) add(events ...stampedEvent) {
	for _, event := range events {
		if event.e.Type != "connected" && b.contains(event.e) {
			continue
		}
		b.events = append(b.events, event)
	}
	if over := len(b.events) - maxBatch; over > 0 {
		b.events = append(b.events[:0], b.events[over:]...)
		b.dropped += over
	}
}

func (b *batch) contains(e port.Change) bool {
	for _, prior := range b.events {
		if prior.e == e {
			return true
		}
	}
	return false
}

func newDispatcher(opts execOptions, r runner, tree func(context.Context) (map[string]port.Entry, error),
	errOut io.Writer, now func() time.Time) *dispatcher {
	return &dispatcher{
		opts: opts, run: r, tree: tree, errOut: errOut, now: now,
		acted: make(map[port.Change]time.Time),
		wake:  make(chan struct{}, 1), done: make(chan struct{}),
	}
}

// add takes a library event; a run follows once the debounce from the
// first event of the batch has passed.
func (d *dispatcher) add(e port.Change) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.pending.add(stampedEvent{at: d.now(), e: e})
	if d.timer == nil {
		d.timer = time.AfterFunc(d.opts.debounce, d.flush)
	}
}

func (d *dispatcher) flush() {
	d.mu.Lock()
	d.flushLocked()
	d.mu.Unlock()
	d.signal()
}

// flushLocked moves the pending events into the queue as one batch, or
// into the last batch waiting when the queue is full.
func (d *dispatcher) flushLocked() {
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	if len(d.pending.events) == 0 {
		return
	}
	b := d.pending
	d.pending = batch{}
	if len(d.queue) < maxQueue {
		d.queue = append(d.queue, b)
		return
	}
	last := &d.queue[len(d.queue)-1]
	last.dropped += b.dropped
	last.add(b.events...)
}

func (d *dispatcher) signal() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// close sends what is pending at once; the runs waiting still happen.
func (d *dispatcher) close() {
	d.mu.Lock()
	d.closed = true
	d.flushLocked()
	d.mu.Unlock()
	d.signal()
}

// work runs the batches in order until close and an empty queue, or until
// ctx ends: then the run under way has the grace period to finish, and the
// batches still waiting are dropped.
func (d *dispatcher) work(ctx context.Context) {
	defer close(d.done)
	for {
		d.mu.Lock()
		if ctx.Err() != nil {
			if n := len(d.queue); n > 0 {
				cliutil.Notef(d.errOut, "exec: %d runs dropped at interrupt", n)
			}
			d.queue = nil
			d.mu.Unlock()
			return
		}
		if len(d.queue) == 0 {
			closed := d.closed
			d.mu.Unlock()
			if closed {
				return
			}
			select {
			case <-d.wake:
			case <-ctx.Done():
			}
			continue
		}
		b := d.queue[0]
		d.queue = d.queue[1:]
		d.mu.Unlock()
		d.runBatch(ctx, b)
	}
}

func (d *dispatcher) suppressRepeats(b batch) batch {
	now := d.now()
	for e, at := range d.acted {
		if now.Sub(at) >= d.opts.repeatWindow {
			delete(d.acted, e)
		}
	}
	kept := b.events[:0]
	for _, s := range b.events {
		if _, repeat := d.acted[s.e]; !repeat {
			kept = append(kept, s)
		}
	}
	b.events = kept
	return b
}

func (d *dispatcher) remember(events []port.Change) {
	if d.opts.repeatWindow == 0 {
		return
	}
	now := d.now()
	for _, e := range events {
		if e.Type != "connected" {
			d.acted[e] = now
		}
	}
}

// runBatch runs the command once for batch, its events filtered and
// described with the library's paths.
func (d *dispatcher) runBatch(ctx context.Context, b batch) {
	b = d.suppressRepeats(b)
	if len(b.events) == 0 && b.dropped == 0 {
		return
	}
	var tree map[string]port.Entry
	if d.tree != nil {
		t, err := d.tree(ctx)
		if err != nil {
			cliutil.Notef(d.errOut, "exec: the library for the paths: %v", err)
			if (d.opts.only != "" || d.opts.under != "") && b.dropped == 0 {
				return
			}
		}
		tree = t
	}
	if ctx.Err() != nil {
		return
	}
	// Without the library, filters cannot be applied: a batch that lost
	// events still runs, with no events, so the program learns of the loss.
	filtersBlind := tree == nil && (d.opts.only != "" || d.opts.under != "")
	if b.dropped > 0 {
		cliutil.Notef(d.errOut, "exec: %d older events of this batch were dropped; the run gets the newest %d (SNCTL_DROPPED)", b.dropped, len(b.events))
	}
	events := make([]execEvent, 0, len(b.events))
	var seen []port.Change
	if !filtersBlind {
		for _, s := range b.events {
			seen = append(seen, s.e)
			ev, ok := d.describe(tree, s)
			if ok {
				events = append(events, ev)
			}
		}
	}
	d.remember(seen)
	// A batch that lost events always runs, even when no kept event passes
	// the filters: the dropped count tells the program to rescan.
	if len(events) == 0 && b.dropped == 0 {
		return
	}
	var last execEvent
	if len(events) > 0 {
		last = events[len(events)-1]
	}
	env := []string{
		"SNCTL_EVENT=" + last.Event,
		"SNCTL_ID=" + last.ID,
		"SNCTL_NAME=" + last.Name,
		"SNCTL_PATH=" + last.Path,
		"SNCTL_KIND=" + last.Kind,
		"SNCTL_TIME=" + last.Time,
		"SNCTL_COUNT=" + strconv.Itoa(len(events)),
		"SNCTL_DROPPED=" + strconv.Itoa(b.dropped),
		"SNCTL_PROFILE=" + d.opts.profile,
	}
	stdin, err := json.Marshal(execInput{Profile: d.opts.profile, Dropped: b.dropped, Events: events}, json.Deterministic(true))
	if err != nil {
		cliutil.Notef(d.errOut, "exec: %v", err)
		return
	}
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), d.opts.timeout)
	defer cancel()
	// Ctrl-C leaves the run its grace period, then ends it.
	stop := context.AfterFunc(ctx, func() {
		t := time.AfterFunc(d.opts.grace, cancel)
		<-runCtx.Done()
		t.Stop()
	})
	defer stop()
	code, err := d.run.Run(runCtx, d.opts.argv, env, append(stdin, '\n'), d.errOut)
	prog := d.opts.argv[0]
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		cliutil.Notef(d.errOut, "exec: %s timed out after %s and was stopped", prog, d.opts.timeout)
	case ctx.Err() != nil && runCtx.Err() != nil:
		cliutil.Notef(d.errOut, "exec: %s was stopped at interrupt", prog)
	case err != nil:
		cliutil.Notef(d.errOut, "exec: %s: %v", prog, err)
	case code != 0:
		cliutil.Notef(d.errOut, "exec: %s exited %d", prog, code)
	}
}

// describe is an event for the command, with the item's path and kind
// from tree; ok false when --only or --under leave it out. An item the
// library no longer holds has no path or kind, and passes only when
// neither filter is set.
func (d *dispatcher) describe(tree map[string]port.Entry, s stampedEvent) (execEvent, bool) {
	e := s.e
	event := e.Kind
	if event == "" {
		event = e.Type
	}
	ev := execEvent{Time: s.at.UTC().Format(time.RFC3339), Event: event, ID: e.ID}
	if it, ok := tree[e.ID]; ok && e.Type == "files" {
		ev.Name, ev.Path, ev.Kind = it.Name, path.Clean("/"+it.Path), onlyFile
		if it.Folder {
			ev.Kind = onlyFolder
		}
	}
	switch d.opts.only {
	case onlyFolder:
		if ev.Kind != onlyFolder {
			return ev, false
		}
	case onlyFile:
		if ev.Kind == "" || ev.Kind == onlyFolder {
			return ev, false
		}
	}
	if u := strings.TrimSuffix(norm.NFC.String(d.opts.under), "/"); u != "" {
		p := norm.NFC.String(ev.Path)
		if p == "" || (p != u && !strings.HasPrefix(p, u+"/")) {
			return ev, false
		}
	}
	return ev, true
}

// lockedWriter serializes the writes of several goroutines.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

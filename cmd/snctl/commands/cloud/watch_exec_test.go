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
	"io/fs"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

// call is one run the fake runner saw.
type call struct {
	argv  []string
	env   map[string]string
	input execInput
}

// fakeRunner records each run; block holds a run until it is closed or the
// run's context ends, and code is every run's exit status.
type fakeRunner struct {
	mu      sync.Mutex
	calls   []call
	block   chan struct{}
	started chan struct{}
	code    int
	err     error
}

func (f *fakeRunner) Run(ctx context.Context, argv, env []string, stdin []byte, out io.Writer) (int, error) {
	c := call{argv: argv, env: map[string]string{}}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		c.env[k] = v
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdin), &c.input); err != nil {
		return 0, err
	}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	_, _ = io.WriteString(out, "ran "+argv[0]+"\n")
	if f.started != nil {
		f.started <- struct{}{}
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return -1, ctx.Err()
		}
	}
	return f.code, f.err
}

func (f *fakeRunner) runs() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func newTestDispatcher(r runner, opts execOptions, errOut io.Writer) *dispatcher {
	if opts.argv == nil {
		opts.argv = []string{"prog"}
	}
	if opts.timeout == 0 {
		opts.timeout = time.Minute
	}
	return newDispatcher(opts, r, nil, errOut, time.Now)
}

func announcement(name string) port.Change {
	return port.Change{Type: "files", Kind: name, ID: name}
}

// Events within the debounce make one run; a later event makes the next.
func TestExecDebounce(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{started: make(chan struct{}, 4)}
	var errOut syncBuffer
	d := newTestDispatcher(r, execOptions{debounce: 30 * time.Millisecond}, &errOut)
	go d.work(t.Context())
	d.add(announcement("a"))
	d.add(announcement("b"))
	<-r.started
	d.add(announcement("c"))
	<-r.started
	d.close()
	<-d.done
	runs := r.runs()
	if len(runs) != 2 || len(runs[0].input.Events) != 2 || runs[1].input.Events[0].ID != "c" {
		t.Fatalf("%+v", runs)
	}
	d.add(announcement("late"))
	if len(r.runs()) != 2 {
		t.Fatal("an event after close ran")
	}
}

func TestExecRepeatedAnnouncements(t *testing.T) {
	t.Parallel()
	for _, window := range []time.Duration{DefaultRepeatWindow, 0} {
		t.Run(window.String(), func(t *testing.T) {
			r := &fakeRunner{}
			now := time.Unix(1700000000, 0)
			listings := 0
			d := newTestDispatcher(r, execOptions{repeatWindow: window}, io.Discard)
			d.now = func() time.Time { return now }
			d.tree = func(context.Context) (map[string]port.Entry, error) {
				listings++
				return map[string]port.Entry{}, nil
			}
			events := []port.Change{{Type: "files", Kind: "STARTSYNC"}, {Type: "files", Kind: "DOWNLOADFILE", ID: "a", Path: "NOTE/Note/"}}
			run := func(changes []port.Change, dropped int) {
				b := batch{dropped: dropped}
				feed := &porttest.Cloud{Changes: changes}
				for e, err := range feed.Watch(t.Context()) {
					if err != nil {
						t.Fatal(err)
					}
					b.add(stampedEvent{at: now, e: e})
				}
				d.runBatch(t.Context(), b)
			}
			run(append(slices.Clone(events), events...), 0)
			if calls := r.runs(); len(calls) != 1 || len(calls[0].input.Events) != 2 || calls[0].input.Dropped != 0 {
				t.Fatalf("initial batch: %+v", calls)
			}
			for range 40 {
				now = now.Add(7 * time.Second)
				run(events, 0)
			}
			want := 1
			if window == 0 {
				want = 41
			}
			if len(r.runs()) != want || listings != want {
				t.Fatalf("repeats: runs %d, listings %d, want %d", len(r.runs()), listings, want)
			}
			// Every field is part of the raw announcement's identity.
			for _, change := range []port.Change{
				{Type: "tasks", Kind: "DOWNLOADFILE", ID: "a", Path: "NOTE/Note/"},
				{Type: "files", Kind: "MODIFYFILE", ID: "a", Path: "NOTE/Note/"},
				{Type: "files", Kind: "DOWNLOADFILE", ID: "b", Path: "NOTE/Note/"},
				{Type: "files", Kind: "DOWNLOADFILE", ID: "a", Path: "NOTE/Note/sub/"},
			} {
				run([]port.Change{change}, 0)
				want++
			}
			connected := port.Change{Type: "connected"}
			run([]port.Change{connected, connected}, 0)
			run([]port.Change{connected}, 0)
			want += 2
			calls := r.runs()
			if len(calls) != want || len(calls[want-2].input.Events) != 2 {
				t.Fatalf("connected events were suppressed: %+v", calls)
			}
			run(events, 7)
			want++
			if calls = r.runs(); len(calls) != want || calls[want-1].input.Dropped != 7 {
				t.Fatalf("lost dropped count: %+v", calls)
			}
			// Suppressed repeats do not extend the window from the last run.
			now = time.Unix(1700000000, 0).Add(DefaultRepeatWindow)
			run(events, 0)
			want++
			if len(r.runs()) != want || listings != want {
				t.Fatalf("expired window: runs %d, listings %d, history %d", len(r.runs()), listings, len(d.acted))
			}
			now = now.Add(DefaultRepeatWindow)
			run(events, 0)
			if len(d.acted) > len(events) {
				t.Fatalf("expired history retained: %d announcements", len(d.acted))
			}
		})
	}
}

func TestExecFilteredAnnouncementsAreRemembered(t *testing.T) {
	t.Parallel()
	for _, opts := range []execOptions{{only: onlyFile}, {under: "/DOCUMENT/Document/elsewhere"}} {
		r := &fakeRunner{}
		opts.repeatWindow = DefaultRepeatWindow
		d := newTestDispatcher(r, opts, io.Discard)
		listings := 0
		d.tree = func(context.Context) (map[string]port.Entry, error) {
			listings++
			return map[string]port.Entry{}, nil
		}
		e := announcement("a")
		for range 2 {
			d.runBatch(t.Context(), batch{events: []stampedEvent{{at: time.Now(), e: e}}})
		}
		if len(r.runs()) != 0 || listings != 1 {
			t.Fatalf("excluded repeat: %d runs, %d listings", len(r.runs()), listings)
		}
	}
}

func TestExecDuplicateBurstDoesNotCountAsDropped(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{code: 3}
	d := newTestDispatcher(r, execOptions{repeatWindow: DefaultRepeatWindow}, io.Discard)
	var b batch
	for range maxBatch + 7 {
		b.add(stampedEvent{at: time.Now(), e: announcement("same")})
	}
	if len(b.events) != 1 || b.dropped != 0 {
		t.Fatalf("duplicate burst: %d events, %d dropped", len(b.events), b.dropped)
	}
	d.runBatch(t.Context(), b)
	d.runBatch(t.Context(), b)
	if len(r.runs()) != 1 {
		t.Fatalf("failed handler repeated within the window: %+v", r.runs())
	}
}

// Runs happen one at a time; past the bound, batches join the last one
// waiting instead of being lost.
func TestExecQueueIsBounded(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, maxQueue+4)}
	var errOut syncBuffer
	d := newTestDispatcher(r, execOptions{debounce: time.Hour}, &errOut)
	go d.work(t.Context())
	d.add(announcement("first"))
	d.flush()
	<-r.started
	for i := range maxQueue + 3 {
		d.add(announcement("e" + string(rune('a'+i))))
		d.flush()
	}
	close(r.block)
	d.close()
	<-d.done
	runs := r.runs()
	if len(runs) != 1+maxQueue || len(runs[maxQueue].input.Events) != 4 {
		t.Fatalf("%d runs, the last of %d events", len(runs), len(runs[len(runs)-1].input.Events))
	}
}

func TestExecOutcomes(t *testing.T) {
	t.Parallel()
	run := func(r runner, opts execOptions) string {
		var errOut syncBuffer
		d := newTestDispatcher(r, opts, &errOut)
		go d.work(t.Context())
		d.add(announcement("a"))
		d.close()
		<-d.done
		return errOut.String()
	}
	if got := run(&fakeRunner{code: 3}, execOptions{}); !strings.Contains(got, "exec: prog exited 3") {
		t.Errorf("%q", got)
	}
	if got := run(&fakeRunner{err: errors.New("no such file")}, execOptions{}); !strings.Contains(got, "exec: prog: no such file") {
		t.Errorf("%q", got)
	}
	if got := run(&fakeRunner{block: make(chan struct{})}, execOptions{timeout: 20 * time.Millisecond}); !strings.Contains(got, "timed out after 20ms") {
		t.Errorf("%q", got)
	}
	// The library for the paths cannot be read: a run goes on without them,
	// unless a filter needs them.
	broken := func(context.Context) (map[string]port.Entry, error) { return nil, errors.New("offline") }
	for only, runs := range map[string]int{"": 1, onlyFile: 0} {
		r := &fakeRunner{}
		var errOut syncBuffer
		d := newDispatcher(execOptions{argv: []string{"p"}, timeout: time.Minute, only: only}, r, broken, &errOut, time.Now)
		go d.work(t.Context())
		d.add(announcement("a"))
		d.close()
		<-d.done
		if len(r.runs()) != runs || !strings.Contains(errOut.String(), "the library for the paths: offline") {
			t.Errorf("only %q: %d runs %q", only, len(r.runs()), errOut.String())
		}
	}
}

// Ctrl-C leaves the run under way its grace period, then stops it, and
// drops the runs still waiting.
func TestExecInterrupt(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	r := &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, 4)}
	var errOut syncBuffer
	d := newTestDispatcher(r, execOptions{debounce: time.Hour, grace: 20 * time.Millisecond}, &errOut)
	go d.work(ctx)
	d.add(announcement("a"))
	d.flush()
	<-r.started
	d.add(announcement("b"))
	d.flush()
	cancel()
	<-d.done
	if got := errOut.String(); !strings.Contains(got, "exec: prog was stopped at interrupt") || !strings.Contains(got, "1 runs dropped at interrupt") || len(r.runs()) != 1 {
		t.Fatalf("%q %d", got, len(r.runs()))
	}
}

// The real runner passes the environment and standard input, and answers
// the exit status.
func TestExecRunner(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	var out bytes.Buffer
	code, err := execRunner{}.Run(t.Context(), []string{"sh", "-c", `cat; printf '%s\n' "$SNCTL_X"; exit 3`}, []string{"SNCTL_X=; rm -rf /"}, []byte("in\n"), &out)
	if err != nil || code != 3 || out.String() != "in\n; rm -rf /\n" {
		t.Fatalf("%d %v %q", code, err, out.String())
	}
	// A program that never starts has no exit status: -1 and the start
	// error, never a panic on the missing process state.
	if code, err := (execRunner{}).Run(t.Context(), []string{"/no/such/program"}, nil, nil, &out); code != -1 || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing program: %d %v", code, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := (execRunner{}).Run(ctx, []string{"sleep", "5"}, nil, nil, &out); err == nil {
		t.Fatal("a stopped run succeeded")
	}
}

// syncBuffer is a buffer the dispatcher and the test share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// A burst within one debounce window keeps the newest maxBatch events and
// counts the rest; the pending batch never grows past the bound.
func TestExecBatchIsBoundedWithinOneWindow(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{}
	var errOut syncBuffer
	d := newTestDispatcher(r, execOptions{debounce: time.Hour}, &errOut)
	const burst = 2500
	for i := range burst {
		d.add(announcement("e" + strconv.Itoa(i)))
		d.mu.Lock()
		n, c := len(d.pending.events), cap(d.pending.events)
		d.mu.Unlock()
		if n > maxBatch || c > 2*maxBatch {
			t.Fatalf("after %d events the pending batch holds %d (capacity %d)", i+1, n, c)
		}
	}
	go d.work(t.Context())
	d.close()
	<-d.done
	runs := r.runs()
	if len(runs) != 1 {
		t.Fatalf("%d runs", len(runs))
	}
	in := runs[0].input
	if len(in.Events) != maxBatch || in.Dropped != burst-maxBatch || in.Events[0].ID != "e1500" || in.Events[maxBatch-1].ID != "e2499" ||
		runs[0].env["SNCTL_DROPPED"] != "1500" || runs[0].env["SNCTL_COUNT"] != "1000" {
		t.Fatalf("%d events, dropped %d, env %v", len(in.Events), in.Dropped, runs[0].env["SNCTL_DROPPED"])
	}
	if got := errOut.String(); strings.Count(got, "older events of this batch were dropped") != 1 || !strings.Contains(got, "exec: 1500 older events") {
		t.Fatalf("%q", got)
	}
}

// A batch that lost events runs even when no kept event passes the filters,
// or when the library cannot be read to apply them: the program must learn
// that it missed events.
func TestExecDroppedRunsDespiteFilters(t *testing.T) {
	t.Parallel()
	for name, tree := range map[string]func(context.Context) (map[string]port.Entry, error){
		"none kept": func(context.Context) (map[string]port.Entry, error) { return map[string]port.Entry{}, nil },
		"no library": func(context.Context) (map[string]port.Entry, error) {
			return nil, errors.New("offline")
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := &fakeRunner{}
			var errOut syncBuffer
			d := newDispatcher(execOptions{argv: []string{"prog"}, timeout: time.Minute, debounce: time.Hour, only: "file"},
				r, tree, &errOut, time.Now)
			for i := range maxBatch + 7 {
				d.add(announcement("e" + strconv.Itoa(i)))
			}
			go d.work(t.Context())
			d.close()
			<-d.done
			runs := r.runs()
			if len(runs) != 1 {
				t.Fatalf("%d runs", len(runs))
			}
			in, env := runs[0].input, runs[0].env
			if len(in.Events) != 0 || in.Dropped != 7 || env["SNCTL_DROPPED"] != "7" || env["SNCTL_COUNT"] != "0" || env["SNCTL_EVENT"] != "" {
				t.Fatalf("%d events, dropped %d, env %v", len(in.Events), in.Dropped, env)
			}
		})
	}
	// Without a loss, a batch the filters empty still makes no run.
	r := &fakeRunner{}
	d := newDispatcher(execOptions{argv: []string{"prog"}, timeout: time.Minute, debounce: time.Hour, only: "file"},
		r, func(context.Context) (map[string]port.Entry, error) { return map[string]port.Entry{}, nil }, io.Discard, time.Now)
	d.add(announcement("gone"))
	go d.work(t.Context())
	d.close()
	<-d.done
	if runs := r.runs(); len(runs) != 0 {
		t.Fatalf("%d runs", len(runs))
	}
}

// Merged into the last batch of a full queue, the batch keeps the bound
// and adds up what both dropped.
func TestExecMergeRespectsTheBound(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, maxQueue+4)}
	var errOut syncBuffer
	d := newTestDispatcher(r, execOptions{debounce: time.Hour}, &errOut)
	go d.work(t.Context())
	d.add(announcement("first"))
	d.flush()
	<-r.started
	for i := range maxQueue {
		d.add(announcement("q" + strconv.Itoa(i)))
		d.flush()
	}
	// The queue is full: 1500 more events, 500 dropped while pending and
	// one more when they join the last batch waiting.
	for i := range 1500 {
		d.add(announcement("m" + strconv.Itoa(i)))
	}
	d.flush()
	d.mu.Lock()
	last := d.queue[len(d.queue)-1]
	d.mu.Unlock()
	if len(last.events) != maxBatch || last.dropped != 501 {
		t.Fatalf("the last batch holds %d, dropped %d", len(last.events), last.dropped)
	}
	close(r.block)
	d.close()
	<-d.done
	runs := r.runs()
	in := runs[len(runs)-1].input
	if len(runs) != 1+maxQueue || len(in.Events) != maxBatch || in.Dropped != 501 || in.Events[0].ID != "m500" || in.Events[maxBatch-1].ID != "m1499" {
		t.Fatalf("%d runs; the last of %d events, dropped %d", len(runs), len(in.Events), in.Dropped)
	}
	if in := runs[1].input; in.Dropped != 0 || runs[1].env["SNCTL_DROPPED"] != "0" {
		t.Fatalf("a whole batch reports drops: %+v", in)
	}
}

// The command gets each SNCTL_ variable once, with the event's value, even
// when snctl's own environment already holds a stale one.
func TestExecRunnerReplacesInheritedVariables(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	r := execRunner{environ: func() []string {
		return []string{"SNCTL_EVENT=stale", "SNCTL_PATH=stale", "SNCTL_KEEP=kept", "PATH=" + os.Getenv("PATH")}
	}}
	var out bytes.Buffer
	if _, err := r.Run(t.Context(), []string{"env"}, []string{"SNCTL_EVENT=DocAdded", "SNCTL_PATH=/Books/Paper"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	seen := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		k, v, _ := strings.Cut(line, "=")
		seen[k] = append(seen[k], v)
	}
	for k, want := range map[string]string{"SNCTL_EVENT": "DocAdded", "SNCTL_PATH": "/Books/Paper", "SNCTL_KEEP": "kept"} {
		if !slices.Equal(seen[k], []string{want}) {
			t.Errorf("%s: %q, want once %q", k, seen[k], want)
		}
	}
}

func TestChildEnv(t *testing.T) {
	t.Parallel()
	base := []string{"snctl_event=stale", "SNCTL_PATH=stale", "=C:=C:\\x", "HOME=/h", "ODD"}
	set := []string{"SNCTL_EVENT=new", "SNCTL_PATH=/p"}
	// Keys compare with case on POSIX: snctl_event is another variable.
	if got := childEnv(base, set, false); !slices.Equal(got, []string{"snctl_event=stale", "=C:=C:\\x", "HOME=/h", "ODD", "SNCTL_EVENT=new", "SNCTL_PATH=/p"}) {
		t.Fatalf("%q", got)
	}
	// Without case on Windows; the per-drive entry is kept.
	if got := childEnv(base, set, true); !slices.Equal(got, []string{"=C:=C:\\x", "HOME=/h", "ODD", "SNCTL_EVENT=new", "SNCTL_PATH=/p"}) {
		t.Fatalf("%q", got)
	}
	for kv, key := range map[string]string{"A=b": "A", "=C:=C:\\x": "=C:", "ODD": "ODD", "": "", "K==v": "K"} {
		if got := envKey(kv); got != key {
			t.Errorf("envKey(%q) = %q, want %q", kv, got, key)
		}
	}
}

func TestExecDescriptions(t *testing.T) {
	tree := map[string]port.Entry{
		"a": {ID: "a", Name: "$(touch sentinel).note", Path: "NOTE/Note/A/a.note"},
		"f": {ID: "f", Name: "A", Path: "NOTE/Note/A", Folder: true},
		"b": {ID: "b", Name: "b.note", Path: "NOTE/Note/AB/b.note"},
	}
	for _, tt := range []struct {
		only, under string
		want        []string
	}{
		{"", "", []string{"a", "f", "b", "gone"}},
		{onlyFile, "/NOTE/Note/A", []string{"a"}},
		{onlyFolder, "", []string{"f"}},
		{"", "/NOTE/Note/A", []string{"a", "f"}},
	} {
		r := &fakeRunner{}
		var diag syncBuffer
		d := newDispatcher(execOptions{argv: []string{"prog"}, timeout: time.Second, debounce: time.Hour, only: tt.only, under: tt.under}, r,
			func(context.Context) (map[string]port.Entry, error) { return tree, nil }, &diag, time.Now)
		for _, id := range []string{"a", "f", "b", "gone"} {
			d.add(port.Change{Type: "files", Kind: "MODIFYFILE", ID: id})
		}
		go d.work(t.Context())
		d.close()
		<-d.done
		runs := r.runs()
		if len(runs) != 1 {
			t.Fatal(runs)
		}
		var got []string
		for _, e := range runs[0].input.Events {
			got = append(got, e.ID)
		}
		if !slices.Equal(got, tt.want) {
			t.Fatal(got, tt.want)
		}
	}
	r := &fakeRunner{}
	d := newTestDispatcher(r, execOptions{}, io.Discard)
	d.add(port.Change{Type: "connected"})
	go d.work(t.Context())
	d.close()
	<-d.done
	if r.runs()[0].env["SNCTL_EVENT"] != "connected" {
		t.Fatal(r.runs())
	}
}

func TestInterruptDuringDescriptionDoesNotStartAChild(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	run := &fakeRunner{}
	d := newDispatcher(execOptions{argv: []string{"prog"}, timeout: time.Second}, run,
		func(context.Context) (map[string]port.Entry, error) { cancel(); return nil, context.Canceled }, io.Discard, time.Now)
	d.runBatch(ctx, batch{events: []stampedEvent{{e: announcement("a"), at: time.Now()}}})
	if len(run.runs()) != 0 {
		t.Fatal("started a child after cancellation")
	}
}

func TestUnderCanonicalEquivalence(t *testing.T) {
	t.Parallel()
	for _, under := range []string{"/NOTE/Note/Café", "/NOTE/Note/Cafe\u0301"} {
		d := dispatcher{opts: execOptions{under: under}}
		for _, name := range []string{"Café", "Cafe\u0301"} {
			for _, suffix := range []string{"", "/a.note", "other/a.note"} {
				original := "/NOTE/Note/" + name + suffix
				tree := map[string]port.Entry{"id": {Path: original}}
				ev, ok := d.describe(tree, stampedEvent{e: port.Change{Type: "files", ID: "id"}})
				if ok != (suffix != "other/a.note") || ev.Path != original {
					t.Fatal(under, original, ev, ok)
				}
			}
		}
	}
}

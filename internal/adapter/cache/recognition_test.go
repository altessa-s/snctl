// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cache

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestRecognitionIndex(t *testing.T) {
	s := fixture(t)
	a, _ := body(t, "alpha")
	b, _ := body(t, "beta")
	if s.Recognition(a.Hash) != nil || s.Recognition("../bad") != nil {
		t.Fatal("missing index is known")
	}
	if _, err := os.Stat(s.Dir); !os.IsNotExist(err) {
		t.Fatal("lookup created cache")
	}
	if err := s.RememberRecognition(t.Context(), "bad", true); err == nil {
		t.Fatal("invalid key accepted")
	}
	var wg sync.WaitGroup
	for _, hash := range []string{a.Hash, b.Hash} {
		wg.Go(func() {
			other := fixtureStore(s)
			if err := other.RememberRecognition(t.Context(), hash, hash == a.Hash); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	s = fixtureStore(s)
	if got := s.Recognition(a.Hash); got == nil || !*got {
		t.Fatal("yes lost")
	}
	if got := s.Recognition(b.Hash); got == nil || *got {
		t.Fatal("no lost")
	}
	if err := s.Clear(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s.Recognition(a.Hash) != nil {
		t.Fatal("clear retained recognition")
	}
	for _, data := range []string{`{"version":2,"bodies":{}}`, "damaged"} {
		if err := os.WriteFile(filepath.Join(s.Dir, recognitionFile), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		s = fixtureStore(s)
		if s.Recognition(a.Hash) != nil {
			t.Fatal("damaged index trusted")
		}
	}
	if err := s.RememberRecognition(t.Context(), a.Hash, true); err != nil {
		t.Fatal("damaged index not repaired", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.RememberRecognition(ctx, a.Hash, true); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func fixtureStore(s *Store) *Store { return &Store{Dir: s.Dir, Now: s.Now} }

func TestRecognitionSnapshotAndBatchMerge(t *testing.T) {
	s := fixture(t)
	a, _ := body(t, "alpha")
	b, _ := body(t, "beta")
	if err := s.RememberRecognitions(t.Context(), map[string]bool{a.Hash: true}); err != nil {
		t.Fatal(err)
	}
	snapshot := s.RecognitionSnapshot()
	snapshot[a.Hash] = false
	if !*s.Recognition(a.Hash) {
		t.Fatal("snapshot aliases store")
	}
	other := fixtureStore(s)
	if err := other.RememberRecognition(t.Context(), b.Hash, false); err != nil {
		t.Fatal(err)
	}
	// A command keeps its snapshot; writes reload disk under the cross-process lock.
	if s.Recognition(b.Hash) != nil {
		t.Fatal("snapshot unexpectedly reread disk")
	}
	if err := s.RememberRecognitions(t.Context(), map[string]bool{a.Hash: false}); err != nil {
		t.Fatal(err)
	}
	latest := fixtureStore(s)
	if got := latest.Recognition(b.Hash); got == nil || *got {
		t.Fatal("concurrent write lost")
	}
	if got := latest.Recognition(a.Hash); got == nil || *got {
		t.Fatal("batch update lost")
	}
	if err := s.RememberRecognitions(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.RememberRecognitions(t.Context(), map[string]bool{"invalid": true}); err == nil {
		t.Fatal("invalid batch accepted")
	}
	// Corruption never means false and is repaired by the next merge.
	for _, contents := range []string{"broken", `{"version":1,"bodies":[]}`} {
		if err := os.WriteFile(filepath.Join(s.Dir, recognitionFile), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		s = fixtureStore(s)
		if s.Recognition(a.Hash) != nil {
			t.Fatal("corruption trusted")
		}
		if err := s.RememberRecognition(t.Context(), a.Hash, true); err != nil {
			t.Fatal(err)
		}
		if got := fixtureStore(s).Recognition(a.Hash); got == nil || !*got {
			t.Fatal("corruption not repaired")
		}
	}
}

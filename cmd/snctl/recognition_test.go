//go:build cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"
)

func recognitionEntries(t *testing.T, text string) []port.Entry {
	t.Helper()
	var entries []port.Entry
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatal(err, text)
	}
	return entries
}

func TestRecognitionLifecycle(t *testing.T) {
	e := newE2E(t)
	e.cloud.Seed("NOTE/Note/Recognized.note", false, porttest.RecognitionNote("Al\u00adpha\r\nbeta", "Гамма"))
	e.cloud.Seed("NOTE/Note/Blank.note", false, porttest.RecognitionNote(""))
	for _, en := range recognitionEntries(t, e.must("ls", "NOTE/Note", "--json")) {
		if en.Recognition != nil {
			t.Fatal("listing guessed recognition", en)
		}
	}
	before := len(e.cloud.Requests())
	entries := recognitionEntries(t, e.must("ls", "NOTE/Note", "--recognition", "--json"))
	for _, en := range entries {
		if en.Recognition == nil || *en.Recognition != (en.Name == "Recognized.note") {
			t.Fatal(en)
		}
	}
	after := len(e.cloud.Requests())
	if after <= before {
		t.Fatal("discovery fetched nothing")
	}
	e.must("--offline", "tree", "NOTE/Note", "--recognition", "--json")
	if len(e.cloud.Requests()) != after {
		t.Fatal("offline discovery contacted cloud")
	}
	found := e.must("--offline", "find", "--text", "ALPHA гамма", "NOTE/Note", "--json")
	if !strings.Contains(found, `"pages": [`) || !strings.Contains(found, "Recognized.note") || strings.Contains(found, "Blank.note") {
		t.Fatal(found)
	}
	if len(e.cloud.Requests()) != after {
		t.Fatal("offline search contacted cloud")
	}
	e.cloud.Seed("NOTE/Note/New.note", false, porttest.RecognitionNote("new words"))
	e.must("get", "NOTE/Note/New.note", "-o", filepath.Join(e.root, "new.note"))
	stat := e.must("--offline", "stat", "NOTE/Note/New.note", "--json")
	if !strings.Contains(stat, `"recognition": true`) {
		t.Fatal(stat)
	}
	// A changed MD5 cannot inherit the earlier boolean.
	e.cloud.Seed("NOTE/Note/Changed.note", false, porttest.RecognitionNote("other"))
	stat = e.must("stat", "NOTE/Note/Changed.note", "--json")
	if !strings.Contains(stat, `"recognition": null`) {
		t.Fatal(stat)
	}
}

func TestRecognitionExportsAndLocalSearch(t *testing.T) {
	e := newE2E(t)
	local := filepath.Join(e.root, "local.note")
	raw := "co\u00adpy\u200b\u200c\u200d\u2060\ufeff\r\nтекст"
	if err := os.WriteFile(local, porttest.RecognitionNote(raw), 0600); err != nil {
		t.Fatal(err)
	}
	clean := e.must("text", local, "--json")
	stored := e.must("text", local, "--raw", "--json")
	if strings.Contains(stored, `"header"`) {
		t.Fatal("text --raw exposed inspection metadata")
	}
	if strings.Contains(clean, "\u00ad") || !strings.Contains(stored, "\u00ad") || !strings.Contains(clean, "copy") {
		t.Fatal(clean, stored)
	}
	for _, ext := range []string{"md", "txt", "json"} {
		dest := filepath.Join(e.root, "export."+ext)
		e.must("text", local, "-o", dest)
		data, err := os.ReadFile(dest)
		if err != nil || strings.Contains(string(data), "\u00ad") || !strings.Contains(string(data), "copy") {
			t.Fatal(ext, err, string(data))
		}
		// Replacements use the same local overwrite rule as render/get.
		e.must("text", local, "-o", dest)
	}
	if out := e.must("find", "--text", "COPY текст", e.root, "--json"); !strings.Contains(out, "local.note") {
		t.Fatal(out)
	}
	if _, _, code := e.run("text", local, "--raw"); code != 2 {
		t.Fatal("raw non-JSON status", code)
	}
	e.cloud.Seed("NOTE/Note/Sub", true, nil)
	e.cloud.Seed("NOTE/Note/Sub/Yes.note", false, porttest.RecognitionNote("folder words"))
	e.cloud.Seed("NOTE/Note/No.note", false, porttest.RecognitionNote(""))
	dir := filepath.Join(e.root, "exports")
	e.must("text", "NOTE/Note", "-o", dir, "--format", "markdown")
	data, err := os.ReadFile(filepath.Join(dir, "Sub", "Yes.md"))
	if err != nil || !strings.Contains(string(data), "## Page 1") || !strings.Contains(string(data), "folder words") {
		t.Fatal(err, string(data))
	}
	if _, err = os.Stat(filepath.Join(dir, "No.md")); !os.IsNotExist(err) {
		t.Fatal("unrecognized notebook exported", err)
	}
}

func TestRecognitionModesAndBudget(t *testing.T) {
	e := newE2E(t)
	e.cloud.Seed("NOTE/Note/Yes.note", false, porttest.RecognitionNote(strings.Repeat("hello ", 150000)))
	e.must("ls", "NOTE/Note", "--json")
	before := len(e.cloud.Requests())
	if _, _, code := e.run("--offline", "ls", "NOTE/Note", "--recognition"); code == 0 {
		t.Fatal("missing body accepted")
	}
	if len(e.cloud.Requests()) != before {
		t.Fatal("offline miss contacted cloud")
	}
	e.s.env["SNCTL_CACHE_BUDGET"] = "1MiB"
	if _, errOut, code := e.run("ls", "NOTE/Note", "--recognition"); code == 0 || !strings.Contains(errOut, "budget") {
		t.Fatal(code, errOut)
	}
	// no-cache allows discovery but leaves no persistent mark.
	entries := recognitionEntries(t, e.must("--no-cache", "ls", "NOTE/Note", "--recognition", "--json"))
	if len(entries) != 1 || entries[0].Recognition == nil || !*entries[0].Recognition {
		t.Fatal(entries)
	}
	entries = recognitionEntries(t, e.must("--no-cache", "ls", "NOTE/Note", "--json"))
	if entries[0].Recognition != nil {
		t.Fatal("no-cache used persistent index")
	}
}

func TestRecognitionCachedSyncFill(t *testing.T) {
	e := newE2E(t)
	e.cloud.Seed("NOTE/Note/Sync.note", false, porttest.RecognitionNote("sync phrase"))
	e.must("sync", "NOTE/Note", "--files")
	var statuses []port.CacheStatus
	if err := json.Unmarshal([]byte(e.must("debug", "cache", "status", "--json")), &statuses); err != nil || len(statuses) != 1 {
		t.Fatal(statuses, err)
	}
	index := filepath.Join(statuses[0].Dir, "recognition")
	// e.run redacts its temporary root in answers; restore it for filesystem checks.
	index = strings.ReplaceAll(index, "$ROOT", e.root)
	data, err := os.ReadFile(index)
	if err != nil || strings.Contains(string(data), "sync phrase") {
		t.Fatal("index stores text or is missing", err)
	}
	if err = os.Remove(index); err != nil {
		t.Fatal(err)
	}
	before := len(e.cloud.Requests())
	e.must("sync", "NOTE/Note", "--files")
	for _, request := range e.cloud.Requests()[before:] {
		if strings.Contains(request.Path, "download") {
			t.Fatal("cached sync downloaded body", request)
		}
	}
	if out := e.must("--offline", "stat", "NOTE/Note/Sync.note", "--json"); !strings.Contains(out, `"recognition": true`) {
		t.Fatal(out)
	}
}

func TestRecognitionScanPreservesPartialListing(t *testing.T) {
	e := newE2E(t)
	e.cloud.Seed("NOTE/Note/Bad.note", false, []byte("damaged"))
	e.cloud.Seed("NOTE/Note/Good.note", false, porttest.RecognitionNote("words"))
	out, errOut, code := e.run("ls", "NOTE/Note", "--recognition", "--json")
	entries := recognitionEntries(t, out)
	if code != 1 || len(entries) != 2 || !strings.Contains(errOut, "Bad.note") {
		t.Fatal(code, out, errOut)
	}
	for _, en := range entries {
		if en.Name == "Bad.note" && en.Recognition != nil {
			t.Fatal("damaged notebook given known state")
		}
		if en.Name == "Good.note" && (en.Recognition == nil || !*en.Recognition) {
			t.Fatal("good result lost")
		}
	}
	plain := e.must("tree", "NOTE/Note", "--format", "plain")
	if plain != "Bad.note\nGood.note\n" {
		t.Fatal("plain tree names differ", plain)
	}
	// Both bodies are cached; damaged input remains unknown and reports a partial failure.
	before := len(e.cloud.Requests())
	out, errOut, code = e.run("--offline", "tree", "NOTE/Note", "--recognition", "--format", "plain")
	if code != 1 || out != "Bad.note\t?\nGood.note\tyes\n" || !strings.Contains(errOut, "Bad.note") {
		t.Fatal(code, out, errOut)
	}
	if strings.Contains(errOut, "cached body missing") || len(e.cloud.Requests()) != before {
		t.Fatal("damaged body was not retained", errOut)
	}
}

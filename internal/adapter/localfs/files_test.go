// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type broken struct{}

func (broken) Read([]byte) (int, error)  { return 0, errors.New("synthetic read failure") }
func (broken) Write([]byte) (int, error) { return 0, errors.New("synthetic write failure") }

func TestStageAndSave(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := Files{Dir: dir}
	source, err := f.Stage(strings.NewReader("synthetic bytes"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := f.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(data) != "synthetic bytes" {
		t.Fatal("staging changed bytes")
	}
	var stdout bytes.Buffer
	if err = f.Save(source, "-", "sample.txt", &stdout); err != nil || stdout.String() != "synthetic bytes" {
		t.Fatal("stdout download failed")
	}
	if err = f.Save(source, dir, "sample.txt", io.Discard); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(dir, "sample.txt")
	if err = f.Save(source, destination, "ignored", io.Discard); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(destination)
	if err != nil || string(data) != "synthetic bytes" {
		t.Fatal("replacement failed")
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatal("destination directory was removed")
	}
	for _, name := range []string{"", ".", "..", "../escape"} {
		if err = f.Save(source, dir, name, io.Discard); err == nil {
			t.Fatalf("unsafe name %q accepted", name)
		}
	}
	nested := filepath.Join(dir, "nested")
	if err = os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if err = f.Save(source, dir, "nested", io.Discard); err == nil {
		t.Fatal("replaced a destination directory")
	}
	if err = f.Save(source, filepath.Join(dir, "missing", "file"), "file", io.Discard); err == nil {
		t.Fatal("missing destination accepted")
	}
	if err = f.Save(source, filepath.Join(destination, "file"), "file", io.Discard); err == nil {
		t.Fatal("invalid destination accepted")
	}
	if err = f.Save(source, "-", "file", broken{}); err == nil {
		t.Fatal("output failure ignored")
	}
	if err = f.Save("absent", "-", "file", io.Discard); err == nil {
		t.Fatal("missing source accepted")
	}
	if err = f.Save(dir, destination, "file", io.Discard); err == nil {
		t.Fatal("directory source accepted")
	}
	if _, err = f.Stage(broken{}); err == nil {
		t.Fatal("staging failure ignored")
	}
	if _, err = (Files{Dir: filepath.Join(dir, "absent")}).Stage(strings.NewReader("data")); err == nil {
		t.Fatal("invalid staging directory accepted")
	}
	if err = f.Remove(source); err != nil {
		t.Fatal(err)
	}
	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if strings.HasPrefix(name.Name(), ".snctl") || strings.HasPrefix(name.Name(), "snctl-upload") {
			t.Fatal("temporary file leaked")
		}
	}
}

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package autostart_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"

	"github.com/altessa-s/snctl/internal/adapter/autostart"
	"github.com/altessa-s/snctl/internal/port"
)

func TestDefinitionsGolden(t *testing.T) {
	t.Parallel()
	spec := port.ServiceSpec{Program: "/opt/My Apps/snctl", Args: []string{"mirror", "run", "--service"},
		Env: map[string]string{"SNCTL_CONFIG_DIR": "/data/<config> & 100%", "SNCTL_SECRETS": "file", "SNCTL_PROFILE": "work"}}
	task := autostart.TaskXML(spec, `PC\owner`)
	units := make([]uint16, 0, len(task)/2)
	for i := 2; i < len(task); i += 2 {
		units = append(units, binary.LittleEndian.Uint16(task[i:]))
	}
	for name, got := range map[string][]byte{
		"launchd": autostart.LaunchAgent(spec, "/data/logs/mirror-service.out"),
		"systemd": autostart.SystemdUnit(spec),
		"windows": []byte(string(utf16.Decode(units))),
	} {
		file := filepath.Join("testdata", name+".golden")
		if os.Getenv("SNCTL_UPDATE_GOLDEN") == "1" {
			if err := os.MkdirAll("testdata", 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, got, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("%s differs:\n%s", name, got)
		}
	}
}

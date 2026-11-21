// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package cloud

import (
	"fmt"
	"testing"

	sncloud "github.com/altessa-s/sncloud-go"
)

func BenchmarkResolveLargeUnicodeTree(b *testing.B) {
	entries := unicodeTree()[:3]
	for folder := range 100 {
		parent := fmt.Sprintf("DOCUMENT/Document/и\u0306/folder-%d", folder)
		entries = append(entries, sncloud.DeviceEntry{ID: sncloud.FileID(len(entries) + 1), Path: parent, IsFolder: true})
		for file := range 100 {
			entries = append(entries, sncloud.DeviceEntry{
				ID: sncloud.FileID(len(entries) + 1), Path: fmt.Sprintf("%s/и\u0306-%d.pdf", parent, file),
			})
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		e, err := resolve(entries, "DOCUMENT/Document/й/folder-99/й-99.pdf")
		if err != nil || e.ID != sncloud.FileID(len(entries)) {
			b.Fatal(e, err)
		}
	}
}

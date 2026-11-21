// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// SafeName keeps cloud names inside one portable filesystem component.
func SafeName(name string) string {
	original := name
	name = strings.Map(func(r rune) rune {
		if r < ' ' || r == 127 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, name)
	name = strings.TrimRight(strings.TrimSpace(name), " .")
	if strings.HasPrefix(name, "..") {
		name = strings.TrimLeft(name, ".")
	}
	if name == "" {
		return "notebook"
	}
	stem := strings.ToUpper(strings.TrimRight(strings.SplitN(name, ".", 2)[0], " "))
	reserved := stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || stem == "CONIN$" || stem == "CONOUT$"
	if strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT") {
		suffix := strings.TrimPrefix(strings.TrimPrefix(stem, "COM"), "LPT")
		reserved = len(suffix) == 1 && suffix[0] >= '1' && suffix[0] <= '9' || suffix == "¹" || suffix == "²" || suffix == "³"
	}
	if reserved {
		name = "_" + name
	}
	const componentLimit = 240
	const extensionLimit = 32
	if len(name) > componentLimit {
		ext := filepath.Ext(name)
		if len(ext) > extensionLimit {
			ext = ""
		}
		hash := sha256.Sum256([]byte(original))
		suffix := fmt.Sprintf("-%x%s", hash[:6], ext)
		cut := componentLimit - len(suffix)
		for !utf8.RuneStart(name[cut]) {
			cut--
		}
		name = name[:cut] + suffix
	}
	return name
}

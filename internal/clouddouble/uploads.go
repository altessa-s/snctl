// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package clouddouble

import (
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

// UploadMode keeps differing server collision behaviors explicit in tests.
type UploadMode int

const (
	// Overwrite matches Private Cloud 26.10.09, keeping the existing identifier.
	Overwrite UploadMode = iota
	// Duplicate creates a second entry at the same path.
	Duplicate
	// Autorename preserves the existing path and assigns a new name.
	Autorename
	// RefuseOverwrite rejects collisions while allowing uploads to unused paths.
	RefuseOverwrite
)

// SetUploadMode selects collision behavior without racing requests.
func (c *Cloud) SetUploadMode(mode UploadMode) { c.mu.Lock(); defer c.mu.Unlock(); c.mode = mode }

// ClearFaults ends persistent injections.
func (c *Cloud) ClearFaults() { c.mu.Lock(); defer c.mu.Unlock(); c.faults = nil }

// ExpireLockAfter bounds a synthetic lock's lifetime.
func (c *Cloud) ExpireLockAfter(ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lockUntil = time.Now().Add(ttl) //nolint:forbidigo // network fault expiry uses elapsed wall time
}

func storageDownload(name string) bool {
	return name == "/storage/download" || name == "/api/oss/download"
}

func (c *Cloud) upload(name string, data []byte) Entry {
	old, exists := c.find(name)
	if !exists || c.mode == Overwrite {
		return c.add(name, false, data)
	}
	if c.mode == Autorename {
		ext := path.Ext(name)
		for n := 1; ; n++ {
			candidate := strings.TrimSuffix(name, ext) + "(" + strconv.Itoa(n) + ")" + ext
			if _, found := c.find(candidate); !found {
				return c.add(candidate, false, data)
			}
		}
	}
	// Hide the old entry only while allocating a distinct identifier.
	delete(c.entries, old.ID)
	e := c.add(name, false, data)
	c.entries[old.ID] = old
	return e
}

const uploadChunkSize = 8 << 20

type formUpload struct {
	key   string
	count int
	parts map[int][]byte
}

func (c *Cloud) part(w http.ResponseWriter, r *http.Request, key string, data []byte) {
	q := r.URL.Query()
	id := q.Get("uploadId")
	number, err := strconv.Atoi(q.Get("partNumber"))
	count, countErr := strconv.Atoi(q.Get("totalChunks"))
	if id == "" || err != nil || countErr != nil || count < 1 || count > storageLimit/uploadChunkSize+1 || number < 1 || number > count {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	upload := c.parts[id]
	if upload == nil {
		upload = &formUpload{key: key, count: count, parts: map[int][]byte{}}
		c.parts[id] = upload
	}
	if upload.key != key || upload.count != count {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	upload.parts[number] = data
	if len(upload.parts) == count {
		var assembled []byte
		for n := 1; n <= count; n++ {
			assembled = append(assembled, upload.parts[n]...)
		}
		c.objects[key] = assembled
	}
	reply(w, map[string]any{"chunkMd5": Sum(data)})
}

func lockLive(until time.Time) bool {
	return time.Now().Before(until) //nolint:forbidigo // network fault expiry uses elapsed wall time
}

// VisibilityMode selects when committed uploads become queryable.
type VisibilityMode int

const (
	// VisibleImmediately exposes committed files even while a lock is held.
	VisibleImmediately VisibilityMode = iota
	// HiddenUntilUnlock hides rejected finishes until the sync lock ends.
	HiddenUntilUnlock
)

// SetVisibilityMode selects the visibility of rejected finishes.
func (c *Cloud) SetVisibilityMode(mode VisibilityMode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.visibility = mode
}

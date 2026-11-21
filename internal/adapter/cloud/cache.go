// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/altessa-s/snctl/internal/port"

	sncloud "github.com/altessa-s/sncloud-go"
)

// SetCache installs the per-invocation read policy. A zero TTL always lists live.
func (c *Client) SetCache(store port.FileCache, offline bool, ttl time.Duration, now func() time.Time, warning func(string)) {
	c.cache, c.offline, c.ttl, c.now, c.warning = store, offline, ttl, now, warning
	c.sessionCache = store
}

func (c *Client) cachedTree(ctx context.Context) ([]sncloud.DeviceEntry, error) {
	if c.offline && c.cache == nil {
		return nil, errors.New("offline: cached listing is unavailable with --no-cache")
	}
	if c.cache != nil && (c.offline || c.ttl > 0) {
		l, err := c.cachedListing()
		age := c.now().Sub(l.Confirmed)
		if err == nil && (c.offline || age >= 0 && age < c.ttl) {
			if c.offline && age > 24*time.Hour && c.warning != nil {
				c.warning("cached listing was confirmed more than a day ago")
			}
			entries, decodeErr := cachedEntries(l)
			if decodeErr == nil {
				c.view = &listing{list: entries}
				return entries, nil
			}
			err = decodeErr
		}
		if c.offline {
			return nil, err
		}
		if err != nil {
			c.cacheDiagnostic("read listing", err)
		}
	}
	entries, err := c.device.List(ctx, "/", true)
	if err != nil {
		return nil, c.failure("list", "/", err)
	}
	if c.cache != nil {
		c.saveListing(ctx, entries)
		c.view = &listing{list: entries}
	}
	return entries, nil
}

func (c *Client) invalidate(ctx context.Context) error {
	if c.cache != nil {
		if err := c.cache.Invalidate(ctx); err != nil {
			if c.offline {
				return err
			}
			c.cacheDiagnostic("invalidate listing", err)
		}
	}
	return nil
}

func (c *Client) cacheEndpoint() string {
	endpoint := c.endpoint
	if endpoint == "official" {
		endpoint = sncloud.Official().AppHost
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	u.User, u.RawQuery, u.Fragment, u.ForceQuery = nil, "", "", false
	return strings.TrimRight(u.String(), "/")
}

func (c *Client) cachedListing() (port.Listing, error) {
	l, err := c.cache.Listing()
	if err != nil {
		return l, err
	}
	account, err := c.session.Account()
	if err != nil {
		return port.Listing{}, errors.New("cannot read saved account identifier for cached listing")
	}
	if l.Endpoint != c.cacheEndpoint() || l.Account != account {
		return port.Listing{}, errors.New("cached listing belongs to another endpoint/account; run sync")
	}
	return l, nil
}

func (c *Client) cacheDiagnostic(operation string, err error) {
	if c.logger != nil {
		c.logger.Debug("cache unavailable", "operation", operation, "cause", err)
	}
}

// RetainUpload keeps the verified local bytes without another cloud download.
func (c *Client) RetainUpload(ctx context.Context, e port.Entry, file string) {
	c.retainBody(ctx, e, file, "keep uploaded body")
}

func (c *Client) retainBody(ctx context.Context, e port.Entry, file, operation string) {
	if c.cache == nil {
		return
	}
	if err := c.cache.Keep(ctx, e, file); err != nil {
		c.cacheDiagnostic(operation, fmt.Errorf("%s: %w", e.Path, err))
	}
}

func (c *Client) saveListing(ctx context.Context, entries []sncloud.DeviceEntry) {
	if c.cache == nil {
		return
	}
	account, err := c.session.Account()
	if err != nil {
		c.cacheDiagnostic("read account identifier", errors.New("saved account unavailable"))
		return
	}
	l := port.Listing{Endpoint: c.cacheEndpoint(), Account: account, Confirmed: c.now(), Entries: make([]port.Entry, 0, len(entries))}
	for _, e := range entries {
		l.Entries = append(l.Entries, deviceEntry(e))
	}
	if err := c.cache.SaveListing(ctx, l); err != nil {
		c.cacheDiagnostic("save listing", err)
	}
}

func cachedEntries(l port.Listing) ([]sncloud.DeviceEntry, error) {
	entries := make([]sncloud.DeviceEntry, 0, len(l.Entries))
	for _, e := range l.Entries {
		id, err := fileID(e.ID)
		if err != nil {
			return nil, errors.New("cached listing has an invalid file identifier; run sync")
		}
		entries = append(entries, sncloud.DeviceEntry{ID: id, Name: e.Name, Path: e.Path, IsFolder: e.Folder, Size: e.Size,
			ContentHash: e.Hash, Modified: e.Modified})
	}
	return entries, nil
}

// SetSessionCache preserves mandatory clearing when online file caching is unavailable.
func (c *Client) SetSessionCache(store port.FileCache) { c.sessionCache = store }

// RetainDrawingInputs selects verified bodies useful to this pass's drawings.
func (c *Client) RetainDrawingInputs(entries []port.Entry) {
	c.drawingInputs = make(map[[2]string]bool, len(entries))
	for _, e := range entries {
		c.drawingInputs[[2]string{e.ID, e.Hash}] = true
	}
}

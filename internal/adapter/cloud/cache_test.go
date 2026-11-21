// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/snctl/internal/port"
)

type cacheDouble struct {
	port.FileCache
	listing port.Listing
	err     error
	clears  int
	keeps   []port.Entry
}

func (c *cacheDouble) Listing() (port.Listing, error) { return c.listing, c.err }
func (c *cacheDouble) SaveListing(_ context.Context, l port.Listing) error {
	c.listing = l
	return c.err
}
func (c *cacheDouble) Clear(context.Context) error      { c.clears++; return c.err }
func (c *cacheDouble) Invalidate(context.Context) error { return c.err }
func (c *cacheDouble) Body(context.Context, port.Entry) (string, bool, error) {
	return "", false, c.err
}

func TestCachedSessionIsolation(t *testing.T) {
	for _, action := range []string{"same", "endpoint", "different", "logout", "failed-login", "failed-logout"} {
		t.Run(action, func(t *testing.T) {
			c := synthetic()
			store := &cacheDouble{listing: port.Listing{Endpoint: "https://viewer.supernote.com", Account: "synthetic"}}
			c.cache, c.sessionCache = store, store
			if action == "endpoint" {
				store.listing.Endpoint = "https://other.invalid"
			}
			if action == "failed-login" || action == "failed-logout" {
				store.err = errors.New("cannot clear cache")
			}
			var err error
			switch action {
			case "logout", "failed-logout":
				err = c.LogOut(t.Context())
			case "same", "endpoint":
				_, err = c.LogIn(t.Context(), "synthetic", "password")
			default:
				_, err = c.LogIn(t.Context(), "other", "password")
			}
			if action == "same" && store.clears != 0 {
				t.Fatal("same account cleared")
			}
			if action != "same" && store.clears != 1 {
				t.Fatal("cache retained")
			}
			if (store.err != nil) != (err != nil) {
				t.Fatal(err)
			}
		})
	}
}

func TestCachedListingPolicies(t *testing.T) {
	now := time.Unix(1000000, 0)
	for _, tc := range []struct {
		name    string
		offline bool
		ttl     time.Duration
		age     time.Duration
		err     bool
	}{
		{"fresh", false, time.Hour, time.Minute, false}, {"expired", false, time.Minute, time.Hour, false},
		{"offline-old", true, 0, 48 * time.Hour, false}, {"missing", true, 0, 0, true}, {"online-missing", false, time.Hour, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := synthetic()
			store := &cacheDouble{listing: port.Listing{Endpoint: "https://viewer.supernote.com", Account: "synthetic", Confirmed: now.Add(-tc.age), Entries: []port.Entry{{ID: "1", Name: "Note", Path: "Note", Folder: true}}}}
			if tc.err {
				store.err = errors.New("missing listing")
			}
			warnings := 0
			c.SetCache(store, tc.offline, tc.ttl, func() time.Time { return now }, func(string) { warnings++ })
			_, err := c.List(t.Context(), "/", true)
			if tc.err && tc.offline && err == nil {
				t.Fatal("missing error")
			}
			if (!tc.err || !tc.offline) && err != nil {
				t.Fatal(err)
			}
			if tc.name == "offline-old" && warnings != 1 {
				t.Fatal("missing warning")
			}
			if tc.offline && len(c.device.(*fakeDeviceAPI).calls) != 0 {
				t.Fatal("offline request")
			}
		})
	}
	c := synthetic()
	store := &cacheDouble{err: errors.New("unwritable")}
	c.SetCache(store, false, 0, func() time.Time { return now }, nil)
	if _, err := c.List(t.Context(), "/", true); err != nil {
		t.Fatal("cache prevented live listing", err)
	}
	if _, err := c.Download(t.Context(), port.Entry{ID: "10"}, nil); err != nil {
		t.Fatal("cache prevented download", err)
	}
	if _, err := c.Upload(t.Context(), "file", "Note/test", nil); err != nil {
		t.Fatal("cache prevented upload", err)
	}
	if err := c.EmptyTrash(t.Context()); err != nil {
		t.Fatal("cache prevented write", err)
	}
	c = synthetic()
	c.offline = true
	if _, err := c.Trash(t.Context()); err == nil {
		t.Fatal("offline trash")
	}
}

func TestListingIdentityAndDamage(t *testing.T) {
	for _, damage := range []string{"endpoint", "account", "identifier", "session"} {
		for _, offline := range []bool{false, true} {
			t.Run(damage+"/"+map[bool]string{false: "online", true: "offline"}[offline], func(t *testing.T) {
				c := synthetic()
				store := &cacheDouble{listing: port.Listing{Endpoint: "https://viewer.supernote.com", Account: "synthetic", Confirmed: time.Unix(100, 0), Entries: []port.Entry{{ID: "1", Path: "Note", Folder: true}}}}
				switch damage {
				case "endpoint":
					store.listing.Endpoint = "https://other.invalid"
				case "account":
					store.listing.Account = "other"
				case "identifier":
					store.listing.Entries[0].ID = "invalid"
				case "session":
					c.session.(*fakeSessionAPI).err = errors.New("private account access failed")
				}
				c.SetCache(store, offline, time.Hour, func() time.Time { return time.Unix(101, 0) }, nil)
				_, err := c.List(t.Context(), "/", true)
				if (err != nil) != offline {
					t.Fatal(err)
				}
				if offline && len(c.device.(*fakeDeviceAPI).calls) != 0 {
					t.Fatal("offline request")
				}
				if !offline && len(c.device.(*fakeDeviceAPI).calls) != 1 {
					t.Fatal("did not refresh listing")
				}
			})
		}
	}
	c := synthetic()
	c.endpoint = "https://user:password@host.invalid/path?token=secret#fragment"
	if got := c.cacheEndpoint(); got != "https://host.invalid/path" {
		t.Fatal(got)
	}
	c.endpoint = "https://[invalid"
	if got := c.cacheEndpoint(); got != "" {
		t.Fatal(got)
	}
}

func TestOfflineCacheFailureDiagnostic(t *testing.T) {
	c := synthetic()
	var log bytes.Buffer
	c.logger = slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	store := &cacheDouble{err: errors.New("profile cache is busy")}
	c.SetCache(store, true, 0, time.Now, nil)
	if _, _, err := c.DownloadSource(t.Context(), port.Entry{ID: "10", Path: "Note/sample.txt"}, nil); err == nil || !strings.Contains(err.Error(), "profile cache is busy") {
		t.Fatal(err)
	}
	if err := c.invalidate(t.Context()); err == nil {
		t.Fatal("offline cache error hidden")
	}
	c.offline = false
	if _, _, err := c.DownloadSource(t.Context(), port.Entry{ID: "10"}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "profile cache is busy") {
		t.Fatal(log.String())
	}
}

func TestUnavailableReadCacheRetainsSessionGuard(t *testing.T) {
	for _, action := range []string{"login", "logout"} {
		t.Run(action, func(t *testing.T) {
			c := synthetic()
			c.SetCache(nil, false, 0, time.Now, nil)
			c.SetSessionCache(&cacheDouble{err: errors.New("cache cannot be cleared")})
			var err error
			if action == "login" {
				_, err = c.LogIn(t.Context(), "other", "password")
			} else {
				err = c.LogOut(t.Context())
			}
			if err == nil {
				t.Fatal("changed session without clearing inaccessible cache")
			}
			for _, call := range c.session.(*fakeSessionAPI).calls {
				if call == "LogIn" || call == "LogOut" {
					t.Fatal("session changed before clearing cache")
				}
			}
		})
	}
}

func (c *cacheDouble) Keep(_ context.Context, e port.Entry, _ string) error {
	c.keeps = append(c.keeps, e)
	return c.err
}
func TestMirrorRetainsOnlySelectedDrawingInputs(t *testing.T) {
	c := synthetic()
	store := &cacheDouble{}
	c.SetCache(store, false, 0, time.Now, nil)
	files, err := c.FreshSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	doc := port.Entry{ID: "10", Path: "Report.pdf", Hash: "pdf"}
	if _, err = files.Download(t.Context(), doc, nil); err != nil {
		t.Fatal(err)
	}
	if len(store.keeps) != 0 {
		t.Fatal("ordinary mirror download was cached", store.keeps)
	}
	// The mirror selects inputs only when pull-new is enabled and a usable sidecar exists.
	selector, ok := files.(interface{ RetainDrawingInputs([]port.Entry) })
	if !ok {
		t.Fatal("snapshot cannot select drawing inputs")
	}
	selector.RetainDrawingInputs([]port.Entry{doc})
	for _, e := range []port.Entry{doc, {ID: "11", Path: "Book.epub"}, {ID: "12", Path: "Other.pdf"}} {
		if _, err = files.Download(t.Context(), e, nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.keeps) != 1 || store.keeps[0] != doc {
		t.Fatal(store.keeps)
	}
}

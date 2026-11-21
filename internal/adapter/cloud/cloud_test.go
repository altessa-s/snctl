// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/altessa-s/snctl/internal/adapter/vault"
	"github.com/altessa-s/snctl/internal/port"
	"github.com/altessa-s/snctl/internal/porttest"

	sncloud "github.com/altessa-s/sncloud-go"
)

func synthetic() *Client {
	return &Client{session: &fakeSessionAPI{}, device: &fakeDeviceAPI{}, web: &fakeWebAPI{}, tasks: &fakeTasksAPI{}, digests: &fakeDigestsAPI{}, feed: &fakeFeedAPI{}, endpoint: "official"}
}

func TestAdapterOperations(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	e := port.Entry{ID: "10", Name: "sample.txt", Path: "Note/sample.txt"}
	operations := []func(*Client) error{
		func(c *Client) error {
			_, err := c.LogIn(ctx, "reader@example.invalid", "synthetic-password")
			return err
		},
		func(c *Client) error { _, err := c.LogIn(ctx, "10000000000", "synthetic-password", 1); return err },
		func(c *Client) error { return c.SendVerificationCode(ctx) }, func(c *Client) error { return c.Verify(ctx, "123456") },
		func(c *Client) error { return c.LogOut(ctx) }, func(c *Client) error { _, _, err := c.Account(ctx); return err },
		func(c *Client) error { _, err := c.Info(ctx); return err }, func(c *Client) error { return c.Close() },
		func(c *Client) error { _, err := c.List(ctx, "Note", true); return err }, func(c *Client) error { _, err := c.Stat(ctx, "Note/sample.txt"); return err },
		func(c *Client) error { _, err := c.Download(ctx, e, nil); return err }, func(c *Client) error { _, err := c.Upload(ctx, "file", "Note/sample.txt", nil); return err },
		func(c *Client) error { _, err := c.Mkdir(ctx, "Note/new"); return err }, func(c *Client) error { return c.Move(ctx, e, "Note/new.txt") },
		func(c *Client) error { return c.Delete(ctx, e, false) }, func(c *Client) error { return c.Delete(ctx, e, true) },
		func(c *Client) error { _, err := c.Trash(ctx); return err }, func(c *Client) error { return c.EmptyTrash(ctx) }, func(c *Client) error { return c.Restore(ctx, "10") },
		func(c *Client) error { _, err := c.Lists(ctx); return err }, func(c *Client) error { _, err := c.AddList(ctx, "Reading"); return err },
		func(c *Client) error { return c.RenameList(ctx, "list", "Read") }, func(c *Client) error { return c.RemoveList(ctx, "list") },
		func(c *Client) error { _, err := c.Tasks(ctx, ""); return err }, func(c *Client) error { _, err := c.Tasks(ctx, "list"); return err },
		func(c *Client) error { _, err := c.AddTask(ctx, port.Task{Title: "Read"}); return err },
		func(c *Client) error {
			_, err := c.EditTask(ctx, "task", port.TaskEdit{Title: new("Read"), Detail: new("Detail"), Done: new(true)})
			return err
		},
		func(c *Client) error { _, err := c.EditTask(ctx, "task", port.TaskEdit{Done: new(false)}); return err },
		func(c *Client) error { return c.RemoveTask(ctx, "task") }, func(c *Client) error { return c.WebList(ctx) },
		func(c *Client) error { _, err := c.HandwritingGet(ctx, "30", nil); return err }, func(c *Client) error { return c.HandwritingPut(ctx, "30", "file", nil) },
	}
	for _, kind := range []port.DigestKind{port.DigestGroups, port.DigestTags, port.DigestItems} {
		operations = append(operations,
			func(c *Client) error { _, err := c.DigestList(ctx, kind); return err },
			func(c *Client) error {
				_, err := c.DigestAdd(ctx, kind, port.DigestEdit{Name: "Read", Content: new("passage")})
				return err
			},
			func(c *Client) error { return c.DigestEdit(ctx, kind, "30", port.DigestEdit{Name: "Read"}) },
			func(c *Client) error { return c.DigestRemove(ctx, kind, "30") })
	}
	for i, op := range operations {
		c := synthetic()
		if err := op(c); err != nil {
			t.Fatalf("operation %d: %v", i, err)
		}
		calls := [][]string{c.session.(*fakeSessionAPI).calls, c.device.(*fakeDeviceAPI).calls, c.web.(*fakeWebAPI).calls, c.tasks.(*fakeTasksAPI).calls, c.digests.(*fakeDigestsAPI).calls}
		for api, names := range calls {
			for _, name := range names {
				c = synthetic()
				failure := errors.New("synthetic private credential")
				switch api {
				case 0:
					c.session = &fakeSessionAPI{err: failure, failAt: name}
				case 1:
					c.device = &fakeDeviceAPI{err: failure, failAt: name}
				case 2:
					c.web = &fakeWebAPI{err: failure, failAt: name}
				case 3:
					c.tasks = &fakeTasksAPI{err: failure, failAt: name}
				case 4:
					c.digests = &fakeDigestsAPI{err: failure, failAt: name}
				}
				err := op(c)
				if err == nil {
					t.Fatalf("operation %d ignored %s", i, name)
				}
				if name != "Close" && strings.Contains(err.Error(), "private credential") {
					t.Fatal("credential leaked")
				}
			}
		}
	}
}

func TestAdapterMissingAndInvalid(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	c := synthetic()
	c.device = &fakeDeviceAPI{empty: true}
	c.tasks = &fakeTasksAPI{empty: true}
	c.digests = &fakeDigestsAPI{empty: true}
	for _, op := range []func() error{
		func() error { _, err := c.Stat(ctx, "absent"); return err }, func() error { _, err := c.EditTask(ctx, "absent", port.TaskEdit{}); return err },
		func() error { _, err := c.HandwritingGet(ctx, "30", nil); return err }, func() error { return c.HandwritingPut(ctx, "30", "file", nil) },
		func() error { _, err := c.Download(ctx, port.Entry{}, nil); return err }, func() error { return c.Move(ctx, port.Entry{}, "name") },
		func() error { return c.Delete(ctx, port.Entry{}, true) }, func() error { return c.Restore(ctx, "bad") },
		func() error { _, err := c.HandwritingGet(ctx, "bad", nil); return err }, func() error { return c.HandwritingPut(ctx, "bad", "file", nil) },
		func() error { _, err := c.DigestList(ctx, "bad"); return err }, func() error { _, err := c.DigestAdd(ctx, "bad", port.DigestEdit{}); return err },
		func() error { return c.DigestEdit(ctx, "bad", "30", port.DigestEdit{}) }, func() error { return c.DigestRemove(ctx, "bad", "30") },
		func() error { return c.DigestEdit(ctx, port.DigestItems, "bad", port.DigestEdit{}) }, func() error { return c.DigestRemove(ctx, port.DigestItems, "bad") },
	} {
		if !errors.Is(op(), port.ErrNotFound) {
			t.Fatal("missing item not classified")
		}
	}
	c = synthetic()
	tasks := c.tasks.(*fakeTasksAPI)
	if _, err := c.EditTask(ctx, "task", port.TaskEdit{Title: new("Changed")}); err != nil {
		t.Fatal(err)
	}
	if tasks.task.Recurrence == nil || tasks.task.Links == nil || !tasks.task.IsReminderOn || tasks.task.Title != "Changed" {
		t.Fatal("task edit lost existing metadata")
	}
}

func TestSafeErrors(t *testing.T) {
	t.Parallel()
	for _, err := range []error{nil, context.Canceled, context.DeadlineExceeded, sncloud.ErrNotLoggedIn, sncloud.ErrSessionExpired, sncloud.ErrAlreadyExists,
		sncloud.ErrNotFound, sncloud.ErrInvalidCredentials, sncloud.ErrInvalidVerificationCode, errors.New("synthetic-password synthetic-token")} {
		got := safe(err)
		if (got == nil) != (err == nil) {
			t.Fatal("lost error")
		}
		if got != nil && (strings.Contains(got.Error(), "synthetic-password") || strings.Contains(got.Error(), "synthetic-token")) {
			t.Fatal("secret leaked")
		}
	}
}

func TestConstructionAndStore(t *testing.T) {
	t.Parallel()
	store := &porttest.Store{}
	v := vault.New(store, port.SecretsFile, "synthetic")
	for _, endpoint := range []string{"official", "http://127.0.0.1:19072"} {
		c, err := New(v, endpoint, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := New(v, "bad", "", nil); err == nil {
		t.Fatal("invalid endpoint accepted")
	}
	c, err := New(v, "http://127.0.0.1:19072", "http://127.0.0.1:18072", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	s := secretStore{v}
	if err = s.Keep("synthetic-token", sncloud.AccessToken); err != nil {
		t.Fatal(err)
	}
	value, err := s.Secret(sncloud.AccessToken)
	if err != nil || value != "synthetic-token" {
		t.Fatal("store did not round trip")
	}
	if err = s.Discard(sncloud.AccessToken); err != nil {
		t.Fatal(err)
	}
}

func TestFeed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	c := synthetic()
	for _, err := range c.Watch(ctx) {
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	c.feed = &fakeFeedAPI{err: errors.New("synthetic-token")}
	for _, err := range c.Watch(ctx) {
		if err == nil || strings.Contains(err.Error(), "synthetic-token") {
			t.Fatal("unsafe feed error")
		}
	}
	e := sncloud.Change{Type: sncloud.ChangeFiles,
		Files: []sncloud.FileChange{{ID: new(sncloud.FileID(10)), Path: new("Note/sample.txt")}, {}},
		Tasks: []sncloud.TaskChange{{TaskID: new("task")}, {}}, Digests: []sncloud.DigestChange{{ID: new(sncloud.DigestID(30))}, {}}}
	if got := changes(e); len(got) != 6 || got[0].ID != "10" || got[2].ID != "task" || got[4].ID != "30" {
		t.Fatal("announcement translation")
	}
}

func TestIdentityIsStablePerProfile(t *testing.T) {
	t.Parallel()
	store := &porttest.Store{}
	v := vault.New(store, port.SecretsFile, "synthetic")
	first, err := New(v, "official", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	client := first.session.(*sncloud.Client)
	if _, err = client.Identity(); err != nil {
		t.Fatal("cannot initialize identity")
	}
	before, found, err := store.Secret(t.Context(), port.SecretClientID)
	if err != nil || !found {
		t.Fatal("identity was not persisted")
	}
	_ = first.Close()
	second, err := New(v, "official", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err = second.session.(*sncloud.Client).Identity(); err != nil {
		t.Fatal("cannot read identity")
	}
	after, found, err := store.Secret(t.Context(), port.SecretClientID)
	if err != nil || !found || before != after {
		t.Fatal("identity changed across invocations")
	}
}

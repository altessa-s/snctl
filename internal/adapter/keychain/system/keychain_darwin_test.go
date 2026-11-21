//go:build darwin && cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package system

import (
	"errors"
	"reflect"
	"testing"

	gokeychain "github.com/keybase/go-keychain"
)

func TestItemPolicy(t *testing.T) {
	t.Parallel()
	want := gokeychain.NewGenericPassword("svc", "work/accessToken", "label", []byte("synthetic"), "")
	want.SetSynchronizable(gokeychain.SynchronizableNo)
	want.SetAccessible(gokeychain.AccessibleAfterFirstUnlockThisDeviceOnly)
	if !reflect.DeepEqual(item("svc", "work/accessToken", "label", "synthetic"), want) {
		t.Fatal("item lacks explicit device-only accessibility or synchronization policy")
	}
}

func TestReplacement(t *testing.T) {
	t.Parallel()
	failed := errors.New("synthetic failure")
	for _, tc := range []struct {
		name                                        string
		initial, read, remove, replacement, restore error
		missing                                     bool
		want                                        error
		adds                                        int
	}{
		{name: "new", adds: 1},
		{name: "add failure", initial: failed, want: failed, adds: 1},
		{name: "replace foreign", initial: gokeychain.ErrorDuplicateItem, adds: 2},
		{name: "read failure", initial: gokeychain.ErrorDuplicateItem, read: failed, want: failed, adds: 1},
		{name: "delete failure", initial: gokeychain.ErrorDuplicateItem, remove: failed, want: failed, adds: 1},
		{name: "already deleted", initial: gokeychain.ErrorDuplicateItem, remove: gokeychain.ErrorItemNotFound, adds: 2},
		{name: "restore", initial: gokeychain.ErrorDuplicateItem, replacement: failed, want: failed, adds: 3},
		{name: "restore failure", initial: gokeychain.ErrorDuplicateItem, replacement: failed, restore: failed, want: errRestore, adds: 3},
		{name: "missing old", initial: gokeychain.ErrorDuplicateItem, missing: true, replacement: failed, want: failed, adds: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adds := 0
			k := itemAPI{
				queryItem: func(q gokeychain.Item) ([]gokeychain.QueryResult, error) {
					want := query("svc", "work/accessToken")
					want.SetMatchLimit(gokeychain.MatchLimitOne)
					want.SetReturnData(true)
					if !reflect.DeepEqual(q, want) {
						t.Fatal("wrong read query")
					}
					if tc.missing {
						return nil, tc.read
					}
					return []gokeychain.QueryResult{{Data: []byte("old")}}, tc.read
				},
				addItem: func(it gokeychain.Item) error {
					adds++
					value := "new"
					if adds == 3 {
						value = "old"
					}
					if !reflect.DeepEqual(it, item("svc", "work/accessToken", "label", value)) {
						t.Fatal("replacement or rollback did not use the explicit item policy")
					}
					return []error{tc.initial, tc.replacement, tc.restore}[adds-1]
				},
				deleteItem: func(q gokeychain.Item) error {
					if !reflect.DeepEqual(q, query("svc", "work/accessToken")) {
						t.Fatal("wrong delete query")
					}
					return tc.remove
				},
			}
			if err := k.Put("svc", "work/accessToken", "label", "new"); !errors.Is(err, tc.want) {
				t.Fatalf("Put error = %v, want %v", err, tc.want)
			}
			if adds != tc.adds {
				t.Fatalf("adds = %d, want %d", adds, tc.adds)
			}
		})
	}
}

func TestGetAndDelete(t *testing.T) {
	t.Parallel()
	failed := errors.New("synthetic failure")
	for _, err := range []error{nil, gokeychain.ErrorItemNotFound, failed} {
		k := itemAPI{deleteItem: func(gokeychain.Item) error { return err }}
		want := err
		if errors.Is(err, gokeychain.ErrorItemNotFound) {
			want = nil
		}
		if got := k.Delete("svc", "account"); !errors.Is(got, want) {
			t.Fatal(got)
		}
	}
	k := itemAPI{queryItem: func(gokeychain.Item) ([]gokeychain.QueryResult, error) {
		return []gokeychain.QueryResult{{Data: []byte("synthetic")}}, nil
	}}
	if value, ok, err := k.Get("svc", "account"); value != "synthetic" || !ok || err != nil {
		t.Fatal("read failed")
	}
}

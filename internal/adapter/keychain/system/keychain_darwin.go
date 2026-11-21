//go:build darwin && cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package system

import (
	"errors"

	gokeychain "github.com/keybase/go-keychain"
)

// Keychain is the login Keychain through Security.framework.
type Keychain struct{}

// itemAPI keeps tests off the login Keychain while exercising the native item policy.
type itemAPI struct {
	queryItem  func(gokeychain.Item) ([]gokeychain.QueryResult, error)
	addItem    func(gokeychain.Item) error
	deleteItem func(gokeychain.Item) error
}

func nativeAPI() itemAPI {
	return itemAPI{
		queryItem:  gokeychain.QueryItem,
		addItem:    gokeychain.AddItem,
		deleteItem: gokeychain.DeleteItem,
	}
}

// Get reads an item from the login Keychain.
func (Keychain) Get(service, account string) (string, bool, error) {
	return nativeAPI().Get(service, account)
}

// Put replaces an item using this device's accessibility policy.
func (Keychain) Put(service, account, label, value string) error {
	return nativeAPI().Put(service, account, label, value)
}

// Delete removes an item from the login Keychain.
func (Keychain) Delete(service, account string) error {
	return nativeAPI().Delete(service, account)
}

func query(service, account string) gokeychain.Item {
	q := gokeychain.NewItem()
	q.SetSecClass(gokeychain.SecClassGenericPassword)
	q.SetService(service)
	q.SetAccount(account)
	return q
}

// Get reads the item of service and account; found false when missing.
func (k itemAPI) Get(service, account string) (string, bool, error) {
	q := query(service, account)
	q.SetMatchLimit(gokeychain.MatchLimitOne)
	q.SetReturnData(true)
	results, err := k.queryItem(q)
	if err != nil {
		return "", false, err
	}
	if len(results) == 0 {
		return "", false, nil
	}
	return string(results[0].Data), true, nil
}

// item is a token as snctl stores it: on this device only, never synced.
func item(service, account, label, value string) gokeychain.Item {
	it := gokeychain.NewGenericPassword(service, account, label, []byte(value), "")
	it.SetSynchronizable(gokeychain.SynchronizableNo)
	it.SetAccessible(gokeychain.AccessibleAfterFirstUnlockThisDeviceOnly)
	return it
}

// Put adds the item. One already there is replaced whole rather than
// updated in place: an update would keep whatever accessibility the old item
// had, and a token must never stay readable before first unlock or move to
// another device. The old value is read first and put back if the new item
// cannot be added, so a failed Put loses nothing.
func (k itemAPI) Put(service, account, label, value string) error {
	err := k.addItem(item(service, account, label, value))
	if !errors.Is(err, gokeychain.ErrorDuplicateItem) {
		return err
	}
	old, found, err := k.Get(service, account)
	if err != nil {
		return err
	}
	if err := k.deleteItem(query(service, account)); err != nil && !errors.Is(err, gokeychain.ErrorItemNotFound) {
		return err
	}
	if err := k.addItem(item(service, account, label, value)); err != nil {
		if found {
			if rerr := k.addItem(item(service, account, label, old)); rerr != nil {
				return errors.Join(err, errRestore)
			}
		}
		return err
	}
	return nil
}

// errRestore means a replaced item could be neither written nor put back.
var errRestore = errors.New("the previous Keychain item could not be restored")

// Delete removes the item; a missing one is not an error.
func (k itemAPI) Delete(service, account string) error {
	err := k.deleteItem(query(service, account))
	if errors.Is(err, gokeychain.ErrorItemNotFound) {
		return nil
	}
	return err
}

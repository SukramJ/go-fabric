// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock

import (
	"reflect"
	"sync"
)

// keyCreation serialises the read-or-create of the credential key per
// AuthStore: two servers that start together on one store before it holds
// a key would otherwise each read none, each generate one, and the later
// SetSetting would win, leaving the other server's credentials under a key
// no restart loads (R3-store-3254). A store is told apart by its value;
// one whose value is not comparable shares a single slot with every other
// such store. A slot lives while a server holds or waits for it, so no
// store is kept beyond its servers' start.
var keyCreation struct {
	mu    sync.Mutex
	slots map[any]*keySlot
}

// keySlot is the one-slot channel of one store and the number of servers
// holding or waiting for it. A channel, so that a waiter is durably
// blocked for a testing/synctest bubble.
type keySlot struct {
	ch    chan struct{}
	users int
}

// incomparableStores is the slot key of every AuthStore whose value is not
// comparable.
type incomparableStores struct{}

// lockKeyCreation takes the slot of auth and returns its release.
func lockKeyCreation(auth AuthStore) (unlock func()) {
	var id any = incomparableStores{}
	if v := reflect.ValueOf(auth); v.Comparable() {
		id = auth
	}
	keyCreation.mu.Lock()
	if keyCreation.slots == nil {
		keyCreation.slots = map[any]*keySlot{}
	}
	slot := keyCreation.slots[id]
	if slot == nil {
		slot = &keySlot{ch: make(chan struct{}, 1)}
		keyCreation.slots[id] = slot
	}
	slot.users++
	keyCreation.mu.Unlock()
	slot.ch <- struct{}{}
	return func() {
		<-slot.ch
		keyCreation.mu.Lock()
		slot.users--
		if slot.users == 0 {
			delete(keyCreation.slots, id)
		}
		keyCreation.mu.Unlock()
	}
}

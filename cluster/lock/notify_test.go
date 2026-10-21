// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/lock"
)

// notifyingLock is the test source with a change notifier.
type notifyingLock struct {
	*fakeLockSource
	cb func()
}

func (n *notifyingLock) OnMatterValueChanged(cb func()) func() {
	n.cb = cb
	return func() {}
}

// TestTheSourcesChangeReachesTheBridge: the server is a
// contract.ChangeNotifier over its source's — the route a pull server's
// change takes to a subscriber — and the change moves its data version.
func TestTheSourcesChangeReachesTheBridge(t *testing.T) {
	src := &notifyingLock{fakeLockSource: &fakeLockSource{}}
	srv := lock.NewDoorLockServer(lock.DoorLockConfig{Source: src})
	before := srv.MatterDataVersion()
	heard := false
	srv.OnMatterValueChanged(func() { heard = true })
	if src.cb == nil {
		t.Fatal("the server did not subscribe to its source")
	}
	src.cb()
	if !heard || srv.MatterDataVersion() == before {
		t.Errorf("heard %v, data version %d → %d", heard, before, srv.MatterDataVersion())
	}
}

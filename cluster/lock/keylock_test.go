// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock

import (
	"testing"
)

// incomparableAuth is an AuthStore whose value cannot be a map key.
type incomparableAuth struct {
	AuthStore
	_ []byte
}

// TestKeyCreationSlotsAreReleased (R3-store-3254): a slot is dropped once
// no server holds or waits for it, and a store whose value is not
// comparable takes the shared slot instead of panicking as a map key.
func TestKeyCreationSlotsAreReleased(t *testing.T) {
	for _, auth := range []AuthStore{incomparableAuth{}, &incomparableAuth{}} {
		unlock := lockKeyCreation(auth)
		unlock()
		keyCreation.mu.Lock()
		n := len(keyCreation.slots)
		keyCreation.mu.Unlock()
		if n != 0 {
			t.Errorf("%T: %d slots left after the release", auth, n)
		}
	}
}

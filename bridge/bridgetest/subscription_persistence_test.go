// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgetest_test

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/bridge/bridgetest"
)

// TestSubscriptionPersistence reads the switch and the former
// subscriptions: on by default with none waiting, off once the bridge
// switches it off, and ErrNoBridge — not a panic — for a nil bridge.
func TestSubscriptionPersistence(t *testing.T) {
	t.Parallel()
	if _, _, err := bridgetest.SubscriptionPersistence(nil); !errors.Is(err, bridgetest.ErrNoBridge) {
		t.Errorf("nil bridge: err = %v, want ErrNoBridge", err)
	}
	b := newBridge(t)
	enabled, former, err := bridgetest.SubscriptionPersistence(b)
	if err != nil || !enabled || former != 0 {
		t.Errorf("default: (%t, %d, %v), want (true, 0, nil)", enabled, former, err)
	}
	b.SetSubscriptionPersistence(false)
	enabled, former, err = bridgetest.SubscriptionPersistence(b)
	if err != nil || enabled || former != 0 {
		t.Errorf("switched off: (%t, %d, %v), want (false, 0, nil)", enabled, former, err)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package pump_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/pump"
)

// notifyingDevice is the test source with a change notifier.
type notifyingDevice struct {
	*device
	cb func()
}

func (n *notifyingDevice) OnMatterValueChanged(cb func()) func() {
	n.cb = cb
	return func() {}
}

// TestTheSourcesChangeReachesTheBridge: the server is a
// contract.ChangeNotifier over its source's — the route a pull server's
// change takes to a subscriber — and the change moves its data version.
func TestTheSourcesChangeReachesTheBridge(t *testing.T) {
	src := &notifyingDevice{device: &device{}}
	srv, err := pump.NewServer(pump.Config{Source: src, Features: pump.FeatureConstantSpeed})
	if err != nil {
		t.Fatal(err)
	}
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

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"crypto/rand"
	"errors"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/peer"
)

// TestNodeIDIsClaimedForTheCommissioning: the node id a commissioning
// determines is held from then until the commissioning returns, so a
// second commissioning with the same explicit node id is ErrNodeIDConflict
// before the first has added its peer — before, both passed and two
// devices got a NOC for one node id. A drawn id skips a held one; the
// release frees the id (review II, R2-P6-conc-1810).
func TestNodeIDIsClaimedForTheCommissioning(t *testing.T) {
	t.Parallel()
	mgr, err := exchange.NewManager(refusingConn{}, exchange.Config{})
	if err != nil {
		t.Fatal(err)
	}
	f, err := NewFabric(faCA(t), FabricConfig{FabricID: 1, RootNodeID: 1})
	if err != nil {
		t.Fatal(err)
	}
	set, err := peer.NewSet(peer.Context{Exchanges: mgr, Names: discovery.NewNames(newFakeMDNS(), discard()), Fabrics: f.Lookup})
	if err != nil {
		t.Fatal(err)
	}
	c := &Commissioner{ctx: Context{Peers: set, Rand: rand.Reader}}

	const id = 0x42
	var wg sync.WaitGroup
	results := make([]error, 8)
	for i := range results {
		wg.Go(func() { _, results[i] = c.determineAddress(f, id) })
	}
	wg.Wait()
	won := 0
	for _, err := range results {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, ErrNodeIDConflict):
			t.Fatal(err)
		}
	}
	if won != 1 {
		t.Fatalf("%d commissionings hold node id %#x, want 1", won, id)
	}
	if err := c.assertPeerAddress(f.AddressOf(id)); !errors.Is(err, ErrNodeIDConflict) {
		t.Fatalf("the early check while the id is held: %v, want ErrNodeIDConflict", err)
	}
	c.releaseAddress(f.AddressOf(id))
	if _, err := c.determineAddress(f, id); err != nil {
		t.Fatalf("after the release: %v", err)
	}
}

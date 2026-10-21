// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build !race

package peer_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/mdns"
)

// TestConnectLiveThroughMulticast: the node advertises its operational
// record with mdns.Zeroconf on the host's multicast; the peer's process
// finds it through a real DNS-SD cache and establishes CASE. Built
// without -race, as the discovery and mdns live tests are (zeroconf's
// upstream Shutdown/recv race); skipped where the host gives no
// multicast.
func TestConnectLiveThroughMulticast(t *testing.T) {
	if testing.Short() {
		t.Skip("multicast; skipped under -short")
	}
	z := mdns.NewZeroconf()
	defer func() { _ = z.Close() }()
	_, _, fabric := commissioned(t, z, "[::]:0")
	names, err := discovery.Open(nil)
	if err != nil {
		t.Skipf("no multicast here: %v", err)
	}
	ls := newLiveSet(t, names, fabric, "[::]:0", nil)
	p, err := ls.set.For(testAddr)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := p.Connect(ctx, peer.ConnectOptions{})
	if err != nil {
		if len(p.Service().Addresses()) == 0 {
			t.Skipf("the node's operational record was not seen: multicast unavailable here (%v)", err)
		}
		t.Fatalf("Connect: %v (addresses %v)", err, p.Service().Addresses())
	}
	if s.PeerNodeID() != testNode {
		t.Fatalf("session to 0x%X", s.PeerNodeID())
	}
	readOnce(t, ls.mgr, s)
}

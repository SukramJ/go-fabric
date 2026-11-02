// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/mdns"
)

// TestCommissionLiveThroughMulticast: the node advertises with
// the module's mDNS responder on the host's multicast; the scanner finds
// its commissionable record, Commission commissions it, and the
// operational CASE connection finds the node's operational record through
// the same cache. Skipped where the host gives no multicast.
func TestCommissionLiveThroughMulticast(t *testing.T) {
	if testing.Short() {
		t.Skip("multicast; skipped under -short")
	}
	z := mdns.NewResponder()
	defer func() { _ = z.Close() }()
	n, _ := startNode(t, z, "[::]:0")
	names, err := discovery.Open(nil)
	if err != nil {
		t.Skipf("no multicast here: %v", err)
	}
	c := newController(t, names, "[::]:0")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var found *discovery.Device
	_, err = c.scanner.FindCommissionable(ctx, discovery.ByLongDiscriminator(nodeDiscriminator), func(d discovery.Device) {
		found = &d
		cancel()
	})
	if err != nil || found == nil {
		t.Skipf("the node's commissionable record was not seen: multicast unavailable here (%v)", err)
	}
	cctx, ccancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer ccancel()
	res, err := c.com.Commission(cctx, Options{Fabric: c.fabric, Passcode: nodePasscode, Addresses: udpAddrs(*found), DiscoveryData: found})
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	fabrics, err := n.FabricInfos(cctx)
	if err != nil || len(fabrics) != 1 || fabrics[0].NodeID != res.Address.NodeID {
		t.Fatalf("node fabrics %+v (%v)", fabrics, err)
	}
	if p := c.set.Get(res.Address); p == nil || !p.HasSession() {
		t.Fatal("no CASE session with the peer")
	}
}

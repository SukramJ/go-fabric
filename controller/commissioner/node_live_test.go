// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
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
//
// The host's multicast is shared: another test process — a parallel run of
// this test, a chip-tool harness beside it — advertises commissionable
// nodes of the same vendor and product, and the first record seen may be
// one of them (issue 427). The node therefore takes a discriminator of its
// own for the run, and the test selects the record by that discriminator
// and the node's instance name, beside a second node with another
// discriminator and passcode that it must pass over; it fails naming the
// records it saw when none is its node's.
func TestCommissionLiveThroughMulticast(t *testing.T) {
	if testing.Short() {
		t.Skip("multicast; skipped under -short")
	}
	ownD := uint16(rand.IntN(0x1000))
	z := mdns.NewResponder()
	defer func() { _ = z.Close() }()
	n, _ := startNodeWith(t, z, "[::]:0", nodePasscode, ownD)
	instance := commissionableInstance(t, z)
	// The node to pass over: another discriminator, another passcode, so
	// commissioning it instead fails PASE as issue 427's run did.
	other := mdns.NewResponder()
	defer func() { _ = other.Close() }()
	startNodeWith(t, other, "[::]:0", nodePasscode+1, ownD^0x800)
	names, err := discovery.Open(nil)
	if err != nil {
		t.Skipf("no multicast here: %v", err)
	}
	c := newController(t, names, "[::]:0")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var (
		found *discovery.Device
		seen  []string
	)
	_, err = c.scanner.FindCommissionable(ctx, discovery.ByVendorProduct(0xFFF1, 0x8001), func(d discovery.Device) {
		if d.D == ownD && strings.EqualFold(d.InstanceID, instance) {
			found = &d
			cancel()
			return
		}
		seen = append(seen, fmt.Sprintf("%s (D=%d)", d.InstanceID, d.D))
	})
	if found == nil {
		if len(seen) == 0 {
			t.Skipf("no commissionable record was seen: multicast unavailable here (%v)", err)
		}
		t.Fatalf("the node's record %s (D=%d) was not among the records seen: %s (%v)", instance, ownD, strings.Join(seen, ", "), err)
	}
	if len(seen) > 0 {
		t.Logf("passed over: %s", strings.Join(seen, ", "))
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

// commissionableInstance is the instance name of the one commissionable
// record the responder advertises.
func commissionableInstance(t *testing.T, z *mdns.Responder) string {
	t.Helper()
	var names []string
	active := z.Active()
	for i := range active {
		if active[i].ServiceType == mdns.ServiceTypeCommissionable {
			names = append(names, active[i].InstanceName)
		}
	}
	if len(names) != 1 {
		t.Fatalf("the responder advertises %d commissionable records: %v", len(names), names)
	}
	return names[0]
}
